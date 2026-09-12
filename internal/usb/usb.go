package usb

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

type usbCommandList []string

func (l *usbCommandList) String() string { return strings.Join(*l, ",") }
func (l *usbCommandList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

type secondsDuration struct{ time.Duration }

func (d *secondsDuration) String() string {
	if d == nil {
		return "0s"
	}
	return d.Duration.String()
}

func (d *secondsDuration) Set(raw string) error {
	if raw == "" {
		return fmt.Errorf("duration must not be empty")
	}
	if parsed, err := time.ParseDuration(raw); err == nil {
		if parsed < 0 || parsed > time.Minute {
			return fmt.Errorf("duration must be between 0 and 1 minute")
		}
		d.Duration = parsed
		return nil
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fmt.Errorf("parse duration %q as Go duration or seconds: %w", raw, err)
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 60 {
		return fmt.Errorf("duration must be finite and between 0 and 60 seconds")
	}
	d.Duration = time.Duration(seconds * float64(time.Second))
	return nil
}

type usbConfig struct {
	list        bool
	port        string
	baud        int
	commands    usbCommandList
	file        string
	interactive bool
	redact      bool
	idle        secondsDuration
	readTimeout secondsDuration
	openDelay   secondsDuration
}

var lucioUSBSecretPatterns = []struct {
	pattern     *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?im)^(\s*PSK\s*:\s*)(.*)$`), `${1}<redacted>`},
	{regexp.MustCompile(`(?im)(wifi_psk_set\s+)(\S.*)$`), `${1}<redacted>`},
	{regexp.MustCompile(`(?im)(wifi_conf_set\s+\S+\s+)(\S+)(\s+\S+\s+\S+)`), `${1}<redacted>${3}`},
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cfg := usbConfig{
		baud:        115200,
		idle:        secondsDuration{Duration: 500 * time.Millisecond},
		readTimeout: secondsDuration{Duration: 5 * time.Second},
		openDelay:   secondsDuration{Duration: 500 * time.Millisecond},
	}
	var showSecrets bool
	fs := flag.NewFlagSet("usb", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: lucio usb [flags]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Experimental Go USB serial implementation; not yet physically validated. See docs/setup.md for the validated setup helper. Output is redacted by default.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Flags:")
		fs.PrintDefaults()
	}
	fs.BoolVar(&cfg.list, "list", false, "list serial ports and exit")
	fs.StringVar(&cfg.port, "port", "", "serial port, e.g. /dev/cu.usbserial-XXXX")
	fs.IntVar(&cfg.baud, "baud", cfg.baud, "serial baud rate")
	fs.Var(&cfg.commands, "cmd", "command to send; may be repeated")
	fs.StringVar(&cfg.file, "file", "", "file containing commands, one per line")
	fs.BoolVar(&cfg.interactive, "interactive", false, "enter simple line mode after --cmd/--file commands")
	fs.BoolVar(&showSecrets, "show-secrets", false, "UNSAFE: disable best-effort Wi-Fi secret redaction")
	fs.Var(&cfg.idle, "idle", "quiet time after data before considering response complete; Go duration or seconds")
	fs.Var(&cfg.readTimeout, "read-timeout", "max time to wait for each response; Go duration or seconds")
	fs.Var(&cfg.openDelay, "open-delay", "delay after opening serial port; Go duration or seconds")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usb: unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	cfg.redact = !showSecrets
	if cfg.readTimeout.Duration <= 0 {
		return fmt.Errorf("usb: --read-timeout must be positive")
	}
	if cfg.list {
		return listUSBPorts(stdout)
	}
	if cfg.port == "" {
		return fmt.Errorf("usb: --port is required unless --list is used")
	}
	if cfg.baud <= 0 {
		return fmt.Errorf("usb: --baud must be positive")
	}
	commands, err := usbCommandsFromConfig(cfg)
	if err != nil {
		return err
	}
	if len(commands) == 0 && !cfg.interactive {
		return fmt.Errorf("usb: no commands supplied; use --cmd, --file, or --interactive")
	}
	return runUSBCommands(ctx, cfg, commands, stdin, stdout)
}

func listUSBPorts(stdout io.Writer) error {
	if detailed, err := enumerator.GetDetailedPortsList(); err == nil && len(detailed) > 0 {
		for _, p := range detailed {
			fields := []string{p.Name}
			if p.Product != "" {
				fields = append(fields, p.Product)
			}
			if p.IsUSB {
				fields = append(fields, "usb")
			}
			if p.VID != "" || p.PID != "" {
				fields = append(fields, "vid="+p.VID, "pid="+p.PID)
			}
			if p.SerialNumber != "" {
				fields = append(fields, "serial="+p.SerialNumber)
			}
			fmt.Fprintln(stdout, strings.Join(fields, "\t"))
		}
		return nil
	}
	ports, err := serial.GetPortsList()
	if err != nil {
		return fmt.Errorf("list serial ports: %w", err)
	}
	if len(ports) == 0 {
		fmt.Fprintln(stdout, "No serial ports found.")
		return nil
	}
	for _, p := range ports {
		fmt.Fprintln(stdout, p)
	}
	return nil
}

func usbCommandsFromConfig(cfg usbConfig) ([]string, error) {
	commands := append([]string(nil), cfg.commands...)
	if cfg.file != "" {
		data, err := os.ReadFile(cfg.file)
		if err != nil {
			return nil, fmt.Errorf("usb: read command file %s: %w", cfg.file, err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			commands = append(commands, line)
		}
	}
	return commands, nil
}

func runUSBCommands(ctx context.Context, cfg usbConfig, commands []string, stdin io.Reader, stdout io.Writer) error {
	mode := &serial.Mode{BaudRate: cfg.baud, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit}
	port, err := serial.Open(cfg.port, mode)
	if err != nil {
		return fmt.Errorf("usb: open %s: %w", cfg.port, err)
	}
	defer port.Close()
	stop := context.AfterFunc(ctx, func() { _ = port.Close() })
	defer stop()
	if err := port.SetReadTimeout(100 * time.Millisecond); err != nil {
		return fmt.Errorf("usb: set read timeout: %w", err)
	}
	if cfg.openDelay.Duration > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.openDelay.Duration):
		}
	}
	if err := usbWriteLine(port, ""); err != nil {
		return err
	}
	banner, err := usbReadAvailable(ctx, port, cfg.idle.Duration, cfg.readTimeout.Duration)
	if err != nil {
		return err
	}
	if banner != "" {
		fmt.Fprint(stdout, redactUSBTextIf(cfg.redact, banner))
	}

	for _, cmd := range commands {
		fmt.Fprintf(stdout, "\n>>> %s\n", redactUSBTextIf(cfg.redact, cmd))
		if err := usbWriteLine(port, cmd); err != nil {
			return err
		}
		out, err := usbReadAvailable(ctx, port, cfg.idle.Duration, cfg.readTimeout.Duration)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, redactUSBTextIf(cfg.redact, out))
	}

	if cfg.interactive {
		fmt.Fprintln(stdout, "\nEntering line mode. Ctrl-D to exit.")
		lines := make(chan string)
		scanErr := make(chan error, 1)
		readCtx, cancelRead := context.WithCancel(ctx)
		defer cancelRead()
		go func() {
			scanner := bufio.NewScanner(stdin)
			for scanner.Scan() {
				select {
				case lines <- scanner.Text():
				case <-readCtx.Done():
					return
				}
			}
			scanErr <- scanner.Err()
			close(lines)
		}()
		for {
			fmt.Fprint(stdout, "> ")
			var cmd string
			var ok bool
			select {
			case <-ctx.Done():
				return ctx.Err()
			case cmd, ok = <-lines:
			}
			if !ok {
				return <-scanErr
			}
			if strings.TrimSpace(cmd) == "" {
				continue
			}
			if err := usbWriteLine(port, cmd); err != nil {
				return err
			}
			out, err := usbReadAvailable(ctx, port, cfg.idle.Duration, cfg.readTimeout.Duration)
			if err != nil {
				return err
			}
			fmt.Fprint(stdout, redactUSBTextIf(cfg.redact, out))
		}
	}
	return nil
}

func usbWriteLine(port serial.Port, line string) error {
	data := []byte(line + "\r\n")
	for len(data) > 0 {
		n, err := port.Write(data)
		if err != nil {
			return fmt.Errorf("usb: write serial command: %w", err)
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	if err := port.Drain(); err != nil {
		return fmt.Errorf("usb: drain serial writes: %w", err)
	}
	return nil
}

func usbReadAvailable(ctx context.Context, port serial.Port, idle, max time.Duration) (string, error) {
	if max <= 0 {
		return "", fmt.Errorf("usb: read timeout must be positive")
	}
	if idle < 0 {
		return "", fmt.Errorf("usb: idle timeout must be non-negative")
	}
	var chunks []byte
	buf := make([]byte, 4096)
	start := time.Now()
	lastData := start
	for time.Since(start) < max {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		n, err := port.Read(buf)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}
			return "", fmt.Errorf("usb: read serial response: %w", err)
		}
		if n > 0 {
			if len(chunks)+n > 1<<20 {
				return "", fmt.Errorf("usb: response exceeds 1 MiB")
			}
			chunks = append(chunks, buf[:n]...)
			lastData = time.Now()
			continue
		}
		if len(chunks) > 0 && time.Since(lastData) >= idle {
			break
		}
	}
	return string(chunks), nil
}

func redactUSBTextIf(redact bool, text string) string {
	if !redact {
		return text
	}
	return redactUSBText(text)
}

func redactUSBText(text string) string {
	for _, p := range lucioUSBSecretPatterns {
		text = p.pattern.ReplaceAllString(text, p.replacement)
	}
	return text
}
