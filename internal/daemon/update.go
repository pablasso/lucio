package daemon

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"sort"
	"time"

	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/delivery"
	"github.com/pablasso/lucio/internal/pv3"
	"github.com/pablasso/lucio/internal/telemetry"
)

type asset struct {
	meta    control.Image
	gray    *image.Gray
	payload []byte
}

func decodeImage(name string, b []byte) (*asset, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("decode PNG header: %w", err)
	}
	if cfg.Width != pv3.PortraitWidth || cfg.Height != pv3.PortraitHeight {
		return nil, fmt.Errorf("PNG must be %dx%d, got %dx%d", pv3.PortraitWidth, pv3.PortraitHeight, cfg.Width, cfg.Height)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("decode PNG: %w", err)
	}
	gray, err := pv3.PortraitGrayFromImage(img)
	if err != nil {
		return nil, err
	}
	payload, err := pv3.BuildImagePayloadFromPortraitGray(gray)
	if err != nil {
		return nil, err
	}
	crc := pv3.DefaultImageChecksum(payload)
	return &asset{gray: gray, payload: payload, meta: control.Image{Name: name, Format: "png", LoadedAt: time.Now().UTC(), Width: cfg.Width, Height: cfg.Height, PayloadBytes: len(payload), Checksum: crc, ChecksumHex: fmt.Sprintf("0x%08x", crc), ChecksumSource: "crc32-ieee-image-payload"}}, nil
}

// submit reserves the complete transaction before the first byte is written.
// The asset belongs exclusively to this operation; there is no global mutable
// upload slot. A busy daemon refuses work rather than silently queueing it.
func (s *Server) submit(a *asset, policy delivery.Policy) (*operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, &control.HTTPError{Status: 503, Code: "stopped", Message: "daemon is stopping"}
	}
	if s.pending != nil {
		return nil, &control.HTTPError{Status: 409, Code: "busy", Message: "an image update is already in flight"}
	}
	cc := s.conn
	if cc == nil {
		return nil, &control.HTTPError{Status: 409, Code: "disconnected", Message: "no identified screen connection"}
	}
	if s.nextPacket == 0 {
		return nil, fmt.Errorf("packet IDs exhausted; restart daemon")
	}
	if s.lastStatus != nil {
		for _, f := range s.lastStatus.Fields {
			if f.ID == 0x36 && f.Value == 0 {
				return nil, &control.HTTPError{Status: 409, Code: "not_ready", Message: "screen reports image push unavailable"}
			}
		}
	}
	packet := s.nextPacket
	var built pv3.ImageBuildResult
	var plan *delivery.SafePartialPlan
	reason, kind := "forced_full", delivery.PolicyFull
	if policy != delivery.PolicyFull {
		p := delivery.NewSafePartialFullPlan(s.display.EligibilityReason, delivery.DefaultSafePartialGeometryOptions())
		if s.display.PartialEligible && s.baseline != nil && s.display.EligibleConnectionID == cc.id {
			var err error
			p, err = delivery.PlanSafePartialGeometry(s.baseline, a.gray, delivery.DefaultSafePartialGeometryOptions())
			if err != nil {
				return nil, err
			}
		}
		plan = &p
		if p.CanPartial() {
			strips := make([]pv3.PartialStrip, len(p.Strips))
			for i, x := range p.Strips {
				strips[i] = pv3.PartialStrip{X: x.X, Y: x.Y, W: x.W, H: x.H, Screens: x.Screens}
			}
			var err error
			built, err = pv3.BuildPartialImageFrameFromPortraitGray(a.gray, strips, cc.device, packet, a.meta.Checksum)
			if err == nil {
				kind = delivery.PolicyPartial
				reason = p.Reason
			} else {
				reason = "partial_build_failed_fallback_full"
			}
		} else {
			reason = "partial_" + p.Reason + "_fallback_full"
		}
	}
	if kind == delivery.PolicyFull {
		var err error
		built, err = pv3.BuildImageFrameFromPayload(a.payload, cc.device, packet, nil)
		if err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	sent := control.SentImage{At: now, Remote: cc.remote, ConnectionID: cc.id, DeviceID: pv3.DeviceIDString(cc.device), PacketID: packet,
		Checksum: built.Checksum, ChecksumHex: fmt.Sprintf("0x%08x", built.Checksum), FrameBytes: pv3.OuterHeaderLen + len(built.Frame.Payload), PayloadBytes: built.PayloadBytes,
		Compressed: built.CompressedBytes, Uncompressed: built.UncompressedBytes, ImageName: a.meta.Name, ChecksumSrc: a.meta.ChecksumSource,
		UpdatePolicy: string(policy), Delivery: string(kind), DeliveryReason: reason, PartialPlan: plan,
	}
	if kind == delivery.PolicyPartial {
		sent.ChecksumSrc = "crc32-ieee-resultant-full-image-payload"
	}
	id := fmt.Sprintf("%s-%d-%d", s.runID, cc.id, packet)
	op := &operation{result: control.Update{ID: id, State: control.UpdateSending, Sent: sent}, asset: a, done: make(chan struct{}), written: make(chan struct{}), deadline: now.Add(s.cfg.ACKTimeout)}
	s.nextPacket++
	s.pending = op
	s.updates[id] = op
	s.order = append(s.order, id)
	if len(s.order) > retainedUpdates {
		delete(s.updates, s.order[0])
		s.order = s.order[1:]
	}
	meta := a.meta
	s.image = &meta
	s.display.PartialEligible = false
	s.display.EligibleConnectionID = 0
	s.display.EligibilityReason = "update_pending"
	s.display.Pending = &control.DisplayPending{At: now, Remote: cc.remote, ConnectionID: cc.id, PacketID: packet, Checksum: sent.Checksum, ChecksumHex: sent.ChecksumHex, ImageName: sent.ImageName, UpdatePolicy: sent.UpdatePolicy, Delivery: sent.Delivery}
	op.timer = time.AfterFunc(time.Until(op.deadline), func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.pending == op {
			s.failLocked(op, "ack_timeout")
			s.dropLocked(cc)
		}
	})
	s.wg.Add(1)
	go s.send(cc, op, built.Frame)
	return op, nil
}
func (s *Server) send(cc *session, op *operation, frame pv3.Frame) {
	defer s.wg.Done()
	defer close(op.written)
	start := time.Now()
	err := s.writeFrame(cc, frame, op.deadline)
	s.mu.Lock()
	op.result.Sent.WriteDuration = time.Since(start).String()
	sent := op.result.Sent
	if err != nil {
		s.failLocked(op, "image_write_failed: "+err.Error())
		s.dropLocked(cc)
	} else {
		op.writeOK = true
		if s.conn == cc {
			s.lastSent = &sent
		}
		if !op.result.Terminal() {
			op.result.State = control.UpdatePending
			if !time.Now().Before(op.deadline) {
				s.failLocked(op, "ack_timeout")
				s.dropLocked(cc)
			} else if op.earlyACK != nil {
				s.ackLocked(op, op.earlyACK)
			}
		}
	}
	s.mu.Unlock()
	eventType := telemetry.ImageSent
	message := ""
	if err != nil {
		eventType = telemetry.ImageFailed
		message = err.Error()
	}
	s.record(telemetry.Event{Type: eventType, At: sent.At, Remote: sent.Remote, ConnectionID: sent.ConnectionID, DeviceID: sent.DeviceID, Image: imageEvent(sent), Error: message})
}
func (s *Server) noteACK(cc *session, packet uint32, ctl pv3.ControlPayload) {
	s.mu.Lock()
	if s.conn != cc {
		s.mu.Unlock()
		return
	}
	op := s.pending
	matched := op != nil && op.result.Sent.PacketID == packet && op.result.Sent.ConnectionID == cc.id && ctl.Code == 1 && ctl.Arg == 0 && time.Now().Before(op.deadline)
	ack := &control.ImageACK{At: time.Now().UTC(), Remote: cc.remote, ConnectionID: cc.id, PacketID: packet, Code: ctl.Code, Arg: ctl.Arg, MatchedLast: matched}
	s.lastACK = ack
	if matched {
		if op.writeOK {
			s.ackLocked(op, ack)
		} else {
			op.earlyACK = ack
		}
	} else {
		s.invalidateLocked("unexpected_or_unsuccessful_image_ack")
		if op != nil {
			op.result.ACK = ack
			s.failLocked(op, "unexpected_or_unsuccessful_image_ack")
			s.dropLocked(cc)
		}
	}
	s.mu.Unlock()
	s.record(telemetry.Event{Type: telemetry.ImageACKReceived, At: ack.At, Remote: cc.remote, ConnectionID: cc.id, DeviceID: pv3.DeviceIDString(cc.device), ACK: &telemetry.ImageACKEvent{PacketID: packet, Code: ctl.Code, Arg: ctl.Arg, MatchedLast: matched}})
}
func (s *Server) ackLocked(op *operation, ack *control.ImageACK) {
	if op.result.Terminal() || s.pending != op {
		return
	}
	sent := op.result.Sent
	s.baseline = op.asset.gray
	s.display.LastAcked = &control.DisplayACKed{At: ack.At, Remote: sent.Remote, ConnectionID: sent.ConnectionID, DeviceID: sent.DeviceID, PacketID: sent.PacketID, Checksum: sent.Checksum, ChecksumHex: sent.ChecksumHex, ImageName: sent.ImageName, UpdatePolicy: sent.UpdatePolicy, Delivery: sent.Delivery, HasFramebuffer: true}
	s.display.Pending = nil
	s.display.PartialEligible = true
	s.display.EligibleConnectionID = sent.ConnectionID
	s.display.EligibilityReason = "acked_" + sent.Delivery + "_current_connection"
	op.result.ACK = ack
	op.result.State = control.UpdateACKed
	s.finishLocked(op)
}
func (s *Server) failLocked(op *operation, reason string) {
	if op.result.Terminal() {
		return
	}
	op.result.State = control.UpdateFailed
	op.result.Error = reason
	s.invalidateLocked(reason)
	s.finishLocked(op)
}
func (s *Server) finishLocked(op *operation) {
	if op.timer != nil {
		op.timer.Stop()
	}
	if s.pending == op {
		s.pending = nil
	}
	op.asset = nil
	s.logger.Printf("update=%s state=%s delivery=%s error=%q", op.result.ID, op.result.State, op.result.Sent.Delivery, op.result.Error)
	s.record(telemetry.Event{Type: "update_finished", Remote: op.result.Sent.Remote, ConnectionID: op.result.Sent.ConnectionID, DeviceID: op.result.Sent.DeviceID, Image: imageEvent(op.result.Sent), Error: op.result.Error})
	close(op.done)
}
func (s *Server) dropLocked(cc *session) {
	if s.conn == cc {
		s.conn = nil
	}
	cc.conn.Close()
}
func (s *Server) invalidateLocked(reason string) {
	now := time.Now().UTC()
	s.baseline = nil
	s.display.PartialEligible = false
	s.display.EligibleConnectionID = 0
	s.display.Pending = nil
	s.display.EligibilityReason = reason
	s.display.LastInvalidatedAt = &now
	s.display.LastInvalidationReason = reason
}
func (s *Server) noteStatus(cc *session, pkt pv3.ScreenPacket) bool {
	tuples := pv3.ParseStatusTuples(pkt.InnerPayload)
	fields := pv3.StatusTupleMap(tuples)
	status := &control.Status{At: time.Now().UTC(), Remote: cc.remote, DeviceID: pv3.DeviceIDString(cc.device), ASCII: pv3.DeviceIDASCII(cc.device), PacketID: pkt.PacketID}
	for id, v := range fields {
		status.Fields = append(status.Fields, control.StatusField{ID: id, Name: pv3.FieldName(id), Value: v, Signed: int32(v), Hex: fmt.Sprintf("0x%08x", v)})
	}
	sort.Slice(status.Fields, func(i, j int) bool { return status.Fields[i].ID < status.Fields[j].ID })
	s.mu.Lock()
	if s.conn != cc {
		s.mu.Unlock()
		return false
	}
	s.lastStatus = status
	if reason, ok := fields[0x00]; ok && reason != 3 {
		if s.pending != nil {
			s.failLocked(s.pending, "screen_boot_or_state_change")
			s.dropLocked(cc)
		}
		s.invalidateLocked("connect_reason_requires_full_baseline")
	} else if s.display.PartialEligible {
		crc, ok := fields[0x09]
		if !ok || s.display.LastAcked == nil || crc != s.display.LastAcked.Checksum {
			s.invalidateLocked("display_state_crc_missing_or_mismatched")
		}
	}
	s.mu.Unlock()
	s.record(telemetry.Event{Type: telemetry.StatusReceived, At: status.At, Remote: cc.remote, ConnectionID: cc.id, DeviceID: status.DeviceID, Status: telemetry.StatusFromTuples(pkt.PacketID, fields)})
	return true
}
func imageEvent(sent control.SentImage) *telemetry.ImageEvent {
	count := 0
	if sent.PartialPlan != nil {
		count = len(sent.PartialPlan.Strips)
	}
	d, _ := time.ParseDuration(sent.WriteDuration)
	return &telemetry.ImageEvent{PacketID: sent.PacketID, Checksum: sent.Checksum, ChecksumHex: sent.ChecksumHex, FrameBytes: sent.FrameBytes, PayloadBytes: sent.PayloadBytes, CompressedBytes: sent.Compressed, UncompressedBytes: sent.Uncompressed, ImageName: sent.ImageName, ChecksumSource: sent.ChecksumSrc, UpdatePolicy: sent.UpdatePolicy, Delivery: sent.Delivery, DeliveryReason: sent.DeliveryReason, PartialStripCount: count, PartialPlan: sent.PartialPlan, WriteDuration: sent.WriteDuration, WriteDurationMS: d.Milliseconds()}
}
