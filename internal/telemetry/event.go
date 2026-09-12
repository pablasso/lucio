package telemetry

import (
	"fmt"
	"github.com/pablasso/lucio/internal/delivery"
	"time"
)

const SchemaV1 = "lucio.telemetry/v1"

const (
	DaemonStarted    = "daemon_started"
	ConnectionOpen   = "connection_opened"
	ConnectionClose  = "connection_closed"
	StatusReceived   = "status_received"
	ImageSent        = "image_sent"
	ImageFailed      = "image_send_failed"
	ImageACKReceived = "image_ack_received"
)

type Recorder interface {
	Record(Event) error
	Close() error
}

type Event struct {
	Schema       string         `json:"schema"`
	Type         string         `json:"type"`
	At           time.Time      `json:"at"`
	RunID        string         `json:"run_id"`
	Remote       string         `json:"remote,omitempty"`
	ConnectionID uint64         `json:"connection_id,omitempty"`
	DeviceID     string         `json:"device_id,omitempty"`
	Daemon       *DaemonEvent   `json:"daemon,omitempty"`
	Status       *StatusEvent   `json:"status,omitempty"`
	Image        *ImageEvent    `json:"image,omitempty"`
	ACK          *ImageACKEvent `json:"ack,omitempty"`
	Error        string         `json:"error,omitempty"`
}

type DaemonEvent struct {
	Version       string `json:"version"`
	GatewayListen string `json:"gateway_listen"`
	HTTPListen    string `json:"http_listen"`
	PID           int    `json:"pid"`
}

type StatusValue struct {
	Value  uint32 `json:"value"`
	Signed int32  `json:"signed"`
	Hex    string `json:"hex"`
}

type StatusEvent struct {
	PacketID          uint32       `json:"packet_id"`
	ConnectReason     *StatusValue `json:"connect_reason,omitempty"`
	DisplayStateCRC   *StatusValue `json:"display_state_crc,omitempty"`
	BatteryPercent    *StatusValue `json:"battery_percent,omitempty"`
	Charger           *StatusValue `json:"charger,omitempty"`
	BatteryCurrent    *StatusValue `json:"battery_current,omitempty"`
	BatteryVoltageMV  *StatusValue `json:"battery_voltage_mv,omitempty"`
	RSSIRaw           *StatusValue `json:"rssi_raw,omitempty"`
	UptimeSeconds     *StatusValue `json:"uptime_seconds,omitempty"`
	TemperatureC      *StatusValue `json:"temperature_c,omitempty"`
	EPDTemperatureC   *StatusValue `json:"epd_temperature_c,omitempty"`
	NextStatusMinutes *StatusValue `json:"next_status_minutes,omitempty"`
	ImagePushAllowed  *StatusValue `json:"image_push_allowed,omitempty"`
}

type ImageEvent struct {
	PacketID          uint32                    `json:"packet_id"`
	Checksum          uint32                    `json:"checksum"`
	ChecksumHex       string                    `json:"checksum_hex"`
	FrameBytes        int                       `json:"frame_bytes"`
	PayloadBytes      int                       `json:"payload_bytes"`
	CompressedBytes   int                       `json:"compressed_bytes"`
	UncompressedBytes int                       `json:"uncompressed_bytes"`
	ImageName         string                    `json:"image_name"`
	ChecksumSource    string                    `json:"checksum_source"`
	UpdatePolicy      string                    `json:"update_policy"`
	Delivery          string                    `json:"delivery"`
	DeliveryReason    string                    `json:"delivery_reason,omitempty"`
	PartialStripCount int                       `json:"partial_strip_count"`
	PartialPlan       *delivery.SafePartialPlan `json:"partial_plan,omitempty"`
	WriteDuration     string                    `json:"write_duration,omitempty"`
	WriteDurationMS   int64                     `json:"write_duration_ms,omitempty"`
}

type ImageACKEvent struct {
	PacketID    uint32 `json:"packet_id"`
	Code        uint32 `json:"code"`
	Arg         uint32 `json:"arg"`
	MatchedLast bool   `json:"matched_last"`
}

type RuntimeStatus struct {
	Error   string `json:"error,omitempty"`
	Enabled bool   `json:"enabled"`
	Schema  string `json:"schema,omitempty"`
	RunID   string `json:"run_id,omitempty"`
}

func StatusFromTuples(packetID uint32, fields map[uint32]uint32) *StatusEvent {
	return &StatusEvent{
		PacketID:          packetID,
		ConnectReason:     statusField(fields, 0x00),
		DisplayStateCRC:   statusField(fields, 0x09),
		BatteryPercent:    statusField(fields, 0x0a),
		Charger:           statusField(fields, 0x0b),
		TemperatureC:      statusField(fields, 0x0c),
		RSSIRaw:           statusField(fields, 0x0d),
		UptimeSeconds:     statusField(fields, 0x0f),
		NextStatusMinutes: statusField(fields, 0x1b),
		BatteryCurrent:    statusField(fields, 0x20),
		BatteryVoltageMV:  statusField(fields, 0x22),
		EPDTemperatureC:   statusField(fields, 0x2d),
		ImagePushAllowed:  statusField(fields, 0x36),
	}
}

func statusField(fields map[uint32]uint32, id uint32) *StatusValue {
	value, ok := fields[id]
	if !ok {
		return nil
	}
	return &StatusValue{Value: value, Signed: int32(value), Hex: fmt.Sprintf("0x%08x", value)}
}
