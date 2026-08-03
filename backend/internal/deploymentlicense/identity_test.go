package deploymentlicense

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityPersistsAndBindsExplicitMachine(t *testing.T) {
	dataDir := t.TempDir()
	first, err := LoadOrCreateIdentity(dataDir)
	if err != nil {
		t.Fatalf("LoadOrCreateIdentity() error = %v", err)
	}
	firstHash, err := first.MachineHash("host-a")
	if err != nil {
		t.Fatalf("MachineHash() error = %v", err)
	}
	second, err := LoadOrCreateIdentity(dataDir)
	if err != nil {
		t.Fatalf("second LoadOrCreateIdentity() error = %v", err)
	}
	secondHash, err := second.MachineHash("host-a")
	if err != nil {
		t.Fatalf("second MachineHash() error = %v", err)
	}
	if first.InstallationID != second.InstallationID || first.PublicKey != second.PublicKey || firstHash != secondHash {
		t.Fatal("persisted identity changed across reload")
	}
	otherHash, err := second.MachineHash("host-b")
	if err != nil {
		t.Fatalf("other MachineHash() error = %v", err)
	}
	if otherHash == firstHash {
		t.Fatal("machine hash did not change for a different explicit host id")
	}
	info, err := os.Stat(filepath.Join(dataDir, identityFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("identity permissions are too broad: %o", info.Mode().Perm())
	}
}

func TestIdentitySignatureVerifies(t *testing.T) {
	identity, err := LoadOrCreateIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := base64.RawStdEncoding.DecodeString(identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	message := "renewal-message"
	signature, err := base64.RawStdEncoding.DecodeString(identity.Sign(message))
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ed25519.PublicKey(publicKey), []byte(message), signature) {
		t.Fatal("identity signature verification failed")
	}
}
