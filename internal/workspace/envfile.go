package workspace

import (
	"os"
	"path/filepath"
)

func (s *Store) DotEnvPath(wsID string) string {
	return filepath.Join(s.Path(wsID), ".env")
}

func (s *Store) ReadDotEnv(wsID string) (string, error) {
	p := s.DotEnvPath(wsID)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

func (s *Store) WriteDotEnv(wsID string, content string) error {
	p := s.DotEnvPath(wsID)
	return os.WriteFile(p, []byte(content), 0600)
}
