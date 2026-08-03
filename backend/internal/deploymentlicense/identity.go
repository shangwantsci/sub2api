package deploymentlicense

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const identityFileName = "instance-identity.json"

type Identity struct {
	InstallationID string `json:"installation_id"`
	PublicKey      string `json:"public_key"`
	PrivateKey     string `json:"private_key"`

	privateKey  ed25519.PrivateKey
	hostSignals int
}

func LoadOrCreateIdentity(dataDir string) (*Identity, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create deployment license data dir: %w", err)
	}
	path := filepath.Join(dataDir, identityFileName)
	if raw, err := os.ReadFile(path); err == nil {
		var identity Identity
		if err := json.Unmarshal(raw, &identity); err != nil {
			return nil, fmt.Errorf("parse deployment identity: %w", err)
		}
		if err := identity.initialize(); err != nil {
			return nil, err
		}
		_ = os.Chmod(path, 0o600)
		return &identity, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read deployment identity: %w", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate deployment identity key: %w", err)
	}
	installBytes := make([]byte, 16)
	if _, err := rand.Read(installBytes); err != nil {
		return nil, fmt.Errorf("generate deployment installation id: %w", err)
	}
	identity := &Identity{
		InstallationID: hex.EncodeToString(installBytes),
		PublicKey:      base64.RawStdEncoding.EncodeToString(publicKey),
		PrivateKey:     base64.RawStdEncoding.EncodeToString(privateKey),
		privateKey:     privateKey,
	}
	encoded, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode deployment identity: %w", err)
	}
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, encoded, 0o600); err != nil {
		return nil, fmt.Errorf("write deployment identity: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("commit deployment identity: %w", err)
	}
	return identity, nil
}

func (i *Identity) initialize() error {
	if strings.TrimSpace(i.InstallationID) == "" || strings.TrimSpace(i.PublicKey) == "" || strings.TrimSpace(i.PrivateKey) == "" {
		return fmt.Errorf("deployment identity is incomplete")
	}
	privateKey, err := base64.RawStdEncoding.DecodeString(i.PrivateKey)
	if err != nil || len(privateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("deployment identity private key is invalid")
	}
	publicKey, err := base64.RawStdEncoding.DecodeString(i.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("deployment identity public key is invalid")
	}
	derivedPublic := ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey)
	if !equalBytes(publicKey, derivedPublic) {
		return fmt.Errorf("deployment identity keypair does not match")
	}
	i.privateKey = ed25519.PrivateKey(privateKey)
	return nil
}

func (i *Identity) Sign(message string) string {
	return base64.RawStdEncoding.EncodeToString(ed25519.Sign(i.privateKey, []byte(message)))
}

func (i *Identity) HostSignalCount() int {
	return i.hostSignals
}

func (i *Identity) MachineHash(explicitMachineID string) (string, error) {
	signals := make([]string, 0, 5)
	if explicit := strings.TrimSpace(explicitMachineID); explicit != "" {
		signals = append(signals, "explicit:"+explicit)
	} else {
		for _, path := range []string{
			"/host/etc/machine-id",
			"/host/sys/class/dmi/id/product_uuid",
			"/host/sys/class/dmi/id/board_serial",
			"/etc/machine-id",
			"/sys/class/dmi/id/product_uuid",
			"/sys/class/dmi/id/board_serial",
		} {
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			value := strings.TrimSpace(string(raw))
			if value != "" && !containsSignal(signals, value) {
				signals = append(signals, path+":"+value)
			}
		}
	}
	i.hostSignals = len(signals)
	signals = append(signals, "installation:"+i.InstallationID, "instance_key:"+i.PublicKey)
	sum := sha256.Sum256([]byte(strings.Join(signals, "\n")))
	return hex.EncodeToString(sum[:]), nil
}

func containsSignal(signals []string, value string) bool {
	for _, signal := range signals {
		if strings.HasSuffix(signal, ":"+value) {
			return true
		}
	}
	return false
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var result byte
	for idx := range a {
		result |= a[idx] ^ b[idx]
	}
	return result == 0
}
