package deploymentlicense

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxResponseBodyBytes = 1 << 20

type Client struct {
	baseURL   string
	http      *http.Client
	userAgent string
}

type ActivateRequest struct {
	ActivationCode    string `json:"activation_code"`
	MachineHash       string `json:"machine_hash"`
	InstancePublicKey string `json:"instance_public_key"`
	InstallationID    string `json:"installation_id"`
	Version           string `json:"version"`
	BuildCommit       string `json:"build_commit,omitempty"`
	BuildType         string `json:"build_type"`
	ImageDigest       string `json:"image_digest,omitempty"`
	AccountCount      int    `json:"account_count,omitempty"`
	UserCount         int    `json:"user_count,omitempty"`
}

type RenewRequest struct {
	InstanceID   string `json:"instance_id"`
	MachineHash  string `json:"machine_hash"`
	Timestamp    int64  `json:"timestamp"`
	Nonce        string `json:"nonce"`
	Version      string `json:"version"`
	BuildCommit  string `json:"build_commit,omitempty"`
	BuildType    string `json:"build_type"`
	ImageDigest  string `json:"image_digest,omitempty"`
	AccountCount int    `json:"account_count,omitempty"`
	UserCount    int    `json:"user_count,omitempty"`
	Signature    string `json:"signature"`
}

type LeaseResponse struct {
	Lease string `json:"lease"`
	// CalibrationProfile is an optional signed Claude Code wire profile. It is
	// empty when the operator has not published one.
	CalibrationProfile string `json:"calibration_profile"`
}

// LeaseResult is what a successful activation or renewal yields.
type LeaseResult struct {
	Lease              string
	CalibrationProfile string
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("license server returned HTTP %d", e.StatusCode)
	}
	return fmt.Sprintf("license server returned HTTP %d: %s", e.StatusCode, e.Message)
}

func NewClient(baseURL string, httpClient *http.Client, version string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:   strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:      httpClient,
		userAgent: "sub2api-license/" + strings.TrimSpace(version),
	}
}

func (c *Client) Activate(ctx context.Context, request ActivateRequest) (LeaseResult, error) {
	var response LeaseResponse
	if err := c.post(ctx, "/v1/activate", request, &response); err != nil {
		return LeaseResult{}, err
	}
	if strings.TrimSpace(response.Lease) == "" {
		return LeaseResult{}, fmt.Errorf("license server activation response omitted lease")
	}
	return LeaseResult{Lease: response.Lease, CalibrationProfile: response.CalibrationProfile}, nil
}

func (c *Client) Renew(
	ctx context.Context,
	identity *Identity,
	instanceID string,
	machineHash string,
	version string,
	buildCommit string,
	buildType string,
	imageDigest string,
	accountCount int,
	userCount int,
) (LeaseResult, error) {
	nonceBytes := make([]byte, 18)
	if _, err := rand.Read(nonceBytes); err != nil {
		return LeaseResult{}, fmt.Errorf("generate renewal nonce: %w", err)
	}
	timestamp := time.Now().UTC().Unix()
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	message := CanonicalRenewalMessage(instanceID, machineHash, timestamp, nonce, version, buildCommit, buildType, imageDigest, accountCount, userCount)
	request := RenewRequest{
		InstanceID:   instanceID,
		MachineHash:  machineHash,
		Timestamp:    timestamp,
		Nonce:        nonce,
		Version:      version,
		BuildCommit:  buildCommit,
		BuildType:    buildType,
		ImageDigest:  imageDigest,
		AccountCount: accountCount,
		UserCount:    userCount,
		Signature:    identity.Sign(message),
	}
	var response LeaseResponse
	if err := c.post(ctx, "/v1/renew", request, &response); err != nil {
		return LeaseResult{}, err
	}
	if strings.TrimSpace(response.Lease) == "" {
		return LeaseResult{}, fmt.Errorf("license server renewal response omitted lease")
	}
	return LeaseResult{Lease: response.Lease, CalibrationProfile: response.CalibrationProfile}, nil
}

func (c *Client) post(ctx context.Context, path string, requestBody any, responseBody any) error {
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("encode license request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("create license request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", c.userAgent)
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("contact license server: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBodyBytes+1))
	if err != nil {
		return fmt.Errorf("read license response: %w", err)
	}
	if len(raw) > maxResponseBodyBytes {
		return fmt.Errorf("license response exceeded size limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var payload struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &payload)
		return &APIError{StatusCode: response.StatusCode, Message: strings.TrimSpace(payload.Error)}
	}
	if err := json.Unmarshal(raw, responseBody); err != nil {
		return fmt.Errorf("decode license response: %w", err)
	}
	return nil
}
