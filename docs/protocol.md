# Supported PV3 subset

Lucio implements the local PV3 display path for the tested 32-inch PP32 v1.1
panel (firmware/bootloader 6.9.3947). This is not a complete Visionect protocol
implementation or a promise of compatibility with other panels.

## Framing and status

The screen opens a TCP connection to port 11113. Each frame starts with five
little-endian uint32 values: version (3), reserved field, direction (0 from
screen, 1 from server), payload length, checksum. The outer header is 20 bytes.
Server checksums are IEEE CRC32 of the payload. Screen-originated checksum
values are not always that function, so Lucio does not apply that validation
to received frames. Neither checksum provides authentication.

Screen packets have a 36-byte header: reserved uint32, 16 device-ID bytes,
class, packet ID, inner length, reserved uint32. Classes used here are control
(1), status (3), and parameter response (8). Status bodies contain uint32
(field ID, value) pairs, ending at field `ffffffff`. Unknown fields remain raw.

Status and parameter responses receive a server control ACK acknowledging the
screen's packet ID. A successful image ACK is screen control code 1, argument 0,
with the image packet ID. These two directions have distinct packet-ID streams.

## Images

The author canvas is 1440×2560 portrait pixels. It is **not** the controller's
2880×640 coordinate system. There are four 1440×640 portrait bands and two
controller rectangle primitives. Screen index 0 interleaves bands 0 and 1;
screen index 1 interleaves bands 3 and 2. Four-pixel groups from each band
alternate, with each portrait row pre-mirrored horizontally. Encoding 4 uses
4 bits per pixel, first pixel in the low nibble. Gray quantization is
`(gray8 * 15 + 127) / 255` with integer division.

Each rectangle has a 24-byte header: uint16 primitive (1), screen index, x, y,
width, height, options (`0102`), reserved; uint32 encoding (4), pixel byte count.
The image has a 20-byte header after the 36-byte image packet header (class 5).
Lucio uses IEEE CRC32 of the **complete target rectangle payload** as its display
state identifier. Partial updates retain that resultant-full-image identifier.
A later reported DisplayStateCRC is supporting state evidence, not a readback
of the physical pixels or proof of visual quality.

Image packets are compressed into independent raw LZ4 blocks of 4800 bytes.
Each block has a 24-byte record header: uint32 index, last index, compressed
length, uncompressed length, and two zero reserved fields. The resulting stream
is enclosed in the normal outer frame. This is not the standard LZ4 frame format.

## Conservative partial updates

A partial refresh requires an ACKed framebuffer on the current connection.
Only full-controller-width strips, aligned to 64 rows, on indexes 0 and 1 are
used. At most four strips / 256 expanded internal rows are selected. Larger
changes fall back to full delivery. Reconnect, timeout, NACK, unexpected ACK,
or contradictory status invalidates the baseline. Full is always the default.
There is no proven battery-saving estimate or automatic anti-ghosting schedule.

Synthetic vectors under `internal/pv3/testdata/` and explicit pixel-mapping tests
are the executable specification. They contain no device captures. Firmware
updates, remote parameter configuration, and factory provisioning are outside
this implementation's scope.
