package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const stickySessionPrefix = "sticky_session:"

type gatewayCache struct {
	rdb *redis.Client
}

func NewGatewayCache(rdb *redis.Client) service.GatewayCache {
	return &gatewayCache{rdb: rdb}
}

// buildSessionKey 构建 session key，包含 groupID 实现分组隔离
// 格式: sticky_session:{groupID}:{sessionHash}
func buildSessionKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", stickySessionPrefix, groupID, sessionHash)
}

func (c *gatewayCache) GetSessionAccountID(ctx context.Context, groupID int64, sessionHash string) (int64, error) {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Get(ctx, key).Int64()
}

func (c *gatewayCache) SetSessionAccountID(ctx context.Context, groupID int64, sessionHash string, accountID int64, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Set(ctx, key, accountID, ttl).Err()
}

func (c *gatewayCache) RefreshSessionTTL(ctx context.Context, groupID int64, sessionHash string, ttl time.Duration) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// DeleteSessionAccountID 删除粘性会话与账号的绑定关系。
// 当检测到绑定的账号不可用（如状态错误、禁用、不可调度等）时调用，
// 以便下次请求能够重新选择可用账号。
//
// DeleteSessionAccountID removes the sticky session binding for the given session.
// Called when the bound account becomes unavailable (e.g., error status, disabled,
// or unschedulable), allowing subsequent requests to select a new available account.
func (c *gatewayCache) DeleteSessionAccountID(ctx context.Context, groupID int64, sessionHash string) error {
	key := buildSessionKey(groupID, sessionHash)
	return c.rdb.Del(ctx, key).Err()
}

// Compile-time assertion: gatewayCache must implement CyberSessionBlockStore.
var _ service.CyberSessionBlockStore = (*gatewayCache)(nil)

const cyberSessionBlockPrefix = "cyber_session_block:"

// SetCyberSessionBlocked 把被 cyber_policy 命中的会话写入屏蔽表（TTL 自动过期）。
// 存储值 "1" 作为存在标记（IsCyberSessionBlocked 只检查 key 是否存在，不读值）。
func (c *gatewayCache) SetCyberSessionBlocked(ctx context.Context, key string, ttl time.Duration) error {
	return c.rdb.Set(ctx, cyberSessionBlockPrefix+key, "1", ttl).Err()
}

// IsCyberSessionBlocked 查询会话是否在屏蔽表中。
func (c *gatewayCache) IsCyberSessionBlocked(ctx context.Context, key string) (bool, error) {
	n, err := c.rdb.Exists(ctx, cyberSessionBlockPrefix+key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

const claudeCodeStickyFirstPrefix = "cc_sticky_first:"

func buildClaudeCodeStickyFirstKey(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%s%d:%s", claudeCodeStickyFirstPrefix, groupID, sessionHash)
}

// GetClaudeCodeStickyFirst 返回 CC 会话首次绑定时间；不存在时返回零值。
func (c *gatewayCache) GetClaudeCodeStickyFirst(ctx context.Context, groupID int64, sessionHash string) (time.Time, error) {
	seconds, err := c.rdb.Get(ctx, buildClaudeCodeStickyFirstKey(groupID, sessionHash)).Int64()
	if err == redis.Nil {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, 0), nil
}

// SetClaudeCodeStickyFirst 仅在不存在时写入首次绑定时间，TTL 2h。
func (c *gatewayCache) SetClaudeCodeStickyFirst(ctx context.Context, groupID int64, sessionHash string) error {
	return c.rdb.SetNX(ctx, buildClaudeCodeStickyFirstKey(groupID, sessionHash), time.Now().Unix(), 2*time.Hour).Err()
}

const claudeCodeRunLeasePrefix = "cc_run_lease:"

func buildClaudeCodeRunLeaseKey(sessionKey string) string {
	return claudeCodeRunLeasePrefix + sessionKey
}

// AcquireClaudeCodeRunLease 尝试成为 session 的 owner；成功返回 true。
func (c *gatewayCache) AcquireClaudeCodeRunLease(ctx context.Context, sessionKey, owner string, ttl time.Duration) (bool, error) {
	if sessionKey == "" || owner == "" {
		return false, nil
	}
	return c.rdb.SetNX(ctx, buildClaudeCodeRunLeaseKey(sessionKey), owner, ttl).Result()
}

var refreshClaudeCodeRunLeaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("pexpire", KEYS[1], ARGV[2])
end
return 0
`)

// RefreshClaudeCodeRunLease 仅当 owner 匹配时续期。
func (c *gatewayCache) RefreshClaudeCodeRunLease(ctx context.Context, sessionKey, owner string, ttl time.Duration) (bool, error) {
	if sessionKey == "" || owner == "" {
		return false, nil
	}
	result, err := refreshClaudeCodeRunLeaseScript.Run(ctx, c.rdb, []string{buildClaudeCodeRunLeaseKey(sessionKey)}, owner, ttl.Milliseconds()).Int64()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

var releaseClaudeCodeRunLeaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0
`)

// ReleaseClaudeCodeRunLease 仅当 owner 匹配时删除 lease。
func (c *gatewayCache) ReleaseClaudeCodeRunLease(ctx context.Context, sessionKey, owner string) error {
	if sessionKey == "" || owner == "" {
		return nil
	}
	return releaseClaudeCodeRunLeaseScript.Run(ctx, c.rdb, []string{buildClaudeCodeRunLeaseKey(sessionKey)}, owner).Err()
}
