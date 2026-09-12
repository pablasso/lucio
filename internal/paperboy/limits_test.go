package paperboy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pablasso/lucio/internal/renderer"
)

func TestDownloadValidationAndAtomicity(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		length     int
		wantError  bool
	}{{"ok", "%PDF-synthetic", 0, false}, {"html", "<html>unavailable", 0, true}, {"empty", "", 0, true}, {"size", "%PDF-", maxPDFBytes + 1, true}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.length > 0 {
					w.Header().Set("Content-Length", fmt.Sprint(tc.length))
				}
				io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "page.pdf")
			os.WriteFile(path, []byte("previous"), 0o600)
			_, err := downloadFile(context.Background(), time.Second, srv.URL, path)
			if (err != nil) != tc.wantError {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			if tc.wantError && string(got) != "previous" {
				t.Fatal("replaced valid output on failure")
			}
		})
	}
}
func TestDownloadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := downloadFile(ctx, time.Second, "http://127.0.0.1:1", filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("ignored canceled context")
	}
}
func TestSubprocessOutputAndConfigAreBounded(t *testing.T) {
	var b boundedBuffer
	p := bytes.Repeat([]byte("x"), 2<<20)
	n, err := b.Write(p)
	if err != nil || n != len(p) || b.Len() != 1<<20 {
		t.Fatal(n, err, b.Len())
	}
	for _, args := range [][]string{{"--render-size", "0"}, {"--render-size", "5121"}, {"--ack-timeout", "11m"}, {"--server", "http://127.0.0.1:18080/path"}} {
		if _, err := parseFlags(args); err == nil {
			t.Fatal(args)
		}
	}
}
func TestPushRequiresIdentityAndMatchingACK(t *testing.T) {
	path := writeFakeLucio(t, t.TempDir(), `printf '%s\n' '{"ok":true,"sent":{"packet_id":3},"ack":{"waited":true,"matched":true,"packet_id":3}}'`)
	_, err := runLucioPush(context.Background(), config{AckTimeout: time.Second}, path, "scene.json", "report.json")
	if err == nil {
		t.Fatal("accepted legacy, uncorrelated completion")
	}
}
func TestLocalPDFToRenderer(t *testing.T) {
	tool, err := findPDFToCairo()
	if err != nil {
		t.Skip("optional PDF integration: install Poppler to run")
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "page")
	if err := renderPDFPage(context.Background(), tool, "../../examples/page.pdf", base, 128); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "source.png")
	normalized, err := normalizeImageFile(base+".png", out)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Width != renderer.CanvasWidth || normalized.Height != renderer.CanvasHeight {
		t.Fatal(normalized)
	}
	scene := filepath.Join(dir, "scene.json")
	if err := writeRendererScene(scene, filepath.Base(out)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(scene)
	if err != nil {
		t.Fatal(err)
	}
	result := renderer.RenderSceneJSON(data, renderer.RenderOptions{ScenePath: scene})
	if !result.Report.OK || len(result.PNG) == 0 {
		t.Fatal(result.Report.Issues)
	}
}
func TestCalendarOutsideTimeDurationRange(t *testing.T) {
	date, err := resolveDate("9999-01-01", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := daysSinceEpoch(date); got != 2932532 {
		t.Fatal(got)
	}
	if _, err := resolveDate("invalid", time.Now()); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatal(err)
	}
}
