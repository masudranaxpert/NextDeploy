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

func TestClassify_GrokLogLevel(t *testing.T) {
	tests := []struct {
		line string
		want LogLevel
	}{
		{
			line: "WARN [INFO] 21:02:54 upload.utils.web_scrape: [Scrape] Extracted 68,460 → 8,660 chars",
			want: LvInfo,
		},
		{
			line: "ERROR [INFO] 21:02:59 upload.utils.web_scrape: [Scrape] Extracted 68,027 → 8,511 chars",
			want: LvInfo,
		},
		{
			line: "INFO [INFO] 21:02:49 llm.services: Generating LLM completion.",
			want: LvInfo,
		},
		{
			line: "DBG [DEBUG] 21:02:51 llm.services: [GLM] Usage: 1250+28=1278 tokens",
			want: LvDebug,
		},
		{
			line: "ERROR [ERROR] 21:02:59 DB connection failed",
			want: LvError,
		},
		{
			line: "WARN [WARN] 21:02:59 Fallback initiated",
			want: LvWarn,
		},
		// Dokploy-style tests: timestamp first with [INFO] / [WARNING]
		{
			line: "2026-10-18 13:56:49 [WARNING] upload.tasks: no site_content_id on task pk=9470",
			want: LvWarn,
		},
		{
			line: "2026-10-18 13:56:52 [INFO] upload.service: created movie title=\"Pinoy Sexy Classics\"",
			want: LvInfo,
		},
		// Non-error key/values should not trigger error
		{
			line: "NOTICE [Job sync-1m] Finished in 326ms, failed: false, skipped: false, error: none",
			want: LvInfo,
		},
		{
			line: "request done, error: null",
			want: LvInfo,
		},
		{
			line: "checks passed, failures=0",
			want: LvInfo,
		},
		// Structured JSON logs
		{
			line: `{"level":"error","message":"DB connection lost"}`,
			want: LvError,
		},
		{
			line: `{"level":"info","message":"server listening on port 8080"}`,
			want: LvInfo,
		},
		// Real errors with keywords
		{
			line: "connection failed: no such host",
			want: LvError,
		},
	}

	for _, tc := range tests {
		got := classify(tc.line)
		if got != tc.want {
			t.Errorf("classify(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
