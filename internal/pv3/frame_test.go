package pv3

import (
	"bytes"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
)

func testDeviceID() (id [16]byte) {
	for i := range id {
		id[i] = byte(i)
	}
	return
}

func TestServerACKVector(t *testing.T) {
	b, err := os.ReadFile("testdata/status-ack.hex")
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString(strings.Join(strings.Fields(string(b)), ""))
	if err != nil {
		t.Fatal(err)
	}
	got := BuildServerControlACK(testDeviceID(), 18)
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("ACK mismatch: %x", got.Bytes())
	}
	if err := ValidateServerChecksum(got); err != nil {
		t.Fatal(err)
	}
}

func TestScreenACK(t *testing.T) {
	b := make([]byte, 44)
	id := testDeviceID()
	copy(b[4:20], id[:])
	le.PutUint32(b[20:], 1)
	le.PutUint32(b[24:], 7)
	le.PutUint32(b[28:], 8)
	le.PutUint32(b[36:], 1)
	pkt, err := ParseScreenPacket(BuildFrame(0, b))
	if err != nil {
		t.Fatal(err)
	}
	ctl, err := ParseControlPayload(pkt.InnerPayload)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.PacketID != 7 || pkt.DeviceID != id || ctl.Code != 1 || ctl.Arg != 0 {
		t.Fatalf("packet=%+v control=%+v", pkt, ctl)
	}
	parsed, err := ParseDeviceIDString(DeviceIDString(id))
	if err != nil || parsed != id {
		t.Fatal(parsed, err)
	}
}

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(b []byte) (int, error) { return w.Buffer.Write(b[:min(3, len(b))]) }

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }
func TestWriteFrameHandlesShortWrites(t *testing.T) {
	f := BuildFrame(1, []byte("hello"))
	var w shortWriter
	if err := WriteFrame(&w, f); err != nil || !bytes.Equal(w.Bytes(), f.Bytes()) {
		t.Fatal(err)
	}
	if err := WriteFrame(zeroWriter{}, f); err != io.ErrShortWrite {
		t.Fatal(err)
	}
}
func TestReadFrameLimits(t *testing.T) {
	f := BuildFrame(0, make([]byte, 44))
	if _, err := ReadFrame(bytes.NewReader(f.Bytes()), 43); err == nil {
		t.Fatal("accepted oversized frame")
	}
	f.Version = 0
	if _, err := ReadFrame(bytes.NewReader(f.Bytes()), 44); err == nil {
		t.Fatal("accepted other protocol")
	}
}
func FuzzFrame(f *testing.F) {
	f.Add(BuildServerControlACK(testDeviceID(), 1).Bytes())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			return
		}
		frame, err := ParseFrame(b)
		if err == nil {
			if !bytes.Equal(frame.Bytes(), b) {
				t.Fatal("frame round trip")
			}
			pkt, err := ParseScreenPacket(frame)
			if err == nil {
				ParseControlPayload(pkt.InnerPayload)
				ParseStatusTuples(pkt.InnerPayload)
			}
		}
		ReadFrame(bytes.NewReader(b), 1<<20)
	})
}
