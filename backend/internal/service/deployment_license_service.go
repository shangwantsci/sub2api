package service

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/deploymentlicense"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
)

const (
	DeploymentLicenseStatusDisabled   = "disabled"
	DeploymentLicenseStatusActive     = "active"
	DeploymentLicenseStatusGrace      = "grace"
	DeploymentLicenseStatusExpired    = "expired"
	DeploymentLicenseStatusUnlicensed = "unlicensed"
	DeploymentLicenseStatusRevoked    = "revoked"
	DeploymentLicenseStatusLimit      = "limit_exceeded"
)

// CapabilityChromeCookieAuth gates the Claude for Chrome cookie OAuth entry
// point. Customer deployments only get it when their lease grants it.
const CapabilityChromeCookieAuth = "chrome_cookie_auth"

// managedCapabilities are the features a customer image must be granted
// explicitly. Everything not listed here is always available, so adding a
// capability here is what makes it restricted — nothing else has to enumerate
// the full feature surface.
var managedCapabilities = map[string]struct{}{
	CapabilityChromeCookieAuth: {},
}

// ManagedCapabilities returns the restricted capability names, sorted, so the
// admin API and docs stay in step with the code.
func ManagedCapabilities() []string {
	names := make([]string, 0, len(managedCapabilities))
	for name := range managedCapabilities {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type deploymentLicenseRuntime struct {
	claims        deploymentlicense.LeaseClaims
	hasLease      bool
	revoked       bool
	lastAttemptAt time.Time
	lastSuccessAt time.Time
	lastError     string
	accountCount  int
	userCount     int
}

type DeploymentLicenseSnapshot struct {
	Enabled           bool      `json:"enabled"`
	Status            string    `json:"status"`
	ManagedCustomerID string    `json:"managed_customer_id,omitempty"`
	BuildVersion      string    `json:"build_version,omitempty"`
	BuildCommit       string    `json:"build_commit,omitempty"`
	ImageDigest       string    `json:"image_digest,omitempty"`
	CustomerID        string    `json:"customer_id,omitempty"`
	InstanceID        string    `json:"instance_id,omitempty"`
	MachineHashPrefix string    `json:"machine_hash_prefix,omitempty"`
	Features          []string  `json:"features,omitempty"`
	MaxAccounts       int       `json:"max_accounts,omitempty"`
	MaxUsers          int       `json:"max_users,omitempty"`
	AccountCount      int       `json:"account_count,omitempty"`
	UserCount         int       `json:"user_count,omitempty"`
	IssuedAt          time.Time `json:"issued_at,omitempty"`
	ExpiresAt         time.Time `json:"expires_at,omitempty"`
	GraceEndsAt       time.Time `json:"grace_ends_at,omitempty"`
	LastAttemptAt     time.Time `json:"last_attempt_at,omitempty"`
	LastSuccessAt     time.Time `json:"last_success_at,omitempty"`
	LastError         string    `json:"last_error,omitempty"`
	HostSignalCount   int       `json:"host_signal_count,omitempty"`
	// Capabilities reports every managed capability and whether this deployment
	// holds it, so the admin UI never has to re-derive the rule from features.
	Capabilities map[string]bool `json:"capabilities,omitempty"`
}

// CalibrationProfilePublisher stores an operator-signed Claude Code wire
// profile. Satisfied by *SettingService.
type CalibrationProfilePublisher interface {
	PublishClaudeCalibratedProfile(ctx context.Context, raw []byte) (*claude.CalibratedProfile, error)
}

type DeploymentLicenseService struct {
	cfg              config.DeploymentLicenseConfig
	buildInfo        BuildInfo
	enabled          bool
	managedBuild     bool
	verificationKey  ed25519.PublicKey
	identity         *deploymentlicense.Identity
	machineHash      string
	client           *deploymentlicense.Client
	resourceClient   *dbent.Client
	profilePublisher CalibrationProfilePublisher
	// appliedProfileVersion avoids rewriting an unchanged profile every hour.
	appliedProfileVersion atomic.Value
	runtime               atomic.Pointer[deploymentLicenseRuntime]
	refreshMu             sync.Mutex
	stopCh                chan struct{}
	stopOnce              sync.Once
	wg                    sync.WaitGroup
}

// SetCalibrationProfilePublisher wires the profile sink after construction,
// keeping the license service out of the setting service's dependency graph.
func (s *DeploymentLicenseService) SetCalibrationProfilePublisher(publisher CalibrationProfilePublisher) {
	if s != nil {
		s.profilePublisher = publisher
	}
}

func NewDeploymentLicenseService(cfg *config.Config, buildInfo BuildInfo, resourceClients ...*dbent.Client) (*DeploymentLicenseService, error) {
	service := &DeploymentLicenseService{
		buildInfo: buildInfo,
		stopCh:    make(chan struct{}),
	}
	service.managedBuild = buildInfo.ManagedCustomerID != "" && buildInfo.ManagedCustomerID != "community"
	if len(resourceClients) > 0 {
		service.resourceClient = resourceClients[0]
	}
	if cfg != nil {
		service.cfg = cfg.DeploymentLicense
	}
	service.enabled = service.managedBuild || service.cfg.Enabled
	if !service.enabled {
		service.runtime.Store(&deploymentLicenseRuntime{})
		return service, nil
	}
	if service.cfg.ServerURL == "" {
		return nil, fmt.Errorf("deployment license server URL is required")
	}
	if service.cfg.DataDir == "" {
		return nil, fmt.Errorf("deployment license data directory is required")
	}
	if service.cfg.RenewalIntervalSeconds < 60 {
		return nil, fmt.Errorf("deployment license renewal interval must be at least 60 seconds")
	}
	if service.cfg.RequestTimeoutSeconds <= 0 || service.cfg.RequestTimeoutSeconds > 60 {
		return nil, fmt.Errorf("deployment license request timeout must be between 1 and 60 seconds")
	}
	if service.cfg.GracePeriodSeconds < 0 {
		return nil, fmt.Errorf("deployment license grace period must be non-negative")
	}
	if service.managedBuild {
		if strings.TrimSpace(buildInfo.LicensePublicKey) == "" {
			return nil, fmt.Errorf("managed customer image is missing its build-time license public key")
		}
		if service.cfg.GracePeriodSeconds > 6*60*60 {
			return nil, fmt.Errorf("managed customer image grace period cannot exceed 6 hours")
		}
		if service.cfg.MachineID != "" {
			return nil, fmt.Errorf("managed customer image cannot override host machine identity")
		}
	}
	verificationValue := strings.TrimSpace(buildInfo.LicensePublicKey)
	if verificationValue == "" {
		verificationValue = service.cfg.PublicKey
	}
	verificationKey, err := deploymentlicense.ParsePublicKey(verificationValue)
	if err != nil {
		return nil, fmt.Errorf("initialize deployment license verification key: %w", err)
	}
	service.verificationKey = verificationKey
	identity, err := deploymentlicense.LoadOrCreateIdentity(service.cfg.DataDir)
	if err != nil {
		return nil, err
	}
	service.identity = identity
	machineHash, err := identity.MachineHash(service.cfg.MachineID)
	if err != nil {
		return nil, fmt.Errorf("compute deployment machine identity: %w", err)
	}
	service.machineHash = machineHash
	if service.managedBuild && buildInfo.BuildType == "release" && identity.HostSignalCount() == 0 {
		return nil, fmt.Errorf("managed customer image requires host machine identity mounts")
	}
	service.client = deploymentlicense.NewClient(
		service.cfg.ServerURL,
		&http.Client{Timeout: time.Duration(service.cfg.RequestTimeoutSeconds) * time.Second},
		buildInfo.Version,
	)
	service.runtime.Store(&deploymentLicenseRuntime{})
	if identity.HostSignalCount() == 0 {
		slog.Warn("deployment license has no host-provided machine signals; bind /etc/machine-id and /sys/class/dmi/id or configure deployment_license.machine_id")
	}
	if cached, err := deploymentlicense.LoadCachedLease(service.cfg.DataDir); err != nil {
		slog.Warn("deployment_license.cached_lease_read_failed", "error", err)
	} else if cached != "" {
		if err := service.acceptLease(cached, false); err != nil {
			slog.Warn("deployment_license.cached_lease_rejected", "error", err)
		}
	}
	return service, nil
}

func (s *DeploymentLicenseService) Enabled() bool {
	return s != nil && s.enabled
}

func (s *DeploymentLicenseService) Initialize(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	if err := s.RefreshNow(ctx); err != nil {
		slog.Warn("deployment_license.initial_refresh_failed", "status", s.Snapshot().Status, "error", err)
	}
}

func (s *DeploymentLicenseService) Start() {
	if !s.Enabled() {
		return
	}
	s.wg.Add(1)
	go s.refreshLoop()
}

func (s *DeploymentLicenseService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *DeploymentLicenseService) refreshLoop() {
	defer s.wg.Done()
	interval := time.Duration(s.cfg.RenewalIntervalSeconds) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			timeout := time.Duration(s.cfg.RequestTimeoutSeconds) * time.Second
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			if err := s.RefreshNow(ctx); err != nil {
				slog.Warn("deployment_license.renew_failed", "status", s.Snapshot().Status, "error", err)
			}
			cancel()
		}
	}
}

func (s *DeploymentLicenseService) RefreshNow(ctx context.Context) error {
	if !s.Enabled() {
		return nil
	}
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.updateRuntime(func(runtime *deploymentLicenseRuntime) {
		runtime.lastAttemptAt = time.Now().UTC()
	})

	current := s.runtime.Load()
	accountCount, userCount := 0, 0
	if current != nil {
		accountCount, userCount = current.accountCount, current.userCount
	}
	if counts, countErr := s.queryResourceCounts(ctx); countErr != nil {
		slog.Warn("deployment_license.resource_count_failed", "error", countErr)
	} else {
		accountCount, userCount = counts.accounts, counts.users
		s.updateRuntime(func(runtime *deploymentLicenseRuntime) {
			runtime.accountCount = accountCount
			runtime.userCount = userCount
		})
		current = s.runtime.Load()
	}
	var result deploymentlicense.LeaseResult
	var err error
	if current != nil && current.hasLease && current.claims.InstanceID != "" {
		result, err = s.client.Renew(
			ctx,
			s.identity,
			current.claims.InstanceID,
			s.machineHash,
			s.buildInfo.Version,
			s.buildInfo.Commit,
			s.buildInfo.BuildType,
			s.cfg.ImageDigest,
			accountCount,
			userCount,
		)
	} else {
		if strings.TrimSpace(s.cfg.ActivationCode) == "" {
			err = fmt.Errorf("deployment activation code is required for first activation")
		} else {
			result, err = s.client.Activate(ctx, deploymentlicense.ActivateRequest{
				ActivationCode:    s.cfg.ActivationCode,
				MachineHash:       s.machineHash,
				InstancePublicKey: s.identity.PublicKey,
				InstallationID:    s.identity.InstallationID,
				Version:           s.buildInfo.Version,
				BuildCommit:       s.buildInfo.Commit,
				BuildType:         s.buildInfo.BuildType,
				ImageDigest:       s.cfg.ImageDigest,
				AccountCount:      accountCount,
				UserCount:         userCount,
			})
		}
	}
	if err != nil {
		var apiErr *deploymentlicense.APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusForbidden || apiErr.StatusCode == http.StatusUnauthorized) {
			s.updateRuntime(func(runtime *deploymentLicenseRuntime) {
				runtime.revoked = true
				runtime.lastError = err.Error()
			})
		} else {
			s.updateRuntime(func(runtime *deploymentLicenseRuntime) {
				runtime.lastError = err.Error()
			})
		}
		return err
	}
	if err := s.acceptLease(result.Lease, true); err != nil {
		s.updateRuntime(func(runtime *deploymentLicenseRuntime) {
			runtime.lastError = err.Error()
		})
		return err
	}
	// Applied after the lease: a bad profile must never cost the deployment its
	// license, and the gateway falls back to built-in constants either way.
	s.applyCalibrationProfile(ctx, result.CalibrationProfile)
	slog.Info("deployment_license.renewed", "instance_id", s.Snapshot().InstanceID, "expires_at", s.Snapshot().ExpiresAt)
	return nil
}

// applyCalibrationProfile verifies an operator-signed Claude Code wire profile
// and stores it, so customer deployments track mimicry updates over the same
// authenticated outbound channel they already use for renewal.
func (s *DeploymentLicenseService) applyCalibrationProfile(ctx context.Context, envelope string) {
	if strings.TrimSpace(envelope) == "" || s.profilePublisher == nil {
		return
	}
	claims, err := deploymentlicense.VerifyProfileEnvelope(envelope, s.verificationKey)
	if err != nil {
		slog.Warn("deployment_license.calibration_profile_rejected", "error", err)
		return
	}
	if applied, _ := s.appliedProfileVersion.Load().(string); applied == claims.CLIVersion {
		return
	}
	if _, err := s.profilePublisher.PublishClaudeCalibratedProfile(ctx, []byte(claims.Profile)); err != nil {
		slog.Warn("deployment_license.calibration_profile_publish_failed",
			"cli_version", claims.CLIVersion, "error", err)
		return
	}
	s.appliedProfileVersion.Store(claims.CLIVersion)
	slog.Info("deployment_license.calibration_profile_applied", "cli_version", claims.CLIVersion)
}

func (s *DeploymentLicenseService) acceptLease(token string, persist bool) error {
	claims, err := deploymentlicense.VerifyLease(token, s.verificationKey)
	if err != nil {
		return err
	}
	if err := deploymentlicense.ValidateLeaseBinding(claims, s.machineHash, s.identity.PublicKey); err != nil {
		return err
	}
	now := time.Now().UTC()
	graceEnd := deploymentlicense.LeaseExpiry(claims).Add(time.Duration(s.cfg.GracePeriodSeconds) * time.Second)
	if persist {
		if err := deploymentlicense.SaveCachedLease(s.cfg.DataDir, token); err != nil {
			return err
		}
	}
	lastAttemptAt := time.Time{}
	lastSuccessAt := time.Unix(claims.IssuedAt, 0).UTC()
	if persist {
		lastAttemptAt = now
		lastSuccessAt = now
	}
	previous := s.runtime.Load()
	accountCount, userCount := 0, 0
	if previous != nil {
		accountCount, userCount = previous.accountCount, previous.userCount
	}
	s.runtime.Store(&deploymentLicenseRuntime{
		claims:        claims,
		hasLease:      true,
		lastAttemptAt: lastAttemptAt,
		lastSuccessAt: lastSuccessAt,
		accountCount:  accountCount,
		userCount:     userCount,
	})
	if !now.Before(graceEnd) {
		// Keep the verified instance binding in memory so an expired installation
		// can authenticate a renewal instead of trying to reuse its activation code.
		return deploymentlicense.ErrLeaseExpired
	}
	return nil
}

// status derives the enforcement state from the in-memory runtime without
// building a snapshot. Gateway middleware calls this on every request, so it
// must stay allocation-free; Snapshot reuses it to keep the two in agreement.
func (s *DeploymentLicenseService) status() string {
	if s == nil || !s.Enabled() {
		return DeploymentLicenseStatusDisabled
	}
	runtime := s.runtime.Load()
	if runtime == nil {
		return DeploymentLicenseStatusUnlicensed
	}
	if runtime.revoked {
		return DeploymentLicenseStatusRevoked
	}
	if !runtime.hasLease {
		return DeploymentLicenseStatusUnlicensed
	}
	expiry := deploymentlicense.LeaseExpiry(runtime.claims)
	now := time.Now().UTC()
	switch {
	case now.Before(expiry):
		if (runtime.claims.MaxAccounts > 0 && runtime.accountCount > runtime.claims.MaxAccounts) ||
			(runtime.claims.MaxUsers > 0 && runtime.userCount > runtime.claims.MaxUsers) {
			return DeploymentLicenseStatusLimit
		}
		return DeploymentLicenseStatusActive
	case now.Before(expiry.Add(time.Duration(s.cfg.GracePeriodSeconds) * time.Second)):
		return DeploymentLicenseStatusGrace
	default:
		return DeploymentLicenseStatusExpired
	}
}

// capabilitySnapshot resolves every managed capability for the admin API.
func (s *DeploymentLicenseService) capabilitySnapshot() map[string]bool {
	names := ManagedCapabilities()
	capabilities := make(map[string]bool, len(names))
	for _, name := range names {
		capabilities[name] = s.HasCapability(name)
	}
	return capabilities
}

func (s *DeploymentLicenseService) Snapshot() DeploymentLicenseSnapshot {
	if s == nil || !s.Enabled() {
		return DeploymentLicenseSnapshot{
			Enabled:      false,
			Status:       DeploymentLicenseStatusDisabled,
			Capabilities: s.capabilitySnapshot(),
		}
	}
	runtime := s.runtime.Load()
	if runtime == nil {
		return DeploymentLicenseSnapshot{Enabled: true, Status: DeploymentLicenseStatusUnlicensed}
	}
	snapshot := DeploymentLicenseSnapshot{
		Enabled:           true,
		Status:            s.status(),
		Capabilities:      s.capabilitySnapshot(),
		ManagedCustomerID: s.buildInfo.ManagedCustomerID,
		BuildVersion:      s.buildInfo.Version,
		BuildCommit:       s.buildInfo.Commit,
		ImageDigest:       s.cfg.ImageDigest,
		LastAttemptAt:     runtime.lastAttemptAt,
		LastSuccessAt:     runtime.lastSuccessAt,
		LastError:         runtime.lastError,
		HostSignalCount:   s.identity.HostSignalCount(),
	}
	if runtime.revoked || !runtime.hasLease {
		return snapshot
	}
	claims := runtime.claims
	expiry := deploymentlicense.LeaseExpiry(claims)
	graceEnd := expiry.Add(time.Duration(s.cfg.GracePeriodSeconds) * time.Second)
	snapshot.CustomerID = claims.CustomerID
	snapshot.InstanceID = claims.InstanceID
	snapshot.Features = append([]string(nil), claims.Features...)
	snapshot.MaxAccounts = claims.MaxAccounts
	snapshot.MaxUsers = claims.MaxUsers
	snapshot.AccountCount = runtime.accountCount
	snapshot.UserCount = runtime.userCount
	snapshot.IssuedAt = time.Unix(claims.IssuedAt, 0).UTC()
	snapshot.ExpiresAt = expiry
	snapshot.GraceEndsAt = graceEnd
	if len(s.machineHash) >= 12 {
		snapshot.MachineHashPrefix = s.machineHash[:12]
	}
	return snapshot
}

// HasCapability reports whether a managed capability is available here.
// Capabilities outside managedCapabilities, and every community deployment,
// are unrestricted; a customer image needs the name in its lease features.
// Allocation-free so it can be called from request handling.
func (s *DeploymentLicenseService) HasCapability(name string) bool {
	if _, managed := managedCapabilities[name]; !managed {
		return true
	}
	if s == nil || !s.Enabled() {
		return true
	}
	runtime := s.runtime.Load()
	if runtime == nil || !runtime.hasLease || runtime.revoked {
		return false
	}
	for _, feature := range runtime.claims.Features {
		if feature == name {
			return true
		}
	}
	return false
}

func (s *DeploymentLicenseService) CanServeGateway() bool {
	switch s.status() {
	case DeploymentLicenseStatusDisabled, DeploymentLicenseStatusActive, DeploymentLicenseStatusGrace:
		return true
	default:
		return false
	}
}

func (s *DeploymentLicenseService) CanMutateAdmin() bool {
	switch s.status() {
	case DeploymentLicenseStatusDisabled, DeploymentLicenseStatusActive, DeploymentLicenseStatusLimit:
		return true
	default:
		return false
	}
}

type deploymentResourceCounts struct {
	accounts int
	users    int
}

func (s *DeploymentLicenseService) queryResourceCounts(ctx context.Context) (deploymentResourceCounts, error) {
	if s.resourceClient == nil {
		return deploymentResourceCounts{}, nil
	}
	accounts, err := s.resourceClient.Account.Query().Count(ctx)
	if err != nil {
		return deploymentResourceCounts{}, fmt.Errorf("count deployment accounts: %w", err)
	}
	users, err := s.resourceClient.User.Query().Count(ctx)
	if err != nil {
		return deploymentResourceCounts{}, fmt.Errorf("count deployment users: %w", err)
	}
	return deploymentResourceCounts{accounts: accounts, users: users}, nil
}

func (s *DeploymentLicenseService) CanCreateAccount(ctx context.Context) (bool, error) {
	snapshot := s.Snapshot()
	if !snapshot.Enabled || snapshot.MaxAccounts <= 0 {
		return true, nil
	}
	counts, err := s.queryResourceCounts(ctx)
	if err != nil {
		return false, err
	}
	s.updateRuntime(func(runtime *deploymentLicenseRuntime) {
		runtime.accountCount = counts.accounts
		runtime.userCount = counts.users
	})
	return counts.accounts < snapshot.MaxAccounts, nil
}

func (s *DeploymentLicenseService) CanCreateUser(ctx context.Context) (bool, error) {
	snapshot := s.Snapshot()
	if !snapshot.Enabled || snapshot.MaxUsers <= 0 {
		return true, nil
	}
	counts, err := s.queryResourceCounts(ctx)
	if err != nil {
		return false, err
	}
	s.updateRuntime(func(runtime *deploymentLicenseRuntime) {
		runtime.accountCount = counts.accounts
		runtime.userCount = counts.users
	})
	return counts.users < snapshot.MaxUsers, nil
}

func (s *DeploymentLicenseService) updateRuntime(update func(*deploymentLicenseRuntime)) {
	current := s.runtime.Load()
	next := &deploymentLicenseRuntime{}
	if current != nil {
		*next = *current
	}
	update(next)
	s.runtime.Store(next)
}
