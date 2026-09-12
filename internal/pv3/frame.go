package pv3

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
)

const (
	Version = uint32(3)

	DirectionScreenToServer = uint32(0)
	DirectionServerToScreen = uint32(1)

	OuterHeaderLen = 20

	ScreenClassControl = uint32(1)
	ScreenClassStatus  = uint32(3)
	ScreenClassParam   = uint32(8)
)

var le = binary.LittleEndian

// Frame is a parsed PV3 outer frame.
//
// The checksum field is crc32(payload) for server-originated frames observed in
// captures. Screen-originated frames use the same slot, but observed values do
// not always equal crc32(payload), so ValidateServerChecksum is intentionally scoped to
// frames we build/send.
type Frame struct {
	Version    uint32
	Field1     uint32
	Direction  uint32
	PayloadLen uint32
	Checksum   uint32
	Payload    []byte
}

func (f Frame) Bytes() []byte {
	out := make([]byte, OuterHeaderLen+len(f.Payload))
	le.PutUint32(out[0x00:], f.Version)
	le.PutUint32(out[0x04:], f.Field1)
	le.PutUint32(out[0x08:], f.Direction)
	le.PutUint32(out[0x0c:], uint32(len(f.Payload)))
	le.PutUint32(out[0x10:], f.Checksum)
	copy(out[OuterHeaderLen:], f.Payload)
	return out
}

func BuildFrame(direction uint32, payload []byte) Frame {
	cp := append([]byte(nil), payload...)
	return Frame{
		Version:    Version,
		Field1:     0,
		Direction:  direction,
		PayloadLen: uint32(len(cp)),
		Checksum:   crc32.ChecksumIEEE(cp),
		Payload:    cp,
	}
}

func ParseFrame(raw []byte) (Frame, error) {
	if len(raw) < OuterHeaderLen {
		return Frame{}, fmt.Errorf("pv3 frame too short: %d", len(raw))
	}
	f := Frame{
		Version:    le.Uint32(raw[0x00:]),
		Field1:     le.Uint32(raw[0x04:]),
		Direction:  le.Uint32(raw[0x08:]),
		PayloadLen: le.Uint32(raw[0x0c:]),
		Checksum:   le.Uint32(raw[0x10:]),
	}
	if uint64(f.PayloadLen) != uint64(len(raw)-OuterHeaderLen) {
		return Frame{}, fmt.Errorf("pv3 length mismatch: header payload=%d raw payload=%d", f.PayloadLen, len(raw)-OuterHeaderLen)
	}
	f.Payload = append([]byte(nil), raw[OuterHeaderLen:]...)
	return f, nil
}

func ReadFrame(r io.Reader, maxPayload uint32) (Frame, error) {
	header := make([]byte, OuterHeaderLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return Frame{}, err
	}
	if le.Uint32(header) != Version || le.Uint32(header[8:]) > DirectionServerToScreen {
		return Frame{}, fmt.Errorf("unsupported PV3 header")
	}
	payloadLen := le.Uint32(header[0x0c:])
	if payloadLen > maxPayload || uint64(payloadLen) > uint64(^uint(0)>>1) {
		return Frame{}, fmt.Errorf("pv3 payload too large: %d > %d", payloadLen, maxPayload)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Frame{}, err
	}
	return Frame{Version: Version, Field1: le.Uint32(header[4:]), Direction: le.Uint32(header[8:]), PayloadLen: payloadLen, Checksum: le.Uint32(header[16:]), Payload: payload}, nil
}

func WriteFrame(w io.Writer, frame Frame) error {
	b := frame.Bytes()
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}

func ValidateServerChecksum(frame Frame) error {
	want := crc32.ChecksumIEEE(frame.Payload)
	if frame.Checksum != want {
		return fmt.Errorf("pv3 checksum mismatch: got 0x%08x want 0x%08x", frame.Checksum, want)
	}
	return nil
}

// ScreenPacket is the common packet shape used by screen-originated PV3 status,
// control ACK, and param response frames.
type ScreenPacket struct {
	Field0       uint32
	DeviceID     [16]byte
	Class        uint32
	PacketID     uint32
	InnerLen     uint32
	Reserved     uint32
	InnerPayload []byte
}

func ParseScreenPacket(frame Frame) (ScreenPacket, error) {
	if frame.Direction != DirectionScreenToServer {
		return ScreenPacket{}, fmt.Errorf("not a screen->server frame: direction=%d", frame.Direction)
	}
	if len(frame.Payload) < 36 {
		return ScreenPacket{}, fmt.Errorf("screen packet payload too short: %d", len(frame.Payload))
	}
	var pkt ScreenPacket
	pkt.Field0 = le.Uint32(frame.Payload[0:])
	copy(pkt.DeviceID[:], frame.Payload[4:20])
	pkt.Class = le.Uint32(frame.Payload[20:])
	pkt.PacketID = le.Uint32(frame.Payload[24:])
	pkt.InnerLen = le.Uint32(frame.Payload[28:])
	pkt.Reserved = le.Uint32(frame.Payload[32:])
	if uint64(pkt.InnerLen) > uint64(len(frame.Payload)-36) {
		return ScreenPacket{}, fmt.Errorf("screen packet inner length %d exceeds payload remainder %d", pkt.InnerLen, len(frame.Payload)-36)
	}
	pkt.InnerPayload = append([]byte(nil), frame.Payload[36:36+int(pkt.InnerLen)]...)
	return pkt, nil
}

type ControlPayload struct {
	Code uint32
	Arg  uint32
}

func ParseControlPayload(inner []byte) (ControlPayload, error) {
	if len(inner) < 8 {
		return ControlPayload{}, fmt.Errorf("control payload too short: %d", len(inner))
	}
	return ControlPayload{Code: le.Uint32(inner[0:]), Arg: le.Uint32(inner[4:])}, nil
}

type StatusTuple struct {
	FieldID uint32
	Value   uint32
}

func ParseStatusTuples(inner []byte) []StatusTuple {
	out := make([]StatusTuple, 0, len(inner)/8)
	for off := 0; off+8 <= len(inner); off += 8 {
		fieldID := le.Uint32(inner[off:])
		value := le.Uint32(inner[off+4:])
		out = append(out, StatusTuple{FieldID: fieldID, Value: value})
		if fieldID == 0xffffffff {
			break
		}
	}
	return out
}

func StatusTupleMap(tuples []StatusTuple) map[uint32]uint32 {
	m := make(map[uint32]uint32, len(tuples))
	for _, t := range tuples {
		if t.FieldID == 0xffffffff {
			continue
		}
		m[t.FieldID] = t.Value
	}
	return m
}

func ClassName(class uint32) string {
	switch class {
	case ScreenClassControl:
		return "control-ack"
	case ScreenClassStatus:
		return "status"
	case ScreenClassParam:
		return "param"
	default:
		return fmt.Sprintf("class-%d", class)
	}
}

func DeviceIDString(deviceID [16]byte) string {
	return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		deviceID[0], deviceID[1], deviceID[2], deviceID[3],
		deviceID[4], deviceID[5],
		deviceID[6], deviceID[7],
		deviceID[8], deviceID[9],
		deviceID[10], deviceID[11], deviceID[12], deviceID[13], deviceID[14], deviceID[15])
}

func ParseDeviceIDString(s string) ([16]byte, error) {
	var out [16]byte
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 32 {
		return out, fmt.Errorf("device id must have 32 hex digits, got %d", len(s))
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return out, err
	}
	copy(out[:], b)
	return out, nil
}

func DeviceIDASCII(deviceID [16]byte) string {
	var b strings.Builder
	for _, c := range deviceID {
		if c >= 32 && c <= 126 {
			b.WriteByte(c)
		} else {
			b.WriteByte('.')
		}
	}
	return b.String()
}

// BuildServerControlACK builds the server->screen control ACK seen after idle
// status and param response packets. The packet ID acknowledges a
// screen-originated packet ID.
func BuildServerControlACK(deviceID [16]byte, packetID uint32) Frame {
	body := make([]byte, 68)
	// body[0:8] stays zero.
	le.PutUint32(body[8:], 44)
	le.PutUint32(body[12:], 44)
	le.PutUint32(body[16:], 1)
	// body[20:28] stays zero.
	copy(body[28:44], deviceID[:])
	le.PutUint32(body[44:], ScreenClassControl)
	le.PutUint32(body[48:], packetID)
	le.PutUint32(body[52:], 8)
	// body[56:60] reserved zero.
	le.PutUint32(body[60:], 1)
	// body[64:68] arg zero.
	return BuildFrame(DirectionServerToScreen, body)
}
