package dockerx

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/docker/docker/pkg/stdcopy"
)

// maxDockerLogFrame caps multiplex frame size to avoid absurd allocations from corrupt headers.
const maxDockerLogFrame = 16 << 20

// DemuxDockerLogsStream converts `docker logs` CLI pipe output to plain UTF-8 text.
// Without a TTY, Docker multiplexes stdout/stderr with an 8-byte header per chunk; reading
// that stream as raw text breaks ANSI sequences and inserts garbage bytes. Compose logs and
// many other sources are already plain text — those pass through unchanged.
func DemuxDockerLogsStream(r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		head := make([]byte, 8)
		n, err := io.ReadFull(r, head)
		if n == 0 {
			return
		}
		if err == io.ErrUnexpectedEOF {
			_, _ = pw.Write(head[:n])
			return
		}
		if err != nil {
			_, _ = pw.Write(head[:n])
			return
		}
		st := head[0]
		fs := int(binary.BigEndian.Uint32(head[4:8]))
		if (st != 1 && st != 2) || fs <= 0 || fs > maxDockerLogFrame {
			if _, werr := pw.Write(head); werr != nil {
				return
			}
			_, _ = io.Copy(pw, r)
			return
		}
		mux := io.MultiReader(bytes.NewReader(head), r)
		_, _ = stdcopy.StdCopy(pw, pw, mux)
	}()
	return pr
}
