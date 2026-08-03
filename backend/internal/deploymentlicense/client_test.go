package deploymentlicense

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientRenewSignsCanonicalRequest(t *testing.T) {
	identity, err := LoadOrCreateIdentity(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publicKeyBytes, err := base64.RawStdEncoding.DecodeString(identity.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/renew" {
			t.Fatalf("path = %q, want /v1/renew", r.URL.Path)
		}
		var request RenewRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		signature, err := base64.RawStdEncoding.DecodeString(request.Signature)
		if err != nil {
			t.Fatal(err)
		}
		message := CanonicalRenewalMessage(
			request.InstanceID,
			request.MachineHash,
			request.Timestamp,
			request.Nonce,
			request.Version,
			request.BuildCommit,
			request.BuildType,
			request.ImageDigest,
			request.AccountCount,
			request.UserCount,
		)
		if !ed25519.Verify(ed25519.PublicKey(publicKeyBytes), []byte(message), signature) {
			t.Fatal("renewal signature verification failed")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"lease":"signed-lease"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, &http.Client{Timeout: time.Second}, "1.0.0")
	result, err := client.Renew(t.Context(), identity, "instance-a", "machine-a", "1.0.0", "abc123", "release", "sha256:digest", 10, 20)
	if err != nil {
		t.Fatalf("Renew() error = %v", err)
	}
	if result.Lease != "signed-lease" {
		t.Fatalf("Renew() lease = %q", result.Lease)
	}
	if result.CalibrationProfile != "" {
		t.Fatalf("Renew() unexpectedly returned a profile: %q", result.CalibrationProfile)
	}
}
