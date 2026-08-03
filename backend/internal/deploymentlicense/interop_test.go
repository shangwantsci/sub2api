package deploymentlicense

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The fixture below was signed by the operator-side license server
// (../sub2api-license-server, internal/license.SignLease) using a deterministic
// Ed25519 key. It pins the exact wire format a customer image must accept: if
// either side changes the header, a claim name, or the signing input, this test
// fails here instead of after a customer deployment stops renewing.
const (
	interopServerPublicKey = "ebVWLo/mVPlAeLES6KmLp5AfhTrmlb7X4OORC60ElmQ"
	interopInstanceKey     = "J07WsmgF0v6NBgUp7povKHY7aXbZxf0d7jj/NrO3aTk"
	interopMachineHash     = "abababababababababababababababababababababababababababababababab"
	interopLease           = "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCIsInYiOjF9.eyJ0eXBlIjoic3ViMmFwaS1kZXBsb3ltZW50LWxlYXNlIiwiY3VzdG9tZXJfaWQiOiJjdXN0b21lci1hIiwiaW5zdGFuY2VfaWQiOiJpbnN0X2ZpeHR1cmUiLCJtYWNoaW5lX2hhc2giOiJhYmFiYWJhYmFiYWJhYmFiYWJhYmFiYWJhYmFiYWJhYmFiYWJhYmFiYWJhYmFiYWJhYmFiYWJhYmFiYWJhYmFiIiwiaW5zdGFuY2VfcHVibGljX2tleSI6IkowN1dzbWdGMHY2TkJnVXA3cG92S0hZN2FYYlp4ZjBkN2pqL05yTzNhVGsiLCJ2ZXJzaW9uIjoiMC4xLjE1NiIsImJ1aWxkX2NvbW1pdCI6ImFiYzEyMzQ1IiwiYnVpbGRfdHlwZSI6InJlbGVhc2UiLCJpbWFnZV9kaWdlc3QiOiJzaGEyNTY6Zml4dHVyZSIsImZlYXR1cmVzIjpbImdhdGV3YXkiLCJhZG1pbiJdLCJtYXhfYWNjb3VudHMiOjUwLCJtYXhfdXNlcnMiOjEwLCJpc3N1ZWRfYXQiOjE4MDAwMDAwMDAsImV4cGlyZXNfYXQiOjE4MDAwODY0MDAsInNlcXVlbmNlIjo3fQ.Uoc_uIy_pdLWqxM8bR68Z41_ojCttgesSD2BPx9scz8W3T7Ru-P5EM710NeyprH37hq011kvGBgTTvwFpJN3Cg"
)

func TestClientAcceptsLeaseSignedByLicenseServer(t *testing.T) {
	verificationKey, err := ParsePublicKey(interopServerPublicKey)
	if err != nil {
		t.Fatalf("parse server public key: %v", err)
	}
	claims, err := VerifyLease(interopLease, verificationKey)
	if err != nil {
		t.Fatalf("client rejected a lease signed by the license server: %v", err)
	}
	if claims.CustomerID != "customer-a" || claims.InstanceID != "inst_fixture" {
		t.Fatalf("unexpected identity claims: %+v", claims)
	}
	if claims.MaxAccounts != 50 || claims.MaxUsers != 10 {
		t.Fatalf("limits did not decode: accounts=%d users=%d", claims.MaxAccounts, claims.MaxUsers)
	}
	if len(claims.Features) != 2 || claims.Features[0] != "gateway" || claims.Features[1] != "admin" {
		t.Fatalf("features did not decode: %v", claims.Features)
	}
	if claims.Sequence != 7 {
		t.Fatalf("sequence = %d, want 7", claims.Sequence)
	}
	if err := ValidateLeaseBinding(claims, interopMachineHash, interopInstanceKey); err != nil {
		t.Fatalf("binding check rejected a matching machine and instance key: %v", err)
	}
}

// Copying the image and its data volume to a second host changes the machine
// hash, so the cached lease must be refused before it is ever trusted.
func TestServerLeaseIsRejectedOnAnotherMachine(t *testing.T) {
	verificationKey, err := ParsePublicKey(interopServerPublicKey)
	if err != nil {
		t.Fatalf("parse server public key: %v", err)
	}
	claims, err := VerifyLease(interopLease, verificationKey)
	if err != nil {
		t.Fatalf("verify lease: %v", err)
	}
	otherMachine := strings.Repeat("cd", 32)
	if err := ValidateLeaseBinding(claims, otherMachine, interopInstanceKey); err == nil {
		t.Fatal("a lease from another machine must not validate")
	}
	otherInstanceKey := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := ValidateLeaseBinding(claims, interopMachineHash, otherInstanceKey); err == nil {
		t.Fatal("a lease bound to a different instance key must not validate")
	}
}

// A customer who swaps in their own signing key must not be able to reuse a
// lease minted by the real license server.
func TestServerLeaseFailsAgainstForeignVerificationKey(t *testing.T) {
	foreignKey, err := ParsePublicKey(interopInstanceKey)
	if err != nil {
		t.Fatalf("parse foreign key: %v", err)
	}
	if _, err := VerifyLease(interopLease, foreignKey); err == nil {
		t.Fatal("lease verified against a key that did not sign it")
	}
}

// interopProfileEnvelope was signed by the license server's
// license.SignProfileEnvelope with the same deterministic key as the lease
// fixture above. It pins the profile-distribution wire format across repos.
const interopProfileEnvelope = "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCIsInYiOjF9.eyJ0eXBlIjoic3ViMmFwaS1jYWxpYnJhdGlvbi1wcm9maWxlIiwiY2xpX3ZlcnNpb24iOiIyLjEuMjE4IiwicHJvZmlsZSI6IntcInNjaGVtYV92ZXJzaW9uXCI6MSxcImNsaV92ZXJzaW9uXCI6XCIyLjEuMjE4XCIsXCJjYXB0dXJlZF9hdFwiOlwiMjAyNi0wOC0wMVQwMDowMDowMFpcIixcInNvdXJjZVwiOlwiY2MtY2FsaWJyYXRlXCIsXCJoZWFkZXJzXCI6e1widGVtcGxhdGVcIjp7XCJ1c2VyLWFnZW50XCI6XCJjbGF1ZGUtY2xpLzIuMS4yMThcIn0sXCJhYnNlbnRcIjpbXCJ4LWNsaWVudC1yZXF1ZXN0LWlkXCJdfSxcImJldGFfcnVsZXNcIjp7XCJtZXNzYWdlczpvcHVzXCI6W1wiYmV0YS1hXCJdfSxcImd1YXJkXCI6e1wic2FsdF92ZXJpZmllZFwiOnRydWUsXCJjaGVja2VkXCI6OCxcIm9rXCI6OH19IiwiaXNzdWVkX2F0IjoxODAwMDAwMDAwfQ.dTeMnZDpst1roU_uZUyfgHUI6KPioOvi-7LK9jMT_NxmsdpytrVjAoOzZUey-LfTz7geN_0QhqjXvtHR_zxEBA"

func TestClientAcceptsProfileEnvelopeFromLicenseServer(t *testing.T) {
	verificationKey, err := ParsePublicKey(interopServerPublicKey)
	if err != nil {
		t.Fatalf("parse server public key: %v", err)
	}
	claims, err := VerifyProfileEnvelope(interopProfileEnvelope, verificationKey)
	if err != nil {
		t.Fatalf("client rejected a profile signed by the license server: %v", err)
	}
	if claims.CLIVersion != "2.1.218" {
		t.Fatalf("cli_version = %q", claims.CLIVersion)
	}
	// The embedded profile must survive transport byte-for-byte, because the
	// gateway validates and stores exactly these bytes.
	if !strings.Contains(claims.Profile, `"schema_version":1`) ||
		!strings.Contains(claims.Profile, `"salt_verified":true`) {
		t.Fatalf("profile payload did not survive transport: %s", claims.Profile)
	}
}

// A profile is only trusted from the operator's signing key: a customer that
// swaps in their own key must not be able to inject wire headers.
func TestProfileEnvelopeRejectsForeignKeyAndTampering(t *testing.T) {
	foreignKey, err := ParsePublicKey(interopInstanceKey)
	if err != nil {
		t.Fatalf("parse foreign key: %v", err)
	}
	if _, err := VerifyProfileEnvelope(interopProfileEnvelope, foreignKey); err == nil {
		t.Fatal("profile verified against a key that did not sign it")
	}

	verificationKey, err := ParsePublicKey(interopServerPublicKey)
	if err != nil {
		t.Fatalf("parse server public key: %v", err)
	}
	segments := strings.Split(interopProfileEnvelope, ".")
	tampered := segments[0] + "." + base64.RawURLEncoding.EncodeToString(
		[]byte(`{"type":"sub2api-calibration-profile","cli_version":"9.9.9","profile":"{}","issued_at":1}`),
	) + "." + segments[2]
	if _, err := VerifyProfileEnvelope(tampered, verificationKey); err == nil {
		t.Fatal("a tampered profile payload must not verify")
	}
}

// A lease and a profile envelope are signed by the same key but must never be
// interchangeable.
func TestLeaseAndProfileEnvelopesAreNotInterchangeable(t *testing.T) {
	verificationKey, err := ParsePublicKey(interopServerPublicKey)
	if err != nil {
		t.Fatalf("parse server public key: %v", err)
	}
	if _, err := VerifyProfileEnvelope(interopLease, verificationKey); err == nil {
		t.Fatal("a lease must not verify as a profile envelope")
	}
	if _, err := VerifyLease(interopProfileEnvelope, verificationKey); err == nil {
		t.Fatal("a profile envelope must not verify as a lease")
	}
}

func TestVerifyProfileEnvelopeRejectsMalformedInput(t *testing.T) {
	verificationKey, err := ParsePublicKey(interopServerPublicKey)
	if err != nil {
		t.Fatalf("parse server public key: %v", err)
	}
	for name, token := range map[string]string{
		"empty":        "",
		"one segment":  "abc",
		"two segments": "abc.def",
		"not base64":   "!!!.???.###",
	} {
		if _, err := VerifyProfileEnvelope(token, verificationKey); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}
