package control

import (
	"github.com/pablasso/lucio/internal/delivery"
	"time"
)

type Image struct {
	Name           string    `json:"name"`
	Format         string    `json:"format"`
	LoadedAt       time.Time `json:"loaded_at"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	PayloadBytes   int       `json:"payload_bytes"`
	Checksum       uint32    `json:"checksum"`
	ChecksumHex    string    `json:"checksum_hex"`
	ChecksumSource string    `json:"checksum_source"`
	SavedPath      string    `json:"saved_path,omitempty"`
}

type StatusField struct {
	ID     uint32 `json:"id"`
	Name   string `json:"name"`
	Value  uint32 `json:"value"`
	Signed int32  `json:"signed"`
	Hex    string `json:"hex"`
}

type Status struct {
	At       time.Time     `json:"at"`
	Remote   string        `json:"remote"`
	DeviceID string        `json:"device_id"`
	ASCII    string        `json:"ascii"`
	PacketID uint32        `json:"packet_id"`
	Fields   []StatusField `json:"fields"`
}

type SentImage struct {
	At             time.Time                 `json:"at"`
	Remote         string                    `json:"remote"`
	ConnectionID   uint64                    `json:"connection_id"`
	DeviceID       string                    `json:"device_id"`
	PacketID       uint32                    `json:"packet_id"`
	Checksum       uint32                    `json:"checksum"`
	ChecksumHex    string                    `json:"checksum_hex"`
	FrameBytes     int                       `json:"frame_bytes"`
	PayloadBytes   int                       `json:"payload_bytes"`
	Compressed     int                       `json:"compressed_bytes"`
	Uncompressed   int                       `json:"uncompressed_bytes"`
	ImageName      string                    `json:"image_name"`
	ChecksumSrc    string                    `json:"checksum_source"`
	UpdatePolicy   string                    `json:"update_policy"`
	Delivery       string                    `json:"delivery"`
	DeliveryReason string                    `json:"delivery_reason,omitempty"`
	PartialPlan    *delivery.SafePartialPlan `json:"partial_plan,omitempty"`
	WriteDuration  string                    `json:"write_duration"`
}

type ImageACK struct {
	At           time.Time `json:"at"`
	Remote       string    `json:"remote"`
	ConnectionID uint64    `json:"connection_id"`
	PacketID     uint32    `json:"packet_id"`
	Code         uint32    `json:"code"`
	Arg          uint32    `json:"arg"`
	MatchedLast  bool      `json:"matched_last"`
}

type DisplayState struct {
	PartialEligible        bool            `json:"partial_eligible"`
	EligibilityReason      string          `json:"eligibility_reason,omitempty"`
	EligibleConnectionID   uint64          `json:"eligible_connection_id,omitempty"`
	LastInvalidatedAt      *time.Time      `json:"last_invalidated_at,omitempty"`
	LastInvalidationReason string          `json:"last_invalidation_reason,omitempty"`
	LastAcked              *DisplayACKed   `json:"last_acked,omitempty"`
	Pending                *DisplayPending `json:"pending,omitempty"`
}

type DisplayACKed struct {
	At             time.Time `json:"at"`
	Remote         string    `json:"remote"`
	ConnectionID   uint64    `json:"connection_id"`
	DeviceID       string    `json:"device_id"`
	PacketID       uint32    `json:"packet_id"`
	Checksum       uint32    `json:"checksum"`
	ChecksumHex    string    `json:"checksum_hex"`
	ImageName      string    `json:"image_name"`
	UpdatePolicy   string    `json:"update_policy"`
	Delivery       string    `json:"delivery"`
	HasFramebuffer bool      `json:"has_framebuffer"`
}

type DisplayPending struct {
	At           time.Time `json:"at"`
	Remote       string    `json:"remote"`
	ConnectionID uint64    `json:"connection_id"`
	PacketID     uint32    `json:"packet_id"`
	Checksum     uint32    `json:"checksum"`
	ChecksumHex  string    `json:"checksum_hex"`
	ImageName    string    `json:"image_name"`
	UpdatePolicy string    `json:"update_policy"`
	Delivery     string    `json:"delivery"`
}
