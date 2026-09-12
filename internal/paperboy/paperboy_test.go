package paperboy

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFreedomForumPDFURL(t *testing.T) {
	date, err := resolveDate("2026-08-23", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range rotation {
		got := freedomForumPDFURL(p.Code, date)
		want := fmt.Sprintf("https://d2dr22b2lm4tvw.cloudfront.net/%s/2026-08-23/front-page.pdf", p.Code)
		if got != want {
			t.Fatalf("freedomForumPDFURL(%s) = %q, want %q", p.Code, got, want)
		}
	}
}

func TestRotationTableMatchesDecidedSchedule(t *testing.T) {
	want := []paper{
		{Name: "NY Times", Code: "ny_nyt"},
		{Name: "Boston Globe", Code: "ma_bg"},
		{Name: "Chicago Tribune", Code: "il_ct"},
		{Name: "SF Chronicle", Code: "ca_sfc"},
	}
	if len(rotation) != len(want) {
		t.Fatalf("rotation has %d entries, want %d", len(rotation), len(want))
	}
	seen := make(map[string]bool)
	for i, p := range rotation {
		if p != want[i] {
			t.Fatalf("rotation[%d] = %+v, want %+v", i, p, want[i])
		}
		if seen[p.Code] {
			t.Fatalf("duplicate rotation code %q", p.Code)
		}
		seen[p.Code] = true
	}
}

func TestDaysSinceEpoch(t *testing.T) {
	loc, err := time.LoadLocation(defaultTimezone)
	if err != nil {
		t.Fatalf("embedded timezone unavailable: %v", err)
	}
	cases := []struct {
		raw  string
		want int
	}{
		{"1970-01-01", 0},
		{"1970-01-02", 1},
		{"2000-03-01", 11017}, // just after a leap day
		{"2024-02-29", 19782}, // leap day itself
		{"2026-08-23", 20688},
	}
	for _, tc := range cases {
		day, err := time.ParseInLocation("2006-01-02", tc.raw, loc)
		if err != nil {
			t.Fatal(err)
		}
		if got := daysSinceEpoch(day); got != tc.want {
			t.Fatalf("daysSinceEpoch(%s) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestPaperForDateFollowsFixedOrder(t *testing.T) {
	date, err := resolveDate("2026-08-23", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	first := paperForDate(date)
	start := -1
	for i, p := range rotation {
		if p == first {
			start = i
		}
	}
	if start < 0 {
		t.Fatalf("paperForDate returned unknown paper %+v", first)
	}
	// Consecutive days advance through the fixed order, wrapping around.
	for offset := 0; offset < 2*len(rotation)+1; offset++ {
		got := paperForDate(date.AddDate(0, 0, offset))
		want := rotation[(start+offset)%len(rotation)]
		if got != want {
			t.Fatalf("paperForDate(+%dd) = %+v, want %+v", offset, got, want)
		}
	}
}

func TestPaperForDateIsIdempotentPerDay(t *testing.T) {
	loc, err := time.LoadLocation(defaultTimezone)
	if err != nil {
		t.Fatalf("embedded timezone unavailable: %v", err)
	}
	date, err := time.ParseInLocation("2006-01-02", "2026-08-24", loc)
	if err != nil {
		t.Fatal(err)
	}
	want := paperForDate(date)
	for _, hour := range []int{0, 5, 12, 23} {
		sameDay := time.Date(2026, time.August, 24, hour, 17, 0, 0, loc)
		if got := paperForDate(sameDay); got != want {
			t.Fatalf("paperForDate at %02d:17 = %+v, want %+v", hour, got, want)
		}
	}
}

func TestResolveDateRejectsBadFormat(t *testing.T) {
	if _, err := resolveDate("2026/06/29", time.Now()); err == nil {
		t.Fatal("expected error for bad date format")
	}
}

func TestParseFlagsDefaults(t *testing.T) {
	cfg, err := parseFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != defaultServerURL {
		t.Fatalf("ServerURL = %q, want %q", cfg.ServerURL, defaultServerURL)
	}
	if cfg.OutPath != defaultOutPath {
		t.Fatalf("OutPath = %q, want %q", cfg.OutPath, defaultOutPath)
	}
	if cfg.RenderSize != defaultRenderSize {
		t.Fatalf("RenderSize = %d, want %d", cfg.RenderSize, defaultRenderSize)
	}
	if cfg.LucioPath != defaultLucioBinary {
		t.Fatalf("LucioPath = %q, want %q", cfg.LucioPath, defaultLucioBinary)
	}
	if cfg.AckTimeout != defaultAckTimeout {
		t.Fatalf("AckTimeout = %s, want %s", cfg.AckTimeout, defaultAckTimeout)
	}
	if cfg.UpdatePolicy != "full" {
		t.Fatalf("UpdatePolicy = %q, want full", cfg.UpdatePolicy)
	}
}

func TestParseFlagsOverrides(t *testing.T) {
	cfg, err := parseFlags([]string{
		"--server", "http://192.0.2.10:18080",
		"--out", "/tmp/paperboy.png",
		"--no-push",
		"--lucio", "/usr/local/bin/lucio",
		"--date", "2026-06-29",
		"--render-size", "2880",
		"--ack-timeout", "45s",
		"--update", "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "http://192.0.2.10:18080" || cfg.OutPath != "/tmp/paperboy.png" || !cfg.NoPush || cfg.LucioPath != "/usr/local/bin/lucio" || cfg.Date != "2026-06-29" || cfg.RenderSize != 2880 || cfg.AckTimeout != 45*time.Second || cfg.UpdatePolicy != "auto" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestParseServerURLValidation(t *testing.T) {
	bad := []string{"", "127.0.0.1:18080", "ftp://127.0.0.1:18080", "http:///missing-host"}
	for _, raw := range bad {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseServerURL(raw); err == nil {
				t.Fatalf("expected %q to be invalid", raw)
			}
		})
	}
}

func TestNormalizeImageContainsNarrowPageWithWhiteMargins(t *testing.T) {
	// A tall (narrow) source must fit entirely inside the canvas: full height
	// preserved, centered horizontally, white bars on the sides.
	src := image.NewNRGBA(image.Rect(0, 0, 2, 4))
	for y := 0; y < 4; y++ {
		src.SetNRGBA(0, y, color.NRGBA{R: 200, A: 255})
		src.SetNRGBA(1, y, color.NRGBA{G: 100, A: 255})
	}

	dst := normalizeImage(src, 4, 4)
	wantCols := []color.NRGBA{
		{R: 255, G: 255, B: 255, A: 255}, // left margin
		{R: 200, G: 0, B: 0, A: 255},     // source column 0
		{R: 0, G: 100, B: 0, A: 255},     // source column 1
		{R: 255, G: 255, B: 255, A: 255}, // right margin
	}
	for y := 0; y < 4; y++ {
		for x, want := range wantCols {
			if got := dst.NRGBAAt(x, y); got != want {
				t.Fatalf("dst(%d,%d) = %#v, want %#v", x, y, got, want)
			}
		}
	}
}

func TestNormalizeImageContainsWidePageWithWhiteBars(t *testing.T) {
	// A wide source must fit entirely inside the canvas: full width preserved,
	// centered vertically, white bars top and bottom.
	src := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for x := 0; x < 4; x++ {
		src.SetNRGBA(x, 0, color.NRGBA{R: 200, A: 255})
		src.SetNRGBA(x, 1, color.NRGBA{G: 100, A: 255})
	}

	dst := normalizeImage(src, 4, 4)
	wantRows := []color.NRGBA{
		{R: 255, G: 255, B: 255, A: 255}, // top bar
		{R: 200, G: 0, B: 0, A: 255},     // source row 0
		{R: 0, G: 100, B: 0, A: 255},     // source row 1
		{R: 255, G: 255, B: 255, A: 255}, // bottom bar
	}
	for y, want := range wantRows {
		for x := 0; x < 4; x++ {
			if got := dst.NRGBAAt(x, y); got != want {
				t.Fatalf("dst(%d,%d) = %#v, want %#v", x, y, got, want)
			}
		}
	}
}

func TestNormalizeImageBlendsTransparencyOverWhite(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 0, G: 0, B: 0, A: 0})
	dst := normalizeImage(src, 1, 1)
	if got, want := dst.NRGBAAt(0, 0), (color.NRGBA{R: 255, G: 255, B: 255, A: 255}); got != want {
		t.Fatalf("transparent pixel = %#v, want %#v", got, want)
	}
}

func TestWriteRendererScene(t *testing.T) {
	dir := t.TempDir()
	scenePath := filepath.Join(dir, "scene.json")
	if err := writeRendererScene(scenePath, "newspaper-source-1440x2560.png"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(scenePath)
	if err != nil {
		t.Fatal(err)
	}
	var scene struct {
		Version string `json:"version"`
		Canvas  struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		} `json:"canvas"`
		Nodes []struct {
			ID  string `json:"id"`
			Typ string `json:"type"`
			Box struct {
				W int `json:"w"`
				H int `json:"h"`
			} `json:"box"`
			Src string `json:"src"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(data, &scene); err != nil {
		t.Fatal(err)
	}
	if scene.Version != "lucio.scene/v1" || scene.Canvas.Width != canvasWidth || scene.Canvas.Height != canvasHeight || len(scene.Nodes) != 1 {
		t.Fatalf("unexpected scene: %+v", scene)
	}
	if node := scene.Nodes[0]; node.ID != "newspaper" || node.Typ != "image" || node.Box.W != canvasWidth || node.Box.H != canvasHeight || node.Src != "newspaper-source-1440x2560.png" {
		t.Fatalf("unexpected image node: %+v", node)
	}
}

func TestRunLucioPush(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args.txt")
	lucioPath := writeFakeLucio(t, dir, `
printf '%s\n' "$@" > "$LUCIO_ARGS"
printf '%s\n' '{"ok":true,"update_id":"synthetic-1-1","state":"acked","render":{"sha256":"abc","output_path":"/tmp/out.png","report_path":"/tmp/report.json"},"sent":{"packet_id":42,"checksum_hex":"0x12345678","frame_bytes":99},"ack":{"waited":true,"matched":true,"packet_id":42}}'
`)
	t.Setenv("LUCIO_ARGS", argsPath)

	cfg := config{ServerURL: "http://127.0.0.1:18080", OutPath: filepath.Join(dir, "out.png"), WorkDir: dir, AckTimeout: 45 * time.Second}
	result, err := runLucioPush(context.Background(), cfg, lucioPath, filepath.Join(dir, "scene.json"), filepath.Join(dir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Sent.PacketID != 42 || !result.Ack.Matched || result.Ack.PacketID != 42 {
		t.Fatalf("unexpected push result: %+v", result)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := string(argsData)
	for _, want := range []string{"push\n", "--scene\n", "--server\n", "http://127.0.0.1:18080\n", "--out\n", cfg.OutPath + "\n", "--report\n", "--update\n", "full\n", "--wait\n", "--ack-timeout\n", "45s\n", "--asset-root\n", dir + "\n"} {
		if !strings.Contains(args, want) {
			t.Fatalf("lucio args missing %q in:\n%s", want, args)
		}
	}
}

func TestPushWithRetries(t *testing.T) {
	dir := t.TempDir()
	countPath := filepath.Join(dir, "count")
	lucioPath := writeFakeLucio(t, dir, `
count=$(cat "$LUCIO_COUNT" 2>/dev/null || echo 0)
count=$((count+1))
echo "$count" > "$LUCIO_COUNT"
if [ "$count" -lt 3 ]; then
  printf 'push: push image to lucio: 409 Conflict: use of closed network connection\n' >&2
  exit 1
fi
printf '%s\n' '{"ok":true,"update_id":"synthetic-1-1","state":"acked","render":{"sha256":"abc","output_path":"/tmp/out.png","report_path":"/tmp/report.json"},"sent":{"packet_id":7,"checksum_hex":"0xdeadbeef","frame_bytes":10},"ack":{"waited":true,"matched":true,"packet_id":7}}'
`)
	t.Setenv("LUCIO_COUNT", countPath)
	oldDelays := pushRetryDelays
	pushRetryDelays = []time.Duration{0, 0, 0}
	defer func() { pushRetryDelays = oldDelays }()

	logger := log.New(io.Discard, "", 0)
	cfg := config{ServerURL: "http://127.0.0.1:18080", OutPath: filepath.Join(dir, "out.png"), WorkDir: dir, AckTimeout: time.Second}
	push, err := pushWithRetries(context.Background(), cfg, lucioPath, filepath.Join(dir, "scene.json"), filepath.Join(dir, "report.json"), logger)
	if err != nil {
		t.Fatal(err)
	}
	if !push.OK || push.Sent.PacketID != 7 {
		t.Fatalf("unexpected push result: %+v", push)
	}
	var count int
	if _, err := fmt.Sscanf(readFile(t, countPath), "%d", &count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("lucio invoked %d times, want 3 (two failures then success)", count)
	}
}

func TestPushWithRetriesExhaustsAttempts(t *testing.T) {
	dir := t.TempDir()
	lucioPath := writeFakeLucio(t, dir, `printf 'always fails\n' >&2; exit 1`)
	oldDelays := pushRetryDelays
	pushRetryDelays = []time.Duration{0, 0}
	defer func() { pushRetryDelays = oldDelays }()

	logger := log.New(io.Discard, "", 0)
	cfg := config{ServerURL: "http://127.0.0.1:18080", OutPath: filepath.Join(dir, "out.png"), WorkDir: dir, AckTimeout: time.Second}
	_, err := pushWithRetries(context.Background(), cfg, lucioPath, filepath.Join(dir, "scene.json"), filepath.Join(dir, "report.json"), logger)
	if err == nil || !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("err = %v, want exhaustion after 3 attempts", err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRunLucioReportsStderr(t *testing.T) {
	dir := t.TempDir()
	lucioPath := writeFakeLucio(t, dir, `
printf 'bad scene\n' >&2
exit 7
`)
	err := runLucio(context.Background(), lucioPath, []string{"render", "--scene", "scene.json"})
	if err == nil || !strings.Contains(err.Error(), "bad scene") {
		t.Fatalf("err = %v, want stderr in error", err)
	}
}

func TestFindLucioConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	lucioPath := writeFakeLucio(t, dir, `exit 0`)
	got, err := findLucio(lucioPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != lucioPath {
		t.Fatalf("findLucio = %q, want %q", got, lucioPath)
	}
}

func TestFindLucioUsesSiblingBeforePath(t *testing.T) {
	dir := t.TempDir()
	lucioPath := writeFakeLucio(t, dir, `exit 0`)
	exePath := filepath.Join(dir, "paperboy")
	if err := os.WriteFile(exePath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	oldExecutablePath := executablePath
	executablePath = func() (string, error) { return exePath, nil }
	defer func() { executablePath = oldExecutablePath }()

	got, err := findLucio("lucio")
	if err != nil {
		t.Fatal(err)
	}
	if got != lucioPath {
		t.Fatalf("findLucio = %q, want sibling %q", got, lucioPath)
	}
}

func writeFakeLucio(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, "lucio")
	content := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNormalizeImageFileWritesLucioCanvas(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "rendered.png")
	outPath := filepath.Join(dir, "today-newspaper.png")

	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 42, G: 43, B: 44, A: 255})
	in, err := os.Create(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(in, src); err != nil {
		in.Close()
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}

	result, err := normalizeImageFile(srcPath, outPath)
	if err != nil {
		t.Fatal(err)
	}
	if result.Width != canvasWidth || result.Height != canvasHeight {
		t.Fatalf("result size = %dx%d, want %dx%d", result.Width, result.Height, canvasWidth, canvasHeight)
	}

	out, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	decoded, err := png.Decode(out)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Bounds(); got.Dx() != canvasWidth || got.Dy() != canvasHeight {
		t.Fatalf("output bounds = %v, want %dx%d", got, canvasWidth, canvasHeight)
	}
}
