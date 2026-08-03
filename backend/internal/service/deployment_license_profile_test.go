package service

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/deploymentlicense"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

type recordingProfilePublisher struct {
	calls    int
	last     string
	failWith error
}

func (p *recordingProfilePublisher) PublishClaudeCalibratedProfile(
	_ context.Context, raw []byte,
) (*claude.CalibratedProfile, error) {
	p.calls++
	p.last = string(raw)
	if p.failWith != nil {
		return nil, p.failWith
	}
	return &claude.CalibratedProfile{}, nil
}

// signProfileEnvelopeForTest mirrors license.SignProfileEnvelope on the
// operator side. The cross-repo format itself is pinned by
// deploymentlicense.TestClientAcceptsProfileEnvelopeFromLicenseServer.
func signProfileEnvelopeForTest(t *testing.T, key ed25519.PrivateKey, cliVersion, profile string) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "EdDSA", "typ": "JWT", "v": 1})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(deploymentlicense.ProfileClaims{
		Type:       deploymentlicense.ProfileEnvelopeType,
		CLIVersion: cliVersion,
		Profile:    profile,
		IssuedAt:   1_800_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	return signingInput + "." +
		base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(signingInput)))
}

func newProfileTestService(t *testing.T) (*DeploymentLicenseService, ed25519.PrivateKey, *recordingProfilePublisher) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &recordingProfilePublisher{}
	service := &DeploymentLicenseService{enabled: true, verificationKey: publicKey}
	service.SetCalibrationProfilePublisher(publisher)
	return service, privateKey, publisher
}

const testCalibrationProfile = `{"schema_version":1,"cli_version":"2.1.218"}`

func TestApplyCalibrationProfilePublishesVerifiedProfile(t *testing.T) {
	service, key, publisher := newProfileTestService(t)
	envelope := signProfileEnvelopeForTest(t, key, "2.1.218", testCalibrationProfile)

	service.applyCalibrationProfile(context.Background(), envelope)

	if publisher.calls != 1 {
		t.Fatalf("publisher called %d times, want 1", publisher.calls)
	}
	// Exact bytes matter: the gateway validates the profile it stores.
	if publisher.last != testCalibrationProfile {
		t.Errorf("published profile = %q", publisher.last)
	}
}

// The profile decides outbound request bytes, so anything not signed by the
// operator's key must be dropped.
func TestApplyCalibrationProfileRejectsUntrustedEnvelopes(t *testing.T) {
	_, foreignKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	service, key, publisher := newProfileTestService(t)

	cases := map[string]string{
		"empty":         "",
		"whitespace":    "   ",
		"garbage":       "not-a-token",
		"foreign key":   signProfileEnvelopeForTest(t, foreignKey, "2.1.218", testCalibrationProfile),
		"wrong type":    signLeaseShapedEnvelope(t, key),
		"empty profile": signProfileEnvelopeForTest(t, key, "2.1.218", ""),
		"no version":    signProfileEnvelopeForTest(t, key, "", testCalibrationProfile),
	}
	for name, envelope := range cases {
		t.Run(name, func(t *testing.T) {
			publisher.calls = 0
			service.applyCalibrationProfile(context.Background(), envelope)
			if publisher.calls != 0 {
				t.Errorf("untrusted profile was published (%d calls)", publisher.calls)
			}
		})
	}
}

// signLeaseShapedEnvelope signs a payload whose type is a lease, not a profile.
func signLeaseShapedEnvelope(t *testing.T, key ed25519.PrivateKey) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "EdDSA", "typ": "JWT", "v": 1})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{
		"type": deploymentlicense.LeaseType, "cli_version": "2.1.218",
		"profile": testCalibrationProfile, "issued_at": 1_800_000_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload)
	return signingInput + "." +
		base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(signingInput)))
}

// Renewal runs hourly; an unchanged profile must not rewrite the setting and
// invalidate the gateway's in-process cache every time.
func TestApplyCalibrationProfileSkipsUnchangedVersion(t *testing.T) {
	service, key, publisher := newProfileTestService(t)
	envelope := signProfileEnvelopeForTest(t, key, "2.1.218", testCalibrationProfile)

	for range 5 {
		service.applyCalibrationProfile(context.Background(), envelope)
	}
	if publisher.calls != 1 {
		t.Fatalf("publisher called %d times for an unchanged profile, want 1", publisher.calls)
	}

	updated := signProfileEnvelopeForTest(t, key, "2.1.219", `{"schema_version":1,"cli_version":"2.1.219"}`)
	service.applyCalibrationProfile(context.Background(), updated)
	if publisher.calls != 2 {
		t.Fatalf("a new CLI version was not applied (%d calls)", publisher.calls)
	}
}

// A rejected profile must be retried on the next renewal, not marked applied.
func TestApplyCalibrationProfileRetriesAfterPublishFailure(t *testing.T) {
	service, key, publisher := newProfileTestService(t)
	publisher.failWith = errors.New("schema mismatch")
	envelope := signProfileEnvelopeForTest(t, key, "2.1.218", testCalibrationProfile)

	service.applyCalibrationProfile(context.Background(), envelope)
	if publisher.calls != 1 {
		t.Fatalf("publisher called %d times, want 1", publisher.calls)
	}

	publisher.failWith = nil
	service.applyCalibrationProfile(context.Background(), envelope)
	if publisher.calls != 2 {
		t.Fatalf("a previously failed profile was not retried (%d calls)", publisher.calls)
	}
}

// Community deployments have no publisher wired; this must be a no-op, not a
// nil dereference on the renewal path.
func TestApplyCalibrationProfileWithoutPublisherIsSafe(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	service := &DeploymentLicenseService{enabled: true, verificationKey: publicKey}
	service.applyCalibrationProfile(context.Background(),
		signProfileEnvelopeForTest(t, privateKey, "2.1.218", testCalibrationProfile))
}
