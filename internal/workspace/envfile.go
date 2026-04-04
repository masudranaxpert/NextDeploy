package workspace

import (
	"os"
	"path/filepath"
)

func (s *Store) DotEnvPath(baseDir string) string {
	return filepath.Join(baseDir, ".env")
}

func (s *Store) ReadDotEnv(baseDir string) (string, error) {
	p := s.DotEnvPath(baseDir)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(b), nil
}

func (s *Store) WriteDotEnv(baseDir string, content string) error {
	p := s.DotEnvPath(baseDir)
	return os.WriteFile(p, []byte(content), 0600)
}
