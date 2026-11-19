package phphosting

import (
	"strings"
	"testing"
)

func TestWithLogRotationOnlyForDetachedRuns(t *testing.T) {
	got := strings.Join(withLogRotation([]string{"run", "-d", "--name", "nd-php83-u2", "img"}), " ")
	if !strings.HasPrefix(got, "run -d --log-driver json-file --log-opt max-size=8m --log-opt max-file=3 --name nd-php83-u2 img") {
		t.Fatalf("detached run without rotated log: %s", got)
	}
	for _, args := range [][]string{
		{"run", "--rm", "img"},
		{"logs", "--since", "168h", "c"},
		{"run"},
	} {
		if strings.Join(withLogRotation(args), " ") != strings.Join(args, " ") {
			t.Fatalf("args changed: %v", args)
		}
	}
}
