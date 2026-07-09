package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/tidwall/gjson"
)

const (
	claudeMimicryGuardOff   = "off"
	claudeMimicryGuardWarn  = "warn"
	claudeMimicryGuardBlock = "block"
)

type claudeMimicryAuditResult struct {
	OK          bool
	ShouldBlock bool
	Findings    []string
}

func normalizeClaudeMimicryGuardMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case claudeMimicryGuardOff:
		return claudeMimicryGuardOff
	case claudeMimicryGuardBlock:
		return claudeMimicryGuardBlock
	case claudeMimicryGuardWarn, "":
		return claudeMimicryGuardWarn
	default:
		return claudeMimicryGuardWarn
	}
}

func (s *GatewayService) enforceClaudeMimicryGuard(ctx context.Context, req *http.Request, body []byte, account *Account) error {
	profile := claude.DefaultClaudeCodeMimicryProfile()
	guardMode := claudeMimicryGuardWarn
	if s.settingService != nil {
		runtimeSettings := s.settingService.GetClaudeMimicryRuntimeSettings(ctx)
		profile = claude.ResolveClaudeCodeMimicryProfile(runtimeSettings.ProfileID)
		guardMode = normalizeClaudeMimicryGuardMode(runtimeSettings.GuardMode)
	}
	modelProfile := claude.ResolveClaudeCodeMimicryModelProfile(gjson.GetBytes(body, "model").String())
	profile.MessageBetas = modelProfile.MessageBetas
	if req != nil && req.URL != nil && strings.Contains(req.URL.Path, "count_tokens") {
		profile.MessageBetas = modelProfile.CountTokensBetas
	}
	audit := evaluateClaudeMimicryGuard(req, body, profile, guardMode)
	if !audit.OK {
		accountID := int64(0)
		if account != nil {
			accountID = account.ID
		}
		endpoint := ""
		requestID := ""
		if req != nil {
			if req.URL != nil {
				endpoint = req.URL.Path
			}
			requestID = getHeaderRaw(req.Header, "x-client-request-id")
		}
		logger.LegacyPrintf("service.gateway", "[ClaudeMimicryGuard] mode=%s profile=%s endpoint=%s account_id=%d request_id=%s findings=%s", guardMode, profile.ID, endpoint, accountID, requestID, strings.Join(audit.Findings, ","))
	}
	if audit.ShouldBlock {
		return fmt.Errorf("claude mimicry guard blocked request: %s", strings.Join(audit.Findings, ","))
	}
	return nil
}

func evaluateClaudeMimicryGuard(req *http.Request, body []byte, profile claude.ClaudeCodeMimicryProfile, mode string) claudeMimicryAuditResult {
	mode = normalizeClaudeMimicryGuardMode(mode)
	if mode == claudeMimicryGuardOff {
		return claudeMimicryAuditResult{OK: true}
	}

	findings := make([]string, 0, 8)
	add := func(code string) {
		findings = append(findings, code)
	}

	if req == nil {
		add("missing_request")
	} else {
		for key, want := range profile.Headers {
			if want == "" {
				continue
			}
			if got := getHeaderRaw(req.Header, key); got != want {
				add("header_mismatch:" + key)
			}
		}
		if got := getHeaderRaw(req.Header, "Accept"); got != "application/json" {
			add("header_mismatch:Accept")
		}
		if got := getHeaderRaw(req.Header, "Accept-Encoding"); got != "gzip, deflate, br, zstd" {
			add("header_mismatch:Accept-Encoding")
		}
		if getHeaderRaw(req.Header, "x-client-request-id") == "" {
			add("missing_x_client_request_id")
		}
		betaHeader := getHeaderRaw(req.Header, "anthropic-beta")
		if betaHeader == "" {
			add("missing_anthropic_beta")
		} else {
			betaSet := buildBetaTokenSet(parseAnthropicBetaHeader(betaHeader))
			for _, beta := range profile.MessageBetas {
				if _, ok := betaSet[beta]; !ok {
					add("missing_beta:" + beta)
				}
			}
			if _, ok := betaSet[claude.BetaOAuth]; ok {
				add("unexpected_oauth_beta")
			}
		}
	}

	system := gjson.GetBytes(body, "system")
	if !system.IsArray() {
		add("missing_system_blocks")
	} else {
		systemBlocks := system.Array()
		if len(systemBlocks) < 3 {
			add("system_block_count")
		}
		billingFound := false
		identityFound := false
		for _, block := range systemBlocks {
			text := block.Get("text").String()
			if strings.Contains(text, "x-anthropic-billing-header:") {
				billingFound = true
				if !strings.Contains(text, "cc_version="+profile.CLIVersion+".") {
					add("billing_version_mismatch")
				}
				if !strings.Contains(text, "cc_entrypoint="+profile.BillingEntrypoint) {
					add("billing_entrypoint_mismatch")
				}
				if strings.Contains(text, "cch=") {
					add("unexpected_cch")
				}
			}
			if strings.TrimSpace(text) == strings.TrimSpace(profile.SystemPrompt) {
				identityFound = true
			}
		}
		if !billingFound {
			add("missing_billing_block")
		}
		if !identityFound {
			add("missing_agent_sdk_identity")
		}
	}

	metadataUserID := gjson.GetBytes(body, "metadata.user_id").String()
	parsedUserID := ParseMetadataUserID(metadataUserID)
	if parsedUserID == nil {
		add("invalid_metadata_user_id")
	} else if req != nil {
		if sessionHeader := getHeaderRaw(req.Header, "X-Claude-Code-Session-Id"); sessionHeader == "" {
			add("missing_session_header")
		} else if sessionHeader != parsedUserID.SessionID {
			add("session_header_mismatch")
		}
	}

	ok := len(findings) == 0
	return claudeMimicryAuditResult{
		OK:          ok,
		ShouldBlock: mode == claudeMimicryGuardBlock && !ok,
		Findings:    findings,
	}
}
