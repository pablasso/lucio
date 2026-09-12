package paperboy

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/delivery"
	"github.com/pablasso/lucio/internal/renderer"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata"
)

const (
	canvasWidth  = renderer.CanvasWidth
	canvasHeight = renderer.CanvasHeight

	defaultServerURL   = control.DefaultURL
	defaultOutPath     = "/tmp/today-newspaper.png"
	defaultLucioBinary = "lucio"
	defaultRenderSize  = 5120
	defaultAckTimeout  = 120 * time.Second
	defaultHTTPTimeout = 120 * time.Second
	defaultTimezone    = "America/Chicago"
)

// pushRetryDelays are the waits between lucio push attempts after a failed
// delivery. Delivery-only retries keep the fetch stage exactly-once so source
// downloads are not repeated. The finite retry window totals 66 minutes,
// plus the time spent making delivery attempts.
var pushRetryDelays = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	45 * time.Minute,
}

var (
	errMissingPDFToCairo = errors.New("missing required dependency: pdftocairo")
	executablePath       = os.Executable
)

const missingPDFToCairoMessage = `Missing required dependency: pdftocairo

Install it with:
  sudo apt install poppler-utils`

const certificateVerificationMessage = `HTTPS certificate verification failed.

Install CA certificates with:
  sudo apt install ca-certificates`

// freedomForumBaseURL hosts Freedom Forum's daily front-page PDFs under the
// pattern {base}/{code}/{YYYY-MM-DD}/front-page.pdf. The scheme is unofficial
// and could change without notice; per the failure policy there is no fallback.
const maxPDFBytes = 32 << 20

const freedomForumBaseURL = "https://d2dr22b2lm4tvw.cloudfront.net"

// paper is one newspaper in the daily rotation.
type paper struct {
	Name string
	Code string
}

// rotation is a fixed, repeating daily-paper order. Source availability and
// permission to use publisher content are separate from Lucio itself.
var rotation = []paper{
	{Name: "NY Times", Code: "ny_nyt"},
	{Name: "Boston Globe", Code: "ma_bg"},
	{Name: "Chicago Tribune", Code: "il_ct"},
	{Name: "SF Chronicle", Code: "ca_sfc"},
}

type config struct {
	ServerURL    string
	OutPath      string
	NoPush       bool
	Date         string
	RenderSize   int
	AckTimeout   time.Duration
	WorkDir      string
	HTTPTimeout  time.Duration
	LucioPath    string
	UpdatePolicy string
	PDFPath      string
	KeepWork     bool
}

func Run(ctx context.Context, args []string, stdout io.Writer) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	logger := log.New(stdout, "", 0)

	pdftocairoPath, err := findPDFToCairo()
	if err != nil {
		return err
	}
	lucioPath, err := findLucio(cfg.LucioPath)
	if err != nil {
		return err
	}

	paperDate, err := resolveDate(cfg.Date, time.Now())
	if err != nil {
		return err
	}
	paper := paperForDate(paperDate)
	pdfURL := freedomForumPDFURL(paper.Code, paperDate)

	if cfg.WorkDir != "" {
		if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
			return fmt.Errorf("create work dir: %w", err)
		}
	}
	runDir, err := os.MkdirTemp(cfg.WorkDir, "paperboy-*")
	if err != nil {
		return fmt.Errorf("create private run dir: %w", err)
	}
	cfg.WorkDir = runDir
	if !cfg.KeepWork {
		defer os.RemoveAll(runDir)
	}
	scanPath := filepath.Join(cfg.WorkDir, "scan.pdf")
	renderBase := filepath.Join(cfg.WorkDir, "rendered")
	renderedPath := renderBase + ".png"
	normalizedSourcePath := filepath.Join(cfg.WorkDir, "newspaper-source-1440x2560.png")
	scenePath := filepath.Join(cfg.WorkDir, "scene.json")
	reportPath := filepath.Join(cfg.WorkDir, "renderer-report.json")

	logger.Printf("paperboy")
	logger.Printf("pdftocairo=%s", pdftocairoPath)
	logger.Printf("lucio=%s", lucioPath)
	logger.Printf("date=%s paper=%s url=%s", paperDate.Format("2006-01-02"), paper.Code, pdfURL)
	logger.Printf("work_dir=%s scan_pdf=%s rendered_png=%s source_png=%s scene=%s report=%s", cfg.WorkDir, scanPath, renderedPath, normalizedSourcePath, scenePath, reportPath)
	logger.Printf("output=%s render_width=%d no_push=%t server=%s update=%s ack_timeout=%s", cfg.OutPath, cfg.RenderSize, cfg.NoPush, cfg.ServerURL, normalizedUpdatePolicy(cfg.UpdatePolicy), cfg.AckTimeout)

	if cfg.PDFPath != "" {
		scanPath, err = filepath.Abs(cfg.PDFPath)
		if err != nil {
			return err
		}
		stat, err := os.Stat(scanPath)
		if err != nil {
			return err
		}
		if !stat.Mode().IsRegular() || stat.Size() > maxPDFBytes {
			return fmt.Errorf("local PDF must be a regular file no larger than 32 MiB")
		}
		logger.Printf("using local PDF %s (no download)", scanPath)
	} else {
		n, err := downloadFile(ctx, cfg.HTTPTimeout, pdfURL, scanPath)
		if err != nil {
			return err
		}
		logger.Printf("downloaded PDF bytes=%d path=%s", n, scanPath)
	}

	if err := renderPDFPage(ctx, pdftocairoPath, scanPath, renderBase, cfg.RenderSize); err != nil {
		return err
	}
	logger.Printf("rendered PDF page path=%s", renderedPath)

	result, err := normalizeImageFile(renderedPath, normalizedSourcePath)
	if err != nil {
		return err
	}
	logger.Printf("normalized image from %dx%d to %dx%d path=%s", result.SourceWidth, result.SourceHeight, result.Width, result.Height, normalizedSourcePath)

	if err := writeRendererScene(scenePath, filepath.Base(normalizedSourcePath)); err != nil {
		return err
	}
	logger.Printf("wrote renderer scene path=%s", scenePath)

	if cfg.NoPush {
		renderCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := runLucio(renderCtx, lucioPath, []string{"render", "--scene", scenePath, "--out", cfg.OutPath, "--report", reportPath, "--asset-root", cfg.WorkDir}); err != nil {
			return err
		}
		logger.Printf("no-push enabled; renderer output=%s report=%s", cfg.OutPath, reportPath)
		return nil
	}

	push, err := pushWithRetries(ctx, cfg, lucioPath, scenePath, reportPath, logger)
	if err != nil {
		return err
	}
	logger.Printf("pushed renderer image packet=%d checksum=%s bytes=%d delivery=%s reason=%s output=%s report=%s", push.Sent.PacketID, push.Sent.ChecksumHex, push.Sent.FrameBytes, push.Sent.Delivery, push.Sent.DeliveryReason, cfg.OutPath, reportPath)
	if push.Ack.Waited && push.Ack.Matched {
		logger.Printf("screen ACK confirmed packet=%d", push.Ack.PacketID)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	cfg := config{
		ServerURL:    defaultServerURL,
		OutPath:      defaultOutPath,
		RenderSize:   defaultRenderSize,
		AckTimeout:   defaultAckTimeout,
		WorkDir:      "",
		HTTPTimeout:  defaultHTTPTimeout,
		LucioPath:    defaultLucioBinary,
		UpdatePolicy: string(delivery.DefaultPolicy),
	}

	fs := flag.NewFlagSet("paperboy", flag.ContinueOnError)
	fs.StringVar(&cfg.WorkDir, "work-dir", "", "parent for a private per-run directory; default is the OS temp directory")
	fs.BoolVar(&cfg.KeepWork, "keep-work", false, "retain this run's inputs and reports for local debugging")
	fs.StringVar(&cfg.PDFPath, "pdf", "", "use a local PDF instead of downloading the daily front page")
	fs.DurationVar(&cfg.HTTPTimeout, "http-timeout", cfg.HTTPTimeout, "maximum download time")
	fs.StringVar(&cfg.ServerURL, "server", cfg.ServerURL, "lucio control origin")
	fs.StringVar(&cfg.OutPath, "out", cfg.OutPath, "output PNG path for the renderer-produced display image")
	fs.StringVar(&cfg.LucioPath, "lucio", cfg.LucioPath, "lucio binary path or name")
	fs.StringVar(&cfg.UpdatePolicy, "update", cfg.UpdatePolicy, fmt.Sprintf("delivery policy passed to lucio push: %s", delivery.Choices()))
	fs.BoolVar(&cfg.NoPush, "no-push", false, "render the PNG but do not push it to lucio")
	fs.StringVar(&cfg.Date, "date", "", "newspaper date as YYYY-MM-DD; default is today in America/Chicago")
	fs.IntVar(&cfg.RenderSize, "render-size", cfg.RenderSize, "longest edge of the intermediate PDF render (1–5120)")
	fs.DurationVar(&cfg.AckTimeout, "ack-timeout", cfg.AckTimeout, "maximum time to wait for the screen image ACK")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: paperboy [flags]\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() != 0 {
		return cfg, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if err := validateConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func validateConfig(cfg config) error {
	if cfg.OutPath == "" {
		return fmt.Errorf("--out must not be empty")
	}
	if cfg.RenderSize <= 0 || cfg.RenderSize > 5120 {
		return fmt.Errorf("--render-size must be between 1 and 5120")
	}
	if cfg.AckTimeout <= 0 || cfg.AckTimeout > 10*time.Minute {
		return fmt.Errorf("--ack-timeout must be positive and at most 10m")
	}
	if cfg.HTTPTimeout <= 0 || cfg.HTTPTimeout > 10*time.Minute {
		return fmt.Errorf("HTTP timeout must be positive and at most 10m")
	}
	if cfg.LucioPath == "" {
		return fmt.Errorf("--lucio must not be empty")
	}
	if _, err := parseServerURL(cfg.ServerURL); err != nil {
		return err
	}
	if _, err := delivery.ParsePolicy(cfg.UpdatePolicy); err != nil {
		return err
	}
	return nil
}

func normalizedUpdatePolicy(raw string) string {
	policy, err := delivery.ParsePolicy(raw)
	if err != nil {
		return raw
	}
	return string(policy)
}

func parseServerURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, fmt.Errorf("--server must not be empty")
	}
	if _, err := control.NewClient(raw); err != nil {
		return nil, err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse --server: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("--server must use http or https")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("--server must include a host")
	}
	return u, nil
}

func findPDFToCairo() (string, error) {
	path, err := exec.LookPath("pdftocairo")
	if err != nil {
		return "", errMissingPDFToCairo
	}
	return path, nil
}

func resolveDate(raw string, now time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(defaultTimezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("load embedded timezone: %w", err)
	}
	if raw == "" {
		return now.In(loc), nil
	}
	t, err := time.ParseInLocation("2006-01-02", raw, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("--date must be YYYY-MM-DD: %w", err)
	}
	return t, nil
}

// daysSinceEpoch returns whole calendar days between 1970-01-01 and the
// calendar date of t. Only the Y/M/D components are used, so the result is
// stable regardless of the location's DST rules; the count itself happens in
// UTC, which has had no offset changes since the epoch.
func daysSinceEpoch(t time.Time) int {
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return int(midnight.Unix() / 86400)
}

// paperForDate deterministically maps a calendar date to a position in the
// fixed rotation: same day -> same paper, so manual re-runs are idempotent and
// no rotation state needs to persist.
func paperForDate(date time.Time) paper {
	days := daysSinceEpoch(date)
	index := ((days % len(rotation)) + len(rotation)) % len(rotation)
	return rotation[index]
}

func freedomForumPDFURL(code string, date time.Time) string {
	return fmt.Sprintf("%s/%s/%s/front-page.pdf", freedomForumBaseURL, code, date.Format("2006-01-02"))
}
