package telemetry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	batteryReportSchemaV1      = "lucio.battery-report/v1"
	maxTelemetryEventLineBytes = 1024 * 1024
)

type Report struct {
	Schema        string                     `json:"schema"`
	Source        string                     `json:"source"`
	GeneratedAt   time.Time                  `json:"generated_at"`
	Window        batteryObservationWindow   `json:"window"`
	Runs          int                        `json:"runs"`
	EventCounts   map[string]int             `json:"event_counts"`
	StatusSamples int                        `json:"status_samples"`
	Updates       batteryUpdateSummary       `json:"updates"`
	Deliveries    []batteryDeliverySummary   `json:"deliveries"`
	Battery       batterySampleSummary       `json:"battery"`
	Observations  []batteryUpdateObservation `json:"observations"`
	Caveats       []string                   `json:"caveats"`
}

type batteryObservationWindow struct {
	FirstAt         *time.Time `json:"first_at,omitempty"`
	LastAt          *time.Time `json:"last_at,omitempty"`
	DurationSeconds float64    `json:"duration_seconds"`
}

type batteryUpdateSummary struct {
	Attempts             int   `json:"attempts"`
	Sent                 int   `json:"sent"`
	SendFailures         int   `json:"send_failures"`
	ACKEvents            int   `json:"ack_events"`
	MatchedACKs          int   `json:"matched_acks"`
	NACKs                int   `json:"nacks"`
	UnmatchedACKs        int   `json:"unmatched_acks"`
	MissingACKs          int   `json:"missing_acks"`
	CorrelatedNextStatus int   `json:"correlated_next_status"`
	FrameBytes           int64 `json:"frame_bytes"`
	PartialStrips        int   `json:"partial_strips"`
}

type batteryDeliverySummary struct {
	Delivery      string `json:"delivery"`
	Attempts      int    `json:"attempts"`
	Sent          int    `json:"sent"`
	SendFailures  int    `json:"send_failures"`
	MatchedACKs   int    `json:"matched_acks"`
	NACKs         int    `json:"nacks"`
	UnmatchedACKs int    `json:"unmatched_acks"`
	MissingACKs   int    `json:"missing_acks"`
	FrameBytes    int64  `json:"frame_bytes"`
	PartialStrips int    `json:"partial_strips"`
}

type batteryStatusPoint struct {
	At                time.Time `json:"at"`
	RunID             string    `json:"run_id"`
	DeviceID          string    `json:"device_id,omitempty"`
	BatteryPercent    *uint32   `json:"battery_percent,omitempty"`
	BatteryVoltageMV  *uint32   `json:"battery_voltage_mv,omitempty"`
	BatteryCurrentRaw *uint32   `json:"battery_current_raw,omitempty"`
	BatteryCurrent    *int32    `json:"battery_current_signed,omitempty"`
	Charger           *uint32   `json:"charger,omitempty"`
	UptimeSeconds     *uint32   `json:"uptime_seconds,omitempty"`
	TemperatureC      *int32    `json:"temperature_c,omitempty"`
	EPDTemperatureC   *int32    `json:"epd_temperature_c,omitempty"`
	DisplayStateCRC   *uint32   `json:"display_state_crc,omitempty"`
}

type batterySampleSummary struct {
	Samples               int                 `json:"samples"`
	First                 *batteryStatusPoint `json:"first,omitempty"`
	Last                  *batteryStatusPoint `json:"last,omitempty"`
	MinPercent            *uint32             `json:"min_percent,omitempty"`
	MaxPercent            *uint32             `json:"max_percent,omitempty"`
	PercentDelta          *int64              `json:"percent_delta,omitempty"`
	PercentPerDay         *float64            `json:"percent_per_day,omitempty"`
	MinVoltageMV          *uint32             `json:"min_voltage_mv,omitempty"`
	MaxVoltageMV          *uint32             `json:"max_voltage_mv,omitempty"`
	VoltageDeltaMV        *int64              `json:"voltage_delta_mv,omitempty"`
	VoltageMVPerDay       *float64            `json:"voltage_mv_per_day,omitempty"`
	ChargerSamples        int                 `json:"charger_samples"`
	ChargerNonzeroSamples int                 `json:"charger_nonzero_samples"`
}

type batteryUpdateObservation struct {
	RunID                      string              `json:"run_id"`
	DeviceID                   string              `json:"device_id,omitempty"`
	ConnectionID               uint64              `json:"connection_id"`
	PacketID                   uint32              `json:"packet_id"`
	ChecksumHex                string              `json:"checksum_hex,omitempty"`
	At                         time.Time           `json:"at"`
	SendSucceeded              bool                `json:"send_succeeded"`
	SendError                  string              `json:"send_error,omitempty"`
	Delivery                   string              `json:"delivery,omitempty"`
	UpdatePolicy               string              `json:"update_policy,omitempty"`
	FrameBytes                 int                 `json:"frame_bytes,omitempty"`
	PartialStripCount          int                 `json:"partial_strip_count"`
	SecondsSincePreviousUpdate *float64            `json:"seconds_since_previous_update,omitempty"`
	ACKAt                      *time.Time          `json:"ack_at,omitempty"`
	ACKCode                    *uint32             `json:"ack_code,omitempty"`
	ACKArg                     *uint32             `json:"ack_arg,omitempty"`
	ACKMatched                 bool                `json:"ack_matched"`
	StatusBefore               *batteryStatusPoint `json:"status_before,omitempty"`
	StatusAfter                *batteryStatusPoint `json:"status_after,omitempty"`
	SecondsToStatusAfter       *float64            `json:"seconds_to_status_after,omitempty"`
	BatteryPercentDelta        *int64              `json:"battery_percent_delta,omitempty"`
	BatteryVoltageDeltaMV      *int64              `json:"battery_voltage_delta_mv,omitempty"`
}

type indexedTelemetryEvent struct {
	Line  int
	Event Event
}

type telemetryEventKey struct {
	RunID        string
	ConnectionID uint64
	PacketID     uint32
}

func readTelemetryEvents(path string) ([]indexedTelemetryEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open telemetry events %s: %w", path, err)
	}
	defer f.Close()
	const maxBytes = 32 << 20
	if stat, err := f.Stat(); err != nil {
		return nil, err
	} else if stat.Size() > maxBytes {
		return nil, fmt.Errorf("analysis is limited to 32 MiB; select a smaller log window")
	}

	var events []indexedTelemetryEvent
	scanner := bufio.NewScanner(io.LimitReader(f, maxBytes+1))
	scanner.Buffer(make([]byte, 64*1024), maxTelemetryEventLineBytes)
	line, bytesRead := 0, 0
	for scanner.Scan() {
		line++
		bytesRead += len(scanner.Bytes()) + 1
		if bytesRead > maxBytes || line > 10000 {
			return nil, fmt.Errorf("analysis is limited to 32 MiB / 10000 lines; select a smaller log window")
		}
		body := strings.TrimSpace(scanner.Text())
		if body == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(body), &event); err != nil {
			return nil, fmt.Errorf("decode telemetry event line %d: %w", line, err)
		}
		if event.Schema != SchemaV1 {
			return nil, fmt.Errorf("telemetry event line %d has unsupported schema %q", line, event.Schema)
		}
		if event.Type == "" || event.RunID == "" || event.At.IsZero() {
			return nil, fmt.Errorf("telemetry event line %d is missing type, run_id, or at", line)
		}
		events = append(events, indexedTelemetryEvent{Line: line, Event: event})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read telemetry events %s: %w", path, err)
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Event.At.Before(events[j].Event.At)
	})
	return events, nil
}

func buildBatteryTelemetryReport(source string, events []indexedTelemetryEvent, now time.Time) Report {
	report := Report{
		Schema:      batteryReportSchemaV1,
		Source:      source,
		GeneratedAt: now.UTC(),
		EventCounts: make(map[string]int),
		Caveats: []string{
			"Battery telemetry is observational; charger state, temperature, reconnects, aging, and workload outside Lucio can affect readings.",
			"Battery percentage is coarse, and multiple updates may occur between status packets; per-update deltas are correlations, not causal energy measurements.",
			"BatteryCurrent units/sign and Charger value semantics are not yet confirmed, so reports preserve raw values without inferring amperes or charging state.",
		},
	}
	if len(events) > 0 {
		first := events[0].Event.At.UTC()
		last := events[len(events)-1].Event.At.UTC()
		report.Window.FirstAt = &first
		report.Window.LastAt = &last
		report.Window.DurationSeconds = last.Sub(first).Seconds()
	}

	runs := map[string]struct{}{}
	var statuses []indexedTelemetryEvent
	var updates []indexedTelemetryEvent
	acksByKey := map[telemetryEventKey][]indexedTelemetryEvent{}
	for _, indexed := range events {
		event := indexed.Event
		runs[event.RunID] = struct{}{}
		report.EventCounts[event.Type]++
		switch event.Type {
		case StatusReceived:
			report.StatusSamples++
			statuses = append(statuses, indexed)
		case ImageSent, ImageFailed:
			updates = append(updates, indexed)
		case ImageACKReceived:
			report.Updates.ACKEvents++
			if event.ACK != nil {
				key := telemetryEventKey{RunID: event.RunID, ConnectionID: event.ConnectionID, PacketID: event.ACK.PacketID}
				acksByKey[key] = append(acksByKey[key], indexed)
			}
		}
	}
	report.Runs = len(runs)

	points := batteryPointsFromStatuses(statuses)
	report.Battery = summarizeBatteryPoints(points)
	report.Observations = correlateBatteryUpdates(updates, statuses, acksByKey)
	report.Deliveries = summarizeBatteryUpdates(&report.Updates, report.Observations)
	return report
}

func batteryPointsFromStatuses(statuses []indexedTelemetryEvent) []batteryStatusPoint {
	points := make([]batteryStatusPoint, 0, len(statuses))
	for _, indexed := range statuses {
		if point := batteryPointFromStatusEvent(indexed.Event); point != nil {
			points = append(points, *point)
		}
	}
	return points
}

func batteryPointFromStatusEvent(event Event) *batteryStatusPoint {
	if event.Status == nil {
		return nil
	}
	status := event.Status
	return &batteryStatusPoint{
		At:                event.At.UTC(),
		RunID:             event.RunID,
		DeviceID:          event.DeviceID,
		BatteryPercent:    statusValueUint32(status.BatteryPercent),
		BatteryVoltageMV:  statusValueUint32(status.BatteryVoltageMV),
		BatteryCurrentRaw: statusValueUint32(status.BatteryCurrent),
		BatteryCurrent:    statusValueInt32(status.BatteryCurrent),
		Charger:           statusValueUint32(status.Charger),
		UptimeSeconds:     statusValueUint32(status.UptimeSeconds),
		TemperatureC:      statusValueInt32(status.TemperatureC),
		EPDTemperatureC:   statusValueInt32(status.EPDTemperatureC),
		DisplayStateCRC:   statusValueUint32(status.DisplayStateCRC),
	}
}

func statusValueUint32(value *StatusValue) *uint32 {
	if value == nil {
		return nil
	}
	out := value.Value
	return &out
}

func statusValueInt32(value *StatusValue) *int32 {
	if value == nil {
		return nil
	}
	out := value.Signed
	return &out
}

func summarizeBatteryPoints(points []batteryStatusPoint) batterySampleSummary {
	var summary batterySampleSummary
	for i := range points {
		point := points[i]
		if point.BatteryPercent == nil && point.BatteryVoltageMV == nil {
			continue
		}
		summary.Samples++
		if summary.First == nil {
			copy := point
			summary.First = &copy
		}
		copy := point
		summary.Last = &copy
		if point.BatteryPercent != nil {
			updateUint32Range(&summary.MinPercent, &summary.MaxPercent, *point.BatteryPercent)
		}
		if point.BatteryVoltageMV != nil {
			updateUint32Range(&summary.MinVoltageMV, &summary.MaxVoltageMV, *point.BatteryVoltageMV)
		}
		if point.Charger != nil {
			summary.ChargerSamples++
			if *point.Charger != 0 {
				summary.ChargerNonzeroSamples++
			}
		}
	}
	if summary.First == nil || summary.Last == nil {
		return summary
	}
	elapsedDays := summary.Last.At.Sub(summary.First.At).Hours() / 24
	if summary.First.BatteryPercent != nil && summary.Last.BatteryPercent != nil {
		delta := int64(*summary.Last.BatteryPercent) - int64(*summary.First.BatteryPercent)
		summary.PercentDelta = &delta
		if elapsedDays > 0 {
			rate := float64(delta) / elapsedDays
			summary.PercentPerDay = &rate
		}
	}
	if summary.First.BatteryVoltageMV != nil && summary.Last.BatteryVoltageMV != nil {
		delta := int64(*summary.Last.BatteryVoltageMV) - int64(*summary.First.BatteryVoltageMV)
		summary.VoltageDeltaMV = &delta
		if elapsedDays > 0 {
			rate := float64(delta) / elapsedDays
			summary.VoltageMVPerDay = &rate
		}
	}
	return summary
}

func updateUint32Range(min, max **uint32, value uint32) {
	if *min == nil || value < **min {
		copy := value
		*min = &copy
	}
	if *max == nil || value > **max {
		copy := value
		*max = &copy
	}
}

func correlateBatteryUpdates(updates, statuses []indexedTelemetryEvent, acksByKey map[telemetryEventKey][]indexedTelemetryEvent) []batteryUpdateObservation {
	observations := make([]batteryUpdateObservation, 0, len(updates))
	lastUpdateByDevice := map[string]time.Time{}
	statusByDevice := map[string][]indexedTelemetryEvent{}
	for _, indexed := range statuses {
		key := telemetryDeviceKey(indexed.Event)
		statusByDevice[key] = append(statusByDevice[key], indexed)
	}
	for _, indexed := range updates {
		event := indexed.Event
		observation := batteryUpdateObservation{
			RunID:         event.RunID,
			DeviceID:      event.DeviceID,
			ConnectionID:  event.ConnectionID,
			At:            event.At.UTC(),
			SendSucceeded: event.Type == ImageSent,
			SendError:     event.Error,
		}
		if event.Image != nil {
			observation.PacketID = event.Image.PacketID
			observation.ChecksumHex = event.Image.ChecksumHex
			observation.Delivery = event.Image.Delivery
			observation.UpdatePolicy = event.Image.UpdatePolicy
			observation.FrameBytes = event.Image.FrameBytes
			observation.PartialStripCount = event.Image.PartialStripCount
		}
		deviceKey := telemetryDeviceKey(event)
		if previous, ok := lastUpdateByDevice[deviceKey]; ok {
			seconds := event.At.Sub(previous).Seconds()
			observation.SecondsSincePreviousUpdate = &seconds
		}
		lastUpdateByDevice[deviceKey] = event.At

		threshold := event.At
		key := telemetryEventKey{RunID: event.RunID, ConnectionID: event.ConnectionID, PacketID: observation.PacketID}
		if ackEvent := firstACKAtOrAfter(acksByKey[key], event.At); ackEvent != nil && ackEvent.Event.ACK != nil {
			ackAt := ackEvent.Event.At.UTC()
			code := ackEvent.Event.ACK.Code
			arg := ackEvent.Event.ACK.Arg
			observation.ACKAt = &ackAt
			observation.ACKCode = &code
			observation.ACKArg = &arg
			observation.ACKMatched = ackEvent.Event.ACK.MatchedLast
			threshold = ackEvent.Event.At
		}

		before, after := surroundingStatusPoints(statusByDevice[deviceKey], event, threshold)
		observation.StatusBefore = before
		observation.StatusAfter = after
		if after != nil {
			seconds := after.At.Sub(threshold).Seconds()
			observation.SecondsToStatusAfter = &seconds
		}
		if before != nil && after != nil {
			if before.BatteryPercent != nil && after.BatteryPercent != nil {
				delta := int64(*after.BatteryPercent) - int64(*before.BatteryPercent)
				observation.BatteryPercentDelta = &delta
			}
			if before.BatteryVoltageMV != nil && after.BatteryVoltageMV != nil {
				delta := int64(*after.BatteryVoltageMV) - int64(*before.BatteryVoltageMV)
				observation.BatteryVoltageDeltaMV = &delta
			}
		}
		observations = append(observations, observation)
	}
	return observations
}

func telemetryDeviceKey(event Event) string {
	if event.DeviceID != "" {
		return "device:" + event.DeviceID
	}
	return fmt.Sprintf("run:%s/connection:%d", event.RunID, event.ConnectionID)
}

func firstACKAtOrAfter(events []indexedTelemetryEvent, at time.Time) *indexedTelemetryEvent {
	for i := range events {
		if !events[i].Event.At.Before(at) {
			return &events[i]
		}
	}
	return nil
}

func surroundingStatusPoints(statuses []indexedTelemetryEvent, update Event, afterThreshold time.Time) (*batteryStatusPoint, *batteryStatusPoint) {
	var before, after *batteryStatusPoint
	i := sort.Search(len(statuses), func(i int) bool { return statuses[i].Event.At.After(update.At) })
	for j := i - 1; j >= 0; j-- {
		if point := batteryPointFromStatusEvent(statuses[j].Event); point != nil {
			before = point
			break
		}
	}
	i = sort.Search(len(statuses), func(i int) bool { return statuses[i].Event.At.After(afterThreshold) })
	for ; i < len(statuses); i++ {
		if point := batteryPointFromStatusEvent(statuses[i].Event); point != nil {
			after = point
			break
		}
	}
	return before, after
}

func summarizeBatteryUpdates(summary *batteryUpdateSummary, observations []batteryUpdateObservation) []batteryDeliverySummary {
	byDelivery := map[string]*batteryDeliverySummary{}
	for i := range observations {
		observation := observations[i]
		delivery := observation.Delivery
		if delivery == "" {
			delivery = "unknown"
		}
		item := byDelivery[delivery]
		if item == nil {
			item = &batteryDeliverySummary{Delivery: delivery}
			byDelivery[delivery] = item
		}
		summary.Attempts++
		item.Attempts++
		summary.FrameBytes += int64(observation.FrameBytes)
		item.FrameBytes += int64(observation.FrameBytes)
		summary.PartialStrips += observation.PartialStripCount
		item.PartialStrips += observation.PartialStripCount
		if observation.SendSucceeded {
			summary.Sent++
			item.Sent++
		} else {
			summary.SendFailures++
			item.SendFailures++
		}
		if observation.StatusAfter != nil {
			summary.CorrelatedNextStatus++
		}
		if observation.ACKCode == nil {
			if observation.SendSucceeded {
				summary.MissingACKs++
				item.MissingACKs++
			}
			continue
		}
		if *observation.ACKCode != 1 || observation.ACKArg == nil || *observation.ACKArg != 0 {
			summary.NACKs++
			item.NACKs++
			continue
		}
		if observation.ACKMatched {
			summary.MatchedACKs++
			item.MatchedACKs++
		} else {
			summary.UnmatchedACKs++
			item.UnmatchedACKs++
		}
	}
	deliveries := make([]batteryDeliverySummary, 0, len(byDelivery))
	for _, item := range byDelivery {
		deliveries = append(deliveries, *item)
	}
	sort.Slice(deliveries, func(i, j int) bool { return deliveries[i].Delivery < deliveries[j].Delivery })
	return deliveries
}

func PrintReport(w io.Writer, report Report, details bool) error {
	fmt.Fprintf(w, "Lucio battery telemetry: source=%s\n", report.Source)
	if report.Window.FirstAt == nil || report.Window.LastAt == nil {
		fmt.Fprintln(w, "Window: no events")
	} else {
		fmt.Fprintf(w, "Window: %s to %s (%s) runs=%d\n", report.Window.FirstAt.Format(time.RFC3339), report.Window.LastAt.Format(time.RFC3339), time.Duration(report.Window.DurationSeconds*float64(time.Second)).Round(time.Second).String(), report.Runs)
	}
	fmt.Fprintf(w, "Events: status=%d image_sent=%d image_failed=%d image_ack=%d\n",
		report.EventCounts[StatusReceived], report.EventCounts[ImageSent], report.EventCounts[ImageFailed], report.EventCounts[ImageACKReceived])
	fmt.Fprintf(w, "Updates: attempts=%d sent=%d failures=%d matched_acks=%d nacks=%d unmatched_acks=%d missing_acks=%d correlated_status=%d frame_bytes=%d\n",
		report.Updates.Attempts, report.Updates.Sent, report.Updates.SendFailures, report.Updates.MatchedACKs, report.Updates.NACKs, report.Updates.UnmatchedACKs, report.Updates.MissingACKs, report.Updates.CorrelatedNextStatus, report.Updates.FrameBytes)

	battery := report.Battery
	if battery.Samples == 0 || battery.First == nil || battery.Last == nil {
		fmt.Fprintln(w, "Battery: no status samples with percentage or voltage")
	} else {
		fmt.Fprintf(w, "Battery: samples=%d first=%s last=%s", battery.Samples, formatBatteryPoint(battery.First), formatBatteryPoint(battery.Last))
		if battery.PercentDelta != nil {
			fmt.Fprintf(w, " percent_delta=%+d", *battery.PercentDelta)
		}
		if battery.VoltageDeltaMV != nil {
			fmt.Fprintf(w, " voltage_delta=%+d mV", *battery.VoltageDeltaMV)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Charger field: samples=%d nonzero=%d (state semantics unconfirmed)\n", battery.ChargerSamples, battery.ChargerNonzeroSamples)
	}

	if len(report.Deliveries) > 0 {
		fmt.Fprintln(w, "Delivery breakdown:")
		fmt.Fprintln(w, "  TYPE      ATTEMPTS SENT FAIL ACKED NACK MISSING BYTES STRIPS")
		for _, delivery := range report.Deliveries {
			fmt.Fprintf(w, "  %-9s %-8d %-4d %-4d %-5d %-4d %-7d %-5d %d\n", delivery.Delivery, delivery.Attempts, delivery.Sent, delivery.SendFailures, delivery.MatchedACKs, delivery.NACKs, delivery.MissingACKs, delivery.FrameBytes, delivery.PartialStrips)
		}
	}

	if details && len(report.Observations) > 0 {
		fmt.Fprintln(w, "Per-update correlations:")
		for _, observation := range report.Observations {
			ack := "missing"
			if observation.ACKCode != nil && observation.ACKArg != nil {
				ack = fmt.Sprintf("code=%d arg=%d matched=%t", *observation.ACKCode, *observation.ACKArg, observation.ACKMatched)
			}
			status := "none"
			if observation.StatusAfter != nil {
				status = formatBatteryPoint(observation.StatusAfter)
			}
			fmt.Fprintf(w, "  %s packet=%d delivery=%s bytes=%d strips=%d sent=%t ack=%s next_status=%s\n", observation.At.Format(time.RFC3339), observation.PacketID, observation.Delivery, observation.FrameBytes, observation.PartialStripCount, observation.SendSucceeded, ack, status)
		}
	}
	fmt.Fprintln(w, "Caveat: observational data does not by itself attribute battery drain to an individual update.")
	return nil
}

func formatBatteryPoint(point *batteryStatusPoint) string {
	if point == nil {
		return "unknown"
	}
	parts := []string{point.At.Format(time.RFC3339)}
	if point.BatteryPercent != nil {
		parts = append(parts, fmt.Sprintf("%d%%", *point.BatteryPercent))
	}
	if point.BatteryVoltageMV != nil {
		parts = append(parts, fmt.Sprintf("%dmV", *point.BatteryVoltageMV))
	}
	return strings.Join(parts, "/")
}

// Analyze reads a single bounded JSONL log. Rotated files can be analyzed individually.
func Analyze(path string) (Report, error) {
	events, err := readTelemetryEvents(path)
	if err != nil {
		return Report{}, err
	}
	return buildBatteryTelemetryReport(path, events, time.Now().UTC()), nil
}
