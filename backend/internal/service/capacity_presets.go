package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 容量档位模板（capacity presets）。
//
// 一张表，设置键 capacity_presets（JSON 对象）。选档位 = 把模板里的 5 项（并发、会话数、RPM、
// 5h 额度、动态降速）复制写进账号；账号上不存档位名，标签由账号的 4 个数值反推
// （并发、会话数、RPM、5h 额度——不含动态降速，见 providerTierMatchesAccount）。
// 因此改模板只影响「之后套用」的账号，存量账号要用「应用到存量」或批量编辑才会变。
//
// 列所有权，避免两个写入者互相回滚：
//   - 数值、名称、动态降速只归本模板表（GET/PUT /admin/settings/capacity-presets）管理；
//   - 「是否对供货商开放」只归供货商设置的 enabled_tier_keys。模板行里没有开放开关。

const (
	// 模板数值的上限护栏。与管理端面板的输入上限对齐，但独立于供货商自定义档的护栏
	// （providerMax*）：两者语义不同，允许各自演进。
	capacityPresetMaxConcurrency     = 50
	capacityPresetMaxSessions        = 50
	capacityPresetMaxBaseRPM         = 500
	capacityPresetMaxWindowCostLimit = 1000.0

	// capacityPresetMaxKeyLength 与 accounts.provider_tier 列的长度上限一致（ent schema MaxLen(20)）：
	// 供货商用某档上号时，key 会原样写进这一列。
	capacityPresetMaxKeyLength = 20

	capacityPresetsDBTimeout = 5 * time.Second
)

// CapacityPreset 是一个容量档位模板。
type CapacityPreset struct {
	// Key 创建后不可改（账号的 provider_tier 列里存的就是它）。不能是 "custom"。
	Key   string `json:"key"`
	Label string `json:"label"`
	// Concurrency 至少为 1：并发 0 在槽位逻辑里是「不限」，与管理员对「最小档」的直觉相反。
	Concurrency int `json:"concurrency"`
	// 以下三项 0 表示对应限制关闭（不限）。
	MaxSessions      int     `json:"max_sessions"`
	BaseRPM          int     `json:"base_rpm"`
	WindowCostLimit  float64 `json:"window_cost_limit"`
	AdaptiveThrottle bool    `json:"adaptive_throttle"`
}

// CapacityPresets 是整张模板表。
type CapacityPresets struct {
	Presets []CapacityPreset `json:"presets"`
	// DefaultKey 是新建账号弹窗默认预选的档位，必须指向 Presets 里存在的 key。
	DefaultKey string `json:"default_key"`
}

// DefaultCapacityPresets 返回首次部署的种子：沿用供货商 1-5 档，key 固定为 "1".."5"。
//
// 设计依据：1 档最接近单个真人使用 Claude Code CLI 的节奏（真人含工具调用大约 5-15 RPM，
// 单 CLI 会话并发为 1），逐档放开到 5 档的最大吞吐。动态降速种子全部开启。
// 这些只是初值，上线后在设置页按实际账号承载能力调整。
func DefaultCapacityPresets() CapacityPresets {
	return CapacityPresets{
		Presets: []CapacityPreset{
			{Key: "1", Label: "入门", Concurrency: 1, MaxSessions: 1, BaseRPM: 10, WindowCostLimit: 20, AdaptiveThrottle: true},
			{Key: "2", Label: "保守", Concurrency: 2, MaxSessions: 2, BaseRPM: 20, WindowCostLimit: 40, AdaptiveThrottle: true},
			{Key: "3", Label: "均衡", Concurrency: 3, MaxSessions: 3, BaseRPM: 30, WindowCostLimit: 60, AdaptiveThrottle: true},
			{Key: "4", Label: "激进", Concurrency: 5, MaxSessions: 5, BaseRPM: 50, WindowCostLimit: 100, AdaptiveThrottle: true},
			{Key: "5", Label: "旗舰", Concurrency: 8, MaxSessions: 8, BaseRPM: 80, WindowCostLimit: 0, AdaptiveThrottle: true},
		},
		DefaultKey: "3",
	}
}

// Find 按 key 查找模板。
func (p CapacityPresets) Find(key string) (CapacityPreset, bool) {
	key = strings.TrimSpace(key)
	for _, preset := range p.Presets {
		if preset.Key == key {
			return preset, true
		}
	}
	return CapacityPreset{}, false
}

// Keys 按表内顺序返回全部 key。永远返回非 nil 切片（序列化成 [] 而不是 null）。
func (p CapacityPresets) Keys() []string {
	keys := make([]string, 0, len(p.Presets))
	for _, preset := range p.Presets {
		keys = append(keys, preset.Key)
	}
	return keys
}

// parseCapacityPresets 宽松解析存储的 JSON，只用于读路径。
//
// 读路径刻意不做严格校验：迁移 227 从旧的供货商档位表原样搬过来的数据可能不满足新规则
// （例如旧表从未要求 4 个数值唯一），读路径若拒绝，整个模板功能就会因为一条历史数据失效。
// 严格校验只发生在写入（ValidateCapacityPresets）。这里只做让数据可用的最小清理：
// 去掉缺 key / key 为 custom / key 重复的行，默认键失效时落到第一行。
// ok=false 表示没有任何可用行，调用方应使用种子值。
func parseCapacityPresets(raw string) (CapacityPresets, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return CapacityPresets{}, false
	}
	var parsed CapacityPresets
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return CapacityPresets{}, false
	}

	seen := make(map[string]struct{}, len(parsed.Presets))
	cleaned := make([]CapacityPreset, 0, len(parsed.Presets))
	for _, preset := range parsed.Presets {
		preset.Key = strings.TrimSpace(preset.Key)
		if preset.Key == "" || preset.Key == ProviderTierCustom {
			continue
		}
		if _, dup := seen[preset.Key]; dup {
			continue
		}
		seen[preset.Key] = struct{}{}
		preset.Label = strings.TrimSpace(preset.Label)
		if preset.Label == "" {
			preset.Label = preset.Key
		}
		cleaned = append(cleaned, preset)
	}
	if len(cleaned) == 0 {
		return CapacityPresets{}, false
	}

	out := CapacityPresets{Presets: cleaned, DefaultKey: strings.TrimSpace(parsed.DefaultKey)}
	if _, ok := out.Find(out.DefaultKey); !ok {
		out.DefaultKey = cleaned[0].Key
	}
	return out, true
}

// ValidateCapacityPresets 严格校验并归一化模板表，返回可直接持久化的副本。
//
// 规则：key 非空、不超过 20 个字符（对应 provider_tier 列）、不含 "/"（它会出现在
// 「应用到存量」的 URL 里）、不是 "custom"、不重复；并发 >= 1，其余数值 >= 0，且不超过上限护栏；
// 4 元组（并发、会话数、RPM、5h 额度）两两不同——档位标签靠这 4 个数值反推，重复就没法区分；
// default_key 必须指向存在的 key。
func ValidateCapacityPresets(in CapacityPresets) (CapacityPresets, error) {
	if len(in.Presets) == 0 {
		return CapacityPresets{}, infraerrors.BadRequest("NO_CAPACITY_PRESET", "at least one capacity preset is required")
	}

	seenKeys := make(map[string]struct{}, len(in.Presets))
	cleaned := make([]CapacityPreset, 0, len(in.Presets))
	for _, preset := range in.Presets {
		preset.Key = strings.TrimSpace(preset.Key)
		if preset.Key == "" {
			return CapacityPresets{}, infraerrors.BadRequest("INVALID_PRESET_KEY", "preset key must not be empty")
		}
		if preset.Key == ProviderTierCustom {
			return CapacityPresets{}, infraerrors.BadRequest("INVALID_PRESET_KEY",
				"\"custom\" is reserved and cannot be used as a preset key")
		}
		if utf8.RuneCountInString(preset.Key) > capacityPresetMaxKeyLength {
			return CapacityPresets{}, infraerrors.BadRequest("INVALID_PRESET_KEY",
				fmt.Sprintf("preset key %q must not exceed %d characters", preset.Key, capacityPresetMaxKeyLength))
		}
		if strings.Contains(preset.Key, "/") {
			return CapacityPresets{}, infraerrors.BadRequest("INVALID_PRESET_KEY",
				fmt.Sprintf("preset key %q must not contain \"/\"", preset.Key))
		}
		if _, dup := seenKeys[preset.Key]; dup {
			return CapacityPresets{}, infraerrors.BadRequest("DUPLICATE_PRESET_KEY",
				fmt.Sprintf("preset key %q is listed more than once", preset.Key))
		}
		seenKeys[preset.Key] = struct{}{}

		preset.Label = strings.TrimSpace(preset.Label)
		if preset.Label == "" {
			preset.Label = preset.Key
		}
		if err := validateCapacityPresetNumbers(preset); err != nil {
			return CapacityPresets{}, err
		}
		cleaned = append(cleaned, preset)
	}

	for i := range cleaned {
		for j := i + 1; j < len(cleaned); j++ {
			if sameCapacityTuple(cleaned[i], cleaned[j]) {
				return CapacityPresets{}, infraerrors.BadRequest("DUPLICATE_PRESET_VALUES",
					fmt.Sprintf("presets %q and %q have identical concurrency, sessions, RPM and window cost; "+
						"labels are derived from these four values, so each preset needs a distinct combination",
						cleaned[i].Key, cleaned[j].Key))
			}
		}
	}

	out := CapacityPresets{Presets: cleaned, DefaultKey: strings.TrimSpace(in.DefaultKey)}
	if out.DefaultKey == "" {
		return CapacityPresets{}, infraerrors.BadRequest("MISSING_DEFAULT_PRESET", "default preset must be set")
	}
	if _, ok := out.Find(out.DefaultKey); !ok {
		return CapacityPresets{}, infraerrors.BadRequest("INVALID_DEFAULT_PRESET",
			fmt.Sprintf("default preset %q must reference an existing preset", out.DefaultKey))
	}
	return out, nil
}

func validateCapacityPresetNumbers(p CapacityPreset) error {
	// 并发下限是 1，不是 0：并发 0 在槽位逻辑里表示「不限」，填 0 的效果与「最小档」恰好相反。
	// 会话数 / RPM / 5h 额度的 0 才是「该限制关闭」。
	if p.Concurrency < 1 || p.Concurrency > capacityPresetMaxConcurrency {
		return infraerrors.BadRequest("INVALID_PRESET_CONCURRENCY",
			fmt.Sprintf("preset %q: concurrency must be between 1 and %d (0 would mean unlimited, not zero capacity)",
				p.Key, capacityPresetMaxConcurrency))
	}
	if p.MaxSessions < 0 || p.MaxSessions > capacityPresetMaxSessions {
		return infraerrors.BadRequest("INVALID_PRESET_SESSIONS",
			fmt.Sprintf("preset %q: max sessions must be between 0 and %d (0 = no limit)", p.Key, capacityPresetMaxSessions))
	}
	if p.BaseRPM < 0 || p.BaseRPM > capacityPresetMaxBaseRPM {
		return infraerrors.BadRequest("INVALID_PRESET_RPM",
			fmt.Sprintf("preset %q: base RPM must be between 0 and %d (0 = no limit)", p.Key, capacityPresetMaxBaseRPM))
	}
	if math.IsNaN(p.WindowCostLimit) || p.WindowCostLimit < 0 || p.WindowCostLimit > capacityPresetMaxWindowCostLimit {
		return infraerrors.BadRequest("INVALID_PRESET_WINDOW_COST",
			fmt.Sprintf("preset %q: window cost limit must be between 0 and %.0f (0 = no limit)", p.Key, capacityPresetMaxWindowCostLimit))
	}
	return nil
}

// sameCapacityTuple 判断两个模板的 4 个数值是否完全相同。金额是 float64，用容差比较。
func sameCapacityTuple(a, b CapacityPreset) bool {
	return a.Concurrency == b.Concurrency &&
		a.MaxSessions == b.MaxSessions &&
		a.BaseRPM == b.BaseRPM &&
		math.Abs(a.WindowCostLimit-b.WindowCostLimit) < 1e-9
}

// GetCapacityPresets 读取模板表。设置缺失、为空或没有任何可用行时返回种子值。
func (s *SettingService) GetCapacityPresets(ctx context.Context) (CapacityPresets, error) {
	if s == nil || s.settingRepo == nil {
		return DefaultCapacityPresets(), fmt.Errorf("setting service unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), capacityPresetsDBTimeout)
	defer cancel()

	raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyCapacityPresets)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultCapacityPresets(), nil
		}
		return DefaultCapacityPresets(), err
	}
	if parsed, ok := parseCapacityPresets(raw); ok {
		return parsed, nil
	}
	if strings.TrimSpace(raw) != "" {
		// 读路径不报错、直接用种子，但存的表明明有内容却不可用（坏 JSON、字段类型不对、没有任何可用行）
		// 必须留下痕迹，否则管理员看到的是种子、保存时会把库里那份覆盖掉而毫无察觉。
		slog.Warn("capacity_presets setting is unusable, falling back to the seed presets",
			"setting", SettingKeyCapacityPresets)
	}
	return DefaultCapacityPresets(), nil
}

// ProviderTierUsageCounter 统计仍把某档位 key 记在 accounts.provider_tier 上的供货商账号数。
// AccountRepository 满足这个接口。
type ProviderTierUsageCounter interface {
	CountByProviderTier(ctx context.Context, tier string) (int, error)
}

// SaveCapacityPresets 校验并保存整张模板表，返回归一化后的结果。
//
// 除了表内规则，还有一条跨表规则：已存在的 key 不能被改名或删除——只要还有供货商账号的
// provider_tier 记着它（「应用到存量」与账号归属都靠这一列）。改名等价于「删掉旧 key + 新增 key」，
// 所以同一条检查就拦住了改名。usage 为 nil 时跳过这条检查（仅供测试）。
func (s *SettingService) SaveCapacityPresets(ctx context.Context, in CapacityPresets, usage ProviderTierUsageCounter) (CapacityPresets, error) {
	if s == nil || s.settingRepo == nil {
		return CapacityPresets{}, fmt.Errorf("setting service unavailable")
	}
	normalized, err := ValidateCapacityPresets(in)
	if err != nil {
		return CapacityPresets{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if usage != nil {
		previous, err := s.GetCapacityPresets(ctx)
		if err != nil {
			return CapacityPresets{}, err
		}
		for _, old := range previous.Presets {
			if _, stillThere := normalized.Find(old.Key); stillThere {
				continue
			}
			count, err := usage.CountByProviderTier(ctx, old.Key)
			if err != nil {
				return CapacityPresets{}, err
			}
			if count > 0 {
				return CapacityPresets{}, infraerrors.BadRequest("CAPACITY_PRESET_IN_USE",
					fmt.Sprintf("preset %q is still used by %d provider account(s) and cannot be renamed or removed", old.Key, count))
			}
		}
	}

	payload, err := json.Marshal(normalized)
	if err != nil {
		return CapacityPresets{}, fmt.Errorf("marshal capacity presets: %w", err)
	}
	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), capacityPresetsDBTimeout)
	defer cancel()
	if err := s.settingRepo.Set(dbCtx, SettingKeyCapacityPresets, string(payload)); err != nil {
		return CapacityPresets{}, err
	}
	return normalized, nil
}
