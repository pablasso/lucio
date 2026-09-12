package pv3

import (
	"bytes"
	"testing"
)

// A small independent test decoder. Production Lucio only encodes LZ4.
func decodeBlock(t *testing.T, b []byte, want int) []byte {
	t.Helper()
	out := make([]byte, 0, want)
	length := func(n int) int {
		if n == 15 {
			for {
				if len(b) == 0 {
					t.Fatal("missing length")
				}
				v := b[0]
				b = b[1:]
				n += int(v)
				if v != 255 {
					break
				}
			}
		}
		return n
	}
	for len(b) > 0 {
		token := b[0]
		b = b[1:]
		n := length(int(token >> 4))
		if n > len(b) || n > want-len(out) {
			t.Fatal("bad literal length")
		}
		out = append(out, b[:n]...)
		b = b[n:]
		if len(b) == 0 {
			break
		}
		if len(b) < 2 {
			t.Fatal("missing offset")
		}
		offset := int(b[0]) + int(b[1])*256
		b = b[2:]
		n = length(int(token&15)) + 4
		if offset == 0 || offset > len(out) || n > want-len(out) {
			t.Fatal("bad match")
		}
		for range n {
			out = append(out, out[len(out)-offset])
		}
	}
	if len(out) != want {
		t.Fatalf("decoded %d expected %d", len(out), want)
	}
	return out
}

func TestLZ4IndependentVector(t *testing.T) {
	if got := lz4LiteralBlock([]byte("Lucio")); !bytes.Equal(got, []byte{0x50, 'L', 'u', 'c', 'i', 'o'}) {
		t.Fatalf("literal: %x", got)
	}
	// Five 'a's: one literal, an overlapping 4-byte match, then five literals.
	if got := decodeBlock(t, []byte{0x10, 'a', 1, 0, 0x50, 'b', 'c', 'd', 'e', 'f'}, 10); string(got) != "aaaaabcdef" {
		t.Fatal(string(got))
	}
}

func TestImageCompressionRecords(t *testing.T) {
	for _, b := range [][]byte{[]byte("Lucio"), bytes.Repeat([]byte("abc xyz"), 4000), make([]byte, 1843304)} {
		records := CompressImagePacketLZ4(b, DefaultImageBlockLen)
		var out []byte
		n := (len(b) + DefaultImageBlockLen - 1) / DefaultImageBlockLen
		for i := 0; len(records) > 0; i++ {
			if len(records) < 24 {
				t.Fatal("header")
			}
			h := records[:24]
			records = records[24:]
			if le.Uint32(h) != uint32(i) || le.Uint32(h[4:]) != uint32(n-1) || le.Uint64(h[16:]) != 0 {
				t.Fatal("record metadata")
			}
			clen, ulen := int(le.Uint32(h[8:])), int(le.Uint32(h[12:]))
			if clen > len(records) || ulen > DefaultImageBlockLen {
				t.Fatal("length")
			}
			// Reusing the table must give exactly the same bytes as a fresh table.
			want := lz4BlockCompressWithTable(b[i*DefaultImageBlockLen:min((i+1)*DefaultImageBlockLen, len(b))], make([]int, 1<<16))
			if !bytes.Equal(want, records[:clen]) {
				t.Fatal("scratch reuse changed compression")
			}
			out = append(out, decodeBlock(t, records[:clen], ulen)...)
			records = records[clen:]
		}
		if !bytes.Equal(out, b) {
			t.Fatal("round trip")
		}
	}
}

func FuzzCompression(f *testing.F) {
	f.Add([]byte("Lucio"))
	f.Add(bytes.Repeat([]byte{0xff}, 64))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > DefaultImageBlockLen {
			return
		}
		comp := lz4BlockCompressWithTable(b, make([]int, 1<<16))
		if !bytes.Equal(decodeBlock(t, comp, len(b)), b) {
			t.Fatal("round trip")
		}
	})
}
func BenchmarkFullImageCompression(b *testing.B) {
	data := make([]byte, 1843304)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		CompressImagePacketLZ4(data, DefaultImageBlockLen)
	}
}
