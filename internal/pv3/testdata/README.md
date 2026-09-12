# Protocol vectors

These are synthetic vectors, not device captures. The device identifier is the
byte sequence `00` through `0f`. No real device or network data is included.

`status-ack.hex` specifies an 88-byte server acknowledgment of screen packet 18:
20-byte outer header, 68-byte control body, and IEEE CRC32 `d7c9e782` of that body.
The fixture is written independently of the Go encoder. Tests fail if it is absent.

Portrait tests use explicit expected rectangle headers and asymmetric pixel
patterns to check band ordering, horizontal mirroring, nibble order, and strip
alignment. Round trips supplement, rather than replace, those checks.
