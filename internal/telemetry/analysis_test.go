package telemetry

import (
	"testing"
	"time"
)

func TestBuildBatteryTelemetryReportCorrelatesAcrossRuns(t *testing.T) {
	events := testBatteryTelemetryEvents()
	report := buildBatteryTelemetryReport("events.jsonl", events, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC))

	if report.Schema != batteryReportSchemaV1 || report.Runs != 2 || report.StatusSamples != 3 {
		t.Fatalf("report identity = %#v", report)
	}
	if report.Updates.Attempts != 3 || report.Updates.Sent != 2 || report.Updates.SendFailures != 1 || report.Updates.ACKEvents != 2 {
		t.Fatalf("update totals = %#v", report.Updates)
	}
	if report.Updates.MatchedACKs != 1 || report.Updates.NACKs != 1 || report.Updates.MissingACKs != 0 || report.Updates.CorrelatedNextStatus != 2 {
		t.Fatalf("update outcomes = %#v", report.Updates)
	}
	if report.Updates.FrameBytes != 170 || report.Updates.PartialStrips != 1 {
		t.Fatalf("update bytes/strips = %#v", report.Updates)
	}
	if report.Battery.Samples != 3 || report.Battery.PercentDelta == nil || *report.Battery.PercentDelta != -2 || report.Battery.VoltageDeltaMV == nil || *report.Battery.VoltageDeltaMV != -100 {
		t.Fatalf("battery summary = %#v", report.Battery)
	}
	if len(report.Observations) != 3 {
		t.Fatalf("observations = %d", len(report.Observations))
	}
	first := report.Observations[0]
	if !first.SendSucceeded || !first.ACKMatched || first.StatusBefore == nil || first.StatusAfter == nil || first.BatteryPercentDelta == nil || *first.BatteryPercentDelta != -1 {
		t.Fatalf("first observation = %#v", first)
	}
	second := report.Observations[1]
	if second.RunID != "run-two" || second.PacketID != 1 || second.ACKCode == nil || *second.ACKCode != 0 || second.StatusAfter == nil || second.PartialStripCount != 1 {
		t.Fatalf("second observation = %#v", second)
	}
	third := report.Observations[2]
	if third.SendSucceeded || third.SendError == "" || third.ACKCode != nil || third.StatusAfter != nil {
		t.Fatalf("failed observation = %#v", third)
	}
	if len(report.Deliveries) != 2 || report.Deliveries[0].Delivery != "full" || report.Deliveries[1].Delivery != "partial" {
		t.Fatalf("deliveries = %#v", report.Deliveries)
	}
}

func testBatteryTelemetryEvents() []indexedTelemetryEvent {
	base := time.Date(2026, 7, 9, 0, 0, 0, 0, time.UTC)
	deviceID := "00010203-0405-0607-0809-0a0b0c0d0e0f"
	status := func(at time.Time, runID string, connectionID uint64, battery, voltage uint32) Event {
		return Event{
			Schema:       SchemaV1,
			Type:         StatusReceived,
			At:           at,
			RunID:        runID,
			ConnectionID: connectionID,
			DeviceID:     deviceID,
			Status: &StatusEvent{
				PacketID:         1,
				BatteryPercent:   testTelemetryValue(battery),
				BatteryVoltageMV: testTelemetryValue(voltage),
				Charger:          testTelemetryValue(0),
			},
		}
	}
	image := func(at time.Time, eventType, runID string, connectionID uint64, packetID uint32, delivery string, frameBytes, strips int) Event {
		return Event{
			Schema:       SchemaV1,
			Type:         eventType,
			At:           at,
			RunID:        runID,
			ConnectionID: connectionID,
			DeviceID:     deviceID,
			Image: &ImageEvent{
				PacketID:          packetID,
				ChecksumHex:       "0x12345678",
				Delivery:          delivery,
				UpdatePolicy:      delivery,
				FrameBytes:        frameBytes,
				PartialStripCount: strips,
			},
		}
	}
	ack := func(at time.Time, runID string, connectionID uint64, packetID, code, arg uint32, matched bool) Event {
		return Event{
			Schema:       SchemaV1,
			Type:         ImageACKReceived,
			At:           at,
			RunID:        runID,
			ConnectionID: connectionID,
			DeviceID:     deviceID,
			ACK:          &ImageACKEvent{PacketID: packetID, Code: code, Arg: arg, MatchedLast: matched},
		}
	}
	events := []Event{
		{Schema: SchemaV1, Type: DaemonStarted, At: base, RunID: "run-one"},
		status(base.Add(time.Second), "run-one", 1, 90, 4100),
		image(base.Add(2*time.Second), ImageSent, "run-one", 1, 1, "full", 100, 0),
		ack(base.Add(3*time.Second), "run-one", 1, 1, 1, 0, true),
		status(base.Add(time.Hour), "run-one", 1, 89, 4050),
		{Schema: SchemaV1, Type: DaemonStarted, At: base.Add(time.Hour + time.Second), RunID: "run-two"},
		image(base.Add(time.Hour+2*time.Second), ImageSent, "run-two", 1, 1, "partial", 20, 1),
		ack(base.Add(time.Hour+3*time.Second), "run-two", 1, 1, 0, 4, false),
		status(base.Add(2*time.Hour), "run-two", 1, 88, 4000),
		image(base.Add(2*time.Hour+time.Second), ImageFailed, "run-two", 1, 2, "full", 50, 0),
	}
	events[len(events)-1].Error = "broken pipe"
	indexed := make([]indexedTelemetryEvent, len(events))
	for i := range events {
		indexed[i] = indexedTelemetryEvent{Line: i + 1, Event: events[i]}
	}
	return indexed
}

func testTelemetryValue(value uint32) *StatusValue {
	return &StatusValue{Value: value, Signed: int32(value)}
}
