package daemon

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/delivery"
	"github.com/pablasso/lucio/internal/pv3"
)

type rig struct {
	s       *Server
	c       *control.Client
	gateway string
	http    string
	cancel  context.CancelFunc
	done    chan struct{}
}

func startRig(t *testing.T, change func(*Config)) *rig {
	t.Helper()
	cfg := DefaultConfig()
	cfg.GatewayListen = "127.0.0.1:0"
	cfg.HTTPListen = "127.0.0.1:0"
	cfg.ACKTimeout = 3 * time.Second
	cfg.WriteTimeout = time.Second
	if change != nil {
		change(&cfg)
	}
	s, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err := net.Listen("tcp", cfg.GatewayListen)
	if err != nil {
		t.Fatal(err)
	}
	h, err := net.Listen("tcp", cfg.HTTPListen)
	if err != nil {
		g.Close()
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &rig{s: s, gateway: g.Addr().String(), http: "http://" + h.Addr().String(), cancel: cancel, done: make(chan struct{})}
	r.c, _ = control.NewClient(r.http)
	var serveErr error
	go func() { serveErr = s.Serve(ctx, g, h); close(r.done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
			if serveErr != nil {
				t.Error(serveErr)
			}
		case <-time.After(6 * time.Second):
			t.Error("daemon shutdown hung")
		}
	})
	return r
}
func fakeID() (id [16]byte) {
	for i := range id {
		id[i] = byte(i)
	}
	return
}
func packet(id [16]byte, class, number uint32, body []byte) pv3.Frame {
	b := make([]byte, 36+len(body))
	copy(b[4:20], id[:])
	binary.LittleEndian.PutUint32(b[20:], class)
	binary.LittleEndian.PutUint32(b[24:], number)
	binary.LittleEndian.PutUint32(b[28:], uint32(len(body)))
	copy(b[36:], body)
	return pv3.BuildFrame(0, b)
}
func statusPacket(id [16]byte, crc uint32) pv3.Frame {
	var b bytes.Buffer
	for _, p := range [][2]uint32{{0, 3}, {9, crc}, {0x0a, 85}, {0x26, 2880}, {0x27, 640}, {0x36, 1}, {0xffffffff, 0}} {
		binary.Write(&b, binary.LittleEndian, p)
	}
	return packet(id, 3, 18, b.Bytes())
}
func (r *rig) screen(t *testing.T) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", r.gateway)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	sendStatus(t, c, 0)
	return c
}
func readFrame(t *testing.T, c net.Conn) pv3.Frame {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	f, err := pv3.ReadFrame(c, 10<<20)
	if err != nil {
		t.Fatal(err)
	}
	if err := pv3.ValidateServerChecksum(f); err != nil {
		t.Fatal(err)
	}
	return f
}
func sendStatus(t *testing.T, c net.Conn, crc uint32) {
	t.Helper()
	if err := pv3.WriteFrame(c, statusPacket(fakeID(), crc)); err != nil {
		t.Fatal(err)
	}
	if f := readFrame(t, c); len(f.Payload) != 68 {
		t.Fatalf("expected status ACK, got %d bytes", len(f.Payload))
	}
}
func ack(t *testing.T, c net.Conn, id, code uint32) {
	t.Helper()
	b := make([]byte, 8)
	binary.LittleEndian.PutUint32(b, code)
	if err := pv3.WriteFrame(c, packet(fakeID(), 1, id, b)); err != nil {
		t.Fatal(err)
	}
}
func pngData(t *testing.T, g *image.Gray) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, g); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func canvas() *image.Gray { return image.NewGray(image.Rect(0, 0, 1440, 2560)) }
func (r *rig) submit(t *testing.T, g *image.Gray, p delivery.Policy) control.Update {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, err := r.c.Submit(ctx, "example.png", pngData(t, g), p)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func (r *rig) wait(t *testing.T, id string) control.Update {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, err := r.c.Wait(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func (r *rig) health(t *testing.T) *control.Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	h, err := r.c.Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
func TestFullPartialAndCRC(t *testing.T) {
	r := startRig(t, nil)
	c := r.screen(t)
	g := canvas()
	u := r.submit(t, g, delivery.PolicyFull)
	f := readFrame(t, c)
	expected, err := pv3.BuildImageFrameFromPortraitGray(g, fakeID(), u.Sent.PacketID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.Bytes(), expected.Frame.Bytes()) {
		t.Fatal("wire image differs from submitted target")
	}
	ack(t, c, u.Sent.PacketID, 1)
	r.wait(t, u.ID)
	sendStatus(t, c, u.Sent.Checksum)
	if !r.health(t).DisplayState.PartialEligible {
		t.Fatal("missing ACKed baseline")
	}
	g.Pix[100*g.Stride+10] = 255
	part := r.submit(t, g, delivery.PolicyAuto)
	readFrame(t, c)
	if part.Sent.Delivery != "partial" || part.Sent.PartialPlan == nil {
		t.Fatalf("not partial: %+v", part)
	}
	ack(t, c, part.Sent.PacketID, 1)
	r.wait(t, part.ID)
	sendStatus(t, c, 0x12345678)
	if r.health(t).DisplayState.PartialEligible {
		t.Fatal("CRC mismatch preserved baseline")
	}
	fallback := r.submit(t, g, delivery.PolicyAuto)
	readFrame(t, c)
	if fallback.Sent.Delivery != "full" {
		t.Fatal("unsafe partial")
	}
	ack(t, c, fallback.Sent.PacketID, 1)
	r.wait(t, fallback.ID)
	// Waiting for an older result is independent of the latest ACK.
	r.wait(t, u.ID)
}
func TestConcurrentSubmissionsAreAtomic(t *testing.T) {
	r := startRig(t, nil)
	c := r.screen(t)
	g1, g2 := canvas(), canvas()
	g2.Pix[0] = 255
	b1, b2 := pngData(t, g1), pngData(t, g2)
	var wg sync.WaitGroup
	type result struct {
		u   control.Update
		err error
		g   *image.Gray
	}
	results := make(chan result, 2)
	for i, b := range [][]byte{b1, b2} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			u, err := r.c.Submit(ctx, "concurrent.png", b, delivery.PolicyFull)
			results <- result{u, err, []*image.Gray{g1, g2}[i]}
		}()
	}
	wg.Wait()
	close(results)
	var winner result
	success, busy := 0, 0
	for x := range results {
		if x.err == nil {
			success++
			winner = x
		} else {
			var e *control.HTTPError
			if !errors.As(x.err, &e) || e.Code != "busy" {
				t.Fatal(x.err)
			}
			busy++
		}
	}
	if success != 1 || busy != 1 {
		t.Fatal(success, busy)
	}
	frame := readFrame(t, c)
	want, _ := pv3.BuildImageFrameFromPortraitGray(winner.g, fakeID(), winner.u.Sent.PacketID, nil)
	if !bytes.Equal(frame.Bytes(), want.Frame.Bytes()) {
		t.Fatal("crossed concurrent images")
	}
	ack(t, c, winner.u.Sent.PacketID, 1)
	r.wait(t, winner.u.ID)
}
func TestAutonomousTimeoutAndLateACK(t *testing.T) {
	r := startRig(t, func(c *Config) { c.ACKTimeout = 150 * time.Millisecond })
	c := r.screen(t)
	r.s.mu.Lock()
	old := r.s.conn
	r.s.mu.Unlock()
	u := r.submit(t, canvas(), delivery.PolicyFull)
	readFrame(t, c)
	time.Sleep(250 * time.Millisecond) // deliberately no health/status polling
	r.s.mu.Lock()
	pending := r.s.pending
	r.s.mu.Unlock()
	if pending != nil {
		t.Fatal("timeout depends on polling")
	}
	r.s.noteACK(old, u.Sent.PacketID, pv3.ControlPayload{Code: 1})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := r.c.Wait(ctx, u.ID)
	if err == nil || result.State != control.UpdateFailed || result.Error != "ack_timeout" {
		t.Fatal(result, err)
	}
	if r.health(t).DisplayState.PartialEligible {
		t.Fatal("late ACK revived baseline")
	}
}
func TestReconnectAndWrongDevice(t *testing.T) {
	r := startRig(t, nil)
	c := r.screen(t)
	u := r.submit(t, canvas(), delivery.PolicyFull)
	readFrame(t, c)
	ack(t, c, u.Sent.PacketID, 1)
	r.wait(t, u.ID)
	before := r.health(t)
	stranger, err := net.Dial("tcp", r.gateway)
	if err != nil {
		t.Fatal(err)
	}
	defer stranger.Close()
	id := fakeID()
	id[0] = 99
	pv3.WriteFrame(stranger, statusPacket(id, 0))
	stranger.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := pv3.ReadFrame(stranger, 1<<20); err == nil {
		t.Fatal("accepted another device")
	}
	if r.health(t).ConnectionID != before.ConnectionID {
		t.Fatal("stranger evicted screen")
	}
	r.s.mu.Lock()
	old := r.s.conn
	r.s.mu.Unlock()
	c2 := r.screen(t)
	r.s.noteACK(old, u.Sent.PacketID, pv3.ControlPayload{Code: 1})
	if r.health(t).DisplayState.PartialEligible {
		t.Fatal("reconnect or old ACK preserved baseline")
	}
	next := r.submit(t, canvas(), delivery.PolicyAuto)
	readFrame(t, c2)
	if next.ID == u.ID || next.Sent.Delivery != "full" {
		t.Fatal(next)
	}
	ack(t, c2, next.Sent.PacketID, 1)
	r.wait(t, next.ID)
}
func TestNACKAndUnexpectedACK(t *testing.T) {
	for _, code := range []uint32{0, 1} {
		t.Run(string(rune('0'+code)), func(t *testing.T) {
			r := startRig(t, nil)
			c := r.screen(t)
			u := r.submit(t, canvas(), delivery.PolicyFull)
			readFrame(t, c)
			id := u.Sent.PacketID
			if code == 1 {
				id++
			}
			ack(t, c, id, code)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			result, err := r.c.Wait(ctx, u.ID)
			if err == nil || result.State != control.UpdateFailed {
				t.Fatal(result, err)
			}
			if r.health(t).DisplayState.PartialEligible {
				t.Fatal("bad ACK created baseline")
			}
		})
	}
}
func TestShutdownPendingUpdate(t *testing.T) {
	r := startRig(t, nil)
	c := r.screen(t)
	u := r.submit(t, canvas(), delivery.PolicyFull)
	readFrame(t, c)
	r.cancel()
	select {
	case <-r.done:
	case <-time.After(4 * time.Second):
		t.Fatal("shutdown hung")
	}
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.updates[u.ID].result.State != control.UpdateFailed {
		t.Fatal("pending update was not failed")
	}
}
func TestHTTPGuardsAndLimits(t *testing.T) {
	r := startRig(t, func(c *Config) { c.MaxUploadBytes = 16 })
	cases := []struct {
		method, path, origin, host, client string
		body                               []byte
		want                               int
	}{
		{"GET", "/healthz", "", "", "", nil, 403},
		{"GET", "/healthz", "https://example.test", "", "1", nil, 403},
		{"GET", "/healthz", "", "example.test:80", "1", nil, 403},
		{"POST", "/api/image?checksum=123", "", "", "1", nil, 400},
		{"POST", "/api/image", "", "", "1", make([]byte, 32), 413},
	}
	for _, x := range cases {
		req, _ := http.NewRequest(x.method, r.http+x.path, bytes.NewReader(x.body))
		req.Header.Set("X-Lucio-Client", x.client)
		req.Header.Set("Origin", x.origin)
		req.Header.Set("Content-Type", "image/png")
		if x.host != "" {
			req.Host = x.host
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != x.want {
			t.Fatalf("%s: %d want %d", x.path, resp.StatusCode, x.want)
		}
	}
}
func TestDefaultConfigSafety(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPListen != control.DefaultAddress || cfg.EventLog != "" || cfg.AllowRemoteControl {
		t.Fatal(cfg)
	}
	cfg.HTTPListen = ":8080"
	if err := cfg.Validate(); err == nil {
		t.Fatal("public default allowed")
	}
	cfg.AllowRemoteControl = true
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.ACKTimeout = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("zero ACK deadline allowed")
	}
}
