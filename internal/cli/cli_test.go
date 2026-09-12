package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/renderer"
	"github.com/pablasso/lucio/internal/telemetry"
)

func run(args []string, stdout, stderr io.Writer) error {
	return Run(context.Background(), args, strings.NewReader(""), stdout, stderr)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func TestReferenceCommands(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"version"}, {"version", "--json"}, {"schema"}, {"describe"}, {"primitives"}, {"primitives", "--format", "markdown"}, {"init"}} {
		var out, errOut bytes.Buffer
		if err := run(args, &out, &errOut); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 || errOut.Len() != 0 {
			t.Fatal(args, out.String(), errOut.String())
		}
	}
	for _, args := range [][]string{{"version", "extra"}, {"schema", "--version", "other"}, {"primitives", "--format", "other"}, {"wat"}} {
		if err := run(args, io.Discard, io.Discard); err == nil {
			t.Fatal(args)
		}
	}
}
func TestRenderFromStdinAndFailureReport(t *testing.T) {
	dir := t.TempDir()
	out, report := filepath.Join(dir, "image.png"), filepath.Join(dir, "report.json")
	data, err := os.ReadFile("../../examples/hello.json")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"render", "--scene", "-", "--out", out, "--report", report, "--report-detail"}
	if err := Run(context.Background(), args, bytes.NewReader(data), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	png, err := os.ReadFile(out)
	if err != nil || len(png) == 0 {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(report)
	var r renderer.Report
	if err := json.Unmarshal(body, &r); err != nil || !r.OK || len(r.DrawOps) == 0 {
		t.Fatal(r, err)
	}
	invalid := strings.NewReader(`{"version":"lucio.scene/v1","nodes":null}`)
	if err := Run(context.Background(), args, invalid, io.Discard, io.Discard); err == nil {
		t.Fatal("accepted invalid scene")
	}
	old, _ := os.ReadFile(out)
	if !bytes.Equal(old, png) {
		t.Fatal("failed render overwrote preview")
	}
	body, _ = os.ReadFile(report)
	json.Unmarshal(body, &r)
	if r.OK {
		t.Fatal("failed report marked OK")
	}
	if err := distinctPaths(out, out, report); err == nil {
		t.Fatal("allowed destructive output collision")
	}
}
func TestPushUsesPerUpdateWait(t *testing.T) {
	received := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := control.Update{ID: "instance-2-7", State: control.UpdatePending, Sent: control.SentImage{PacketID: 7, ConnectionID: 2, Delivery: "full"}}
		switch r.URL.Path {
		case "/api/image":
			if r.Header.Get("X-Lucio-Client") != "1" || r.URL.Query().Get("update") != "full" {
				t.Error("request contract")
			}
			io.Copy(io.Discard, r.Body)
			received = true
		case "/api/updates/instance-2-7":
			u.State = control.UpdateACKed
			u.ACK = &control.ImageACK{PacketID: 7, ConnectionID: 2, MatchedLast: true}
		default:
			t.Error("unexpected polling path", r.URL.Path)
		}
		writeJSON(w, 200, u)
	}))
	defer srv.Close()
	var out, stderr bytes.Buffer
	if err := run([]string{"push", "--scene", "../../examples/hello.json", "--server", srv.URL, "--wait"}, &out, &stderr); err != nil {
		t.Fatal(err)
	}
	var result pushResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !received || !result.OK || !result.Ack.Matched || result.UpdateID != "instance-2-7" || result.Render.SHA256 == "" {
		t.Fatal(result)
	}
}
func TestFailedRenderNeverContactsDaemon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected network request") }))
	defer srv.Close()
	data := strings.NewReader(`{"version":"lucio.scene/v1","nodes":[{"id":"bad","type":"text","box":{"x":0,"y":0,"w":10,"h":10},"text":"too long","font":{"size":40}}]}`)
	if err := Run(context.Background(), []string{"push", "--scene", "-", "--server", srv.URL}, data, io.Discard, io.Discard); err == nil {
		t.Fatal("expected failure")
	}
}
func TestBatteryCommandAndMalformedLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	r, err := telemetry.Open(path, 4096)
	if err != nil {
		t.Fatal(err)
	}
	r.Record(telemetry.Event{Schema: telemetry.SchemaV1, Type: telemetry.StatusReceived, At: time.Now(), RunID: "synthetic", Status: telemetry.StatusFromTuples(1, map[uint32]uint32{10: 90})})
	r.Close()
	for _, jsonFlag := range []bool{true, false} {
		args := []string{"battery", "--events", path}
		if jsonFlag {
			args = append(args, "--json")
		}
		var out bytes.Buffer
		if err := run(args, &out, io.Discard); err != nil || out.Len() == 0 {
			t.Fatal(err)
		}
	}
	os.WriteFile(path, []byte("not-json\n"), 0o600)
	if err := run([]string{"battery", "--events", path}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatal(err)
	}
}
