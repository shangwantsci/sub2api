package deploymentlicense

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func signTestLease(t *testing.T, privateKey ed25519.PrivateKey, claims LeaseClaims) string {
	t.Helper()
	headerJSON, err := json.Marshal(leaseHeader{Algorithm: "EdDSA", Type: "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	header := base64.RawURLEncoding.EncodeToString(headerJSON)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	signingInput := header + "." + payload
	signature := base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, []byte(signingInput)))
	return signingInput + "." + signature
}

func TestVerifyLeaseAndBinding(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	token := signTestLease(t, privateKey, LeaseClaims{
		Type:              LeaseType,
		CustomerID:        "customer-a",
		InstanceID:        "instance-a",
		MachineHash:       "machine-a",
		InstancePublicKey: "instance-key-a",
		Version:           "1.2.3",
		BuildType:         "release",
		IssuedAt:          now.Unix(),
		ExpiresAt:         now.Add(24 * time.Hour).Unix(),
		Sequence:          1,
	})
	claims, err := VerifyLease(token, publicKey)
	if err != nil {
		t.Fatalf("VerifyLease() error = %v", err)
	}
	if claims.CustomerID != "customer-a" || claims.InstanceID != "instance-a" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if err := ValidateLeaseBinding(claims, "machine-a", "instance-key-a"); err != nil {
		t.Fatalf("ValidateLeaseBinding() error = %v", err)
	}
	if err := ValidateLeaseBinding(claims, "machine-b", "instance-key-a"); err == nil {
		t.Fatal("expected machine mismatch error")
	}
}

func TestVerifyLeaseRejectsTamper(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	token := signTestLease(t, privateKey, LeaseClaims{
		Type:              LeaseType,
		CustomerID:        "customer-a",
		InstanceID:        "instance-a",
		MachineHash:       "machine-a",
		InstancePublicKey: "instance-key-a",
		IssuedAt:          now.Unix(),
		ExpiresAt:         now.Add(time.Hour).Unix(),
		Sequence:          1,
	})
	parts := []byte(token)
	parts[len(parts)/2] ^= 1
	if _, err := VerifyLease(string(parts), publicKey); err == nil {
		t.Fatal("expected tampered lease rejection")
	}
}

func TestParsePublicKeyBase64(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.RawStdEncoding.EncodeToString(publicKey)
	parsed, err := ParsePublicKey(encoded)
	if err != nil {
		t.Fatalf("ParsePublicKey() error = %v", err)
	}
	if !equalBytes(parsed, publicKey) {
		t.Fatal("parsed public key does not match")
	}
}

func TestCanonicalRenewalMessage(t *testing.T) {
	got := CanonicalRenewalMessage("i", "m", 123, "n", "v", "commit", "release", "sha256:digest", 10, 20)
	want := "i\nm\n123\nn\nv\ncommit\nrelease\nsha256:digest\n10\n20"
	if got != want {
		t.Fatalf("CanonicalRenewalMessage() = %q, want %q", got, want)
	}
}
