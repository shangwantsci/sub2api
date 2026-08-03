package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// Simulates the existing production build: customer_id=community, no license
// env vars at all. Nothing about licensing may activate.
func TestCommunityBuildIsUntouched(t *testing.T) {
	cfg := &config.Config{} // zero value == deployment_license.enabled false
	build := BuildInfo{Version: "0.1.156", Commit: "abc", BuildType: "release", ManagedCustomerID: "community"}

	svc, err := NewDeploymentLicenseService(cfg, build)
	if err != nil {
		t.Fatalf("community build failed to construct: %v", err)
	}
	if svc.Enabled() {
		t.Fatal("licensing must stay off for a community build")
	}
	if !svc.CanServeGateway() || !svc.CanMutateAdmin() {
		t.Fatal("community build must not be gated")
	}
	if got := svc.Snapshot().Status; got != DeploymentLicenseStatusDisabled {
		t.Fatalf("status = %q, want disabled", got)
	}
	// RefreshNow must be a no-op, never contacting a license server.
	if err := svc.RefreshNow(context.Background()); err != nil {
		t.Fatalf("RefreshNow on a disabled service: %v", err)
	}
	svc.Initialize(context.Background())
	svc.Start()
	svc.Stop()

}

// Even if the public key gets baked in by CI, community stays off.
func TestCommunityBuildWithBakedKeyStaysOff(t *testing.T) {
	svc, err := NewDeploymentLicenseService(&config.Config{}, BuildInfo{
		Version: "0.1.156", BuildType: "release", ManagedCustomerID: "community",
		LicensePublicKey: "ebVWLo/mVPlAeLES6KmLp5AfhTrmlb7X4OORC60ElmQ",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if svc.Enabled() {
		t.Fatal("a baked public key must not switch licensing on for community")
	}
}

// And update/rollback must stay available on the community build.
func TestCommunityBuildKeepsSelfUpdate(t *testing.T) {
	svc := ProvideUpdateService(&updateServiceCacheStub{}, &updateServiceGitHubClientStub{},
		BuildInfo{Version: "0.1.156", BuildType: "release", ManagedCustomerID: "community"},
		&config.Config{})
	if svc.disabled {
		t.Fatal("community build must keep self-update enabled")
	}
}
