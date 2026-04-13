package logview

import "bytes"

// StripDockerLogChunk removes ANSI SGR color sequences from streaming docker log bytes.
// accum holds an incomplete escape tail from the previous read; pass nil on the first chunk.
// newAccum must be stored and passed back on the next call for the same stream.
func StripDockerLogChunk(accum, chunk []byte) (out []byte, newAccum []byte) {
	if len(accum) == 0 && len(chunk) == 0 {
		return nil, nil
	}
	data := append(append([]byte(nil), accum...), chunk...)
	data = ansiOSC.ReplaceAll(data, nil)
	lastEsc := bytes.LastIndexByte(data, 0x1b)
	if lastEsc < 0 {
		return ansiSeq.ReplaceAll(data, nil), nil
	}
	safe := data[:lastEsc]
	risky := data[lastEsc:]
	for {
		loc := ansiSeq.FindIndex(risky)
		if len(loc) == 0 || loc[0] != 0 {
			break
		}
		risky = risky[loc[1]:]
	}
	if len(risky) == 0 {
		return ansiSeq.ReplaceAll(safe, nil), nil
	}
	if risky[0] == 0x1b {
		return ansiSeq.ReplaceAll(safe, nil), risky
	}
	out = append(ansiSeq.ReplaceAll(safe, nil), risky...)
	return out, nil
}

// FlushDockerLogAccum emits any remaining bytes after the stream ends. Complete ANSI
// sequences are stripped; a truncated tail (rare) is passed through unchanged.
func FlushDockerLogAccum(accum []byte) []byte {
	if len(accum) == 0 {
		return nil
	}
	return ansiSeq.ReplaceAll(ansiOSC.ReplaceAll(accum, nil), nil)
}
