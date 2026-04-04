package workspace

import "fmt"

// FormatByteSize renders n as a compact human-readable size (e.g. 64.7 MB).
func FormatByteSize(n int64) string {
	if n < 0 {
		n = 0
	}
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	v := float64(n)
	labels := []string{"KB", "MB", "GB", "TB"}
	for i := 0; i < len(labels); i++ {
		v /= 1024
		if v < 1024 || i == len(labels)-1 {
			if v >= 100 {
				return fmt.Sprintf("%.0f %s", v, labels[i])
			}
			return fmt.Sprintf("%.1f %s", v, labels[i])
		}
	}
	return fmt.Sprintf("%.1f TB", v)
}
