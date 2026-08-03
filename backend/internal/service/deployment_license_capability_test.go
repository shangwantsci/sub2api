package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestHasCapabilityFollowsLeaseFeatures(t *testing.T) {
	cases := map[string]struct {
		features []string
		want     bool
	}{
		"granted":               {[]string{"gateway", CapabilityChromeCookieAuth}, true},
		"only capability":       {[]string{CapabilityChromeCookieAuth}, true},
		"not granted":           {[]string{"gateway", "admin"}, false},
		"no features at all":    {nil, false},
		"empty feature list":    {[]string{}, false},
		"similar but different": {[]string{"chrome_cookie"}, false},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			service := newHotPathLicenseService(t)
			runtime := *service.runtime.Load()
			runtime.claims.Features = testCase.features
			service.runtime.Store(&runtime)
			if got := service.HasCapability(CapabilityChromeCookieAuth); got != testCase.want {
				t.Errorf("HasCapability = %v, want %v", got, testCase.want)
			}
		})
	}
}

// Without a valid lease a customer image must not expose managed capabilities,
// even during the grace period where gateway traffic still flows.
func TestHasCapabilityDeniedWithoutUsableLease(t *testing.T) {
	for name, mutate := range map[string]func(*deploymentLicenseRuntime){
		"no lease": func(r *deploymentLicenseRuntime) { r.hasLease = false },
		"revoked":  func(r *deploymentLicenseRuntime) { r.revoked = true },
	} {
		t.Run(name, func(t *testing.T) {
			service := newHotPathLicenseService(t)
			runtime := *service.runtime.Load()
			runtime.claims.Features = []string{CapabilityChromeCookieAuth}
			mutate(&runtime)
			service.runtime.Store(&runtime)
			if service.HasCapability(CapabilityChromeCookieAuth) {
				t.Error("managed capability must be denied without a usable lease")
			}
		})
	}
}

// The community build must keep every capability, otherwise enabling licensing
// upstream would silently remove features from the operator's own deployment.
func TestHasCapabilityUnrestrictedForCommunityBuild(t *testing.T) {
	service, err := NewDeploymentLicenseService(&config.Config{},
		BuildInfo{Version: "0.1.156", BuildType: "release", ManagedCustomerID: "community"})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if !service.HasCapability(CapabilityChromeCookieAuth) {
		t.Error("community build must keep the Chrome cookie capability")
	}
	var nilService *DeploymentLicenseService
	if !nilService.HasCapability(CapabilityChromeCookieAuth) {
		t.Error("a nil service must not restrict capabilities")
	}
}

// Only names in managedCapabilities are gated; anything else stays open so new
// features are not accidentally withheld from customers.
func TestHasCapabilityIgnoresUnmanagedNames(t *testing.T) {
	service := newHotPathLicenseService(t)
	runtime := *service.runtime.Load()
	runtime.claims.Features = []string{"gateway"}
	service.runtime.Store(&runtime)
	for _, name := range []string{"some_future_feature", "gateway", ""} {
		if !service.HasCapability(name) {
			t.Errorf("unmanaged capability %q must not be restricted", name)
		}
	}
}

func TestManagedCapabilitiesIsStable(t *testing.T) {
	names := ManagedCapabilities()
	if len(names) != 1 || names[0] != CapabilityChromeCookieAuth {
		t.Fatalf("ManagedCapabilities() = %v; update the customer docs when this changes", names)
	}
}

// Capability checks sit on admin request handling; keep them allocation-free.
func TestHasCapabilityDoesNotAllocate(t *testing.T) {
	service := newHotPathLicenseService(t)
	runtime := *service.runtime.Load()
	runtime.claims.Features = []string{"gateway", CapabilityChromeCookieAuth}
	service.runtime.Store(&runtime)
	if allocs := testing.AllocsPerRun(200, func() {
		service.HasCapability(CapabilityChromeCookieAuth)
	}); allocs != 0 {
		t.Errorf("HasCapability allocates %.1f times per call, want 0", allocs)
	}
}
