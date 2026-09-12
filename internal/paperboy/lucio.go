package paperboy

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/pablasso/lucio/internal/renderer"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type lucioPushResult struct {
	UpdateID     string `json:"update_id"`
	State        string `json:"state"`
	OK           bool   `json:"ok"`
	UpdatePolicy string `json:"update_policy"`
	Render       struct {
		SHA256     string `json:"sha256"`
		OutputPath string `json:"output_path"`
		ReportPath string `json:"report_path"`
	} `json:"render"`
	Sent struct {
		PacketID       uint32 `json:"packet_id"`
		ChecksumHex    string `json:"checksum_hex"`
		FrameBytes     int    `json:"frame_bytes"`
		UpdatePolicy   string `json:"update_policy"`
		Delivery       string `json:"delivery"`
		DeliveryReason string `json:"delivery_reason"`
	} `json:"sent"`
	Ack struct {
		Waited   bool   `json:"waited"`
		Matched  bool   `json:"matched"`
		PacketID uint32 `json:"packet_id"`
	} `json:"ack"`
}

func findLucio(configured string) (string, error) {
	if configured == "" {
		configured = defaultLucioBinary
	}
	if filepath.Base(configured) == configured {
		if exe, err := executablePath(); err == nil {
			sibling := filepath.Join(filepath.Dir(exe), configured)
			if isExecutableFile(sibling) {
				return sibling, nil
			}
		}
		path, err := exec.LookPath(configured)
		if err == nil {
			return path, nil
		}
		local := filepath.Join("/usr/local/bin", configured)
		if isExecutableFile(local) {
			return local, nil
		}
		return "", fmt.Errorf("missing required dependency: %s", configured)
	}
	if isExecutableFile(configured) {
		return configured, nil
	}
	return "", fmt.Errorf("lucio binary %s not found or not executable", configured)
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}

func writeRendererScene(path, src string) error {
	scene := renderer.Scene{
		Schema:  renderer.SchemaURLV1,
		Version: renderer.SceneVersionV1,
		Canvas: renderer.Canvas{
			Width:      renderer.CanvasWidth,
			Height:     renderer.CanvasHeight,
			Background: renderer.DefaultCanvasBackground,
		},
		Nodes: []renderer.Node{
			{
				ID:   "newspaper",
				Type: renderer.NodeTypeImage,
				Box: renderer.Box{
					X: 0,
					Y: 0,
					W: renderer.CanvasWidth,
					H: renderer.CanvasHeight,
				},
				Src: src,
			},
		},
	}
	data, err := json.MarshalIndent(scene, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create scene dir: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// pushWithRetries runs the lucio push, retrying delivery failures with fixed
// backoff. Each retry re-invokes lucio against the already-rendered scene, so
// a dropped screen connection during one attempt does not waste the day's
// fetch. The final error is returned only after all attempts are exhausted.
func pushWithRetries(ctx context.Context, cfg config, lucioPath, scenePath, reportPath string, logger *log.Logger) (lucioPushResult, error) {
	totalAttempts := len(pushRetryDelays) + 1
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return lucioPushResult{}, err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, cfg.AckTimeout+3*time.Minute)
		push, err := runLucioPush(attemptCtx, cfg, lucioPath, scenePath, reportPath)
		cancel()
		if err == nil {
			return push, nil
		}
		if attempt >= totalAttempts {
			return lucioPushResult{}, fmt.Errorf("push failed after %d attempts: %w", attempt, err)
		}
		delay := pushRetryDelays[attempt-1]
		logger.Printf("push attempt %d/%d failed: %v; retrying in %s", attempt, totalAttempts, err, delay)
		select {
		case <-ctx.Done():
			return lucioPushResult{}, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func runLucioPush(ctx context.Context, cfg config, lucioPath, scenePath, reportPath string) (lucioPushResult, error) {
	stdout, err := runLucioOutput(ctx, lucioPath, []string{
		"push",
		"--scene", scenePath,
		"--server", cfg.ServerURL,
		"--out", cfg.OutPath,
		"--report", reportPath,
		"--update", normalizedUpdatePolicy(cfg.UpdatePolicy),
		"--wait",
		"--ack-timeout", cfg.AckTimeout.String(),
		"--asset-root", cfg.WorkDir,
	})
	if err != nil {
		return lucioPushResult{}, err
	}
	var result lucioPushResult
	if err := json.Unmarshal(stdout, &result); err != nil {
		return lucioPushResult{}, fmt.Errorf("decode lucio push output: %w", err)
	}
	if !result.OK || result.UpdateID == "" || result.State != "acked" || result.Sent.PacketID == 0 || !result.Ack.Waited || !result.Ack.Matched || result.Ack.PacketID != result.Sent.PacketID {
		return lucioPushResult{}, fmt.Errorf("lucio push returned invalid result: %s", strings.TrimSpace(string(stdout)))
	}
	return result, nil
}

func runLucio(ctx context.Context, lucioPath string, args []string) error {
	_, err := runLucioOutput(ctx, lucioPath, args)
	return err
}

func runLucioOutput(ctx context.Context, lucioPath string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, lucioPath, args...)
	cmd.WaitDelay = time.Second
	var stdout boundedBuffer
	var stderr boundedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("lucio %s failed: %w: %s", strings.Join(args, " "), err, msg)
		}
		return nil, fmt.Errorf("lucio %s failed: %w", strings.Join(args, " "), err)
	}
	return stdout.Bytes(), nil
}

// ErrorMessage adds actionable hints without disabling TLS verification.
func ErrorMessage(err error) string {
	if errors.Is(err, errMissingPDFToCairo) {
		return missingPDFToCairoMessage
	}
	if isCertificateVerificationError(err) {
		return certificateVerificationMessage
	}
	return fmt.Sprintf("paperboy: %v", err)
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (1 << 20) - b.Len()
	if remaining > 0 {
		_, _ = b.Buffer.Write(p[:min(n, remaining)])
	}
	return n, nil
}

func isCertificateVerificationError(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	if errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "x509") || strings.Contains(msg, "certificate signed by unknown authority") || strings.Contains(msg, "certificate verification")
}
