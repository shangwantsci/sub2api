package deploymentlicense

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// ProfileEnvelopeType marks a signed Claude Code calibration profile handed
// down by the license server. It must match the constant in the license
// server's internal/license package.
const ProfileEnvelopeType = "sub2api-calibration-profile"

// maxProfileBytes mirrors the license server's bound on a profile.
const maxProfileBytes = 512 * 1024

// ProfileClaims is the verified content of a calibration profile envelope.
type ProfileClaims struct {
	Type       string `json:"type"`
	CLIVersion string `json:"cli_version"`
	Profile    string `json:"profile"`
	IssuedAt   int64  `json:"issued_at"`
}

// VerifyProfileEnvelope authenticates a calibration profile against the
// build-time license key. The profile decides the bytes this gateway puts on
// the wire to Anthropic, so an unverified one is never applied.
func VerifyProfileEnvelope(token string, verificationKey ed25519.PublicKey) (ProfileClaims, error) {
	var claims ProfileClaims
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return claims, fmt.Errorf("%w: expected three profile segments", ErrInvalidLease)
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, fmt.Errorf("%w: decode profile header", ErrInvalidLease)
	}
	var header leaseHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return claims, fmt.Errorf("%w: parse profile header", ErrInvalidLease)
	}
	if header.Algorithm != "EdDSA" || header.Type != "JWT" {
		return claims, fmt.Errorf("%w: unsupported profile header", ErrInvalidLease)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != ed25519.SignatureSize {
		return claims, fmt.Errorf("%w: decode profile signature", ErrInvalidLease)
	}
	if !ed25519.Verify(verificationKey, []byte(parts[0]+"."+parts[1]), signature) {
		return claims, fmt.Errorf("%w: profile signature verification failed", ErrInvalidLease)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, fmt.Errorf("%w: decode profile payload", ErrInvalidLease)
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return ProfileClaims{}, fmt.Errorf("%w: parse profile payload", ErrInvalidLease)
	}
	if claims.Type != ProfileEnvelopeType ||
		strings.TrimSpace(claims.CLIVersion) == "" ||
		strings.TrimSpace(claims.Profile) == "" ||
		claims.IssuedAt <= 0 ||
		len(claims.Profile) > maxProfileBytes {
		return ProfileClaims{}, fmt.Errorf("%w: incomplete profile claims", ErrInvalidLease)
	}
	return claims, nil
}
