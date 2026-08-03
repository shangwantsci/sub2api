package deploymentlicense

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const leaseFileName = "lease.jwt"

func LoadCachedLease(dataDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dataDir, leaseFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read cached deployment lease: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

func SaveCachedLease(dataDir, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("cannot cache empty deployment lease")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create deployment license data dir: %w", err)
	}
	path := filepath.Join(dataDir, leaseFileName)
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("write cached deployment lease: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("commit cached deployment lease: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	return nil
}
