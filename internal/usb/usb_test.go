package usb

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRedactUSBText(t *testing.T) {
	input := strings.Join([]string{
		"PSK: supersecret",
		"wifi_psk_set anothersecret",
		"wifi_conf_set MySSID thirdsecret wpa2 0",
	}, "\n")
	out := redactUSBText(input)
	for _, secret := range []string{"supersecret", "anothersecret", "thirdsecret"} {
		if strings.Contains(out, secret) {
			t.Fatalf("redacted output still contains %q:\n%s", secret, out)
		}
	}
	for _, want := range []string{"PSK: <redacted>", "wifi_psk_set <redacted>", "wifi_conf_set MySSID <redacted> wpa2 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("redacted output missing %q:\n%s", want, out)
		}
	}
}

func TestUSBCommandsFromConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.txt")
	if err := os.WriteFile(path, []byte("# comment\nhelp\n\nserver_tcp_get\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := usbCommandsFromConfig(usbConfig{commands: usbCommandList{"uuid_get"}, file: path})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"uuid_get", "help", "server_tcp_get"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestSecondsDurationAcceptsSecondsFloat(t *testing.T) {
	var d secondsDuration
	if err := d.Set("0.5"); err != nil {
		t.Fatal(err)
	}
	if d.Duration.String() != "500ms" {
		t.Fatalf("duration = %s, want 500ms", d.Duration)
	}
	if err := d.Set("2s"); err != nil {
		t.Fatal(err)
	}
	if d.Duration.String() != "2s" {
		t.Fatalf("duration = %s, want 2s", d.Duration)
	}
}
