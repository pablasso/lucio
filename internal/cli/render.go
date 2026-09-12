package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/delivery"
	"github.com/pablasso/lucio/internal/renderer"
)

type pushResult struct {
	OK           bool   `json:"ok"`
	UpdateID     string `json:"update_id"`
	State        string `json:"state"`
	UpdatePolicy string `json:"update_policy"`
	Render       struct {
		SHA256     string `json:"sha256"`
		OutputPath string `json:"output_path,omitempty"`
		ReportPath string `json:"report_path,omitempty"`
	} `json:"render"`
	Sent control.SentImage `json:"sent"`
	Ack  struct {
		Waited   bool   `json:"waited"`
		Matched  bool   `json:"matched"`
		PacketID uint32 `json:"packet_id,omitempty"`
	} `json:"ack"`
	Error string `json:"error,omitempty"`
}

func runRender(ctx context.Context, command string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flags(command, stderr)
	var scenePath, outPath, reportPath, assetRoot string
	var detail, wait bool
	server := control.DefaultURL
	policyRaw := string(delivery.DefaultPolicy)
	ackTimeout := 2 * time.Minute
	fs.StringVar(&scenePath, "scene", "", "scene JSON file, or - for stdin (required)")
	fs.StringVar(&outPath, "out", "", "PNG output file (required for render, optional for push)")
	fs.StringVar(&reportPath, "report", "", "optional validation/layout report JSON file")
	fs.StringVar(&assetRoot, "asset-root", "", "base directory for relative image assets (not a sandbox)")
	fs.BoolVar(&detail, "report-detail", false, "include source-mapped drawing operation details")
	if command == "push" {
		fs.StringVar(&server, "server", server, "Lucio control origin")
		fs.StringVar(&policyRaw, "update", policyRaw, "delivery preference: "+delivery.Choices()+"; partial may fall back to full")
		fs.BoolVar(&wait, "wait", false, "wait for this update's matching physical-device ACK")
		fs.DurationVar(&ackTimeout, "ack-timeout", ackTimeout, "maximum client wait; does not override daemon's ACK deadline")
	}
	if err := parse(fs, args); err != nil {
		return err
	}
	if scenePath == "" {
		return fmt.Errorf("--scene is required")
	}
	if command == "render" && outPath == "" {
		return fmt.Errorf("--out is required")
	}
	if ackTimeout <= 0 {
		return fmt.Errorf("--ack-timeout must be positive")
	}
	policy, err := delivery.ParsePolicy(policyRaw)
	if err != nil {
		return err
	}
	var client *control.Client
	if command == "push" {
		client, err = control.NewClient(server)
		if err != nil {
			return err
		}
	}
	if err := distinctPaths(scenePath, outPath, reportPath); err != nil {
		return err
	}
	data, err := readScene(scenePath, stdin)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rendered := renderer.RenderSceneJSON(data, renderer.RenderOptions{ScenePath: scenePath, AssetRoot: assetRoot, ReportDetail: detail})
	report := rendered.Report
	report.Output.Path = outPath
	if reportPath != "" {
		b, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := writeAtomic(reportPath, append(b, '\n')); err != nil {
			return fmt.Errorf("write report: %w", err)
		}
	}
	if !report.OK {
		return fmt.Errorf("render failed with %d error(s): %s", renderer.CountErrors(report.Issues), firstIssue(report))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if outPath != "" {
		if err := writeAtomic(outPath, rendered.PNG); err != nil {
			return fmt.Errorf("write PNG: %w", err)
		}
	}
	if command == "render" {
		return nil
	}
	name := "scene.png"
	if scenePath != "-" {
		name = filepath.Base(scenePath) + ".png"
	}
	if outPath != "" {
		name = filepath.Base(outPath)
	}
	uploadCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	update, err := client.Submit(uploadCtx, name, rendered.PNG, policy)
	cancel()
	if err != nil {
		return err
	}
	result := pushResult{UpdateID: update.ID, State: update.State, UpdatePolicy: string(policy), Sent: update.Sent}
	result.Render.SHA256 = report.Output.SHA256
	result.Render.OutputPath = outPath
	result.Render.ReportPath = reportPath
	result.Ack.Waited = wait
	if update.State == control.UpdateFailed {
		err = fmt.Errorf("update failed: %s", update.Error)
	} else if wait {
		waitCtx, cancel := context.WithTimeout(ctx, ackTimeout)
		completed, waitErr := client.Wait(waitCtx, update.ID)
		cancel()
		err = waitErr
		if completed.ID != "" {
			result.State = completed.State
			result.Sent = completed.Sent
		}
		if err == nil {
			result.Ack.Matched = true
			result.Ack.PacketID = completed.ACK.PacketID
		}
	}
	result.OK = err == nil
	if err != nil {
		result.Error = err.Error()
	}
	if writeErr := jsonOut(stdout, result); writeErr != nil {
		return writeErr
	}
	return err
}
func firstIssue(r renderer.Report) string {
	for _, i := range r.Issues {
		if i.Severity == renderer.SeverityError {
			return i.Path + ": " + i.Message
		}
	}
	return "see validation report"
}
func readScene(path string, stdin io.Reader) ([]byte, error) {
	input := stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		input = f
	}
	// One excess byte lets the renderer produce a normal resource-limit report.
	return io.ReadAll(io.LimitReader(input, renderer.MaxSceneBytes+1))
}
func distinctPaths(scene, out, report string) error {
	seen := map[string]bool{}
	for _, p := range []string{scene, out, report} {
		if p == "" || p == "-" {
			continue
		}
		a, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		if seen[a] {
			return fmt.Errorf("scene, PNG, and report paths must be distinct")
		}
		seen[a] = true
	}
	return nil
}
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".lucio-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
