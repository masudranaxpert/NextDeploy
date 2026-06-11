package logview

import (
	"bytes"
	"testing"
)

func TestStripDockerLogChunk_wholeLine(t *testing.T) {
	in := []byte("\x1b[32m[INFO]\x1b[0m hello\n")
	out, tail := StripDockerLogChunk(nil, in)
	if len(tail) != 0 {
		t.Fatalf("unexpected tail: %q", tail)
	}
	want := []byte("[INFO] hello\n")
	if !bytes.Equal(out, want) {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestStripDockerLogChunk_splitEscape(t *testing.T) {
	out1, accum := StripDockerLogChunk(nil, []byte("pre \x1b[3"))
	if string(out1) != "pre " {
		t.Fatalf("expected first out %q, got %q", "pre ", out1)
	}
	if string(accum) != "\x1b[3" {
		t.Fatalf("accum: got %q want %q", accum, "\x1b[3")
	}
	out2, accum := StripDockerLogChunk(accum, []byte("2mok\n"))
	want := []byte("ok\n")
	if !bytes.Equal(out2, want) {
		t.Fatalf("second out: got %q want %q", out2, want)
	}
	if len(accum) != 0 {
		t.Fatalf("unexpected tail: %q", accum)
	}
}

func TestStripDockerLogChunk_multipleLeadingAnsi(t *testing.T) {
	in := []byte("\x1b[32m\x1b[1mtext")
	out, tail := StripDockerLogChunk(nil, in)
	if len(tail) != 0 {
		t.Fatalf("unexpected tail: %q", tail)
	}
	if string(out) != "text" {
		t.Fatalf("got %q", out)
	}
}

func TestFlushDockerLogAccum(t *testing.T) {
	out := FlushDockerLogAccum([]byte("\x1b[32mleft"))
	if string(out) != "left" {
		t.Fatalf("got %q", out)
	}
}
