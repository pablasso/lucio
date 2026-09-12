package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/pv3"
	"github.com/pablasso/lucio/internal/telemetry"
)

const maxHandshakes = 8
const retainedUpdates = 32

type session struct {
	conn    net.Conn
	writeMu sync.Mutex
	id      uint64
	device  [16]byte
	remote  string
	opened  time.Time
}
type operation struct {
	result   control.Update // accessed only under Server.mu
	asset    *asset
	done     chan struct{}
	written  chan struct{}
	writeOK  bool
	earlyACK *control.ImageACK
	deadline time.Time
	timer    *time.Timer
}

type Server struct {
	cfg            Config
	logger         *log.Logger
	runID          string
	mu             sync.Mutex
	closed         bool
	stopped        chan struct{}
	serving        bool
	sessions       map[net.Conn]struct{}
	conn           *session
	pinned         [16]byte
	nextConnection uint64
	nextPacket     uint32
	lastStatus     *control.Status
	lastSent       *control.SentImage
	lastACK        *control.ImageACK
	image          *control.Image
	display        control.DisplayState
	baseline       *image.Gray
	pending        *operation
	updates        map[string]*operation
	order          []string
	wg             sync.WaitGroup
	uploadGate     chan struct{}
	handshakeGate  chan struct{}
	recorder       telemetry.Recorder
	telemetryMu    sync.Mutex
	telemetryError string
	closeOnce      sync.Once
	closeErr       error
}

func New(cfg Config, logger *log.Logger) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	id, err := telemetry.NewRunID()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	s := &Server{cfg: cfg, logger: logger, runID: id, nextPacket: 1, stopped: make(chan struct{}), sessions: map[net.Conn]struct{}{}, updates: map[string]*operation{}, uploadGate: make(chan struct{}, 1), handshakeGate: make(chan struct{}, maxHandshakes)}
	if cfg.DeviceID != "" {
		s.pinned, _ = pv3.ParseDeviceIDString(cfg.DeviceID)
	}
	s.display.EligibilityReason = "no_acked_baseline"
	if cfg.EventLog != "" {
		r, err := telemetry.Open(cfg.EventLog, cfg.EventLogMaxBytes)
		if err != nil {
			return nil, err
		}
		s.recorder = r
		if err := r.Record(telemetry.Event{Schema: telemetry.SchemaV1, Type: telemetry.DaemonStarted, At: time.Now().UTC(), RunID: id, Daemon: &telemetry.DaemonEvent{Version: cfg.Version, GatewayListen: cfg.GatewayListen, HTTPListen: cfg.HTTPListen, PID: os.Getpid()}}); err != nil {
			r.Close()
			return nil, err
		}
	}
	return s, nil
}

// ListenAndServe binds both listeners before accepting work. Cancellation stops
// the daemon, not the screen, and does not send or persist an image.
func ListenAndServe(ctx context.Context, cfg Config, logger *log.Logger) error {
	s, err := New(cfg, logger)
	if err != nil {
		return err
	}
	defer s.Close()
	gateway, err := net.Listen("tcp", cfg.GatewayListen)
	if err != nil {
		return err
	}
	defer gateway.Close()
	ipc, err := net.Listen("tcp", cfg.HTTPListen)
	if err != nil {
		return err
	}
	defer ipc.Close()
	return s.Serve(ctx, gateway, ipc)
}

// Serve owns both supplied listeners. Each Server may be served only once.
func (s *Server) Serve(ctx context.Context, gateway, ipc net.Listener) error {
	s.mu.Lock()
	if s.serving || s.closed {
		s.mu.Unlock()
		return fmt.Errorf("server already started or closed")
	}
	s.serving = true
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	h := &http.Server{Handler: s.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: s.cfg.ACKTimeout + s.cfg.WriteTimeout + 30*time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	errs := make(chan error, 2)
	go func() { errs <- s.accept(gateway) }()
	go func() { errs <- h.Serve(ipc) }()
	s.logger.Printf("listening gateway=%s control=%s instance=%s", gateway.Addr(), ipc.Addr(), s.runID)
	var result error
	select {
	case <-ctx.Done():
	case <-s.stopped:
	case result = <-errs:
	}
	s.stop()
	cancel()
	gateway.Close()
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 3*time.Second)
	if err := h.Shutdown(shutdown); err != nil {
		h.Close()
	}
	cancelShutdown()
	ipc.Close()
	s.wg.Wait()
	if err := s.Close(); result == nil {
		result = err
	}
	if errors.Is(result, net.ErrClosed) || errors.Is(result, http.ErrServerClosed) {
		return nil
	}
	return result
}

func (s *Server) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.stopped)
	if s.pending != nil {
		s.failLocked(s.pending, "daemon_stopped")
	}
	s.invalidateLocked("daemon_stopped")
	for c := range s.sessions {
		c.Close()
	}
	s.conn = nil
}
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		s.stop()
		s.wg.Wait()
		if s.recorder != nil {
			s.closeErr = s.recorder.Close()
		}
	})
	return s.closeErr
}

func (s *Server) accept(ln net.Listener) error {
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		select {
		case s.handshakeGate <- struct{}{}:
		default:
			c.Close()
			continue
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			<-s.handshakeGate
			c.Close()
			return net.ErrClosed
		}
		s.sessions[c] = struct{}{}
		s.wg.Add(1)
		s.mu.Unlock()
		go func() { defer s.wg.Done(); s.handle(c) }()
	}
}

func (s *Server) handle(c net.Conn) {
	var cc *session
	handshake := true
	defer func() {
		if handshake {
			<-s.handshakeGate
		}
		c.Close()
		s.mu.Lock()
		delete(s.sessions, c)
		if cc != nil && s.conn == cc {
			if s.pending != nil {
				s.failLocked(s.pending, "connection_closed")
			}
			s.conn = nil
			s.invalidateLocked("connection_closed_requires_full_baseline")
		}
		s.mu.Unlock()
		if cc != nil {
			s.record(telemetry.Event{Type: telemetry.ConnectionClose, Remote: cc.remote, ConnectionID: cc.id, DeviceID: pv3.DeviceIDString(cc.device)})
		}
	}()
	c.SetReadDeadline(time.Now().Add(s.cfg.HandshakeTimeout))
	frame, err := pv3.ReadFrame(c, s.cfg.MaxFrameBytes)
	if err != nil {
		return
	}
	pkt, err := pv3.ParseScreenPacket(frame)
	if err != nil || pkt.Class != pv3.ScreenClassStatus || !validStatus(pkt) {
		return
	}
	s.mu.Lock()
	if s.closed || (s.pinned != [16]byte{} && s.pinned != pkt.DeviceID) {
		s.mu.Unlock()
		return
	}
	s.pinned = pkt.DeviceID
	old := s.conn
	if s.pending != nil {
		s.failLocked(s.pending, "connection_replaced")
	}
	s.nextConnection++
	cc = &session{conn: c, id: s.nextConnection, device: pkt.DeviceID, remote: c.RemoteAddr().String(), opened: time.Now().UTC()}
	s.conn = cc
	s.lastStatus = nil
	s.invalidateLocked("new_connection_requires_full_baseline")
	s.mu.Unlock()
	if old != nil {
		old.conn.Close()
	}
	<-s.handshakeGate
	handshake = false
	s.logger.Printf("screen connected device=%s connection=%d", pv3.DeviceIDString(cc.device), cc.id)
	s.record(telemetry.Event{Type: telemetry.ConnectionOpen, At: cc.opened, Remote: cc.remote, ConnectionID: cc.id, DeviceID: pv3.DeviceIDString(cc.device)})
	for {
		if pkt.DeviceID != cc.device {
			return
		}
		switch pkt.Class {
		case pv3.ScreenClassStatus:
			if !validStatus(pkt) {
				return
			}
			if !s.noteStatus(cc, pkt) {
				return
			}
			if err := s.writeFrame(cc, pv3.BuildServerControlACK(cc.device, pkt.PacketID), time.Time{}); err != nil {
				return
			}
		case pv3.ScreenClassControl:
			ctl, err := pv3.ParseControlPayload(pkt.InnerPayload)
			if err != nil {
				return
			}
			s.noteACK(cc, pkt.PacketID, ctl)
		case pv3.ScreenClassParam:
			if err := s.writeFrame(cc, pv3.BuildServerControlACK(cc.device, pkt.PacketID), time.Time{}); err != nil {
				return
			}
		default:
			s.logger.Printf("ignoring screen class=%d connection=%d", pkt.Class, cc.id)
		}
		deadline := time.Time{}
		if s.cfg.ReadTimeout > 0 {
			deadline = time.Now().Add(s.cfg.ReadTimeout)
		}
		c.SetReadDeadline(deadline)
		// Idle connections can remain quiet for hours. Once a frame starts,
		// however, its bounded payload must finish within the handshake budget.
		var first [1]byte
		if _, err = io.ReadFull(c, first[:]); err != nil {
			return
		}
		c.SetReadDeadline(time.Now().Add(s.cfg.HandshakeTimeout))
		frame, err = pv3.ReadFrame(io.MultiReader(bytes.NewReader(first[:]), c), s.cfg.MaxFrameBytes)
		if err != nil {
			return
		}
		pkt, err = pv3.ParseScreenPacket(frame)
		if err != nil {
			return
		}
	}
}
func validStatus(pkt pv3.ScreenPacket) bool {
	if pkt.DeviceID == [16]byte{} || len(pkt.InnerPayload) < 8 || len(pkt.InnerPayload)%8 != 0 {
		return false
	}
	fields := pv3.StatusTupleMap(pv3.ParseStatusTuples(pkt.InnerPayload))
	if w, ok := fields[0x26]; ok && w != pv3.InternalRectWidth {
		return false
	}
	if h, ok := fields[0x27]; ok && h != pv3.InternalRectHeight {
		return false
	}
	return true
}
func (s *Server) writeFrame(cc *session, f pv3.Frame, deadline time.Time) error {
	cc.writeMu.Lock()
	defer cc.writeMu.Unlock()
	limit := time.Now().Add(s.cfg.WriteTimeout)
	if !deadline.IsZero() && deadline.Before(limit) {
		limit = deadline
	}
	if err := cc.conn.SetWriteDeadline(limit); err != nil {
		return err
	}
	return pv3.WriteFrame(cc.conn, f)
}

func (s *Server) record(e telemetry.Event) {
	if s.recorder == nil {
		return
	}
	e.Schema = telemetry.SchemaV1
	e.RunID = s.runID
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	} else {
		e.At = e.At.UTC()
	}
	if err := s.recorder.Record(e); err != nil {
		s.telemetryMu.Lock()
		s.telemetryError = err.Error()
		s.telemetryMu.Unlock()
		s.logger.Printf("event log error: %v", err)
	}
}
func (s *Server) telemetryStatus() telemetry.RuntimeStatus {
	s.telemetryMu.Lock()
	defer s.telemetryMu.Unlock()
	return telemetry.RuntimeStatus{Enabled: s.recorder != nil, Schema: telemetry.SchemaV1, RunID: s.runID, Error: s.telemetryError}
}
