package service

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/deploymentlicense"
)

func mintDeploymentLeaseForTest(t *testing.T, privateKey ed25519.PrivateKey, claims deploymentlicense.LeaseClaims) string {
	t.Helper()
	headerJSON, err := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT"})
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

func TestDeploymentLicenseGraceState(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DeploymentLicense: config.DeploymentLicenseConfig{
			Enabled:                true,
			ServerURL:              "http://127.0.0.1:3900",
			PublicKey:              base64.RawStdEncoding.EncodeToString(publicKey),
			DataDir:                t.TempDir(),
			MachineID:              "test-host-a",
			RenewalIntervalSeconds: 3600,
			RequestTimeoutSeconds:  1,
			GracePeriodSeconds:     60,
		},
	}
	service, err := NewDeploymentLicenseService(cfg, BuildInfo{Version: "1.0.0", BuildType: "release"})
	if err != nil {
		t.Fatalf("NewDeploymentLicenseService() error = %v", err)
	}
	now := time.Now().UTC()
	token := mintDeploymentLeaseForTest(t, privateKey, deploymentlicense.LeaseClaims{
		Type:              deploymentlicense.LeaseType,
		CustomerID:        "customer-a",
		InstanceID:        "instance-a",
		MachineHash:       service.machineHash,
		InstancePublicKey: service.identity.PublicKey,
		Version:           "1.0.0",
		BuildType:         "release",
		IssuedAt:          now.Add(-time.Hour).Unix(),
		ExpiresAt:         now.Add(-10 * time.Second).Unix(),
		Sequence:          1,
	})
	if err := service.acceptLease(token, false); err != nil {
		t.Fatalf("acceptLease() error = %v", err)
	}
	if got := service.Snapshot().Status; got != DeploymentLicenseStatusGrace {
		t.Fatalf("Snapshot().Status = %q, want grace", got)
	}
	if !service.CanServeGateway() {
		t.Fatal("grace state must keep existing gateway traffic available")
	}
	if service.CanMutateAdmin() {
		t.Fatal("grace state must make admin mutations read-only")
	}
}

func TestDeploymentLicenseRejectsBeyondGrace(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DeploymentLicense: config.DeploymentLicenseConfig{
			Enabled:                true,
			ServerURL:              "http://127.0.0.1:3900",
			PublicKey:              base64.RawStdEncoding.EncodeToString(publicKey),
			DataDir:                t.TempDir(),
			MachineID:              "test-host-a",
			RenewalIntervalSeconds: 3600,
			RequestTimeoutSeconds:  1,
			GracePeriodSeconds:     30,
		},
	}
	service, err := NewDeploymentLicenseService(cfg, BuildInfo{Version: "1.0.0", BuildType: "release"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	token := mintDeploymentLeaseForTest(t, privateKey, deploymentlicense.LeaseClaims{
		Type:              deploymentlicense.LeaseType,
		CustomerID:        "customer-a",
		InstanceID:        "instance-a",
		MachineHash:       service.machineHash,
		InstancePublicKey: service.identity.PublicKey,
		IssuedAt:          now.Add(-time.Hour).Unix(),
		ExpiresAt:         now.Add(-time.Minute).Unix(),
		Sequence:          1,
	})
	if err := service.acceptLease(token, false); err == nil {
		t.Fatal("expected lease beyond grace to be rejected")
	}
}

func TestDeploymentLicenseDisabledIsNoop(t *testing.T) {
	service, err := NewDeploymentLicenseService(&config.Config{}, BuildInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if service.Snapshot().Status != DeploymentLicenseStatusDisabled || !service.CanServeGateway() || !service.CanMutateAdmin() {
		t.Fatal("disabled deployment licensing must preserve existing behavior")
	}
}

func TestManagedCustomerBuildCannotDisableLicensing(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewDeploymentLicenseService(&config.Config{
		DeploymentLicense: config.DeploymentLicenseConfig{
			Enabled:                false,
			ServerURL:              "http://127.0.0.1:3900",
			DataDir:                t.TempDir(),
			RenewalIntervalSeconds: 3600,
			RequestTimeoutSeconds:  1,
			GracePeriodSeconds:     60,
		},
	}, BuildInfo{
		Version:           "1.0.0",
		BuildType:         "test",
		LicensePublicKey:  base64.RawStdEncoding.EncodeToString(publicKey),
		ManagedCustomerID: "customer-a",
	})
	if err != nil {
		t.Fatalf("NewDeploymentLicenseService() error = %v", err)
	}
	if !service.Enabled() || service.Snapshot().Status != DeploymentLicenseStatusUnlicensed {
		t.Fatal("managed build must remain licensed even when config enabled=false")
	}
}

func TestDeploymentLicenseResourceLimits(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	entClient := newPaymentConfigServiceTestClient(t)
	service, err := NewDeploymentLicenseService(&config.Config{
		DeploymentLicense: config.DeploymentLicenseConfig{
			Enabled:                true,
			ServerURL:              "http://127.0.0.1:3900",
			PublicKey:              base64.RawStdEncoding.EncodeToString(publicKey),
			DataDir:                t.TempDir(),
			MachineID:              "test-host-a",
			RenewalIntervalSeconds: 3600,
			RequestTimeoutSeconds:  1,
			GracePeriodSeconds:     60,
		},
	}, BuildInfo{Version: "1.0.0", BuildType: "test"}, entClient)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	token := mintDeploymentLeaseForTest(t, privateKey, deploymentlicense.LeaseClaims{
		Type:              deploymentlicense.LeaseType,
		CustomerID:        "customer-a",
		InstanceID:        "instance-a",
		MachineHash:       service.machineHash,
		InstancePublicKey: service.identity.PublicKey,
		Version:           "1.0.0",
		BuildType:         "test",
		MaxAccounts:       1,
		MaxUsers:          1,
		IssuedAt:          now.Unix(),
		ExpiresAt:         now.Add(time.Hour).Unix(),
		Sequence:          1,
	})
	if err := service.acceptLease(token, false); err != nil {
		t.Fatal(err)
	}
	if allowed, err := service.CanCreateAccount(t.Context()); err != nil || !allowed {
		t.Fatalf("empty account pool should have capacity: allowed=%v err=%v", allowed, err)
	}
	if allowed, err := service.CanCreateUser(t.Context()); err != nil || !allowed {
		t.Fatalf("empty user table should have capacity: allowed=%v err=%v", allowed, err)
	}
	if _, err := entClient.Account.Create().SetName("a").SetPlatform("anthropic").SetType("oauth").Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := entClient.User.Create().SetEmail("user@example.com").SetPasswordHash("hash").Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if allowed, err := service.CanCreateAccount(t.Context()); err != nil || allowed {
		t.Fatalf("account limit should be enforced: allowed=%v err=%v", allowed, err)
	}
	if allowed, err := service.CanCreateUser(t.Context()); err != nil || allowed {
		t.Fatalf("user limit should be enforced: allowed=%v err=%v", allowed, err)
	}
	if _, err := entClient.Account.Create().SetName("b").SetPlatform("anthropic").SetType("oauth").Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if allowed, err := service.CanCreateAccount(t.Context()); err != nil || allowed {
		t.Fatalf("over-limit account pool should stay blocked: allowed=%v err=%v", allowed, err)
	}
	if service.Snapshot().Status != DeploymentLicenseStatusLimit || service.CanServeGateway() || !service.CanMutateAdmin() {
		t.Fatal("over-limit state must stop gateway while preserving administrative cleanup")
	}
}

func TestDeploymentLicenseActivationAndRenewal(t *testing.T) {
	serverPublicKey, serverPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var instancePublicKey string
	var machineHash string
	var activateCalls int
	var renewCalls int
	licenseServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		now := time.Now().UTC()
		switch r.URL.Path {
		case "/v1/activate":
			var request deploymentlicense.ActivateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.ActivationCode != "one-time-code" {
				t.Fatalf("activation code = %q", request.ActivationCode)
			}
			activateCalls++
			instancePublicKey = request.InstancePublicKey
			machineHash = request.MachineHash
			token := mintDeploymentLeaseForTest(t, serverPrivateKey, deploymentlicense.LeaseClaims{
				Type:              deploymentlicense.LeaseType,
				CustomerID:        "customer-a",
				InstanceID:        "instance-a",
				MachineHash:       machineHash,
				InstancePublicKey: instancePublicKey,
				Version:           request.Version,
				BuildCommit:       request.BuildCommit,
				BuildType:         request.BuildType,
				ImageDigest:       request.ImageDigest,
				MaxAccounts:       100,
				MaxUsers:          10,
				IssuedAt:          now.Unix(),
				ExpiresAt:         now.Add(24 * time.Hour).Unix(),
				Sequence:          1,
			})
			_ = json.NewEncoder(w).Encode(deploymentlicense.LeaseResponse{Lease: token})
		case "/v1/renew":
			var request deploymentlicense.RenewRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			publicKeyBytes, err := base64.RawStdEncoding.DecodeString(instancePublicKey)
			if err != nil {
				t.Fatal(err)
			}
			signature, err := base64.RawStdEncoding.DecodeString(request.Signature)
			if err != nil {
				t.Fatal(err)
			}
			message := deploymentlicense.CanonicalRenewalMessage(
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
				t.Fatal("renewal signature failed")
			}
			renewCalls++
			token := mintDeploymentLeaseForTest(t, serverPrivateKey, deploymentlicense.LeaseClaims{
				Type:              deploymentlicense.LeaseType,
				CustomerID:        "customer-a",
				InstanceID:        "instance-a",
				MachineHash:       machineHash,
				InstancePublicKey: instancePublicKey,
				Version:           request.Version,
				BuildCommit:       request.BuildCommit,
				BuildType:         request.BuildType,
				ImageDigest:       request.ImageDigest,
				MaxAccounts:       100,
				MaxUsers:          10,
				IssuedAt:          now.Unix(),
				ExpiresAt:         now.Add(24 * time.Hour).Unix(),
				Sequence:          2,
			})
			_ = json.NewEncoder(w).Encode(deploymentlicense.LeaseResponse{Lease: token})
		default:
			http.NotFound(w, r)
		}
	}))
	defer licenseServer.Close()

	service, err := NewDeploymentLicenseService(&config.Config{
		DeploymentLicense: config.DeploymentLicenseConfig{
			Enabled:                true,
			ServerURL:              licenseServer.URL,
			ActivationCode:         "one-time-code",
			PublicKey:              base64.RawStdEncoding.EncodeToString(serverPublicKey),
			DataDir:                t.TempDir(),
			MachineID:              "test-host-a",
			ImageDigest:            "sha256:test",
			RenewalIntervalSeconds: 3600,
			RequestTimeoutSeconds:  2,
			GracePeriodSeconds:     60,
		},
	}, BuildInfo{Version: "1.0.0", Commit: "abcdef12", BuildType: "test"})
	if err != nil {
		t.Fatal(err)
	}
	service.Initialize(t.Context())
	if activateCalls != 1 || service.Snapshot().Status != DeploymentLicenseStatusActive {
		t.Fatalf("activation failed: calls=%d snapshot=%+v", activateCalls, service.Snapshot())
	}
	if err := service.RefreshNow(t.Context()); err != nil {
		t.Fatalf("RefreshNow() error = %v", err)
	}
	if renewCalls != 1 || service.Snapshot().Status != DeploymentLicenseStatusActive {
		t.Fatalf("renewal failed: calls=%d snapshot=%+v", renewCalls, service.Snapshot())
	}
	if service.Snapshot().MaxAccounts != 100 || service.Snapshot().MaxUsers != 10 {
		t.Fatalf("lease limits missing from snapshot: %+v", service.Snapshot())
	}
}
