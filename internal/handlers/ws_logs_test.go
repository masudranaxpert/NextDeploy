package handlers

import (
	"bytes"
	"testing"
)

func TestSplitCompleteLogLines(t *testing.T) {
	t.Run("buffers partial line without newline", func(t *testing.T) {
		emit, rest := splitCompleteLogLines([]byte("partial-line"))
		if len(emit) != 0 {
			t.Fatalf("expected empty emit, got %q", emit)
		}
		if string(rest) != "partial-line" {
			t.Fatalf("expected rest %q, got %q", "partial-line", rest)
		}
	})

	t.Run("splits complete lines and preserves trailing partial", func(t *testing.T) {
		emit, rest := splitCompleteLogLines([]byte("line1\nline2\npartial"))
		if string(emit) != "line1\nline2\n" {
			t.Fatalf("expected emit %q, got %q", "line1\nline2\n", emit)
		}
		if string(rest) != "partial" {
			t.Fatalf("expected rest %q, got %q", "partial", rest)
		}
	})

	t.Run("handles exact newline termination without remainder", func(t *testing.T) {
		emit, rest := splitCompleteLogLines([]byte("line1\n"))
		if string(emit) != "line1\n" {
			t.Fatalf("expected emit %q, got %q", "line1\n", emit)
		}
		if len(rest) != 0 {
			t.Fatalf("expected empty rest, got %q", rest)
		}
	})

	t.Run("flushes oversized partial buffer over 16 KiB", func(t *testing.T) {
		oversized := bytes.Repeat([]byte("a"), 16*1024+1)
		emit, rest := splitCompleteLogLines(oversized)
		if !bytes.Equal(emit, oversized) {
			t.Fatalf("expected full oversized buffer emitted (len=%d), got len=%d", len(oversized), len(emit))
		}
		if len(rest) != 0 {
			t.Fatalf("expected empty rest, got len=%d", len(rest))
		}
	})
}
