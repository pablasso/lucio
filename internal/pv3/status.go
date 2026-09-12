package pv3

import (
	"fmt"
	"sort"
)

type FieldInfo struct {
	ID         uint32 `json:"id"`
	Hex        string `json:"hex"`
	Name       string `json:"name"`
	Confidence string `json:"confidence"`
	Notes      string `json:"notes,omitempty"`
}

var knownStatusFields = []FieldInfo{
	{ID: 0x00, Name: "ConnectReason", Confidence: "high", Notes: "Known values include 1=reboot and 3=heartbeat."},
	{ID: 0x01, Name: "ErrorCode", Confidence: "medium"},
	{ID: 0x02, Name: "MobileModuleId_or_MobileLinkType", Confidence: "low"},
	{ID: 0x03, Name: "ApplicationVersion", Confidence: "high"},
	{ID: 0x04, Name: "WiFiModuleId", Confidence: "medium"},
	{ID: 0x05, Name: "MobileModuleId_or_MobileLinkType", Confidence: "low"},
	{ID: 0x06, Name: "unknown_6", Confidence: "low"},
	{ID: 0x07, Name: "MobSecurity", Confidence: "medium"},
	{ID: 0x08, Name: "DisplayIds", Confidence: "high"},
	{ID: 0x09, Name: "DisplayStateCRC", Confidence: "high"},
	{ID: 0x0a, Name: "Battery", Confidence: "high", Notes: "Observed as battery percentage."},
	{ID: 0x0b, Name: "Charger", Confidence: "high"},
	{ID: 0x0c, Name: "Temperature", Confidence: "high"},
	{ID: 0x0d, Name: "RSSI", Confidence: "high", Notes: "Reported as the raw VSS value."},
	{ID: 0x0f, Name: "Uptime", Confidence: "high", Notes: "Seconds since screen boot/reconnect context in observed captures."},
	{ID: 0x10, Name: "FirmwareMajor", Confidence: "high"},
	{ID: 0x11, Name: "FirmwareMinor", Confidence: "high"},
	{ID: 0x12, Name: "FirmwareRevision", Confidence: "high"},
	{ID: 0x13, Name: "BootloaderMajor", Confidence: "high"},
	{ID: 0x14, Name: "BootloaderMinor", Confidence: "high"},
	{ID: 0x15, Name: "BootloaderRevision", Confidence: "high"},
	{ID: 0x16, Name: "HardwareNameID", Confidence: "high"},
	{ID: 0x17, Name: "HardwareMajor", Confidence: "high"},
	{ID: 0x18, Name: "HardwareMinor", Confidence: "high"},
	{ID: 0x19, Name: "HardwareRevision", Confidence: "medium-high"},
	{ID: 0x1a, Name: "HardwareFirmwareIface", Confidence: "medium"},
	{ID: 0x1b, Name: "NextStatus", Confidence: "high", Notes: "Observed status interval in minutes."},
	{ID: 0x1d, Name: "NetworkErrorCount_or_touch/button_count", Confidence: "low"},
	{ID: 0x1e, Name: "TouchEventCount_or_ProximityCount", Confidence: "low"},
	{ID: 0x1f, Name: "ButtonCount_or_ProximityCount", Confidence: "low"},
	{ID: 0x20, Name: "BatteryCurrent", Confidence: "medium"},
	{ID: 0x22, Name: "BatteryVoltage", Confidence: "high", Notes: "Millivolts in observed captures."},
	{ID: 0x23, Name: "unknown_35", Confidence: "low"},
	{ID: 0x24, Name: "EventQueueOverFlow", Confidence: "medium"},
	{ID: 0x26, Name: "DisplayWidth", Confidence: "high", Notes: "Internal/controller width, not portrait canvas width."},
	{ID: 0x27, Name: "DisplayHeight", Confidence: "high", Notes: "Internal/controller height."},
	{ID: 0x28, Name: "ProtocolVersion", Confidence: "high"},
	{ID: 0x2b, Name: "PV2Features", Confidence: "high"},
	{ID: 0x2d, Name: "EPDTempSensor", Confidence: "high", Notes: "Degrees C when not -1."},
	{ID: 0x2e, Name: "TouchFirmwareMajor", Confidence: "medium"},
	{ID: 0x2f, Name: "MobileLinkType_or_MobileModuleId_or_other", Confidence: "low"},
	{ID: 0x32, Name: "GTIN_chunk_0", Confidence: "high"},
	{ID: 0x33, Name: "GTIN_chunk_1", Confidence: "high"},
	{ID: 0x34, Name: "GTIN_chunk_2", Confidence: "high"},
	{ID: 0x35, Name: "GTIN_chunk_3", Confidence: "high"},
	{ID: 0x36, Name: "ImagePushAllowed", Confidence: "high"},
	{ID: 0x38, Name: "NumSupportedDisplays", Confidence: "medium-high"},
	{ID: 0x3c, Name: "FsTotalSize", Confidence: "high"},
	{ID: 0x3d, Name: "FsFreeSize", Confidence: "high"},
	{ID: 0x46, Name: "unknown_70", Confidence: "low"},
	{ID: 0x47, Name: "unknown_71", Confidence: "low"},
	{ID: 0x57, Name: "unknown_87", Confidence: "high"},
	{ID: 0x58, Name: "unknown_88", Confidence: "medium"},
	{ID: 0x59, Name: "unknown_89", Confidence: "high"},
	{ID: 0x5a, Name: "unknown_90", Confidence: "high"},
}

var statusNames = func() map[uint32]string {
	out := make(map[uint32]string, len(knownStatusFields))
	for i := range knownStatusFields {
		knownStatusFields[i].Hex = fmt.Sprintf("0x%02x", knownStatusFields[i].ID)
		out[knownStatusFields[i].ID] = knownStatusFields[i].Name
	}
	return out
}()

func KnownFields() []FieldInfo {
	out := append([]FieldInfo(nil), knownStatusFields...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	for i := range out {
		out[i].Hex = fmt.Sprintf("0x%02x", out[i].ID)
	}
	return out
}

func FieldName(id uint32) string {
	if name := statusNames[id]; name != "" {
		return name
	}
	return fmt.Sprintf("field_%d", id)
}
