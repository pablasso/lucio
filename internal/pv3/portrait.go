package pv3

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
)

const (
	PortraitWidth  = 1440
	PortraitHeight = 2560
	BandHeight     = 640

	InternalRectWidth  = 2880
	InternalRectHeight = 640

	ImagePacketType          = uint32(5)
	ImagePrimitiveRectangle  = uint16(1)
	ImageRectangleOptions    = uint16(0x0102)
	ImageRectangleEncoding4  = uint32(4)
	ImageRectanglePixelBytes = 921600 // 2880 * 640 * 4 bpp / 8
	PartialRowAlignment      = 64
)

// ImageBuildResult describes a generated server->screen image frame.
type ImageBuildResult struct {
	Frame             Frame
	Meta              ImagePacketMeta
	UncompressedBytes int
	CompressedBytes   int
	PayloadBytes      int
	Checksum          uint32
}

// PortraitGrayFromImage converts an exactly 1440x2560 image to gray8 blended
// over white. Lucio intentionally requires the already-rendered portrait
// geometry rather than resizing or cropping.
func PortraitGrayFromImage(src image.Image) (*image.Gray, error) {
	b := src.Bounds()
	if b.Dx() != PortraitWidth || b.Dy() != PortraitHeight {
		return nil, fmt.Errorf("portrait image must be %dx%d, got %dx%d", PortraitWidth, PortraitHeight, b.Dx(), b.Dy())
	}
	gray := image.NewGray(image.Rect(0, 0, PortraitWidth, PortraitHeight))
	for y := 0; y < PortraitHeight; y++ {
		for x := 0; x < PortraitWidth; x++ {
			r, g, bb, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			// RGBA returns alpha-premultiplied 16-bit channels. Blend transparent
			// pixels onto white without un-premultiplying.
			wr := r + (0xffff - a)
			wg := g + (0xffff - a)
			wb := bb + (0xffff - a)
			if wr > 0xffff {
				wr = 0xffff
			}
			if wg > 0xffff {
				wg = 0xffff
			}
			if wb > 0xffff {
				wb = 0xffff
			}
			y16 := (299*wr + 587*wg + 114*wb + 500) / 1000
			gray.Pix[gray.PixOffset(x, y)] = uint8((y16 + 128) / 257)
		}
	}
	return gray, nil
}

// BuildImagePayloadFromPortraitGray builds the VSS image payload from a
// human-readable physical portrait image: two rectangle primitives for the
// observed 32"/four-band display. The returned bytes start at the first
// rectangle header; callers still need to prepend the 20-byte image header and
// 36-byte packet header.
//
// The tested eink-flip display path presents each logical band mirrored
// horizontally relative to packet order. VSS-generated packets therefore contain
// horizontally pre-flipped rows. Keep that pre-flip here so callers can upload a
// normal/readable 1440x2560 image and have it appear readable on the physical
// screen.
func BuildImagePayloadFromPortraitGray(gray *image.Gray) ([]byte, error) {
	if err := validatePortraitGray(gray); err != nil {
		return nil, err
	}
	payload := make([]byte, 0, 2*(24+ImageRectanglePixelBytes))
	payload = appendRectPrimitive(payload, gray, 0, 0, BandHeight)
	// Odd screen indexes use serpentine band order in the decoded portrait
	// mapping: internal band A maps to the bottom portrait band, and internal
	// band B maps to the preceding band.
	payload = appendRectPrimitive(payload, gray, 1, 3*BandHeight, 2*BandHeight)
	return payload, nil
}

// PartialStrip describes the conservative partial-update shape currently used
// by lucio. X/W are internal/controller coordinates and must
// cover the full observed internal width; Y/H select aligned internal rows.
// Screens must contain non-duplicated screen indexes 0 and/or 1. Options may
// be left zero to use the proven default 0x0102, and non-default options are
// rejected by the builder.
type PartialStrip struct {
	X       int
	Y       int
	W       int
	H       int
	Screens []int
	Options uint16
}

// BuildPartialImagePayloadFromPortraitGray builds rectangle primitives for a
// small set of full-width internal row strips from a complete target portrait
// framebuffer. It intentionally supports only the proven safe v1 partial shape:
// full internal width, 64-row alignment, encoding-4, rectangle options 0x0102,
// and screen indexes 0/1. Callers should keep a complete target framebuffer and
// use the returned payload as a partial delivery of that target state.
func BuildPartialImagePayloadFromPortraitGray(gray *image.Gray, strips []PartialStrip) ([]byte, uint32, error) {
	if err := validatePortraitGray(gray); err != nil {
		return nil, 0, err
	}
	if len(strips) == 0 {
		return nil, 0, fmt.Errorf("no partial strips")
	}
	payload := make([]byte, 0)
	var primitives uint32
	for i, strip := range strips {
		if strip.Options == 0 {
			strip.Options = ImageRectangleOptions
		}
		if strip.Options != ImageRectangleOptions {
			return nil, 0, fmt.Errorf("partial strip %d has unsupported rectangle options 0x%04x", i, strip.Options)
		}
		if strip.X != 0 || strip.W != InternalRectWidth {
			return nil, 0, fmt.Errorf("partial strip %d must cover full internal width x=0 w=%d, got x=%d w=%d", i, InternalRectWidth, strip.X, strip.W)
		}
		if strip.Y < 0 || strip.Y >= InternalRectHeight || strip.H <= 0 || strip.H > InternalRectHeight-strip.Y {
			return nil, 0, fmt.Errorf("partial strip %d has invalid y/h: y=%d h=%d", i, strip.Y, strip.H)
		}
		if strip.Y%PartialRowAlignment != 0 || strip.H%PartialRowAlignment != 0 {
			return nil, 0, fmt.Errorf("partial strip %d must use %d-row alignment: y=%d h=%d", i, PartialRowAlignment, strip.Y, strip.H)
		}
		if len(strip.Screens) == 0 {
			return nil, 0, fmt.Errorf("partial strip %d has no screens", i)
		}
		seenScreens := map[int]bool{}
		for _, screen := range strip.Screens {
			if seenScreens[screen] {
				return nil, 0, fmt.Errorf("partial strip %d repeats screen %d", i, screen)
			}
			seenScreens[screen] = true
			switch screen {
			case 0:
				payload = appendPartialRectPrimitive(payload, gray, 0, strip.Y, strip.H, strip.Options, 0, BandHeight)
			case 1:
				payload = appendPartialRectPrimitive(payload, gray, 1, strip.Y, strip.H, strip.Options, 3*BandHeight, 2*BandHeight)
			default:
				return nil, 0, fmt.Errorf("partial strip %d has unsupported screen %d", i, screen)
			}
			primitives++
		}
	}
	if primitives == 0 {
		return nil, 0, fmt.Errorf("partial strips produced no primitives")
	}
	return payload, primitives, nil
}

func validatePortraitGray(gray *image.Gray) error {
	if gray == nil {
		return fmt.Errorf("nil portrait image")
	}
	b := gray.Bounds()
	if b.Min.X != 0 || b.Min.Y != 0 || b.Dx() != PortraitWidth || b.Dy() != PortraitHeight {
		return fmt.Errorf("portrait gray image must have bounds (0,0)-(%d,%d), got %v", PortraitWidth, PortraitHeight, b)
	}
	if gray.Stride < PortraitWidth || len(gray.Pix) < PortraitWidth || gray.Stride > (len(gray.Pix)-PortraitWidth)/(PortraitHeight-1) {
		return fmt.Errorf("portrait gray has invalid pixel storage or stride")
	}
	return nil
}

func appendRectPrimitive(out []byte, gray *image.Gray, screenIndex uint16, bandAY, bandBY int) []byte {
	return appendPartialRectPrimitive(out, gray, screenIndex, 0, InternalRectHeight, ImageRectangleOptions, bandAY, bandBY)
}

func appendPartialRectPrimitive(out []byte, gray *image.Gray, screenIndex uint16, internalY, internalH int, options uint16, bandAY, bandBY int) []byte {
	pixelBytes := InternalRectWidth * internalH / 2
	header := make([]byte, 24)
	binary.LittleEndian.PutUint16(header[0:], ImagePrimitiveRectangle)
	binary.LittleEndian.PutUint16(header[2:], screenIndex)
	// x remains zero.
	binary.LittleEndian.PutUint16(header[6:], uint16(internalY))
	binary.LittleEndian.PutUint16(header[8:], InternalRectWidth)
	binary.LittleEndian.PutUint16(header[10:], uint16(internalH))
	binary.LittleEndian.PutUint16(header[12:], options)
	// reserved16 remains zero.
	binary.LittleEndian.PutUint32(header[16:], ImageRectangleEncoding4)
	binary.LittleEndian.PutUint32(header[20:], uint32(pixelBytes))
	out = append(out, header...)
	out = appendInterleavedRectDataRange(out, gray, bandAY, bandBY, internalY, internalH)
	return out
}

func appendInterleavedRectDataRange(out []byte, gray *image.Gray, bandAY, bandBY, startRow, rowCount int) []byte {
	const pixelsPerQuant = 4
	groups := PortraitWidth / pixelsPerQuant
	for row := startRow; row < startRow+rowCount; row++ {
		aOff := gray.PixOffset(0, bandAY+row)
		bOff := gray.PixOffset(0, bandBY+row)
		aRow := gray.Pix[aOff : aOff+PortraitWidth]
		bRow := gray.Pix[bOff : bOff+PortraitWidth]
		for g := 0; g < groups; g++ {
			// Packet logical X is mirrored by the physical eink-flip display path,
			// so pre-flip each 1440px band horizontally. Reverse both group order
			// and pixel order within each 4-pixel/2-byte quant.
			x := PortraitWidth - (g+1)*pixelsPerQuant
			out = appendPacked4Reversed(out, aRow[x:x+pixelsPerQuant])
			out = appendPacked4Reversed(out, bRow[x:x+pixelsPerQuant])
		}
	}
	return out
}

func appendPacked4Reversed(out []byte, p []byte) []byte {
	// Encoding-4 stores the first pixel of each byte in the low nibble. VSS's
	// eink-flip mapper also stores each logical row right-to-left for this panel.
	// For a left-to-right 4-pixel source group [p0 p1 p2 p3], the packet pixel
	// stream must therefore be [p3 p2 p1 p0], encoded low-nibble-first as:
	//   byte0 low=p3 high=p2, byte1 low=p1 high=p0.
	return append(out,
		grayNibble(p[2])<<4|grayNibble(p[3]),
		grayNibble(p[0])<<4|grayNibble(p[1]),
	)
}

func grayNibble(v byte) byte {
	return byte((uint16(v)*15 + 127) / 255)
}

// DefaultImageChecksum is lucio's local display-state CRC for
// generated frames. Captured VSS checksums are not this simple function, but the
// field is used as a display-state identifier by the supported firmware.
// This is not a cryptographic integrity or physical pixel-verification check.
func DefaultImageChecksum(imagePayload []byte) uint32 {
	return crc32.ChecksumIEEE(imagePayload)
}

func BuildImageFrameFromPortraitGray(gray *image.Gray, deviceID [16]byte, packetID uint32, checksumOverride *uint32) (ImageBuildResult, error) {
	payload, err := BuildImagePayloadFromPortraitGray(gray)
	if err != nil {
		return ImageBuildResult{}, err
	}
	return BuildImageFrameFromPayload(payload, deviceID, packetID, checksumOverride)
}

func BuildPartialImageFrameFromPortraitGray(gray *image.Gray, strips []PartialStrip, deviceID [16]byte, packetID uint32, checksum uint32) (ImageBuildResult, error) {
	payload, primitiveCount, err := BuildPartialImagePayloadFromPortraitGray(gray, strips)
	if err != nil {
		return ImageBuildResult{}, err
	}
	return buildImageFrameFromPayloadWithPrimitiveCount(payload, primitiveCount, deviceID, packetID, checksum)
}

func BuildImageFrameFromPayload(imagePayload []byte, deviceID [16]byte, packetID uint32, checksumOverride *uint32) (ImageBuildResult, error) {
	checksum := DefaultImageChecksum(imagePayload)
	if checksumOverride != nil {
		checksum = *checksumOverride
	}
	return buildImageFrameFromPayloadWithPrimitiveCount(imagePayload, 2, deviceID, packetID, checksum)
}

func buildImageFrameFromPayloadWithPrimitiveCount(imagePayload []byte, primitiveCount uint32, deviceID [16]byte, packetID uint32, checksum uint32) (ImageBuildResult, error) {
	uncompressed, meta, err := BuildImagePacketFromPayloadWithPrimitiveCount(imagePayload, primitiveCount, deviceID, packetID, checksum)
	if err != nil {
		return ImageBuildResult{}, err
	}
	compressed := CompressImagePacketLZ4(uncompressed, DefaultImageBlockLen)
	frame := BuildFrame(DirectionServerToScreen, compressed)
	return ImageBuildResult{
		Frame:             frame,
		Meta:              meta,
		UncompressedBytes: len(uncompressed),
		CompressedBytes:   len(compressed),
		PayloadBytes:      len(imagePayload),
		Checksum:          checksum,
	}, nil
}

func BuildImagePacketFromPayload(imagePayload []byte, deviceID [16]byte, packetID uint32, checksum uint32) ([]byte, ImagePacketMeta, error) {
	return BuildImagePacketFromPayloadWithPrimitiveCount(imagePayload, 2, deviceID, packetID, checksum)
}

func BuildImagePacketFromPayloadWithPrimitiveCount(imagePayload []byte, primitiveCount uint32, deviceID [16]byte, packetID uint32, checksum uint32) ([]byte, ImagePacketMeta, error) {
	if len(imagePayload) == 0 {
		return nil, ImagePacketMeta{}, fmt.Errorf("empty image payload")
	}
	if primitiveCount == 0 {
		return nil, ImagePacketMeta{}, fmt.Errorf("primitive count must be non-zero")
	}
	imagePayloadLen := uint32(len(imagePayload))
	packetPayloadLen := uint32(ImageHeaderLen) + imagePayloadLen
	uncompressed := make([]byte, ImagePacketHeaderLen+ImageHeaderLen+len(imagePayload))
	// packet field0/reserved stays zero.
	copy(uncompressed[4:20], deviceID[:])
	le.PutUint32(uncompressed[20:], ImagePacketType)
	le.PutUint32(uncompressed[24:], packetID)
	le.PutUint32(uncompressed[28:], packetPayloadLen)
	// uncompressed[32:36] reserved zero.
	le.PutUint32(uncompressed[36:], checksum)
	le.PutUint32(uncompressed[40:], primitiveCount)
	// uncompressed[44:48] field2 zero.
	le.PutUint32(uncompressed[48:], imagePayloadLen)
	// uncompressed[52:56] field4 zero.
	copy(uncompressed[56:], imagePayload)
	meta, err := ParseImagePacketMeta(uncompressed)
	if err != nil {
		return nil, ImagePacketMeta{}, err
	}
	return uncompressed, meta, nil
}
