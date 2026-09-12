package usb

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"go.bug.st/serial"
)

type stubPort struct {
	serial.Port
	output bytes.Buffer
	input  []byte
}

func (p *stubPort) Write(b []byte) (int, error) { return p.output.Write(b[:min(2, len(b))]) }
func (p *stubPort) Drain() error                { return nil }
func (p *stubPort) Read(b []byte) (int, error) {
	n := copy(b, p.input)
	p.input = p.input[n:]
	return n, nil
}
func TestSerialLineAndReadWithoutHardware(t *testing.T) {
	p := &stubPort{input: []byte("OK\r\n")}
	if err := usbWriteLine(p, "help"); err != nil || p.output.String() != "help\r\n" {
		t.Fatal(p.output.String(), err)
	}
	text, err := usbReadAvailable(context.Background(), p, time.Millisecond, time.Second)
	if err != nil || text != "OK\r\n" {
		t.Fatal(text, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := usbReadAvailable(ctx, p, time.Millisecond, time.Second); err == nil {
		t.Fatal("ignored cancellation")
	}
}
func TestResponseAndDurationLimits(t *testing.T) {
	p := &stubPort{input: []byte(strings.Repeat("x", (1<<20)+1))}
	if _, err := usbReadAvailable(context.Background(), p, time.Millisecond, time.Second); err == nil {
		t.Fatal("unbounded response")
	}
	for _, s := range []string{"NaN", "Inf", "-1", "61", "2m"} {
		var d secondsDuration
		if err := d.Set(s); err == nil {
			t.Fatal(s)
		}
	}
}
