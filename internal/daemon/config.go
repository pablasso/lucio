// Package daemon owns the screen connection and the complete image-update
// lifecycle. Rendering and content fetching remain outside this package.
package daemon

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/pv3"
	"github.com/pablasso/lucio/internal/telemetry"
)

type Config struct {
	GatewayListen      string
	HTTPListen         string
	AllowRemoteControl bool
	DeviceID           string
	MaxFrameBytes      uint32
	MaxUploadBytes     int64
	HandshakeTimeout   time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	ACKTimeout         time.Duration
	EventLog           string
	EventLogMaxBytes   int64
	Version            string
}

func DefaultConfig() Config {
	return Config{GatewayListen: "0.0.0.0:11113", HTTPListen: control.DefaultAddress,
		MaxFrameBytes: 64 << 10, MaxUploadBytes: 32 << 20, HandshakeTimeout: 15 * time.Second,
		WriteTimeout: 60 * time.Second, ACKTimeout: 2 * time.Minute, EventLogMaxBytes: telemetry.DefaultMaxBytes,
	}
}
func (c Config) Validate() error {
	for _, a := range []string{c.GatewayListen, c.HTTPListen} {
		host, port, err := net.SplitHostPort(a)
		if err != nil {
			return fmt.Errorf("invalid listen address %q: %w", a, err)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return fmt.Errorf("invalid listen port %q", port)
		}
		// Literal addresses avoid DNS rebinding and startup-dependent resolution.
		if host != "" && net.ParseIP(host) == nil {
			return fmt.Errorf("listen addresses must use literal IPs: %q", a)
		}
	}
	host, _, _ := net.SplitHostPort(c.HTTPListen)
	if !c.AllowRemoteControl && !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("control listener must be loopback; remote control requires --allow-remote-control and external access protection")
	}
	if c.MaxFrameBytes < 44 || c.MaxFrameBytes > 1<<20 {
		return fmt.Errorf("max frame bytes must be between 44 and 1048576")
	}
	if c.MaxUploadBytes <= 0 || c.MaxUploadBytes > 64<<20 {
		return fmt.Errorf("max upload bytes must be between 1 and 67108864")
	}
	if c.HandshakeTimeout <= 0 || c.HandshakeTimeout > time.Minute || c.ReadTimeout < 0 || c.WriteTimeout <= 0 || c.WriteTimeout > 10*time.Minute || c.ACKTimeout <= 0 || c.ACKTimeout > 10*time.Minute {
		return fmt.Errorf("invalid timeouts: handshake 0–1m, write/ACK 0–10m (exclusive lower bound), read >= 0")
	}
	if c.EventLogMaxBytes <= 0 || c.EventLogMaxBytes > 1<<30 {
		return fmt.Errorf("event log maximum must be between 1 byte and 1 GiB")
	}
	if c.DeviceID != "" {
		id, err := pv3.ParseDeviceIDString(c.DeviceID)
		if err != nil || id == [16]byte{} {
			return fmt.Errorf("device-id must be a nonzero 16-byte hexadecimal ID")
		}
	}
	return nil
}
