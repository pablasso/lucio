package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/delivery"
)

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/image", s.handleImage)
	mux.HandleFunc("GET /api/updates/{id}", s.handleUpdate)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("X-Lucio-Client") != "1" || r.Header.Get("Origin") != "" {
			writeError(w, 403, "forbidden", "private IPC requires a Lucio client; browser origins are not allowed")
			return
		}
		if !s.cfg.AllowRemoteControl {
			host, _, err := net.SplitHostPort(r.Host)
			if err != nil || !net.ParseIP(host).IsLoopback() {
				writeError(w, 403, "forbidden", "control Host must be loopback")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	v := control.Snapshot{OK: !s.closed, RunID: s.runID, Connected: s.conn != nil, HasDevice: s.conn != nil, ImageLoaded: s.image != nil, NextPacketID: s.nextPacket, LastStatus: s.lastStatus, LastSent: s.lastSent, LastAck: s.lastACK, DisplayState: s.display, Telemetry: s.telemetryStatus(), Image: s.image}
	if s.conn != nil {
		v.Remote = s.conn.remote
		v.ConnectedAt = s.conn.opened
		v.ConnectionID = s.conn.id
		v.DeviceID = s.lastDeviceIDLocked()
	}
	// Serialize while holding the lock; no pointers to mutable state escape to
	// concurrent HTTP encoders. The actual network write happens after unlocking.
	b, err := json.Marshal(v)
	s.mu.Unlock()
	writeEncoded(w, b, err)
}
func (s *Server) lastDeviceIDLocked() string {
	// Device IDs are byte ordered, not RFC4122-endian converted.
	return fmt.Sprintf("%x-%x-%x-%x-%x", s.conn.device[:4], s.conn.device[4:6], s.conn.device[6:8], s.conn.device[8:10], s.conn.device[10:])
}
func query(r *http.Request, allowed ...string) (url.Values, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, err
	}
	for key, values := range q {
		found := false
		for _, a := range allowed {
			if key == a {
				found = true
			}
		}
		if !found || len(values) != 1 {
			return nil, fmt.Errorf("unknown or repeated query parameter %q", key)
		}
	}
	return q, nil
}
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	q, err := query(r, "name", "update")
	if err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	policy, err := delivery.ParsePolicy(q.Get("update"))
	if err != nil {
		writeError(w, 400, "invalid", err.Error())
		return
	}
	name := q.Get("name")
	if name == "" {
		name = "image.png"
	}
	if len(name) > 128 || strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		writeError(w, 400, "invalid", "image name must be a short basename without control characters")
		return
	}
	if r.Header.Get("Content-Type") != "image/png" {
		writeError(w, 415, "invalid", "expected image/png")
		return
	}
	select {
	case s.uploadGate <- struct{}{}:
		defer func() { <-s.uploadGate }()
	default:
		writeError(w, 409, "busy", "another upload is being decoded")
		return
	}
	s.mu.Lock()
	busy, closed := s.pending != nil, s.closed
	s.mu.Unlock()
	if closed {
		writeError(w, 503, "stopped", "daemon is stopping")
		return
	}
	if busy {
		writeError(w, 409, "busy", "an image update is already in flight")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.cfg.MaxUploadBytes))
	if err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			writeError(w, 413, "too_large", "image upload exceeds byte limit")
		} else {
			writeError(w, 400, "invalid", err.Error())
		}
		return
	}
	asset, err := decodeImage(name, data)
	if err != nil {
		writeError(w, 400, "invalid_image", err.Error())
		return
	}
	if r.Context().Err() != nil {
		return
	}
	op, err := s.submit(asset, policy)
	if err != nil {
		var e *control.HTTPError
		if errors.As(err, &e) {
			writeError(w, e.Status, e.Code, e.Message)
		} else {
			writeError(w, 500, "internal", err.Error())
		}
		return
	}
	select {
	case <-r.Context().Done():
		return
	case <-op.written:
	}
	s.writeUpdate(w, op)
}
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	q, err := query(r, "wait")
	if err != nil || (q.Get("wait") != "" && q.Get("wait") != "1") {
		writeError(w, 400, "invalid", "wait must be 1 or omitted")
		return
	}
	s.mu.Lock()
	op := s.updates[r.PathValue("id")]
	s.mu.Unlock()
	if op == nil {
		writeError(w, 404, "unknown_update", "update expired or belongs to another daemon instance")
		return
	}
	if q.Get("wait") == "1" {
		select {
		case <-r.Context().Done():
			return
		case <-op.done:
		}
	}
	s.writeUpdate(w, op)
}
func (s *Server) writeUpdate(w http.ResponseWriter, op *operation) {
	s.mu.Lock()
	b, err := json.Marshal(op.result)
	s.mu.Unlock()
	writeEncoded(w, b, err)
}
func writeEncoded(w http.ResponseWriter, b []byte, err error) {
	if err != nil {
		writeError(w, 500, "internal", "failed to encode response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(append(b, '\n'))
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(control.ErrorResponse{Code: code, Error: message})
}
