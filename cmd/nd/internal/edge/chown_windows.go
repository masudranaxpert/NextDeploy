//go:build windows

package edge

// ChownGroup is a no-op on Windows because POSIX group ownership does not apply.
func ChownGroup(_, _ string, _ bool) error {
	return nil
}
