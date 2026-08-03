package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/deploymentlicense"
)

func newHotPathLicenseService(t *testing.T) *DeploymentLicenseService {
	t.Helper()
	service := &DeploymentLicenseService{enabled: true}
	service.cfg.GracePeriodSeconds = 21600
	service.machineHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	service.identity = &deploymentlicense.Identity{}
	now := time.Now().UTC()
	service.runtime.Store(&deploymentLicenseRuntime{
		hasLease: true,
		claims: deploymentlicense.LeaseClaims{
			CustomerID: "customer-a",
			InstanceID: "inst_test",
			Features:   []string{"gateway", "admin", "provider"},
			IssuedAt:   now.Add(-time.Hour).Unix(),
			ExpiresAt:  now.Add(23 * time.Hour).Unix(),
			Sequence:   1,
		},
	})
	return service
}

// Every gateway request runs these two checks. Building a full snapshot here
// used to copy the Features slice, costing one heap allocation per request;
// keep the enforcement path allocation-free.
func TestGatewayLicenseChecksDoNotAllocate(t *testing.T) {
	service := newHotPathLicenseService(t)
	if allocs := testing.AllocsPerRun(200, func() {
		if !service.CanServeGateway() {
			t.Fatal("expected the gateway to be servable")
		}
	}); allocs != 0 {
		t.Errorf("CanServeGateway allocates %.1f times per call, want 0", allocs)
	}
	if allocs := testing.AllocsPerRun(200, func() {
		if !service.CanMutateAdmin() {
			t.Fatal("expected admin mutations to be allowed")
		}
	}); allocs != 0 {
		t.Errorf("CanMutateAdmin allocates %.1f times per call, want 0", allocs)
	}
}

// status and Snapshot must never disagree: the middleware enforces status while
// operators debug with the snapshot.
func TestStatusAgreesWithSnapshot(t *testing.T) {
	now := time.Now().UTC()
	cases := map[string]struct {
		mutate func(*deploymentLicenseRuntime)
		want   string
	}{
		"active":   {func(*deploymentLicenseRuntime) {}, DeploymentLicenseStatusActive},
		"revoked":  {func(r *deploymentLicenseRuntime) { r.revoked = true }, DeploymentLicenseStatusRevoked},
		"no lease": {func(r *deploymentLicenseRuntime) { r.hasLease = false }, DeploymentLicenseStatusUnlicensed},
		"grace": {func(r *deploymentLicenseRuntime) {
			r.claims.ExpiresAt = now.Add(-time.Hour).Unix()
		}, DeploymentLicenseStatusGrace},
		"beyond grace": {func(r *deploymentLicenseRuntime) {
			r.claims.ExpiresAt = now.Add(-10 * time.Hour).Unix()
		}, DeploymentLicenseStatusExpired},
		"account limit": {func(r *deploymentLicenseRuntime) {
			r.claims.MaxAccounts = 5
			r.accountCount = 6
		}, DeploymentLicenseStatusLimit},
		"user limit": {func(r *deploymentLicenseRuntime) {
			r.claims.MaxUsers = 5
			r.userCount = 6
		}, DeploymentLicenseStatusLimit},
		"at the limit is still active": {func(r *deploymentLicenseRuntime) {
			r.claims.MaxAccounts = 5
			r.accountCount = 5
		}, DeploymentLicenseStatusActive},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			service := newHotPathLicenseService(t)
			runtime := *service.runtime.Load()
			testCase.mutate(&runtime)
			service.runtime.Store(&runtime)
			if got := service.status(); got != testCase.want {
				t.Errorf("status() = %q, want %q", got, testCase.want)
			}
			if got := service.Snapshot().Status; got != testCase.want {
				t.Errorf("Snapshot().Status = %q, want %q", got, testCase.want)
			}
		})
	}
}

// A disabled deployment must never be blocked by license enforcement.
func TestDisabledServiceAlwaysAllows(t *testing.T) {
	var service *DeploymentLicenseService
	if !service.CanServeGateway() || !service.CanMutateAdmin() {
		t.Fatal("a nil license service must not block traffic")
	}
	disabled := &DeploymentLicenseService{}
	if disabled.status() != DeploymentLicenseStatusDisabled {
		t.Fatalf("status() = %q, want disabled", disabled.status())
	}
	if !disabled.CanServeGateway() || !disabled.CanMutateAdmin() {
		t.Fatal("a disabled license service must not block traffic")
	}
}
