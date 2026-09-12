// Package cli provides Lucio's supported command-line interface.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/pablasso/lucio/internal/daemon"
	"github.com/pablasso/lucio/internal/renderer"
	"github.com/pablasso/lucio/internal/telemetry"
	"github.com/pablasso/lucio/internal/usb"
)

// Version is set at release build time. It is deliberately independent of the
// scene and local event schema versions.
var Version = "dev"

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return flag.ErrHelp
	}
	switch args[0] {
	case "serve":
		return runServe(ctx, args[1:], stdout, stderr)
	case "render", "push":
		return runRender(ctx, args[0], args[1:], stdin, stdout, stderr)
	case "status":
		return runStatus(ctx, args[1:], stdout, stderr)
	case "devices":
		return runDevices(ctx, args[1:], stdout, stderr)
	case "fields":
		return runFields(args[1:], stdout, stderr)
	case "battery":
		return runBattery(args[1:], stdout, stderr)
	case "usb":
		return usb.Run(ctx, args[1:], stdin, stdout, stderr)
	case "version", "schema", "describe", "primitives", "init":
		return runReference(args[0], args[1:], stdout, stderr)
	case "help", "--help", "-h":
		usage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q; see lucio help", args[0])
	}
}
func usage(w io.Writer) {
	fmt.Fprint(w, `Usage: lucio <command> [flags]

Display content:
  init         Print a starter JSON scene
  render       Render a scene to a PNG and optional report, offline
  push         Render and send a scene; --wait confirms the device ACK
  serve        Run the single-screen LAN gateway and private local control

Inspect:
  status       Show cached screen/daemon status (--json available)
  devices      Show the one observed device (--json available)
  fields       List known status fields and confidence
  battery      Analyze optional local event logs

Reference and setup:
  describe     Explain the scene contract
  primitives   List primitive metadata (JSON or Markdown)
  schema       Print the embedded scene JSON Schema
  version      Print version and renderer capabilities
  usb          Experimental Go USB serial CLI; see docs/setup.md

Use lucio <command> --help for command flags.
`)
}
func flags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintf(stderr, "Usage: lucio %s [flags]\n\n", name); fs.PrintDefaults() }
	return fs
}
func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%s: unexpected positional arguments: %s", fs.Name(), strings.Join(fs.Args(), " "))
	}
	return nil
}
func jsonOut(w io.Writer, v any) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(v)
}

func runServe(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cfg := daemon.DefaultConfig()
	cfg.Version = Version
	fs := flags("serve", stderr)
	fs.StringVar(&cfg.GatewayListen, "gateway-listen", cfg.GatewayListen, "LAN PV3 TCP listen address")
	fs.StringVar(&cfg.HTTPListen, "http-listen", cfg.HTTPListen, "private local control listen address")
	fs.BoolVar(&cfg.AllowRemoteControl, "allow-remote-control", false, "UNSAFE without external protection: permit non-loopback control binding")
	fs.StringVar(&cfg.DeviceID, "device-id", "", "expected device ID; empty pins the first identified device for this run")
	maxFrame := uint(cfg.MaxFrameBytes)
	fs.UintVar(&maxFrame, "max-frame-bytes", maxFrame, "maximum incoming PV3 payload (44–1048576)")
	fs.Int64Var(&cfg.MaxUploadBytes, "max-upload-bytes", cfg.MaxUploadBytes, "maximum PNG upload bytes (up to 64 MiB)")
	fs.DurationVar(&cfg.HandshakeTimeout, "handshake-timeout", cfg.HandshakeTimeout, "initial identification and partial-frame read timeout")
	fs.DurationVar(&cfg.ReadTimeout, "read-timeout", cfg.ReadTimeout, "idle screen read timeout; 0 allows quiet connections")
	fs.DurationVar(&cfg.WriteTimeout, "write-timeout", cfg.WriteTimeout, "maximum time for a socket write")
	fs.DurationVar(&cfg.ACKTimeout, "ack-timeout", cfg.ACKTimeout, "maximum sending + device ACK time (required, up to 10m)")
	fs.StringVar(&cfg.EventLog, "event-log", "", "opt-in LOCAL JSONL path; no network analytics")
	fs.Int64Var(&cfg.EventLogMaxBytes, "event-log-max-bytes", cfg.EventLogMaxBytes, "rotate event log at this size; retain three backups")
	if err := parse(fs, args); err != nil {
		return err
	}
	if maxFrame > 1<<20 {
		return fmt.Errorf("max-frame-bytes exceeds 1048576")
	}
	cfg.MaxFrameBytes = uint32(maxFrame)
	return daemon.ListenAndServe(ctx, cfg, log.New(stdout, "", log.LstdFlags|log.Lmicroseconds))
}
func runReference(command string, args []string, stdout, stderr io.Writer) error {
	fs := flags(command, stderr)
	version := renderer.SceneVersionV1
	template := "dashboard"
	format := "json"
	asJSON := false
	switch command {
	case "version":
		fs.BoolVar(&asJSON, "json", false, "machine-readable version and capabilities")
	case "init":
		fs.StringVar(&template, "template", template, "starter: "+strings.Join(renderer.TemplateNames(), ", "))
	case "primitives":
		fs.StringVar(&format, "format", format, "json or markdown")
	}
	if command != "version" {
		fs.StringVar(&version, "version", version, "scene contract version")
	}
	if err := parse(fs, args); err != nil {
		return err
	}
	if version != renderer.SceneVersionV1 {
		return fmt.Errorf("unsupported scene version %q", version)
	}
	switch command {
	case "version":
		caps := renderer.Capabilities()
		if asJSON {
			return jsonOut(stdout, map[string]any{"lucio_version": Version, "renderer": caps})
		}
		_, err := fmt.Fprintf(stdout, "lucio %s\nscene: %s\ncanvas: %dx%d\n", Version, caps.SceneVersion, renderer.CanvasWidth, renderer.CanvasHeight)
		return err
	case "schema":
		b, err := renderer.SchemaJSON(version)
		if err != nil {
			return err
		}
		_, err = stdout.Write(b)
		return err
	case "describe":
		_, err := io.WriteString(stdout, renderer.DescribeMarkdown())
		return err
	case "init":
		b, err := renderer.TemplateJSON(template)
		if err != nil {
			return err
		}
		_, err = stdout.Write(b)
		return err
	case "primitives":
		if format == "markdown" {
			_, err := io.WriteString(stdout, renderer.PrimitivesMarkdown())
			return err
		}
		if format != "json" {
			return fmt.Errorf("format must be json or markdown")
		}
		return jsonOut(stdout, renderer.Capabilities())
	}
	return nil
}
func runBattery(args []string, stdout, stderr io.Writer) error {
	fs := flags("battery", stderr)
	path := "/var/log/lucio/events.jsonl"
	asJSON, details := false, false
	fs.StringVar(&path, "events", path, "local JSONL file (rotated files can be analyzed individually)")
	fs.BoolVar(&asJSON, "json", false, "machine-readable observational report")
	fs.BoolVar(&details, "details", false, "per-update correlations")
	if err := parse(fs, args); err != nil {
		return err
	}
	report, err := telemetry.Analyze(path)
	if err != nil {
		return err
	}
	if asJSON {
		return jsonOut(stdout, report)
	}
	return telemetry.PrintReport(stdout, report, details)
}
