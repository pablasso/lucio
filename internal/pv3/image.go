package pv3

import "fmt"

const (
	CompressionRecordLen = 24
	ImagePacketHeaderLen = 36
	ImageHeaderLen       = 20
	DefaultImageBlockLen = 4800
)

type ImagePacketMeta struct {
	PacketType      uint32
	PacketID        uint32
	PayloadLen      uint32
	ImageChecksum   uint32
	NrPrimitives    uint32
	ImagePayloadLen uint32
}

func ParseImagePacketMeta(b []byte) (ImagePacketMeta, error) {
	if len(b) < ImagePacketHeaderLen+ImageHeaderLen {
		return ImagePacketMeta{}, fmt.Errorf("uncompressed image packet too short: %d", len(b))
	}
	return ImagePacketMeta{
		PacketType: le.Uint32(b[20:]), PacketID: le.Uint32(b[24:]),
		PayloadLen: le.Uint32(b[28:]), ImageChecksum: le.Uint32(b[36:]),
		NrPrimitives: le.Uint32(b[40:]), ImagePayloadLen: le.Uint32(b[48:]),
	}, nil
}

// CompressImagePacketLZ4 wraps independent raw LZ4 blocks in PV3 records.
// The scratch table is reused within a packet, never shared across callers.
func CompressImagePacketLZ4(b []byte, blockLen int) []byte {
	if blockLen <= 0 {
		blockLen = DefaultImageBlockLen
	}
	if len(b) == 0 {
		return nil
	}
	n := (len(b)-1)/blockLen + 1
	out := make([]byte, 0, len(b)+n*CompressionRecordLen)
	table := make([]int, 1<<16)
	for i, start := 0, 0; start < len(b); i, start = i+1, start+blockLen {
		chunk := b[start:min(start+blockLen, len(b))]
		comp := lz4BlockCompressWithTable(chunk, table)
		var header [CompressionRecordLen]byte
		le.PutUint32(header[0:], uint32(i))
		le.PutUint32(header[4:], uint32(n-1))
		le.PutUint32(header[8:], uint32(len(comp)))
		le.PutUint32(header[12:], uint32(len(chunk)))
		out = append(out, header[:]...)
		out = append(out, comp...)
	}
	return out
}
