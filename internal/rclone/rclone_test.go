package rclone

import "testing"

func TestExtractGoogleAuthCode(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{
			input:    "http://127.0.0.1:53682/?state=xyz&code=4/0AbCdEf_123",
			expected: "4/0AbCdEf_123",
		},
		{
			input:    "http://127.0.0.1:53682/auth?state=xyz&code=4%2F0AbCdEf_123&scope=drive",
			expected: "4/0AbCdEf_123",
		},
		{
			input:    "4/0AbCdEf_123",
			expected: "4/0AbCdEf_123",
		},
		{
			input:    "  4/0AbCdEf_123  ",
			expected: "4/0AbCdEf_123",
		},
	}

	for _, c := range cases {
		got := ExtractGoogleAuthCode(c.input)
		if got != c.expected {
			t.Errorf("ExtractGoogleAuthCode(%q) = %q; want %q", c.input, got, c.expected)
		}
	}
}
