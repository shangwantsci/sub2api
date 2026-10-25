package service

import "context"

// Fingerprint 是 OAuth 账号用量查询时复用的客户端指纹缓存。
// 旧的 IdentityService（生成/改写 Claude Code 请求身份）已随 P2 删除；
// 这里仅保留 account_usage_service 需要的缓存类型。
type Fingerprint struct {
	ClientID                string
	UserAgent               string
	StainlessLang           string
	StainlessPackageVersion string
	StainlessOS             string
	StainlessArch           string
	StainlessRuntime        string
	StainlessRuntimeVersion string
	UpdatedAt               int64 `json:",omitempty"`
}

// IdentityCache defines cache operations used by account usage queries.
type IdentityCache interface {
	GetFingerprint(ctx context.Context, accountID int64) (*Fingerprint, error)
	SetFingerprint(ctx context.Context, accountID int64, fp *Fingerprint) error
	GetMaskedSessionID(ctx context.Context, accountID int64) (string, error)
	SetMaskedSessionID(ctx context.Context, accountID int64, sessionID string) error
}
