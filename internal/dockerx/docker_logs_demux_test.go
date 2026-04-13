package dockerx

import (
	"bytes"
	"io"
	"testing"
)

func TestDemuxDockerLogsStream_shortPlain(t *testing.T) {
	r := DemuxDockerLogsStream(bytes.NewReader([]byte("ok\n")))
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "ok\n" {
		t.Fatalf("got %q", out)
	}
}

func TestDemuxDockerLogsStream_multiplexedStdout(t *testing.T) {
	hdr := []byte{1, 0, 0, 0, 0, 0, 0, 5}
	hdr = append(hdr, []byte("hello")...)
	r := DemuxDockerLogsStream(bytes.NewReader(hdr))
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "hello" {
		t.Fatalf("got %q", out)
	}
}
