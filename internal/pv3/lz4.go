package pv3

// These are raw LZ4 blocks, not LZ4 frames. PV3 supplies the record framing.
func lz4LiteralBlock(src []byte) []byte {
	return appendLiteralRun(make([]byte, 0, len(src)+1+len(src)/255+1), src)
}

func lz4BlockCompressWithTable(src []byte, table []int) []byte {
	const (
		minMatch     = 4
		hashLog      = 16
		mfLimit      = 12 // keep final bytes literal for decoder compatibility
		lastLiterals = 5
		prime32      = uint32(2654435761)
	)
	if len(src) == 0 {
		return []byte{0}
	}
	if len(src) < mfLimit {
		return lz4LiteralBlock(src)
	}
	for i := range table {
		table[i] = -1
	}
	out := make([]byte, 0, len(src))
	anchor, pos := 0, 0
	matchLimit, maxMatchEnd := len(src)-mfLimit, len(src)-lastLiterals
	for pos <= matchLimit {
		seq := le.Uint32(src[pos:])
		h := int((seq * prime32) >> (32 - hashLog))
		ref := table[h]
		table[h] = pos
		if ref >= 0 && pos-ref <= 0xffff && equal4(src[ref:], src[pos:]) {
			matchLen := minMatch
			for pos+matchLen < maxMatchEnd && src[ref+matchLen] == src[pos+matchLen] {
				matchLen++
			}
			out = appendLZ4Sequence(out, src[anchor:pos], pos-ref, matchLen)
			pos += matchLen
			anchor = pos
			continue
		}
		pos++
	}
	return appendLiteralRun(out, src[anchor:])
}

func equal4(a, b []byte) bool {
	return len(a) >= 4 && len(b) >= 4 && a[0] == b[0] && a[1] == b[1] && a[2] == b[2] && a[3] == b[3]
}

func appendLZ4Sequence(out, literals []byte, offset, matchLen int) []byte {
	litLen, ml := len(literals), matchLen-4
	tokenPos := len(out)
	out = append(out, 0)
	var token byte
	if litLen >= 15 {
		token |= 0xf0
		out = appendLenBytes(out, litLen-15)
	} else {
		token |= byte(litLen << 4)
	}
	out = append(out, literals...)
	out = append(out, byte(offset), byte(offset>>8))
	if ml >= 15 {
		token |= 0x0f
		out = appendLenBytes(out, ml-15)
	} else {
		token |= byte(ml)
	}
	out[tokenPos] = token
	return out
}

func appendLiteralRun(out, literals []byte) []byte {
	if len(literals) >= 15 {
		out = append(out, 0xf0)
		out = appendLenBytes(out, len(literals)-15)
	} else {
		out = append(out, byte(len(literals)<<4))
	}
	return append(out, literals...)
}

func appendLenBytes(out []byte, n int) []byte {
	for n >= 255 {
		out = append(out, 255)
		n -= 255
	}
	return append(out, byte(n))
}
