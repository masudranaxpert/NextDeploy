package cmd

import (
	"testing"
)

func TestPosixQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "''"},
		{"abc", "abc"},
		{"my-app_1.0:test", "my-app_1.0:test"},
		{"hello world", "'hello world'"},
		{"print(x)", "'print(x)'"},
		{"foo'bar", `'foo'\''bar'`},
		{"$VAR", "'$VAR'"},
		{"a; b", "'a; b'"},
	}

	for _, tc := range tests {
		got := posixQuote(tc.input)
		if got != tc.want {
			t.Errorf("posixQuote(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestPosixJoin(t *testing.T) {
	args := []string{"python", "-c", "print('hello')"}
	got := posixJoin(args)
	want := "python -c 'print('\\''hello'\\'')'"
	if got != want {
		t.Errorf("posixJoin(%v) = %q, want %q", args, got, want)
	}
}

func TestIsSafeTarPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"src/sub/app.js", true},
		{".env.example", true},
		{"", false},
		{"../evil", false},
		{"/root/evil", false},
		{"a/../../b", false},
		{"..", false},
	}

	for _, tc := range tests {
		got := isSafeTarPath(tc.path)
		if got != tc.want {
			t.Errorf("isSafeTarPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestHasHelpFlag(t *testing.T) {
	if !hasHelpFlag([]string{"-h"}) {
		t.Errorf("expected -h to be detected as help")
	}
	if !hasHelpFlag([]string{"foo", "--help"}) {
		t.Errorf("expected --help to be detected as help")
	}
	if hasHelpFlag([]string{"foo", "bar"}) {
		t.Errorf("expected no help flag")
	}
}
