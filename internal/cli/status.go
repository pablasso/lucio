package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pablasso/lucio/internal/control"
	"github.com/pablasso/lucio/internal/pv3"
	"github.com/pablasso/lucio/internal/telemetry"
)

const (
	defaultControlServerURL = control.DefaultURL
	defaultControlTimeout   = 5 * time.Second
	cachedStatusNote        = "Values are cached from the latest screen-sent status packet observed by lucio; they are not live-polled on demand."
)

type lucioStatusReport struct {
	RunID            string                  `json:"run_id"`
	OK               bool                    `json:"ok"`
	Server           string                  `json:"server"`
	QueriedAt        time.Time               `json:"queried_at"`
	CachedStatusNote string                  `json:"cached_status_note"`
	Connected        bool                    `json:"connected"`
	HasDevice        bool                    `json:"has_device"`
	Remote           string                  `json:"remote,omitempty"`
	ConnectedAt      *time.Time              `json:"connected_at,omitempty"`
	ConnectionID     uint64                  `json:"connection_id,omitempty"`
	NextPacketID     uint32                  `json:"next_packet_id"`
	Device           *deviceStatusReport     `json:"device,omitempty"`
	Image            *control.Image          `json:"image,omitempty"`
	LastSent         *control.SentImage      `json:"last_sent,omitempty"`
	LastAck          *control.ImageACK       `json:"last_ack,omitempty"`
	DisplayState     control.DisplayState    `json:"display_state"`
	Telemetry        telemetry.RuntimeStatus `json:"telemetry"`
	RawStatus        *control.Status         `json:"raw_status,omitempty"`
}

type lucioDevicesReport struct {
	OK               bool                 `json:"ok"`
	Server           string               `json:"server"`
	QueriedAt        time.Time            `json:"queried_at"`
	CachedStatusNote string               `json:"cached_status_note"`
	MultiDevice      bool                 `json:"multi_device"`
	Devices          []deviceStatusReport `json:"devices"`
}

type deviceStatusReport struct {
	DeviceID          string                `json:"device_id,omitempty"`
	ASCII             string                `json:"ascii,omitempty"`
	Connected         bool                  `json:"connected"`
	Remote            string                `json:"remote,omitempty"`
	ConnectionID      uint64                `json:"connection_id,omitempty"`
	ConnectedAt       *time.Time            `json:"connected_at,omitempty"`
	StatusAt          *time.Time            `json:"status_at,omitempty"`
	StatusAgeSeconds  int64                 `json:"status_age_seconds,omitempty"`
	StatusPacketID    uint32                `json:"status_packet_id,omitempty"`
	ConnectReason     *decodedStatusValue   `json:"connect_reason,omitempty"`
	BatteryPercent    *decodedStatusValue   `json:"battery_percent,omitempty"`
	BatteryVoltageMV  *decodedStatusValue   `json:"battery_voltage_mv,omitempty"`
	RSSIRaw           *decodedStatusValue   `json:"rssi_raw,omitempty"`
	UptimeSeconds     *decodedStatusValue   `json:"uptime_seconds,omitempty"`
	Firmware          string                `json:"firmware,omitempty"`
	Bootloader        string                `json:"bootloader,omitempty"`
	DisplayStateCRC   *decodedStatusValue   `json:"display_state_crc,omitempty"`
	EPDTempSensorC    *decodedStatusValue   `json:"epd_temp_sensor_c,omitempty"`
	TemperatureC      *decodedStatusValue   `json:"temperature_c,omitempty"`
	InternalGeometry  *geometryReport       `json:"internal_geometry,omitempty"`
	PortraitGeometry  geometryReport        `json:"portrait_geometry"`
	ProtocolVersion   *decodedStatusValue   `json:"protocol_version,omitempty"`
	ImagePushAllowed  *bool                 `json:"image_push_allowed,omitempty"`
	NextStatusMinutes *decodedStatusValue   `json:"next_status_minutes,omitempty"`
	LastSent          *control.SentImage    `json:"last_sent,omitempty"`
	LastAck           *control.ImageACK     `json:"last_ack,omitempty"`
	RawFields         []control.StatusField `json:"raw_fields,omitempty"`
}

type decodedStatusValue struct {
	Value  uint32 `json:"value"`
	Signed int32  `json:"signed"`
	Hex    string `json:"hex"`
	Label  string `json:"label,omitempty"`
}

type geometryReport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

func runStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var serverURL string
	var jsonOut bool
	var rawFields bool
	var timeout time.Duration
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&serverURL, "server", defaultControlServerURL, "lucio daemon base URL")
	fs.BoolVar(&jsonOut, "json", false, "write machine-readable JSON")
	fs.BoolVar(&rawFields, "raw-fields", false, "include raw decoded status fields in human output")
	fs.DurationVar(&timeout, "timeout", defaultControlTimeout, "HTTP request timeout")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: lucio status [flags]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Show cached daemon and screen status from the running lucio service.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("status: unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if timeout <= 0 {
		return fmt.Errorf("status: --timeout must be positive")
	}

	health, err := fetchDaemonHealth(ctx, serverURL, timeout)
	if err != nil {
		return fmt.Errorf("status: %w", err)
	}
	report := makeLucioStatusReport(serverURL, health, time.Now())
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	return printStatusHuman(stdout, report, rawFields)
}

func runDevices(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var serverURL string
	var jsonOut bool
	var timeout time.Duration
	fs := flag.NewFlagSet("devices", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&serverURL, "server", defaultControlServerURL, "lucio daemon base URL")
	fs.BoolVar(&jsonOut, "json", false, "write machine-readable JSON")
	fs.DurationVar(&timeout, "timeout", defaultControlTimeout, "HTTP request timeout")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: lucio devices [flags]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "List devices known to the running lucio service.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("devices: unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if timeout <= 0 {
		return fmt.Errorf("devices: --timeout must be positive")
	}

	health, err := fetchDaemonHealth(ctx, serverURL, timeout)
	if err != nil {
		return fmt.Errorf("devices: %w", err)
	}
	report := lucioDevicesReport{
		OK:               health.OK,
		Server:           serverURL,
		QueriedAt:        time.Now(),
		CachedStatusNote: cachedStatusNote,
		MultiDevice:      false,
		Devices:          devicesFromHealth(health, time.Now()),
	}
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	return printDevicesHuman(stdout, report)
}

func runFields(args []string, stdout, stderr io.Writer) error {
	var jsonOut bool
	fs := flag.NewFlagSet("fields", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&jsonOut, "json", false, "write machine-readable JSON")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: lucio fields [flags]")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "List known PV3 screen status tuple fields decoded by lucio.")
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, "Flags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("fields: unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	fields := pv3.KnownFields()
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"fields": fields})
	}
	fmt.Fprintln(stdout, "Known PV3 status fields")
	fmt.Fprintln(stdout, "ID   HEX   NAME                                      CONFIDENCE")
	for _, f := range fields {
		fmt.Fprintf(stdout, "%-4d %-5s %-41s %s\n", f.ID, f.Hex, f.Name, f.Confidence)
	}
	return nil
}

func fetchDaemonHealth(ctx context.Context, serverURL string, timeout time.Duration) (*control.Snapshot, error) {
	client, err := control.NewClient(serverURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	health, err := client.Health(ctx)
	if err != nil {
		return nil, err
	}
	health.LastStatus = enrichStatusSnapshot(health.LastStatus)
	return health, nil
}

func makeLucioStatusReport(serverURL string, health *control.Snapshot, now time.Time) lucioStatusReport {
	connectedAt := timePtrIfNonZero(health.ConnectedAt)
	return lucioStatusReport{
		RunID:            health.RunID,
		OK:               health.OK,
		Server:           serverURL,
		QueriedAt:        now,
		CachedStatusNote: cachedStatusNote,
		Connected:        health.Connected,
		HasDevice:        health.HasDevice,
		Remote:           health.Remote,
		ConnectedAt:      connectedAt,
		ConnectionID:     health.ConnectionID,
		NextPacketID:     health.NextPacketID,
		Device:           deviceFromHealth(health, now),
		Image:            health.Image,
		LastSent:         health.LastSent,
		LastAck:          health.LastAck,
		DisplayState:     health.DisplayState,
		Telemetry:        health.Telemetry,
		RawStatus:        health.LastStatus,
	}
}

func devicesFromHealth(health *control.Snapshot, now time.Time) []deviceStatusReport {
	dev := deviceFromHealth(health, now)
	if dev == nil {
		return nil
	}
	return []deviceStatusReport{*dev}
}

func deviceFromHealth(health *control.Snapshot, now time.Time) *deviceStatusReport {
	if health == nil {
		return nil
	}
	status := health.LastStatus
	deviceID := health.DeviceID
	remote := health.Remote
	ascii := ""
	if status != nil {
		if deviceID == "" {
			deviceID = status.DeviceID
		}
		if remote == "" {
			remote = status.Remote
		}
		ascii = status.ASCII
	}
	if deviceID == "" && !health.HasDevice && status == nil {
		return nil
	}

	dev := &deviceStatusReport{
		DeviceID:         deviceID,
		ASCII:            ascii,
		Connected:        health.Connected,
		Remote:           remote,
		ConnectionID:     health.ConnectionID,
		ConnectedAt:      timePtrIfNonZero(health.ConnectedAt),
		PortraitGeometry: geometryReport{Width: pv3.PortraitWidth, Height: pv3.PortraitHeight},
		LastSent:         health.LastSent,
		LastAck:          health.LastAck,
	}
	if status == nil {
		return dev
	}

	dev.StatusAt = &status.At
	if !status.At.IsZero() {
		age := now.Sub(status.At)
		if age < 0 {
			age = 0
		}
		dev.StatusAgeSeconds = int64(age / time.Second)
	}
	dev.StatusPacketID = status.PacketID
	dev.ConnectReason = decodedField(status, 0x00)
	if dev.ConnectReason != nil {
		dev.ConnectReason.Label = connectReasonLabel(dev.ConnectReason.Value)
	}
	dev.BatteryPercent = decodedField(status, 0x0a)
	dev.BatteryVoltageMV = decodedField(status, 0x22)
	dev.RSSIRaw = decodedField(status, 0x0d)
	dev.UptimeSeconds = decodedField(status, 0x0f)
	dev.Firmware = versionString(status, 0x10, 0x11, 0x12)
	dev.Bootloader = versionString(status, 0x13, 0x14, 0x15)
	dev.DisplayStateCRC = decodedField(status, 0x09)
	dev.EPDTempSensorC = decodedField(status, 0x2d)
	dev.TemperatureC = decodedField(status, 0x0c)
	if w, okW := fieldByID(status, 0x26); okW {
		if h, okH := fieldByID(status, 0x27); okH {
			dev.InternalGeometry = &geometryReport{Width: int(w.Value), Height: int(h.Value)}
		}
	}
	dev.ProtocolVersion = decodedField(status, 0x28)
	if f, ok := fieldByID(status, 0x36); ok {
		allowed := f.Value == 1
		dev.ImagePushAllowed = &allowed
	}
	dev.NextStatusMinutes = decodedField(status, 0x1b)
	dev.RawFields = append([]control.StatusField(nil), status.Fields...)
	return dev
}

func printStatusHuman(w io.Writer, report lucioStatusReport, rawFields bool) error {
	fmt.Fprintf(w, "Lucio daemon: ok=%t connected=%t server=%s\n", report.OK, report.Connected, report.Server)
	if report.Telemetry.Enabled {
		fmt.Fprintf(w, "Telemetry: enabled schema=%s run_id=%s\n", report.Telemetry.Schema, report.Telemetry.RunID)
	} else {
		fmt.Fprintln(w, "Telemetry: disabled")
	}
	if report.Telemetry.Error != "" {
		fmt.Fprintf(w, "Event log ERROR: %s\n", report.Telemetry.Error)
	}
	fmt.Fprintf(w, "Instance: %s\n", report.RunID)
	fmt.Fprintf(w, "Note: %s\n", report.CachedStatusNote)
	if report.Connected {
		fmt.Fprintf(w, "Connection: remote=%s connection_id=%d", report.Remote, report.ConnectionID)
		if report.ConnectedAt != nil {
			fmt.Fprintf(w, " connected_at=%s", report.ConnectedAt.Format(time.RFC3339))
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "Connection: not connected")
	}

	dev := report.Device
	if dev == nil {
		fmt.Fprintln(w, "Device: none observed yet")
	} else {
		fmt.Fprintf(w, "Device: id=%s", valueOrUnknown(dev.DeviceID))
		if dev.ASCII != "" {
			fmt.Fprintf(w, " ascii=%q", dev.ASCII)
		}
		fmt.Fprintln(w)
		if dev.StatusAt != nil {
			fmt.Fprintf(w, "Latest status: packet=%d at=%s age=%s\n", dev.StatusPacketID, dev.StatusAt.Format(time.RFC3339), formatAge(time.Duration(dev.StatusAgeSeconds)*time.Second))
		} else {
			fmt.Fprintln(w, "Latest status: none observed yet")
		}
		fmt.Fprintf(w, "  connect_reason: %s\n", formatDecoded(dev.ConnectReason, ""))
		fmt.Fprintf(w, "  battery: %s", formatDecoded(dev.BatteryPercent, "%"))
		if dev.BatteryVoltageMV != nil {
			fmt.Fprintf(w, " (%s)", formatDecoded(dev.BatteryVoltageMV, " mV"))
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  rssi: %s raw\n", formatDecoded(dev.RSSIRaw, ""))
		fmt.Fprintf(w, "  uptime: %s\n", formatUptime(dev.UptimeSeconds))
		if dev.Firmware != "" {
			fmt.Fprintf(w, "  firmware: %s\n", dev.Firmware)
		}
		if dev.Bootloader != "" {
			fmt.Fprintf(w, "  bootloader: %s\n", dev.Bootloader)
		}
		fmt.Fprintf(w, "  display_state_crc: %s\n", formatHexDecoded(dev.DisplayStateCRC))
		fmt.Fprintf(w, "  epd_temp: %s\n", formatTemperature(dev.EPDTempSensorC))
		if dev.InternalGeometry != nil {
			fmt.Fprintf(w, "  display: internal %dx%d; portrait %dx%d\n", dev.InternalGeometry.Width, dev.InternalGeometry.Height, dev.PortraitGeometry.Width, dev.PortraitGeometry.Height)
		} else {
			fmt.Fprintf(w, "  display: portrait %dx%d\n", dev.PortraitGeometry.Width, dev.PortraitGeometry.Height)
		}
		if dev.ImagePushAllowed != nil {
			fmt.Fprintf(w, "  image_push_allowed: %t\n", *dev.ImagePushAllowed)
		}
		if dev.NextStatusMinutes != nil {
			fmt.Fprintf(w, "  next_status: %d min\n", dev.NextStatusMinutes.Value)
		}
	}

	if report.Image != nil {
		fmt.Fprintf(w, "Loaded image: name=%s checksum=%s payload=%d bytes\n", report.Image.Name, report.Image.ChecksumHex, report.Image.PayloadBytes)
	} else {
		fmt.Fprintln(w, "Loaded image: none")
	}
	if report.LastSent != nil {
		fmt.Fprintf(w, "Last sent: packet=%d checksum=%s image=%s delivery=%s requested=%s frame=%d bytes\n", report.LastSent.PacketID, report.LastSent.ChecksumHex, report.LastSent.ImageName, report.LastSent.Delivery, report.LastSent.UpdatePolicy, report.LastSent.FrameBytes)
	} else {
		fmt.Fprintln(w, "Last sent: none")
	}
	if report.LastAck != nil {
		fmt.Fprintf(w, "Last ACK: packet=%d code=%d arg=%d matched_last=%t at=%s\n", report.LastAck.PacketID, report.LastAck.Code, report.LastAck.Arg, report.LastAck.MatchedLast, report.LastAck.At.Format(time.RFC3339))
	} else {
		fmt.Fprintln(w, "Last ACK/NACK: none")
	}
	fmt.Fprintf(w, "Display state: partial_eligible=%t reason=%s\n", report.DisplayState.PartialEligible, valueOrUnknown(report.DisplayState.EligibilityReason))
	if rawFields && report.RawStatus != nil {
		fmt.Fprintln(w, "Raw status fields:")
		for _, f := range report.RawStatus.Fields {
			fmt.Fprintf(w, "  %-3d %-5s %-38s value=%d signed=%d\n", f.ID, f.Hex, f.Name, f.Value, f.Signed)
		}
	}
	return nil
}

func printDevicesHuman(w io.Writer, report lucioDevicesReport) error {
	fmt.Fprintf(w, "Lucio devices: count=%d server=%s\n", len(report.Devices), report.Server)
	fmt.Fprintf(w, "Note: %s\n", report.CachedStatusNote)
	if len(report.Devices) == 0 {
		fmt.Fprintln(w, "No devices observed yet.")
		return nil
	}
	fmt.Fprintln(w, "DEVICE_ID                              CONNECTED REMOTE                    STATUS_AGE BATTERY RSSI FW       DISPLAY_CRC LAST_ACK")
	for _, dev := range report.Devices {
		age := "-"
		if dev.StatusAt != nil {
			age = formatAge(time.Duration(dev.StatusAgeSeconds) * time.Second)
		}
		battery := "-"
		if dev.BatteryPercent != nil {
			battery = fmt.Sprintf("%d%%", dev.BatteryPercent.Value)
		}
		rssi := "-"
		if dev.RSSIRaw != nil {
			rssi = fmt.Sprintf("%d", dev.RSSIRaw.Value)
		}
		crc := "-"
		if dev.DisplayStateCRC != nil {
			crc = dev.DisplayStateCRC.Hex
		}
		lastACK := "-"
		if dev.LastAck != nil {
			lastACK = fmt.Sprintf("pkt=%d matched=%t", dev.LastAck.PacketID, dev.LastAck.MatchedLast)
		}
		fmt.Fprintf(w, "%-38s %-9t %-25s %-10s %-7s %-4s %-8s %-11s %s\n", valueOrUnknown(dev.DeviceID), dev.Connected, valueOrDash(dev.Remote), age, battery, rssi, valueOrDash(dev.Firmware), crc, lastACK)
	}
	return nil
}

func enrichStatusSnapshot(in *control.Status) *control.Status {
	if in == nil {
		return nil
	}
	out := *in
	out.Fields = append([]control.StatusField(nil), in.Fields...)
	for i := range out.Fields {
		out.Fields[i].Name = pv3.FieldName(out.Fields[i].ID)
		if out.Fields[i].Hex == "" {
			out.Fields[i].Hex = fmt.Sprintf("0x%08x", out.Fields[i].Value)
		}
	}
	return &out
}

func decodedField(status *control.Status, id uint32) *decodedStatusValue {
	f, ok := fieldByID(status, id)
	if !ok {
		return nil
	}
	return &decodedStatusValue{Value: f.Value, Signed: f.Signed, Hex: f.Hex}
}

func fieldByID(status *control.Status, id uint32) (control.StatusField, bool) {
	if status == nil {
		return control.StatusField{}, false
	}
	for _, f := range status.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return control.StatusField{}, false
}

func versionString(status *control.Status, majorID, minorID, revisionID uint32) string {
	major, ok1 := fieldByID(status, majorID)
	minor, ok2 := fieldByID(status, minorID)
	rev, ok3 := fieldByID(status, revisionID)
	if !ok1 || !ok2 || !ok3 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", major.Value, minor.Value, rev.Value)
}

func connectReasonLabel(v uint32) string {
	switch v {
	case 1:
		return "reboot"
	case 3:
		return "heartbeat"
	default:
		return "unknown"
	}
}

func formatDecoded(v *decodedStatusValue, unit string) string {
	if v == nil {
		return "unknown"
	}
	base := fmt.Sprintf("%d%s", v.Value, unit)
	if v.Label != "" {
		base += fmt.Sprintf(" (%s)", v.Label)
	}
	return base
}

func formatHexDecoded(v *decodedStatusValue) string {
	if v == nil {
		return "unknown"
	}
	return v.Hex
}

func formatTemperature(v *decodedStatusValue) string {
	if v == nil {
		return "unknown"
	}
	if v.Signed < 0 {
		return fmt.Sprintf("unavailable (%s)", v.Hex)
	}
	return fmt.Sprintf("%d C", v.Signed)
}

func formatUptime(v *decodedStatusValue) string {
	if v == nil {
		return "unknown"
	}
	d := time.Duration(v.Value) * time.Second
	return fmt.Sprintf("%d s (%s)", v.Value, formatAge(d))
}

func formatAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return "<1s"
	}
	return d.Truncate(time.Second).String()
}

func timePtrIfNonZero(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func valueOrUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
