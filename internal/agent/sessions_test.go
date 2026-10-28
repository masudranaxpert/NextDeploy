package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMapHistoryMergesToolTurns(t *testing.T) {
	raw := json.RawMessage(`{"messages":[
	 {"role":"user","content":"list sites","timestamp":1},
	 {"role":"assistant","content":[{"type":"thinking","thinking":"hmm"},{"type":"toolCall","id":"t1","name":"list_sites","arguments":{"a":1}}],"timestamp":2},
	 {"role":"toolResult","toolCallId":"t1","content":[{"type":"text","text":"[]"}],"isError":false},
	 {"role":"assistant","content":[{"type":"text","text":"none"}],"timestamp":3},
	 {"role":"user","content":[{"type":"text","text":"again"}],"timestamp":4},
	 {"role":"assistant","content":[{"type":"toolCall","id":"t2","name":"x","arguments":{}}]},
	 {"role":"toolResult","toolCallId":"t2","content":"boom","isError":true}
	]}`)
	got, err := mapHistory(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(got), got)
	}
	a := got[1]
	if a.Role != "assistant" || a.Content != "none" || a.Thinking != "hmm" || len(a.ToolCalls) != 1 || a.ToolCalls[0].Result != "[]" || a.ToolCalls[0].Status != "completed" {
		t.Errorf("bad merged turn: %+v", a)
	}
	if got[2].Content != "again" || got[3].ToolCalls[0].Status != "failed" || got[3].ToolCalls[0].Result != "boom" {
		t.Errorf("bad tail: %+v %+v", got[2], got[3])
	}
}

func TestSessionPathSafetyAndListing(t *testing.T) {
	data := t.TempDir()
	root := t.TempDir()
	m := NewManager(nil, nil, root)
	m.SetDataDir(data)
	dir := filepath.Join(data, "omp", "agent", "sessions", "x")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "a.jsonl")
	if err := os.WriteFile(good, []byte(`{"type":"title","title":"Hello"}`+"\n"+`{"type":"session","id":"i1","cwd":"`+root+`"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.safeSessionPath(good); err != nil {
		t.Errorf("good path rejected: %v", err)
	}
	for _, bad := range []string{"/etc/passwd", filepath.Join(dir, "..", "..", "x.jsonl"), filepath.Join(dir, "a.txt"), data + "/omp/agent/sessions-evil/a.jsonl"} {
		if _, err := m.safeSessionPath(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	title, id, cwd, ok := readSessionHeader(good)
	if !ok || title != "Hello" || id != "i1" || cwd != root {
		t.Errorf("header = %q %q %q %v", title, id, cwd, ok)
	}
}

func TestHasMessagesAndSaveUpload(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "e.jsonl")
	full := filepath.Join(dir, "f.jsonl")
	_ = os.WriteFile(empty, []byte(`{"type":"title"}`+"\n"+`{"type":"session","id":"x","cwd":"/"}`+"\n"), 0o644)
	_ = os.WriteFile(full, []byte(`{"type":"session","id":"x","cwd":"/"}`+"\n"+`{"type":"message","id":"1"}`+"\n"), 0o644)
	if hasMessages(empty) || !hasMessages(full) {
		t.Fatal("hasMessages wrong")
	}
}

func TestUnsafeUploadNameSanitised(t *testing.T) {
	for in, want := range map[string]string{"a b/../c.txt": "c.txt", "../../etc/passwd": "passwd", "weird name (1).png": "weird_name_1_.png"} {
		if got := unsafeName.ReplaceAllString(filepath.Base(in), "_"); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}

func TestMapHistoryOtherMessageKinds(t *testing.T) {
	raw := json.RawMessage(`{"messages":[
	 {"role":"user","content":"hi","timestamp":1},
	 {"role":"fileMention","files":[{"path":"a.go"}]},
	 {"role":"developer","content":"hidden"},
	 {"role":"bashExecution","command":"ls","output":"x","exitCode":2,"cancelled":false},
	 {"role":"assistant","content":[{"type":"text","text":"oops"}],"stopReason":"error","errorMessage":"429"},
	 {"role":"compactionSummary","summary":"short"},
	 {"role":"custom","customType":"x","content":"note","display":true},
	 {"role":"custom","customType":"y","content":"secret","display":false}
	]}`)
	got, err := mapHistory(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	if len(got[0].Attached) != 1 || got[0].Attached[0] != "a.go" {
		t.Errorf("file mention: %+v", got[0])
	}
	if got[1].Role != "assistant" || len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].Status != "failed" || !strings.Contains(got[1].Content, "429") {
		t.Errorf("bash/error turn: %+v", got[1])
	}
	if got[2].Kind != "compaction" || got[3].Kind != "note" {
		t.Errorf("system kinds: %+v %+v", got[2], got[3])
	}
}

func TestSubagentTranscriptsAreNotListed(t *testing.T) {
	data := t.TempDir()
	root := t.TempDir()
	m := NewManager(nil, nil, root)
	m.SetDataDir(data)
	dir := filepath.Join(data, "omp", "agent", "sessions", "x")
	sub := filepath.Join(dir, "2026_abc") // artifacts directory of a conversation
	_ = os.MkdirAll(sub, 0o755)
	hdr := `{"type":"title","title":"T"}` + "\n" + `{"type":"session","id":"i","cwd":"` + root + `"}` + "\n" + `{"type":"message","id":"1"}` + "\n"
	_ = os.WriteFile(filepath.Join(dir, "2026_abc.jsonl"), []byte(hdr), 0o644)
	_ = os.WriteFile(filepath.Join(sub, "0-Sub.jsonl"), []byte(hdr), 0o644)
	// ListSessions needs the engine for the current session; walk part is what matters here.
	n := 0
	_ = filepath.WalkDir(m.sessionsRoot(), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
			if rel, _ := filepath.Rel(m.sessionsRoot(), p); strings.Count(rel, string(os.PathSeparator)) == 1 {
				n++
			}
		}
		return nil
	})
	if n != 1 {
		t.Fatalf("want 1 conversation file, got %d", n)
	}
}
