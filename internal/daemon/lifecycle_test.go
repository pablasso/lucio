package daemon

import (
	"context"
	"image"
	"testing"
	"time"

	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/delivery"
	"github.com/pablasso/lucio/internal/pv3"
)

func TestReplacementFailsPendingAndIgnoresOldStatus(t *testing.T) {
	r := startRig(t, nil)
	c := r.screen(t)
	u := r.submit(t, canvas(), delivery.PolicyFull)
	readFrame(t, c)
	r.s.mu.Lock()
	old := r.s.conn
	r.s.mu.Unlock()
	next := r.screen(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := r.c.Wait(ctx, u.ID)
	if err == nil || result.Error != "connection_replaced" {
		t.Fatal(result, err)
	}
	frame := statusPacket(fakeID(), 0xffffffff)
	pkt, _ := pv3.ParseScreenPacket(frame)
	if r.s.noteStatus(old, pkt) {
		t.Fatal("accepted stale status")
	}
	h := r.health(t)
	if h.DisplayState.PartialEligible || h.ConnectionID == old.id {
		t.Fatal(h)
	}
	n := r.submit(t, canvas(), delivery.PolicyAuto)
	readFrame(t, next)
	ack(t, next, n.Sent.PacketID, 1)
	r.wait(t, n.ID)
}
func TestCloseStopsServing(t *testing.T) {
	r := startRig(t, nil)
	r.screen(t)
	if err := r.s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-r.done:
	case <-time.After(time.Second):
		t.Fatal("Close left listeners running")
	}
}
func TestImageDimensionPreflight(t *testing.T) {
	data := pngData(t, image.NewGray(image.Rect(0, 0, 1, 1)))
	if _, err := decodeImage("small.png", data); err == nil {
		t.Fatal("decoded unsupported dimensions")
	}
}
func TestStatusSnapshotsAreIndependent(t *testing.T) {
	r := startRig(t, nil)
	c := r.screen(t)
	h := r.health(t)
	h.LastStatus.Fields[0].Value = 999
	sendStatus(t, c, 123)
	latest := r.health(t)
	if latest.LastStatus.Fields[0].Value == 999 {
		t.Fatal("client mutated server state")
	}
	if h.LastStatus.Fields[1].Value == 123 {
		t.Fatal("snapshot changed after return")
	}
	if latest.Telemetry.Enabled {
		t.Fatal("telemetry enabled by default")
	}
}
func TestClientInstanceMismatch(t *testing.T) {
	r := startRig(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := r.c.Wait(ctx, "another-instance-1-1")
	if err == nil || result.State == control.UpdateACKed {
		t.Fatal("accepted unrelated daemon result")
	}
}
