package deploymentlicense

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

const LeaseType = "sub2api-deployment-lease"

var (
	ErrInvalidLease = errors.New("invalid deployment lease")
	ErrLeaseExpired = errors.New("deployment lease expired")
)

type LeaseClaims struct {
	Type              string   `json:"type"`
	CustomerID        string   `json:"customer_id"`
	InstanceID        string   `json:"instance_id"`
	MachineHash       string   `json:"machine_hash"`
	InstancePublicKey string   `json:"instance_public_key"`
	Version           string   `json:"version"`
	BuildCommit       string   `json:"build_commit,omitempty"`
	BuildType         string   `json:"build_type"`
	ImageDigest       string   `json:"image_digest,omitempty"`
	Features          []string `json:"features,omitempty"`
	MaxAccounts       int      `json:"max_accounts,omitempty"`
	MaxUsers          int      `json:"max_users,omitempty"`
	IssuedAt          int64    `json:"issued_at"`
	ExpiresAt         int64    `json:"expires_at"`
	Sequence          int64    `json:"sequence"`
}

type leaseHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}

func ParsePublicKey(value string) (ed25519.PublicKey, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, fmt.Errorf("%w: empty verification key", ErrInvalidLease)
	}
	if block, _ := pem.Decode([]byte(value)); block != nil {
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%w: parse PEM public key: %v", ErrInvalidLease, err)
		}
		key, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%w: verification key is not Ed25519", ErrInvalidLease)
		}
		return key, nil
	}
	for _, encoding := range []*base64.Encoding{
		base64.RawStdEncoding,
		base64.StdEncoding,
		base64.RawURLEncoding,
		base64.URLEncoding,
	} {
		decoded, err := encoding.DecodeString(value)
		if err == nil && len(decoded) == ed25519.PublicKeySize {
			return ed25519.PublicKey(decoded), nil
		}
	}
	return nil, fmt.Errorf("%w: verification key must be a base64 or PEM Ed25519 public key", ErrInvalidLease)
}

func VerifyLease(token string, verificationKey ed25519.PublicKey) (LeaseClaims, error) {
	var claims LeaseClaims
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return claims, fmt.Errorf("%w: expected three token segments", ErrInvalidLease)
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, fmt.Errorf("%w: decode header", ErrInvalidLease)
	}
	var header leaseHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return claims, fmt.Errorf("%w: parse header", ErrInvalidLease)
	}
	if header.Algorithm != "EdDSA" || header.Type != "JWT" {
		return claims, fmt.Errorf("%w: unsupported token header", ErrInvalidLease)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != ed25519.SignatureSize {
		return claims, fmt.Errorf("%w: decode signature", ErrInvalidLease)
	}
	if !ed25519.Verify(verificationKey, []byte(parts[0]+"."+parts[1]), signature) {
		return claims, fmt.Errorf("%w: signature verification failed", ErrInvalidLease)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims, fmt.Errorf("%w: decode payload", ErrInvalidLease)
	}
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return claims, fmt.Errorf("%w: parse payload", ErrInvalidLease)
	}
	if claims.Type != LeaseType || claims.CustomerID == "" || claims.InstanceID == "" ||
		claims.MachineHash == "" || claims.InstancePublicKey == "" ||
		claims.IssuedAt <= 0 || claims.ExpiresAt <= claims.IssuedAt || claims.Sequence <= 0 {
		return LeaseClaims{}, fmt.Errorf("%w: incomplete claims", ErrInvalidLease)
	}
	return claims, nil
}

func ValidateLeaseBinding(claims LeaseClaims, machineHash, instancePublicKey string) error {
	if claims.MachineHash != machineHash {
		return fmt.Errorf("%w: machine binding mismatch", ErrInvalidLease)
	}
	if claims.InstancePublicKey != instancePublicKey {
		return fmt.Errorf("%w: instance key binding mismatch", ErrInvalidLease)
	}
	return nil
}

func LeaseExpiry(claims LeaseClaims) time.Time {
	return time.Unix(claims.ExpiresAt, 0).UTC()
}

func CanonicalRenewalMessage(
	instanceID, machineHash string,
	timestamp int64,
	nonce, version, buildCommit, buildType, imageDigest string,
	accountCount, userCount int,
) string {
	return strings.Join([]string{
		strings.TrimSpace(instanceID),
		strings.TrimSpace(machineHash),
		fmt.Sprintf("%d", timestamp),
		strings.TrimSpace(nonce),
		strings.TrimSpace(version),
		strings.TrimSpace(buildCommit),
		strings.TrimSpace(buildType),
		strings.TrimSpace(imageDigest),
		fmt.Sprintf("%d", accountCount),
		fmt.Sprintf("%d", userCount),
	}, "\n")
}
