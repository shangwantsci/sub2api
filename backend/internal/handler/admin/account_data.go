package admin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"log/slog"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	dataType       = "sub2api-data"
	legacyDataType = "sub2api-bundle"
	dataVersion    = 1
	dataPageCap    = 1000
)

type DataPayload struct {
	Type       string        `json:"type,omitempty"`
	Version    int           `json:"version,omitempty"`
	ExportedAt string        `json:"exported_at"`
	Proxies    []DataProxy   `json:"proxies"`
	Accounts   []DataAccount `json:"accounts"`
	// SkippedShadows 记录导出时被排除的 spark 影子账号数量(见 ExportData)。仅作可见性提示,
	// 导入侧忽略该字段;omitempty 保持向后兼容。
	SkippedShadows int `json:"skipped_shadows,omitempty"`
}

type DataProxy struct {
	ProxyKey        string `json:"proxy_key"`
	Name            string `json:"name"`
	Protocol        string `json:"protocol"`
	Host            string `json:"host"`
	Port            int    `json:"port"`
	Username        string `json:"username,omitempty"`
	Password        string `json:"password,omitempty"`
	Status          string `json:"status"`
	ExpiresAt       *int64 `json:"expires_at,omitempty"`        // unix 秒，与 DataAccount.ExpiresAt 风格一致
	FallbackMode    string `json:"fallback_mode,omitempty"`     // none/direct/proxy
	BackupProxyName string `json:"backup_proxy_name,omitempty"` // 备用代理 name（跨实例按 name 反查）
	ExpiryWarnDays  int    `json:"expiry_warn_days,omitempty"`
	// ProviderUserID 是代理归属：空表示平台自有，非空表示某个供号商上号时自带。
	//
	// 必须随备份走。丢了这一列，供号商自带的代理恢复后会被当成平台自有，
	// 管理员一旦把它勾进自动分配池，就会把这家自费的出口分给另一家供号商，
	// 两家账号还会共用同一个出口 IP。
	//
	// 注意跨实例恢复时用户 id 未必对得上（proxies 上没有外键约束），
	// 悬空的 id 只会让这条代理永远进不了自动分配池，是安全方向的降级。
	ProviderUserID *int64 `json:"provider_user_id,omitempty"`
	// AutoAssignable 是管理员是否把这条代理放进供号商自动分配池。
	AutoAssignable bool `json:"auto_assignable,omitempty"`
}

// DataAccount 是管理员显式备份导出使用的账号结构，故意不走 dto.Account 的脱敏路径，
// Credentials 原文返回。这是"管理员备份"这一显式行为的一部分；如未来需要导出脱敏版本，
// 应新增独立结构而非修改这里。
// 注意:本结构不含 parent_account_id/quota_dimension——spark 影子账号在 ExportData 处被显式
// 排除(影子不持凭据、通用凭据型导入强制 credentials 非空无法重建父子链接),不在此表达。
// 影子的独立调度配置(priority/并发/分组/status 管理员可单独调)亦不在本备份范围,属已知局限
// (外审第6轮裁决:保持排除 + 前端警告,而非升级格式做完整往返)。
type DataAccount struct {
	Name               string         `json:"name"`
	Notes              *string        `json:"notes,omitempty"`
	Platform           string         `json:"platform"`
	Type               string         `json:"type"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra,omitempty"`
	ProxyKey           *string        `json:"proxy_key,omitempty"`
	Concurrency        int            `json:"concurrency"`
	Priority           int            `json:"priority"`
	RateMultiplier     *float64       `json:"rate_multiplier,omitempty"`
	ExpiresAt          *int64         `json:"expires_at,omitempty"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired,omitempty"`
	// ProviderUserID / ProviderTier 是供号商归属与速率档位。
	//
	// 必须随备份走：结算完全按 provider_user_id 聚合，丢了这一列，恢复出来的账号
	// 就不再计入任何供号商 —— 直接少付钱，而且全程没有任何报错。
	//
	// 只对**同实例**恢复有效：users 表没有导出功能，换一个实例恢复时这个 id 指向的
	// 用户根本不存在，归属等于没恢复。跨实例迁移只能用整库备份。
	ProviderUserID *int64  `json:"provider_user_id,omitempty"`
	ProviderTier   *string `json:"provider_tier,omitempty"`
	// Status / Schedulable 记录账号当时是否启用、是否参与调度。
	//
	// 不带的话导入一律建成 active + schedulable，被手工停用（含风控停用）的账号
	// 恢复后会立刻重新接客。
	Status      string `json:"status,omitempty"`
	Schedulable *bool  `json:"schedulable,omitempty"`
	// GroupIDs 是账号绑定的分组。
	//
	// 一个组都没绑的账号不参与调度，用量恒为 0，结算自然也是 0 —— 归属恢复对了也白搭。
	// 与 provider_user_id 一样只对同实例恢复有效；导入时会过滤掉本实例不存在的分组
	// 并在结果里报出来。
	GroupIDs []int64 `json:"group_ids,omitempty"`
}

type DataImportRequest struct {
	Data                 DataPayload `json:"data"`
	SkipDefaultGroupBind *bool       `json:"skip_default_group_bind"`
}

type DataImportResult struct {
	ProxyCreated   int               `json:"proxy_created"`
	ProxyReused    int               `json:"proxy_reused"`
	ProxyFailed    int               `json:"proxy_failed"`
	AccountCreated int               `json:"account_created"`
	AccountFailed  int               `json:"account_failed"`
	Errors         []DataImportError `json:"errors,omitempty"`
}

type DataImportError struct {
	Kind     string `json:"kind"`
	Name     string `json:"name,omitempty"`
	ProxyKey string `json:"proxy_key,omitempty"`
	Message  string `json:"message"`
}

func buildProxyKey(protocol, host string, port int, username, password string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%s", strings.TrimSpace(protocol), strings.TrimSpace(host), port, strings.TrimSpace(username), strings.TrimSpace(password))
}

// buildOwnedProxyKey 在连接串指纹上叠加归属。
//
// 两个供号商完全可能从同一家买到同一个 host:port:user:pass。不带归属的话，导入时
// 后一条会命中前一条被「复用」掉，于是 B 的账号走上了 A 自费的出口 —— 两家账号共用
// 同一个出口 IP，正是本 fork 最不能出的问题。
//
// 只在有归属时加后缀，平台代理保持原格式：旧备份根本没有 provider_user_id 字段，
// 其中的代理会被当成平台自有，key 仍与库里的平台代理一致，不会因为格式变化而重复建号。
func buildOwnedProxyKey(base string, providerUserID *int64) string {
	if providerUserID == nil {
		return base
	}
	return base + "|provider:" + strconv.FormatInt(*providerUserID, 10)
}

// providerOwnerChecker 校验备份声明的归属在本实例上确实是一个供号商。
//
// 归属是个裸 id，跨实例恢复时它可能指向不存在的用户，甚至指向另一个真实用户：
// 前者让这批账号的用量不计入任何供号商（少付），后者直接把钱付给了错的人。
// 校验不过就丢掉归属并报出来 —— 落成「管理员自有」还能人工纠正，挂到错误的
// 供号商名下则会一路静默付错。
type providerOwnerChecker struct {
	svc   service.AdminService
	cache map[int64]bool
}

func newProviderOwnerChecker(svc service.AdminService) *providerOwnerChecker {
	return &providerOwnerChecker{svc: svc, cache: make(map[int64]bool)}
}

func (c *providerOwnerChecker) isProvider(ctx context.Context, id int64) bool {
	if c == nil || c.svc == nil || id <= 0 {
		return false
	}
	if ok, found := c.cache[id]; found {
		return ok
	}
	user, err := c.svc.GetUser(ctx, id)
	ok := err == nil && user != nil && user.IsProviderUser()
	c.cache[id] = ok
	return ok
}

// sanitize 返回可安全写入的归属，以及需要报给管理员的说明（为空表示无异常）。
func (c *providerOwnerChecker) sanitize(ctx context.Context, owner *int64) (*int64, string) {
	if owner == nil {
		return nil, ""
	}
	if *owner <= 0 {
		return nil, fmt.Sprintf("invalid provider_user_id %d; ownership dropped", *owner)
	}
	if !c.isProvider(ctx, *owner) {
		return nil, fmt.Sprintf(
			"provider_user_id %d is not a provider on this instance; ownership dropped", *owner)
	}
	return owner, ""
}

// proxyOwnershipMismatch 在复用已有代理时比对归属与共享开关，不一致时返回一条说明。
//
// 复用路径刻意不改这两个字段：归属只在创建时确定（UpdateProxyInput 里根本没有它），
// 而共享开关是安全敏感的，一份旧备份不该悄悄把管理员后来关掉的开关重新打开。
// 但静默忽略同样危险——恢复方会以为归属跟着回来了，所以差异必须显式报出来。
func proxyOwnershipMismatch(item DataProxy, existing service.Proxy) string {
	var parts []string
	if !sameProviderOwner(item.ProviderUserID, existing.ProviderUserID) {
		parts = append(parts, fmt.Sprintf("provider_user_id=%s (existing %s)",
			formatProviderOwner(item.ProviderUserID), formatProviderOwner(existing.ProviderUserID)))
	}
	if item.AutoAssignable != existing.AutoAssignable {
		parts = append(parts, fmt.Sprintf("auto_assignable=%t (existing %t)",
			item.AutoAssignable, existing.AutoAssignable))
	}
	if len(parts) == 0 {
		return ""
	}
	return "reused existing proxy keeps its own ownership settings; import declared " +
		strings.Join(parts, ", ")
}

// loadKnownGroupIDs 取本实例现有的分组 id 集合。
//
// 必须用 IncludingInactive：GetAllGroups 实际是 ListActive，停用的分组会被当成
// 「不存在」而把绑定丢掉——同实例恢复时那明明是一个真实存在、只是暂时停用的分组。
func (h *AccountHandler) loadKnownGroupIDs(ctx context.Context) (map[int64]struct{}, error) {
	groups, err := h.adminService.GetAllGroupsIncludingInactive(ctx)
	if err != nil {
		return nil, fmt.Errorf("load groups for import: %w", err)
	}
	known := make(map[int64]struct{}, len(groups))
	for i := range groups {
		known[groups[i].ID] = struct{}{}
	}
	return known, nil
}

// filterKnownGroupIDs 把备份里的分组 id 拆成「本实例存在的」与「对不上的」两组。
func filterKnownGroupIDs(ids []int64, known map[int64]struct{}) (kept, missing []int64) {
	for _, id := range ids {
		if _, ok := known[id]; ok {
			kept = append(kept, id)
			continue
		}
		missing = append(missing, id)
	}
	return kept, missing
}

func sameProviderOwner(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func formatProviderOwner(v *int64) string {
	if v == nil {
		return "none"
	}
	return strconv.FormatInt(*v, 10)
}

func (h *AccountHandler) ExportData(c *gin.Context) {
	ctx := c.Request.Context()

	selectedIDs, err := parseAccountIDs(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	accounts, err := h.resolveExportAccounts(ctx, selectedIDs, c)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 排除 spark 影子账号:影子不持凭据,通用凭据型导出无法表达父子链接、导入侧又强制 credentials
	// 非空——若混入会产出无法还原的坏备份(导入即失败)。影子的独立调度配置(priority/并发/分组/
	// status,管理员可单独调)随之不进备份,还原后需在重建的影子上重新调优;前端按 skipped_shadows
	// 提示用户(外审第5轮发现、第6轮裁决:保持排除 + 警告,不做完整往返)。
	skippedShadows := 0
	exportable := make([]service.Account, 0, len(accounts))
	for i := range accounts {
		if accounts[i].IsCredentialShadow() {
			skippedShadows++
			continue
		}
		exportable = append(exportable, accounts[i])
	}
	accounts = exportable
	if skippedShadows > 0 {
		slog.Info("export_skipped_spark_shadows", "count", skippedShadows)
	}

	includeProxies, err := parseIncludeProxies(c)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	var proxies []service.Proxy
	if includeProxies {
		proxies, err = h.resolveExportProxies(ctx, accounts)
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	} else {
		proxies = []service.Proxy{}
	}

	// 构建 id→name 映射，用于导出备用代理 name
	proxyNameByID := make(map[int64]string, len(proxies))
	for i := range proxies {
		proxyNameByID[proxies[i].ID] = proxies[i].Name
	}

	proxyKeyByID := make(map[int64]string, len(proxies))
	dataProxies := make([]DataProxy, 0, len(proxies))
	for i := range proxies {
		p := proxies[i]
		key := buildOwnedProxyKey(
			buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password), p.ProviderUserID)
		proxyKeyByID[p.ID] = key

		var expiresAt *int64
		if p.ExpiresAt != nil {
			v := p.ExpiresAt.Unix()
			expiresAt = &v
		}
		var backupProxyName string
		if p.BackupProxyID != nil {
			backupProxyName = proxyNameByID[*p.BackupProxyID]
		}
		dataProxies = append(dataProxies, DataProxy{
			ProxyKey:        key,
			Name:            p.Name,
			Protocol:        p.Protocol,
			Host:            p.Host,
			Port:            p.Port,
			Username:        p.Username,
			Password:        p.Password,
			Status:          p.Status,
			ExpiresAt:       expiresAt,
			FallbackMode:    p.FallbackMode,
			BackupProxyName: backupProxyName,
			ExpiryWarnDays:  p.ExpiryWarnDays,
			ProviderUserID:  p.ProviderUserID,
			AutoAssignable:  p.AutoAssignable,
		})
	}

	dataAccounts := make([]DataAccount, 0, len(accounts))
	for i := range accounts {
		acc := accounts[i]
		var proxyKey *string
		if acc.ProxyID != nil {
			if key, ok := proxyKeyByID[*acc.ProxyID]; ok {
				proxyKey = &key
			}
		}
		var expiresAt *int64
		if acc.ExpiresAt != nil {
			v := acc.ExpiresAt.Unix()
			expiresAt = &v
		}
		dataAccounts = append(dataAccounts, DataAccount{
			Name:               acc.Name,
			Notes:              acc.Notes,
			Platform:           acc.Platform,
			Type:               acc.Type,
			Credentials:        acc.Credentials,
			Extra:              acc.Extra,
			ProxyKey:           proxyKey,
			Concurrency:        acc.Concurrency,
			Priority:           acc.Priority,
			RateMultiplier:     acc.RateMultiplier,
			ExpiresAt:          expiresAt,
			AutoPauseOnExpired: &acc.AutoPauseOnExpired,
			ProviderUserID:     acc.ProviderUserID,
			ProviderTier:       acc.ProviderTier,
			Status:             acc.Status,
			Schedulable:        &acc.Schedulable,
			GroupIDs:           append([]int64(nil), acc.GroupIDs...),
		})
	}

	payload := DataPayload{
		// 这两个字段的常量早就定义了，但导出侧一直没赋值，导致真实导出的 JSON 里
		// 根本没有 type/version，validateDataHeader 对空值放行等于没有格式校验。
		Type:           dataType,
		Version:        dataVersion,
		ExportedAt:     time.Now().UTC().Format(time.RFC3339),
		Proxies:        dataProxies,
		Accounts:       dataAccounts,
		SkippedShadows: skippedShadows,
	}

	response.Success(c, payload)
}

func (h *AccountHandler) ImportData(c *gin.Context) {
	var req DataImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	if err := validateDataHeader(req.Data); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	executeAdminIdempotentJSON(c, "admin.accounts.import_data", req, service.DefaultWriteIdempotencyTTL(), func(ctx context.Context) (any, error) {
		return h.importData(ctx, req)
	})
}

func (h *AccountHandler) importData(ctx context.Context, req DataImportRequest) (DataImportResult, error) {
	skipDefaultGroupBind := true
	if req.SkipDefaultGroupBind != nil {
		skipDefaultGroupBind = *req.SkipDefaultGroupBind
	}

	dataPayload := req.Data
	result := DataImportResult{}

	existingProxies, err := h.listAllProxies(ctx)
	if err != nil {
		return result, err
	}

	proxyKeyToID := make(map[string]int64, len(existingProxies))
	// existingProxyByKey 供复用路径比对归属，避免为此再查一次库。
	existingProxyByKey := make(map[string]service.Proxy, len(existingProxies))
	// proxyNameToID 用于 backup_proxy_name 反查：DB 已有 + 本批次新建均会写入
	proxyNameToID := make(map[string]int64, len(existingProxies))
	for i := range existingProxies {
		p := existingProxies[i]
		key := buildOwnedProxyKey(
			buildProxyKey(p.Protocol, p.Host, p.Port, p.Username, p.Password), p.ProviderUserID)
		proxyKeyToID[key] = p.ID
		existingProxyByKey[key] = p
		if p.Name != "" {
			proxyNameToID[p.Name] = p.ID
		}
	}
	ownerChecker := newProviderOwnerChecker(h.adminService)

	for i := range dataPayload.Proxies {
		item := dataPayload.Proxies[i]
		key := item.ProxyKey
		if key == "" {
			// 必须按备份声明的归属重算，才能与账号侧的 proxy_key 引用对上。
			key = buildOwnedProxyKey(
				buildProxyKey(item.Protocol, item.Host, item.Port, item.Username, item.Password),
				item.ProviderUserID)
		}
		if err := validateDataProxy(item); err != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:     "proxy",
				Name:     item.Name,
				ProxyKey: key,
				Message:  err.Error(),
			})
			continue
		}
		normalizedStatus := normalizeProxyStatus(item.Status)
		if existingID, ok := proxyKeyToID[key]; ok {
			proxyKeyToID[key] = existingID
			result.ProxyReused++
			if msg := proxyOwnershipMismatch(item, existingProxyByKey[key]); msg != "" {
				result.Errors = append(result.Errors, DataImportError{
					Kind:     "proxy",
					Name:     item.Name,
					ProxyKey: key,
					Message:  msg,
				})
			}
			if normalizedStatus != "" {
				if proxy, getErr := h.adminService.GetProxy(ctx, existingID); getErr == nil && proxy != nil && proxy.Status != normalizedStatus {
					// 同步 status 时传入完整字段，避免零值覆盖已存在代理的有效期/fallback 配置。
					var existingExpiresAt *time.Time
					if item.ExpiresAt != nil {
						t := time.Unix(*item.ExpiresAt, 0).UTC()
						existingExpiresAt = &t
					}
					existingFallbackMode := item.FallbackMode
					if existingFallbackMode == "" {
						existingFallbackMode = service.FallbackModeNone
					}
					var existingBackupProxyID *int64
					if item.BackupProxyName != "" {
						if bid, ok := proxyNameToID[item.BackupProxyName]; ok {
							existingBackupProxyID = &bid
						}
					}
					_, _ = h.adminService.UpdateProxy(ctx, existingID, &service.UpdateProxyInput{
						Status:         normalizedStatus,
						ExpiresAt:      existingExpiresAt,
						FallbackMode:   existingFallbackMode,
						BackupProxyID:  existingBackupProxyID,
						ExpiryWarnDays: item.ExpiryWarnDays,
						Name:           proxy.Name,
						Protocol:       proxy.Protocol,
						Host:           proxy.Host,
						Port:           proxy.Port,
						Username:       proxy.Username,
						Password:       proxy.Password,
					})
				}
			}
			continue
		}

		// 解析 expires_at（unix 秒 → *time.Time）
		var expiresAt *time.Time
		if item.ExpiresAt != nil {
			t := time.Unix(*item.ExpiresAt, 0).UTC()
			expiresAt = &t
		}

		// 解析 backup_proxy_name → backup_proxy_id
		fallbackMode := item.FallbackMode
		var backupProxyID *int64
		if item.BackupProxyName != "" {
			if bid, ok := proxyNameToID[item.BackupProxyName]; ok {
				backupProxyID = &bid
			} else {
				// 查不到备用代理：降级 fallback_mode=none，记录 warning
				fallbackMode = service.FallbackModeNone
				result.Errors = append(result.Errors, DataImportError{
					Kind:     "proxy",
					Name:     item.Name,
					ProxyKey: key,
					Message:  fmt.Sprintf("backup_proxy_name %q not found, fallback_mode downgraded to none", item.BackupProxyName),
				})
			}
		}

		proxyOwner, proxyOwnerNote := ownerChecker.sanitize(ctx, item.ProviderUserID)
		autoAssignable := item.AutoAssignable
		if proxyOwnerNote != "" {
			// 归属没落上，就绝不能顺带把这条代理变成可共享的平台代理。
			autoAssignable = false
			result.Errors = append(result.Errors, DataImportError{
				Kind: "proxy", Name: item.Name, ProxyKey: key, Message: proxyOwnerNote,
			})
		}

		created, createErr := h.adminService.CreateProxy(ctx, &service.CreateProxyInput{
			Name:           defaultProxyName(item.Name),
			Protocol:       item.Protocol,
			Host:           item.Host,
			Port:           item.Port,
			Username:       item.Username,
			Password:       item.Password,
			ExpiresAt:      expiresAt,
			FallbackMode:   fallbackMode,
			BackupProxyID:  backupProxyID,
			ExpiryWarnDays: item.ExpiryWarnDays,
			ProviderUserID: proxyOwner,
			AutoAssignable: autoAssignable,
		})
		if createErr != nil {
			result.ProxyFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:     "proxy",
				Name:     item.Name,
				ProxyKey: key,
				Message:  createErr.Error(),
			})
			continue
		}
		proxyKeyToID[key] = created.ID
		// 把新建代理的 name 也加入反查表，供后续批内代理引用
		if created.Name != "" {
			proxyNameToID[created.Name] = created.ID
		}
		result.ProxyCreated++

		if normalizedStatus != "" && normalizedStatus != created.Status {
			// 新建后同步 status 时，传入完整字段，避免零值覆盖刚创建的有效期/fallback 配置。
			_, _ = h.adminService.UpdateProxy(ctx, created.ID, &service.UpdateProxyInput{
				Status:         normalizedStatus,
				ExpiresAt:      expiresAt,
				FallbackMode:   fallbackMode,
				BackupProxyID:  backupProxyID,
				ExpiryWarnDays: item.ExpiryWarnDays,
				Name:           created.Name,
				Protocol:       created.Protocol,
				Host:           created.Host,
				Port:           created.Port,
				Username:       created.Username,
				Password:       created.Password,
			})
		}
	}

	// 备份里的分组 id 只在同实例有意义。先取一份本实例现有的分组，导入时把对不上的
	// 过滤掉：让账号建成功、少绑几个组，比整批失败可用得多；但绝不能静默丢弃——
	// 一个组都没绑上的账号不参与调度，用量恒为 0，结算也就恒为 0。
	knownGroupIDs, err := h.loadKnownGroupIDs(ctx)
	if err != nil {
		return result, err
	}

	// 收集需要异步设置隐私的 Antigravity OAuth 账号
	var privacyAccounts []*service.Account

	for i := range dataPayload.Accounts {
		item := dataPayload.Accounts[i]
		if err := validateDataAccount(item); err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    item.Name,
				Message: err.Error(),
			})
			continue
		}

		var proxyID *int64
		if item.ProxyKey != nil && *item.ProxyKey != "" {
			if id, ok := proxyKeyToID[*item.ProxyKey]; ok {
				proxyID = &id
			} else {
				result.AccountFailed++
				result.Errors = append(result.Errors, DataImportError{
					Kind:     "account",
					Name:     item.Name,
					ProxyKey: *item.ProxyKey,
					Message:  "proxy_key not found",
				})
				continue
			}
		}

		enrichCredentialsFromIDToken(&item)

		groupIDs, missingGroups := filterKnownGroupIDs(item.GroupIDs, knownGroupIDs)
		if len(missingGroups) > 0 {
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    item.Name,
				Message: fmt.Sprintf("group ids %v do not exist on this instance; the account was imported without them", missingGroups),
			})
		}

		accountOwner, accountOwnerNote := ownerChecker.sanitize(ctx, item.ProviderUserID)
		accountTier := item.ProviderTier
		if accountOwnerNote != "" {
			// 没有归属的档位没有意义：按档位回填只筛 provider_user_id 非空的账号。
			accountTier = nil
			result.Errors = append(result.Errors, DataImportError{
				Kind: "account", Name: item.Name, Message: accountOwnerNote,
			})
		}

		accountInput := &service.CreateAccountInput{
			Name:                 item.Name,
			Notes:                item.Notes,
			Platform:             item.Platform,
			Type:                 item.Type,
			Credentials:          item.Credentials,
			Extra:                item.Extra,
			ProxyID:              proxyID,
			Concurrency:          item.Concurrency,
			Priority:             item.Priority,
			RateMultiplier:       item.RateMultiplier,
			GroupIDs:             groupIDs,
			ExpiresAt:            item.ExpiresAt,
			AutoPauseOnExpired:   item.AutoPauseOnExpired,
			SkipDefaultGroupBind: skipDefaultGroupBind,
			ProviderUserID:       accountOwner,
			ProviderTier:         accountTier,
			Status:               item.Status,
			Schedulable:          item.Schedulable,
		}

		created, err := h.adminService.CreateAccount(ctx, accountInput)
		if err != nil {
			result.AccountFailed++
			result.Errors = append(result.Errors, DataImportError{
				Kind:    "account",
				Name:    item.Name,
				Message: err.Error(),
			})
			continue
		}
		// 收集 Antigravity OAuth 账号，稍后异步设置隐私
		if created.Platform == service.PlatformAntigravity && created.Type == service.AccountTypeOAuth {
			privacyAccounts = append(privacyAccounts, created)
		}
		h.scheduleGrokImportProbe(created)
		result.AccountCreated++
	}

	// 异步设置 Antigravity 隐私，避免大量导入时阻塞请求
	if len(privacyAccounts) > 0 {
		adminSvc := h.adminService
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("import_antigravity_privacy_panic", "recover", r)
				}
			}()
			bgCtx := context.Background()
			for _, acc := range privacyAccounts {
				adminSvc.ForceAntigravityPrivacy(bgCtx, acc)
			}
			slog.Info("import_antigravity_privacy_done", "count", len(privacyAccounts))
		}()
	}

	return result, nil
}

func (h *AccountHandler) listAllProxies(ctx context.Context) ([]service.Proxy, error) {
	page := 1
	pageSize := dataPageCap
	var out []service.Proxy
	for {
		items, total, err := h.adminService.ListProxies(ctx, page, pageSize, "", "", "", "created_at", "desc")
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) >= int(total) || len(items) == 0 {
			break
		}
		page++
	}
	return out, nil
}

func (h *AccountHandler) listAccountsFiltered(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode, sortBy, sortOrder string) ([]service.Account, error) {
	page := 1
	pageSize := dataPageCap
	var out []service.Account
	for {
		items, total, err := h.adminService.ListAccounts(ctx, page, pageSize, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
		if len(out) >= int(total) || len(items) == 0 {
			break
		}
		page++
	}
	return out, nil
}

func (h *AccountHandler) resolveExportAccounts(ctx context.Context, ids []int64, c *gin.Context) ([]service.Account, error) {
	if len(ids) > 0 {
		accounts, err := h.adminService.GetAccountsByIDs(ctx, ids)
		if err != nil {
			return nil, err
		}
		out := make([]service.Account, 0, len(accounts))
		for _, acc := range accounts {
			if acc == nil {
				continue
			}
			out = append(out, *acc)
		}
		return out, nil
	}

	platform := c.Query("platform")
	accountType := c.Query("type")
	status := c.Query("status")
	privacyMode := strings.TrimSpace(c.Query("privacy_mode"))
	search := strings.TrimSpace(c.Query("search"))
	sortBy := c.DefaultQuery("sort_by", "name")
	sortOrder := c.DefaultQuery("sort_order", "asc")
	if len(search) > 100 {
		search = search[:100]
	}

	groupID := int64(0)
	if groupIDStr := c.Query("group"); groupIDStr != "" {
		if groupIDStr == accountListGroupUngroupedQueryValue {
			groupID = service.AccountListGroupUngrouped
		} else {
			parsedGroupID, parseErr := strconv.ParseInt(groupIDStr, 10, 64)
			if parseErr != nil || parsedGroupID <= 0 {
				return nil, infraerrors.BadRequest("INVALID_GROUP_FILTER", "invalid group filter")
			}
			groupID = parsedGroupID
		}
	}

	return h.listAccountsFiltered(ctx, platform, accountType, status, search, groupID, privacyMode, sortBy, sortOrder)
}

func (h *AccountHandler) resolveExportProxies(ctx context.Context, accounts []service.Account) ([]service.Proxy, error) {
	if len(accounts) == 0 {
		return []service.Proxy{}, nil
	}

	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for i := range accounts {
		if accounts[i].ProxyID == nil {
			continue
		}
		id := *accounts[i].ProxyID
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return []service.Proxy{}, nil
	}

	return h.adminService.GetProxiesByIDs(ctx, ids)
}

func parseAccountIDs(c *gin.Context) ([]int64, error) {
	values := c.QueryArray("ids")
	if len(values) == 0 {
		raw := strings.TrimSpace(c.Query("ids"))
		if raw != "" {
			values = []string{raw}
		}
	}
	if len(values) == 0 {
		return nil, nil
	}

	ids := make([]int64, 0, len(values))
	for _, item := range values {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				return nil, fmt.Errorf("invalid account id: %s", part)
			}
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func parseIncludeProxies(c *gin.Context) (bool, error) {
	raw := strings.TrimSpace(strings.ToLower(c.Query("include_proxies")))
	if raw == "" {
		return true, nil
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return true, fmt.Errorf("invalid include_proxies value: %s", raw)
	}
}

func validateDataHeader(payload DataPayload) error {
	if payload.Type != "" && payload.Type != dataType && payload.Type != legacyDataType {
		return fmt.Errorf("unsupported data type: %s", payload.Type)
	}
	if payload.Version != 0 && payload.Version != dataVersion {
		return fmt.Errorf("unsupported data version: %d", payload.Version)
	}
	if payload.Proxies == nil {
		return errors.New("proxies is required")
	}
	if payload.Accounts == nil {
		return errors.New("accounts is required")
	}
	return nil
}

func validateDataProxy(item DataProxy) error {
	if strings.TrimSpace(item.Protocol) == "" {
		return errors.New("proxy protocol is required")
	}
	if strings.TrimSpace(item.Host) == "" {
		return errors.New("proxy host is required")
	}
	if item.Port <= 0 || item.Port > 65535 {
		return errors.New("proxy port is invalid")
	}
	switch item.Protocol {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("proxy protocol is invalid: %s", item.Protocol)
	}
	if item.Status != "" {
		normalizedStatus := normalizeProxyStatus(item.Status)
		if normalizedStatus != service.StatusActive && normalizedStatus != "inactive" {
			return fmt.Errorf("proxy status is invalid: %s", item.Status)
		}
	}
	if item.ProviderUserID != nil && *item.ProviderUserID <= 0 {
		return fmt.Errorf("proxy provider_user_id is invalid: %d", *item.ProviderUserID)
	}
	// 供号商自带的代理绝不能进共享池。service.CreateProxy 也硬拒绝这个组合
	// （ErrProxyProviderOwnedNotAssignable），这里提前拦是为了让错误带上 proxy_key，
	// 管理员能定位到是备份里的哪一条，而不是收到一条没有上下文的 service 层报错。
	if item.ProviderUserID != nil && item.AutoAssignable {
		return errors.New("provider-owned proxy cannot be auto-assignable")
	}
	return nil
}

func validateDataAccount(item DataAccount) error {
	if strings.TrimSpace(item.Name) == "" {
		return errors.New("account name is required")
	}
	if strings.TrimSpace(item.Platform) == "" {
		return errors.New("account platform is required")
	}
	if strings.TrimSpace(item.Type) == "" {
		return errors.New("account type is required")
	}
	if len(item.Credentials) == 0 {
		return errors.New("account credentials is required")
	}
	switch item.Type {
	case service.AccountTypeOAuth, service.AccountTypeSetupToken, service.AccountTypeAPIKey, service.AccountTypeUpstream:
	default:
		return fmt.Errorf("account type is invalid: %s", item.Type)
	}
	if item.RateMultiplier != nil && *item.RateMultiplier < 0 {
		return errors.New("rate_multiplier must be >= 0")
	}
	if item.Concurrency < 0 {
		return errors.New("concurrency must be >= 0")
	}
	if item.Priority < 0 {
		return errors.New("priority must be >= 0")
	}
	return nil
}

func defaultProxyName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "imported-proxy"
	}
	return name
}

// enrichCredentialsFromIDToken performs best-effort extraction of user info fields
// (email, plan_type, chatgpt_account_id, etc.) from id_token in credentials.
// Only applies to OpenAI OAuth accounts. Skips expired token errors silently.
// Existing credential values are never overwritten — only missing fields are filled.
func enrichCredentialsFromIDToken(item *DataAccount) {
	if item.Credentials == nil {
		return
	}
	// Only enrich OpenAI OAuth accounts
	platform := strings.ToLower(strings.TrimSpace(item.Platform))
	if platform != service.PlatformOpenAI {
		return
	}
	if strings.ToLower(strings.TrimSpace(item.Type)) != service.AccountTypeOAuth {
		return
	}

	idToken, _ := item.Credentials["id_token"].(string)
	if strings.TrimSpace(idToken) == "" {
		return
	}

	// DecodeIDToken skips expiry validation — safe for imported data
	claims, err := openai.DecodeIDToken(idToken)
	if err != nil {
		slog.Debug("import_enrich_id_token_decode_failed", "account", item.Name, "error", err)
		return
	}

	userInfo := claims.GetUserInfo()
	if userInfo == nil {
		return
	}

	// Fill missing fields only (never overwrite existing values)
	setIfMissing := func(key, value string) {
		if value == "" {
			return
		}
		if existing, _ := item.Credentials[key].(string); existing == "" {
			item.Credentials[key] = value
		}
	}

	setIfMissing("email", userInfo.Email)
	setIfMissing("plan_type", userInfo.PlanType)
	setIfMissing("chatgpt_account_id", userInfo.ChatGPTAccountID)
	setIfMissing("chatgpt_user_id", userInfo.ChatGPTUserID)
	setIfMissing("organization_id", userInfo.OrganizationID)
}

func normalizeProxyStatus(status string) string {
	normalized := strings.TrimSpace(strings.ToLower(status))
	switch normalized {
	case "":
		return ""
	case service.StatusActive:
		return service.StatusActive
	case "inactive", service.StatusDisabled:
		return "inactive"
	case "expired":
		// 导入 expired 代理按 inactive 处理，避免导入即触发到期改投逻辑
		return "inactive"
	default:
		return normalized
	}
}
