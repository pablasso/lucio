package pv3

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestPortraitMappingVector(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, PortraitWidth, PortraitHeight))
	// Four different gray ramps at the RIGHT edge of the four portrait bands.
	// Expected first packet group is independently specified below.
	for band := 0; band < 4; band++ {
		for x := 0; x < 4; x++ {
			gray.SetGray(PortraitWidth-4+x, band*BandHeight, color.Gray{Y: byte((4*band + x) * 17)})
		}
	}
	payload, err := BuildImagePayloadFromPortraitGray(gray)
	if err != nil {
		t.Fatal(err)
	}
	// Header: primitive 1, screen 0, x/y 0, w=2880 h=640,
	// options=0x0102, encoding=4, data length=921600.
	wantHeader := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0x40, 0x0b, 0x80, 2, 2, 1, 0, 0, 4, 0, 0, 0, 0, 0x10, 0x0e, 0}
	if !bytes.Equal(payload[:24], wantHeader) {
		t.Fatalf("header: %x", payload[:24])
	}
	if !bytes.Equal(payload[24:28], []byte{0x23, 0x01, 0x67, 0x45}) {
		t.Fatalf("screen 0 mapping: %x", payload[24:28])
	}
	off := 24 + ImageRectanglePixelBytes
	if payload[off+2] != 1 || !bytes.Equal(payload[off+24:off+28], []byte{0xef, 0xcd, 0xab, 0x89}) {
		t.Fatalf("screen 1 mapping: %x", payload[off+24:off+28])
	}
	if len(payload) != 2*(24+ImageRectanglePixelBytes) {
		t.Fatal(len(payload))
	}
}

func TestPartialRowsMatchFullPacket(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, PortraitWidth, PortraitHeight))
	for i := range gray.Pix {
		gray.Pix[i] = byte(i * 17)
	}
	full, err := BuildImagePayloadFromPortraitGray(gray)
	if err != nil {
		t.Fatal(err)
	}
	part, n, err := BuildPartialImagePayloadFromPortraitGray(gray, []PartialStrip{{W: InternalRectWidth, Y: 64, H: 64, Screens: []int{0, 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatal(n)
	}
	for i := 0; i < 2; i++ {
		p := i * (24 + InternalRectWidth*64/2)
		f := i * (24 + ImageRectanglePixelBytes)
		if le.Uint16(part[p+6:]) != 64 || le.Uint16(part[p+10:]) != 64 {
			t.Fatal("strip header")
		}
		if !bytes.Equal(part[p+24:p+24+InternalRectWidth*64/2], full[f+24+64*InternalRectWidth/2:f+24+128*InternalRectWidth/2]) {
			t.Fatal("strip data")
		}
	}
	b, err := BuildPartialImageFrameFromPortraitGray(gray, []PartialStrip{{W: InternalRectWidth, H: 64, Screens: []int{0, 1}}}, testDeviceID(), 77, 0x12345678)
	if err != nil || b.Meta.PacketID != 77 || b.Meta.NrPrimitives != 2 || b.Checksum != 0x12345678 {
		t.Fatal(b.Meta, err)
	}
}

func TestRejectInvalidGeometry(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, PortraitWidth, PortraitHeight))
	cases := []PartialStrip{
		{X: 1, W: InternalRectWidth, H: 64, Screens: []int{0}},
		{W: 1, H: 64, Screens: []int{0}},
		{W: InternalRectWidth, Y: 1, H: 64, Screens: []int{0}},
		{W: InternalRectWidth, H: 63, Screens: []int{0}},
		{W: InternalRectWidth, Y: 640, H: 64, Screens: []int{0}},
		{W: InternalRectWidth, H: int(^uint(0) >> 1), Screens: []int{0}},
		{W: InternalRectWidth, H: 64, Screens: []int{2}},
		{W: InternalRectWidth, H: 64, Screens: []int{0, 0}},
		{W: InternalRectWidth, H: 64},
		{W: InternalRectWidth, H: 64, Screens: []int{0}, Options: 0x0402},
	}
	for _, s := range cases {
		if _, _, err := BuildPartialImagePayloadFromPortraitGray(gray, []PartialStrip{s}); err == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
	for _, g := range []*image.Gray{nil, image.NewGray(image.Rect(0, 0, 10, 10)), {Rect: gray.Rect, Stride: PortraitWidth, Pix: []byte{0}}} {
		if _, err := BuildImagePayloadFromPortraitGray(g); err == nil {
			t.Fatal("accepted invalid framebuffer")
		}
	}
}
