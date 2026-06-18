package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	anthropicSessionImportProxyModeAuto  = "auto"
	anthropicSessionImportProxyModeFixed = "fixed"
	anthropicSessionImportProxyModeNone  = "none"
)

type AnthropicSessionImportRequest struct {
	Content                 string         `json:"content"`
	Contents                []string       `json:"contents"`
	SessionKeys             []string       `json:"session_keys"`
	GroupIDs                []int64        `json:"group_ids"`
	ProxyMode               string         `json:"proxy_mode"`
	FixedProxyID            *int64         `json:"fixed_proxy_id"`
	AccountConcurrency      *int           `json:"account_concurrency"`
	Priority                *int           `json:"priority"`
	RateMultiplier          *float64       `json:"rate_multiplier"`
	LoadFactor              *int           `json:"load_factor"`
	ExpiresAt               *int64         `json:"expires_at"`
	AutoPauseOnExpired      *bool          `json:"auto_pause_on_expired"`
	CredentialExtras        map[string]any `json:"credential_extras"`
	Extra                   map[string]any `json:"extra"`
	UpdateExisting          *bool          `json:"update_existing"`
	SkipDefaultGroupBind    *bool          `json:"skip_default_group_bind"`
	ConfirmMixedChannelRisk *bool          `json:"confirm_mixed_channel_risk"`
	JobConcurrency          int            `json:"job_concurrency"`
	DelayMinMS              int            `json:"delay_min_ms"`
	DelayMaxMS              int            `json:"delay_max_ms"`
}

type AnthropicSessionImportResult struct {
	Total     int                          `json:"total"`
	Processed int                          `json:"processed"`
	Created   int                          `json:"created"`
	Updated   int                          `json:"updated"`
	Duplicate int                          `json:"duplicate"`
	Failed    int                          `json:"failed"`
	Items     []AnthropicSessionImportItem `json:"items,omitempty"`
}

type AnthropicSessionImportItem struct {
	Index          int    `json:"index"`
	Name           string `json:"name,omitempty"`
	Action         string `json:"action"`
	AccountID      int64  `json:"account_id,omitempty"`
	ProxyID        *int64 `json:"proxy_id,omitempty"`
	ProxyName      string `json:"proxy_name,omitempty"`
	SessionKeyHash string `json:"session_key_hash,omitempty"`
	Message        string `json:"message,omitempty"`
}

type AnthropicSessionImportJobSnapshot struct {
	ID         string                       `json:"id"`
	Status     string                       `json:"status"`
	Progress   float64                      `json:"progress"`
	Result     AnthropicSessionImportResult `json:"result"`
	Error      string                       `json:"error,omitempty"`
	StartedAt  time.Time                    `json:"started_at"`
	UpdatedAt  time.Time                    `json:"updated_at"`
	FinishedAt *time.Time                   `json:"finished_at,omitempty"`
	Cancelable bool                         `json:"cancelable"`
}

const (
	anthropicSessionImportJobRunning   = "running"
	anthropicSessionImportJobCompleted = "completed"
	anthropicSessionImportJobFailed    = "failed"
	anthropicSessionImportJobCanceled  = "canceled"
)

type anthropicSessionImportJob struct {
	mu         sync.Mutex
	id         string
	status     string
	result     AnthropicSessionImportResult
	errMessage string
	startedAt  time.Time
	updatedAt  time.Time
	finishedAt *time.Time
	cancel     context.CancelFunc
}

func (h *AccountHandler) StartAnthropicSessionImport(c *gin.Context) {
	var req AnthropicSessionImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if validationErr := validateAnthropicSessionImportRequest(req); validationErr != nil {
		response.BadRequest(c, validationErr.Error())
		return
	}
	entries := parseAnthropicSessionImportEntries(req)
	if len(entries) == 0 {
		response.BadRequest(c, "请输入至少一个 session key")
		return
	}
	if h.oauthService == nil {
		response.Error(c, http.StatusServiceUnavailable, "OAuth service is unavailable")
		return
	}

	h.anthropicSessionImportMu.Lock()
	defer h.anthropicSessionImportMu.Unlock()
	if h.anthropicSessionImportJobs == nil {
		h.anthropicSessionImportJobs = map[string]*anthropicSessionImportJob{}
	}
	if h.anthropicSessionImportActive != "" {
		if active := h.anthropicSessionImportJobs[h.anthropicSessionImportActive]; active != nil && active.isRunning() {
			response.Error(c, http.StatusConflict, "已有 Anthropic session key 导入任务正在运行")
			return
		}
		h.anthropicSessionImportActive = ""
	}

	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	job := &anthropicSessionImportJob{
		id:        newAnthropicSessionImportJobID(),
		status:    anthropicSessionImportJobRunning,
		result:    AnthropicSessionImportResult{Total: len(entries), Items: []AnthropicSessionImportItem{}},
		startedAt: now,
		updatedAt: now,
		cancel:    cancel,
	}
	h.anthropicSessionImportJobs[job.id] = job
	h.anthropicSessionImportActive = job.id

	go h.runAnthropicSessionImportJob(ctx, job, req, entries)
	response.Accepted(c, job.snapshot())
}

func (h *AccountHandler) GetAnthropicSessionImport(c *gin.Context) {
	job := h.getAnthropicSessionImportJob(c.Param("id"))
	if job == nil {
		response.Error(c, http.StatusNotFound, "导入任务不存在")
		return
	}
	response.Success(c, job.snapshot())
}

func (h *AccountHandler) CancelAnthropicSessionImport(c *gin.Context) {
	job := h.getAnthropicSessionImportJob(c.Param("id"))
	if job == nil {
		response.Error(c, http.StatusNotFound, "导入任务不存在")
		return
	}
	job.mu.Lock()
	cancel := job.cancel
	if job.status != anthropicSessionImportJobRunning {
		job.mu.Unlock()
		response.Success(c, job.snapshot())
		return
	}
	job.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	response.Success(c, job.snapshot())
}

func (h *AccountHandler) runAnthropicSessionImportJob(ctx context.Context, job *anthropicSessionImportJob, req AnthropicSessionImportRequest, entries []anthropicSessionImportEntry) {
	executor := newAnthropicSessionImportExecutor(anthropicSessionImportExecutorDeps{
		cookieAuth: func(ctx context.Context, sessionKey string, proxyID *int64) (*service.TokenInfo, error) {
			return h.oauthService.CookieAuth(ctx, &service.CookieAuthInput{
				SessionKey: sessionKey,
				ProxyID:    proxyID,
				Scope:      "inference",
			})
		},
		listAccounts: func(ctx context.Context) ([]service.Account, error) {
			return h.listAccountsFiltered(ctx, service.PlatformAnthropic, service.AccountTypeSetupToken, "", "", 0, "", "created_at", "desc")
		},
		listProxies: func(ctx context.Context) ([]service.ProxyWithAccountCount, error) {
			return h.adminService.GetAllProxiesWithAccountCount(ctx)
		},
		createAccount: h.adminService.CreateAccount,
		updateAccount: h.adminService.UpdateAccount,
	})

	result := executor.runWithProgress(ctx, req, entries, func(partial AnthropicSessionImportResult) {
		job.setResult(partial)
	})
	if ctx.Err() != nil {
		job.finish(anthropicSessionImportJobCanceled, result, "")
	} else if result.Failed > 0 && result.Processed == result.Failed {
		job.finish(anthropicSessionImportJobFailed, result, "所有导入项均失败")
	} else {
		job.finish(anthropicSessionImportJobCompleted, result, "")
	}

	h.anthropicSessionImportMu.Lock()
	if h.anthropicSessionImportActive == job.id {
		h.anthropicSessionImportActive = ""
	}
	h.anthropicSessionImportMu.Unlock()
}

func (h *AccountHandler) getAnthropicSessionImportJob(id string) *anthropicSessionImportJob {
	h.anthropicSessionImportMu.Lock()
	defer h.anthropicSessionImportMu.Unlock()
	return h.anthropicSessionImportJobs[id]
}

func (j *anthropicSessionImportJob) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status == anthropicSessionImportJobRunning
}

func (j *anthropicSessionImportJob) setResult(result AnthropicSessionImportResult) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.result = result
	j.updatedAt = time.Now()
}

func (j *anthropicSessionImportJob) finish(status string, result AnthropicSessionImportResult, errMessage string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	j.status = status
	j.result = result
	j.errMessage = errMessage
	j.updatedAt = now
	j.finishedAt = &now
}

func (j *anthropicSessionImportJob) snapshot() AnthropicSessionImportJobSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	result := j.result
	result.Items = append([]AnthropicSessionImportItem(nil), j.result.Items...)
	progress := 0.0
	if result.Total > 0 {
		progress = float64(result.Processed) / float64(result.Total)
	}
	if j.status != anthropicSessionImportJobRunning && result.Total > 0 {
		progress = 1
	}
	return AnthropicSessionImportJobSnapshot{
		ID:         j.id,
		Status:     j.status,
		Progress:   progress,
		Result:     result,
		Error:      j.errMessage,
		StartedAt:  j.startedAt,
		UpdatedAt:  j.updatedAt,
		FinishedAt: j.finishedAt,
		Cancelable: j.status == anthropicSessionImportJobRunning,
	}
}

type anthropicSessionImportEntry struct {
	Index      int
	SessionKey string
}

type anthropicSessionImportExecutorDeps struct {
	cookieAuth    func(context.Context, string, *int64) (*service.TokenInfo, error)
	listAccounts  func(context.Context) ([]service.Account, error)
	listProxies   func(context.Context) ([]service.ProxyWithAccountCount, error)
	createAccount func(context.Context, *service.CreateAccountInput) (*service.Account, error)
	updateAccount func(context.Context, int64, *service.UpdateAccountInput) (*service.Account, error)
	now           func() time.Time
}

type anthropicSessionImportExecutor struct {
	deps anthropicSessionImportExecutorDeps
}

func newAnthropicSessionImportExecutor(deps anthropicSessionImportExecutorDeps) *anthropicSessionImportExecutor {
	if deps.now == nil {
		deps.now = time.Now
	}
	return &anthropicSessionImportExecutor{deps: deps}
}

func (e *anthropicSessionImportExecutor) run(ctx context.Context, req AnthropicSessionImportRequest, entries []anthropicSessionImportEntry) AnthropicSessionImportResult {
	return e.runWithProgress(ctx, req, entries, nil)
}

func (e *anthropicSessionImportExecutor) runWithProgress(ctx context.Context, req AnthropicSessionImportRequest, entries []anthropicSessionImportEntry, onProgress func(AnthropicSessionImportResult)) AnthropicSessionImportResult {
	result := AnthropicSessionImportResult{
		Total: len(entries),
		Items: make([]AnthropicSessionImportItem, 0, len(entries)),
	}

	existingAccounts, err := e.deps.listAccounts(ctx)
	if err != nil {
		result = failAllAnthropicSessionImportEntries(result, entries, err)
		notifyAnthropicSessionImportProgress(onProgress, result)
		return result
	}
	accountIndex := buildAnthropicSessionImportAccountIndex(existingAccounts)

	proxyPicker, err := e.newProxyPicker(ctx, req)
	if err != nil {
		result = failAllAnthropicSessionImportEntries(result, entries, err)
		notifyAnthropicSessionImportProgress(onProgress, result)
		return result
	}

	updateExisting := true
	if req.UpdateExisting != nil {
		updateExisting = *req.UpdateExisting
	}
	accountConcurrency := 10
	if req.AccountConcurrency != nil {
		accountConcurrency = *req.AccountConcurrency
	}
	priority := 1
	if req.Priority != nil {
		priority = *req.Priority
	}
	skipDefaultGroupBind := req.SkipDefaultGroupBind != nil && *req.SkipDefaultGroupBind
	skipMixedChannelCheck := req.ConfirmMixedChannelRisk != nil && *req.ConfirmMixedChannelRisk

	seenIdentity := map[string]int{}
	for _, entry := range entries {
		if ctx.Err() != nil {
			break
		}
		sleepAnthropicSessionImportDelay(ctx, req)
		item := AnthropicSessionImportItem{
			Index:          entry.Index,
			SessionKeyHash: shortAnthropicSessionKeyHash(entry.SessionKey),
		}
		sessionKey := strings.TrimSpace(entry.SessionKey)
		if sessionKey == "" {
			item.Action = "failed"
			item.Message = "session key 为空"
			result.Processed++
			result.Failed++
			result.Items = append(result.Items, item)
			notifyAnthropicSessionImportProgress(onProgress, result)
			continue
		}

		proxy := proxyPicker.next()
		item.ProxyID = proxy.id
		item.ProxyName = proxy.name

		tokenInfo, authErr := e.deps.cookieAuth(ctx, sessionKey, proxy.id)
		if authErr != nil {
			item.Action = "failed"
			item.Message = sanitizeAnthropicSessionImportError(authErr)
			result.Processed++
			result.Failed++
			result.Items = append(result.Items, item)
			notifyAnthropicSessionImportProgress(onProgress, result)
			continue
		}

		credentials := buildAnthropicSessionImportCredentials(tokenInfo, sessionKey, req.CredentialExtras)
		extra := buildAnthropicSessionImportExtra(tokenInfo, req.Extra, e.deps.now())
		name := buildAnthropicSessionImportAccountName(tokenInfo, extra, entry.Index)
		item.Name = name
		identityKeys := buildAnthropicSessionImportIdentityKeys(credentials, extra)
		if duplicateIndex, ok := firstSeenAnthropicSessionIdentity(seenIdentity, identityKeys); ok {
			item.Action = "duplicate"
			item.Message = fmt.Sprintf("与第 %d 条导入项重复，已跳过", duplicateIndex)
			result.Processed++
			result.Duplicate++
			result.Items = append(result.Items, item)
			notifyAnthropicSessionImportProgress(onProgress, result)
			continue
		}
		markAnthropicSessionIdentitySeen(seenIdentity, identityKeys, entry.Index)

		if existing := accountIndex.Find(identityKeys); existing != nil && updateExisting {
			updateInput := &service.UpdateAccountInput{
				Credentials:           mergeCodexImportMap(existing.Credentials, credentials),
				Extra:                 mergeCodexImportMap(existing.Extra, extra),
				Concurrency:           req.AccountConcurrency,
				Priority:              req.Priority,
				RateMultiplier:        req.RateMultiplier,
				LoadFactor:            req.LoadFactor,
				ExpiresAt:             req.ExpiresAt,
				AutoPauseOnExpired:    req.AutoPauseOnExpired,
				SkipMixedChannelCheck: skipMixedChannelCheck,
			}
			if len(req.GroupIDs) > 0 {
				groupIDs := append([]int64(nil), req.GroupIDs...)
				updateInput.GroupIDs = &groupIDs
			}
			if proxy.id != nil {
				updateInput.ProxyID = proxy.id
			}
			updated, updateErr := e.deps.updateAccount(ctx, existing.ID, updateInput)
			if updateErr != nil {
				item.Action = "failed"
				item.Message = sanitizeAnthropicSessionImportError(updateErr)
				result.Processed++
				result.Failed++
				result.Items = append(result.Items, item)
				notifyAnthropicSessionImportProgress(onProgress, result)
				continue
			}
			item.Action = "updated"
			if updated != nil {
				item.AccountID = updated.ID
				item.Name = updated.Name
			} else {
				item.AccountID = existing.ID
			}
			result.Processed++
			result.Updated++
			result.Items = append(result.Items, item)
			notifyAnthropicSessionImportProgress(onProgress, result)
			continue
		}

		created, createErr := e.deps.createAccount(ctx, &service.CreateAccountInput{
			Name:                  name,
			Platform:              service.PlatformAnthropic,
			Type:                  service.AccountTypeSetupToken,
			Credentials:           credentials,
			Extra:                 extra,
			ProxyID:               proxy.id,
			Concurrency:           accountConcurrency,
			Priority:              priority,
			RateMultiplier:        req.RateMultiplier,
			LoadFactor:            req.LoadFactor,
			GroupIDs:              append([]int64(nil), req.GroupIDs...),
			ExpiresAt:             req.ExpiresAt,
			AutoPauseOnExpired:    req.AutoPauseOnExpired,
			SkipDefaultGroupBind:  skipDefaultGroupBind,
			SkipMixedChannelCheck: skipMixedChannelCheck,
		})
		if createErr != nil {
			item.Action = "failed"
			item.Message = sanitizeAnthropicSessionImportError(createErr)
			result.Processed++
			result.Failed++
			result.Items = append(result.Items, item)
			notifyAnthropicSessionImportProgress(onProgress, result)
			continue
		}
		item.Action = "created"
		if created != nil {
			item.AccountID = created.ID
			item.Name = created.Name
		}
		result.Processed++
		result.Created++
		result.Items = append(result.Items, item)
		notifyAnthropicSessionImportProgress(onProgress, result)
	}

	return result
}

type anthropicSessionImportProxyPick struct {
	id   *int64
	name string
}

type anthropicSessionImportProxyPicker struct {
	proxies []service.ProxyWithAccountCount
	nextIdx int
}

func (e *anthropicSessionImportExecutor) newProxyPicker(ctx context.Context, req AnthropicSessionImportRequest) (*anthropicSessionImportProxyPicker, error) {
	mode := strings.TrimSpace(req.ProxyMode)
	if mode == "" {
		mode = anthropicSessionImportProxyModeAuto
	}
	if mode == anthropicSessionImportProxyModeNone {
		return &anthropicSessionImportProxyPicker{}, nil
	}
	if mode == anthropicSessionImportProxyModeFixed {
		if req.FixedProxyID == nil {
			return nil, fmt.Errorf("fixed_proxy_id 不能为空")
		}
		return &anthropicSessionImportProxyPicker{proxies: []service.ProxyWithAccountCount{{
			Proxy: service.Proxy{
				ID:     *req.FixedProxyID,
				Status: service.StatusActive,
			},
		}}}, nil
	}

	proxies, err := e.deps.listProxies(ctx)
	if err != nil {
		return nil, err
	}
	now := e.deps.now()
	active := make([]service.ProxyWithAccountCount, 0, len(proxies))
	for _, proxy := range proxies {
		if proxy.IsActive() && !proxy.IsExpired(now) {
			active = append(active, proxy)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		if active[i].AccountCount != active[j].AccountCount {
			return active[i].AccountCount < active[j].AccountCount
		}
		return active[i].ID < active[j].ID
	})
	return &anthropicSessionImportProxyPicker{proxies: active}, nil
}

func (p *anthropicSessionImportProxyPicker) next() anthropicSessionImportProxyPick {
	if p == nil || len(p.proxies) == 0 {
		return anthropicSessionImportProxyPick{}
	}
	proxy := p.proxies[p.nextIdx%len(p.proxies)]
	p.nextIdx++
	id := proxy.ID
	name := strings.TrimSpace(proxy.Name)
	if name == "" {
		name = fmt.Sprintf("Proxy #%d", proxy.ID)
	}
	return anthropicSessionImportProxyPick{id: &id, name: name}
}

func buildAnthropicSessionImportCredentials(tokenInfo *service.TokenInfo, sessionKey string, extras map[string]any) map[string]any {
	credentials := map[string]any{
		"session_key": strings.TrimSpace(sessionKey),
	}
	if tokenInfo != nil {
		if tokenInfo.AccessToken != "" {
			credentials["access_token"] = tokenInfo.AccessToken
		}
		if tokenInfo.TokenType != "" {
			credentials["token_type"] = tokenInfo.TokenType
		}
		if tokenInfo.ExpiresIn != 0 {
			credentials["expires_in"] = tokenInfo.ExpiresIn
		}
		if tokenInfo.ExpiresAt != 0 {
			credentials["expires_at"] = tokenInfo.ExpiresAt
		}
		if tokenInfo.RefreshToken != "" {
			credentials["refresh_token"] = tokenInfo.RefreshToken
		}
		if tokenInfo.Scope != "" {
			credentials["scope"] = tokenInfo.Scope
		}
		if tokenInfo.OrgUUID != "" {
			credentials["org_uuid"] = tokenInfo.OrgUUID
		}
		if tokenInfo.AccountUUID != "" {
			credentials["account_uuid"] = tokenInfo.AccountUUID
		}
		if tokenInfo.EmailAddress != "" {
			credentials["email_address"] = tokenInfo.EmailAddress
		}
	}
	return mergeCodexImportMap(credentials, extras)
}

func buildAnthropicSessionImportExtra(tokenInfo *service.TokenInfo, reqExtra map[string]any, now time.Time) map[string]any {
	extra := mergeCodexImportMap(nil, reqExtra)
	extra["import_source"] = "bulk_session_key"
	extra["imported_at"] = now.UTC().Format(time.RFC3339)
	if tokenInfo == nil {
		return extra
	}
	if tokenInfo.OrgUUID != "" {
		extra["org_uuid"] = tokenInfo.OrgUUID
	}
	if tokenInfo.AccountUUID != "" {
		extra["account_uuid"] = tokenInfo.AccountUUID
	}
	if tokenInfo.EmailAddress != "" {
		extra["email_address"] = tokenInfo.EmailAddress
	}
	return extra
}

func buildAnthropicSessionImportAccountName(tokenInfo *service.TokenInfo, extra map[string]any, index int) string {
	email := ""
	if tokenInfo != nil {
		email = strings.TrimSpace(tokenInfo.EmailAddress)
	}
	if email == "" {
		email = fmt.Sprintf("Anthropic SetupToken #%d", index)
	}
	plan := detectAnthropicSessionImportSubscription(extra)
	if plan == "" {
		return email
	}
	return email + " " + plan
}

func detectAnthropicSessionImportSubscription(values map[string]any) string {
	for _, key := range []string{
		"anthropic_subscription_type",
		"subscription_type",
		"plan_type",
		"plan",
		"claude_plan",
	} {
		if value := strings.TrimSpace(fmt.Sprint(values[key])); value != "" && value != "<nil>" {
			return normalizeAnthropicSessionImportSubscription(value)
		}
	}
	return ""
}

func normalizeAnthropicSessionImportSubscription(value string) string {
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	switch {
	case strings.Contains(lower, "max"):
		return "Max"
	case strings.Contains(lower, "team"):
		return "Team"
	case strings.Contains(lower, "pro"):
		return "Pro"
	}
	return value
}

type anthropicSessionImportAccountIndex struct {
	accountsByKey map[string]service.Account
}

func buildAnthropicSessionImportAccountIndex(accounts []service.Account) *anthropicSessionImportAccountIndex {
	index := &anthropicSessionImportAccountIndex{accountsByKey: map[string]service.Account{}}
	for _, account := range accounts {
		if account.Platform != service.PlatformAnthropic || account.Type != service.AccountTypeSetupToken {
			continue
		}
		for _, key := range buildAnthropicSessionImportIdentityKeys(account.Credentials, account.Extra) {
			index.accountsByKey[key] = account
		}
	}
	return index
}

func (i *anthropicSessionImportAccountIndex) Find(keys []string) *service.Account {
	if i == nil {
		return nil
	}
	for _, key := range keys {
		if account, ok := i.accountsByKey[key]; ok {
			return &account
		}
	}
	return nil
}

func buildAnthropicSessionImportIdentityKeys(credentials, extra map[string]any) []string {
	keys := make([]string, 0, 4)
	for _, source := range []map[string]any{credentials, extra} {
		if accountUUID := strings.TrimSpace(fmt.Sprint(source["account_uuid"])); accountUUID != "" && accountUUID != "<nil>" {
			keys = append(keys, "account:"+strings.ToLower(accountUUID))
		}
		if orgUUID := strings.TrimSpace(fmt.Sprint(source["org_uuid"])); orgUUID != "" && orgUUID != "<nil>" {
			keys = append(keys, "org:"+strings.ToLower(orgUUID))
		}
		if email := strings.TrimSpace(fmt.Sprint(source["email_address"])); email != "" && email != "<nil>" {
			keys = append(keys, "email:"+strings.ToLower(email))
		}
	}
	if sessionKey := strings.TrimSpace(fmt.Sprint(credentials["session_key"])); sessionKey != "" && sessionKey != "<nil>" {
		keys = append(keys, "session:"+shortAnthropicSessionKeyHash(sessionKey))
	}
	return keys
}

func firstSeenAnthropicSessionIdentity(seen map[string]int, keys []string) (int, bool) {
	for _, key := range keys {
		if index, ok := seen[key]; ok {
			return index, true
		}
	}
	return 0, false
}

func markAnthropicSessionIdentitySeen(seen map[string]int, keys []string, index int) {
	for _, key := range keys {
		seen[key] = index
	}
}

func shortAnthropicSessionKeyHash(sessionKey string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sessionKey)))
	return hex.EncodeToString(sum[:])[:12]
}

func sanitizeAnthropicSessionImportError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	for _, tokenPrefix := range []string{"sk-ant-", "sk-ant-sid"} {
		if strings.Contains(message, tokenPrefix) {
			return "导入失败，认证服务返回错误"
		}
	}
	return message
}

func failAllAnthropicSessionImportEntries(result AnthropicSessionImportResult, entries []anthropicSessionImportEntry, err error) AnthropicSessionImportResult {
	message := sanitizeAnthropicSessionImportError(err)
	for _, entry := range entries {
		result.Processed++
		result.Failed++
		result.Items = append(result.Items, AnthropicSessionImportItem{
			Index:          entry.Index,
			Action:         "failed",
			SessionKeyHash: shortAnthropicSessionKeyHash(entry.SessionKey),
			Message:        message,
		})
	}
	return result
}

func validateAnthropicSessionImportRequest(req AnthropicSessionImportRequest) error {
	if req.AccountConcurrency != nil && *req.AccountConcurrency < 0 {
		return fmt.Errorf("account_concurrency must be >= 0")
	}
	if req.Priority != nil && *req.Priority < 0 {
		return fmt.Errorf("priority must be >= 0")
	}
	if req.RateMultiplier != nil && *req.RateMultiplier < 0 {
		return fmt.Errorf("rate_multiplier must be >= 0")
	}
	if req.LoadFactor != nil && *req.LoadFactor > 10000 {
		return fmt.Errorf("load_factor must be <= 10000")
	}
	if req.JobConcurrency < 0 {
		return fmt.Errorf("job_concurrency must be >= 0")
	}
	if req.DelayMinMS < 0 || req.DelayMaxMS < 0 {
		return fmt.Errorf("delay must be >= 0")
	}
	if req.DelayMaxMS > 0 && req.DelayMinMS > req.DelayMaxMS {
		return fmt.Errorf("delay_min_ms must be <= delay_max_ms")
	}
	return nil
}

func parseAnthropicSessionImportEntries(req AnthropicSessionImportRequest) []anthropicSessionImportEntry {
	values := make([]string, 0, len(req.SessionKeys)+len(req.Contents)+1)
	values = append(values, req.SessionKeys...)
	values = append(values, req.Contents...)
	if strings.TrimSpace(req.Content) != "" {
		values = append(values, strings.Split(req.Content, "\n")...)
	}
	entries := make([]anthropicSessionImportEntry, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		entries = append(entries, anthropicSessionImportEntry{
			Index:      len(entries) + 1,
			SessionKey: value,
		})
	}
	return entries
}

func notifyAnthropicSessionImportProgress(onProgress func(AnthropicSessionImportResult), result AnthropicSessionImportResult) {
	if onProgress == nil {
		return
	}
	result.Items = append([]AnthropicSessionImportItem(nil), result.Items...)
	onProgress(result)
}

func sleepAnthropicSessionImportDelay(ctx context.Context, req AnthropicSessionImportRequest) {
	if req.DelayMaxMS <= 0 {
		return
	}
	minMS := req.DelayMinMS
	maxMS := req.DelayMaxMS
	if maxMS < minMS {
		maxMS = minMS
	}
	delayMS := minMS
	if maxMS > minMS {
		delayMS += cryptoRandomInt(maxMS - minMS + 1)
	}
	timer := time.NewTimer(time.Duration(delayMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func cryptoRandomInt(max int) int {
	if max <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(n.Int64())
}

func newAnthropicSessionImportJobID() string {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err == nil {
		return "asi_" + hex.EncodeToString(random)
	}
	return fmt.Sprintf("asi_%d", time.Now().UnixNano())
}
