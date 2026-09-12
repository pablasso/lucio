package cli

import (
	"bytes"
	"encoding/json"
	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/pv3"
	"github.com/pablasso/lucio/internal/telemetry"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStatusCommandHuman(t *testing.T) {
	ts := newStatusTestServer(t)
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	if err := run([]string{"status", "--server", ts.URL}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{
		"Lucio daemon: ok=true connected=true",
		"Telemetry: enabled schema=lucio.telemetry/v1 run_id=test-run",
		"cached from the latest screen-sent status",
		"Device: id=00010203-0405-0607-0809-0a0b0c0d0e0f",
		"battery: 85% (3978 mV)",
		"firmware: 6.9.3947",
		"display_state_crc: 0xea0c7313",
		"display: internal 2880x640; portrait 1440x2560",
		"Last sent: packet=7 checksum=0xea0c7313 image=today.png delivery=full requested=full",
		"Last ACK: packet=7 code=1 arg=0 matched_last=true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Raw status fields:") {
		t.Fatalf("status output included raw fields without --raw-fields:\n%s", out)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestStatusCommandRawFieldsAndJSON(t *testing.T) {
	ts := newStatusTestServer(t)
	defer ts.Close()

	var rawOut, rawErr bytes.Buffer
	if err := run([]string{"status", "--server", ts.URL, "--raw-fields"}, &rawOut, &rawErr); err != nil {
		t.Fatal(err)
	}
	if out := rawOut.String(); !strings.Contains(out, "Raw status fields:") || !strings.Contains(out, "Battery") {
		t.Fatalf("raw-fields output missing decoded field names:\n%s", out)
	}

	var jsonOut, jsonErr bytes.Buffer
	if err := run([]string{"status", "--server", ts.URL, "--json"}, &jsonOut, &jsonErr); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Connected bool `json:"connected"`
		Device    struct {
			Firmware        string                 `json:"firmware"`
			BatteryPercent  struct{ Value uint32 } `json:"battery_percent"`
			DisplayStateCRC struct{ Hex string }   `json:"display_state_crc"`
		} `json:"device"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil {
		t.Fatalf("decode status json: %v\n%s", err, jsonOut.String())
	}
	if !decoded.Connected || decoded.Device.Firmware != "6.9.3947" || decoded.Device.BatteryPercent.Value != 85 || decoded.Device.DisplayStateCRC.Hex != "0xea0c7313" {
		t.Fatalf("unexpected status json: %+v", decoded)
	}
}

func TestDevicesCommandHumanAndJSON(t *testing.T) {
	ts := newStatusTestServer(t)
	defer ts.Close()

	var stdout, stderr bytes.Buffer
	if err := run([]string{"devices", "--server", ts.URL}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	out := stdout.String()
	for _, want := range []string{"Lucio devices: count=1", "00010203-0405-0607-0809-0a0b0c0d0e0f", "85%", "6.9.3947", "pkt=7 matched=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("devices output missing %q:\n%s", want, out)
		}
	}

	var jsonOut, jsonErr bytes.Buffer
	if err := run([]string{"devices", "--server", ts.URL, "--json"}, &jsonOut, &jsonErr); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		MultiDevice bool `json:"multi_device"`
		Devices     []struct {
			DeviceID string `json:"device_id"`
			Firmware string `json:"firmware"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil {
		t.Fatalf("decode devices json: %v\n%s", err, jsonOut.String())
	}
	if decoded.MultiDevice || len(decoded.Devices) != 1 || decoded.Devices[0].Firmware != "6.9.3947" {
		t.Fatalf("unexpected devices json: %+v", decoded)
	}
}

func TestFieldsCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"fields"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if out := stdout.String(); !strings.Contains(out, "Known PV3 status fields") || !strings.Contains(out, "Battery") || !strings.Contains(out, "DisplayStateCRC") {
		t.Fatalf("fields output missing expected entries:\n%s", out)
	}

	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"fields", "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Fields []pv3.FieldInfo `json:"fields"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode fields json: %v\n%s", err, stdout.String())
	}
	if len(decoded.Fields) == 0 || decoded.Fields[0].Name != "ConnectReason" {
		t.Fatalf("unexpected fields json: %+v", decoded)
	}
}

func newStatusTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	statusAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	connectedAt := statusAt.Add(-10 * time.Second)
	ackAt := statusAt.Add(5 * time.Second)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, control.Snapshot{RunID: "synthetic-instance",
			OK:           true,
			Connected:    true,
			HasDevice:    true,
			ImageLoaded:  true,
			NextPacketID: 8,
			Remote:       "192.0.2.20:50945",
			ConnectedAt:  connectedAt,
			ConnectionID: 1,
			DeviceID:     "00010203-0405-0607-0809-0a0b0c0d0e0f",
			LastStatus: &control.Status{
				At:       statusAt,
				Remote:   "192.0.2.20:50945",
				DeviceID: "00010203-0405-0607-0809-0a0b0c0d0e0f",
				ASCII:    "synthetic-device",
				PacketID: 4,
				Fields: []control.StatusField{
					{ID: 0x00, Name: "field_0", Value: 3, Signed: 3, Hex: "0x00000003"},
					{ID: 0x09, Name: "field_9", Value: 0xea0c7313, Signed: -368282861, Hex: "0xea0c7313"},
					{ID: 0x0a, Name: "field_10", Value: 85, Signed: 85, Hex: "0x00000055"},
					{ID: 0x0d, Name: "field_13", Value: 53, Signed: 53, Hex: "0x00000035"},
					{ID: 0x0f, Name: "field_15", Value: 744, Signed: 744, Hex: "0x000002e8"},
					{ID: 0x10, Name: "field_16", Value: 6, Signed: 6, Hex: "0x00000006"},
					{ID: 0x11, Name: "field_17", Value: 9, Signed: 9, Hex: "0x00000009"},
					{ID: 0x12, Name: "field_18", Value: 3947, Signed: 3947, Hex: "0x00000f6b"},
					{ID: 0x13, Name: "field_19", Value: 6, Signed: 6, Hex: "0x00000006"},
					{ID: 0x14, Name: "field_20", Value: 9, Signed: 9, Hex: "0x00000009"},
					{ID: 0x15, Name: "field_21", Value: 3947, Signed: 3947, Hex: "0x00000f6b"},
					{ID: 0x1b, Name: "field_27", Value: 60, Signed: 60, Hex: "0x0000003c"},
					{ID: 0x22, Name: "field_34", Value: 3978, Signed: 3978, Hex: "0x00000f8a"},
					{ID: 0x26, Name: "field_38", Value: 2880, Signed: 2880, Hex: "0x00000b40"},
					{ID: 0x27, Name: "field_39", Value: 640, Signed: 640, Hex: "0x00000280"},
					{ID: 0x28, Name: "field_40", Value: 3, Signed: 3, Hex: "0x00000003"},
					{ID: 0x2d, Name: "field_45", Value: 25, Signed: 25, Hex: "0x00000019"},
					{ID: 0x36, Name: "field_54", Value: 1, Signed: 1, Hex: "0x00000001"},
				},
			},
			Image:        &control.Image{Name: "today.png", Format: "png", LoadedAt: statusAt, Width: 1440, Height: 2560, PayloadBytes: 1843248, Checksum: 0xea0c7313, ChecksumHex: "0xea0c7313", ChecksumSource: "crc32-ieee-image-payload"},
			LastSent:     &control.SentImage{At: statusAt, Remote: "192.0.2.20:50945", ConnectionID: 1, DeviceID: "00010203-0405-0607-0809-0a0b0c0d0e0f", PacketID: 7, Checksum: 0xea0c7313, ChecksumHex: "0xea0c7313", FrameBytes: 1234, PayloadBytes: 1843248, ImageName: "today.png", UpdatePolicy: "full", Delivery: "full", DeliveryReason: "forced_full"},
			LastAck:      &control.ImageACK{At: ackAt, Remote: "192.0.2.20:50945", ConnectionID: 1, PacketID: 7, Code: 1, Arg: 0, MatchedLast: true},
			DisplayState: control.DisplayState{PartialEligible: true, EligibilityReason: "acked_full_frame_current_connection", EligibleConnectionID: 1},
			Telemetry:    telemetry.RuntimeStatus{Enabled: true, Schema: telemetry.SchemaV1, RunID: "test-run"},
		})
	}))
}
