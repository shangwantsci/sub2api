package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	tempUnschedPrefix                  = "temp_unsched:account:"
	anthropicOpaque429BackoffKeyPrefix = "anthropic_429:backoff:account:"
	anthropicOpaque429BackoffTTL       = 30 * time.Minute
	anthropicOpaque429DedupeWindow     = 30 * time.Second
)

var tempUnschedSetScript = redis.NewScript(`
	local key = KEYS[1]
	local new_until = tonumber(ARGV[1])
	local new_value = ARGV[2]
	local new_ttl = tonumber(ARGV[3])

	local existing = redis.call('GET', key)
	if existing then
		local ok, existing_data = pcall(cjson.decode, existing)
		if ok and existing_data and existing_data.until_unix then
			local existing_until = tonumber(existing_data.until_unix)
			if existing_until and new_until <= existing_until then
				return 0
			end
		end
	end

	redis.call('SET', key, new_value, 'EX', new_ttl)
	return 1
`)

var anthropicOpaque429BackoffScript = redis.NewScript(`
	redis.replicate_commands()
	local key = KEYS[1]
	local dedupe_window = tonumber(ARGV[1])
	local ttl = tonumber(ARGV[2])
	local time_result = redis.call('TIME')
	local now = tonumber(time_result[1])
	local streak = tonumber(redis.call('HGET', key, 'streak')) or 0
	local last = tonumber(redis.call('HGET', key, 'last_increment_at')) or 0
	if last == 0 or now - last >= dedupe_window then
		streak = streak + 1
		redis.call('HSET', key, 'streak', streak, 'last_increment_at', now)
	end
	redis.call('EXPIRE', key, ttl)
	return streak
`)

type tempUnschedCache struct {
	rdb *redis.Client
}

func NewTempUnschedCache(rdb *redis.Client) service.TempUnschedCache {
	return &tempUnschedCache{rdb: rdb}
}

// SetTempUnsched 设置临时不可调度状态（只延长不缩短）
func (c *tempUnschedCache) SetTempUnsched(ctx context.Context, accountID int64, state *service.TempUnschedState) error {
	key := fmt.Sprintf("%s%d", tempUnschedPrefix, accountID)

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	ttl := time.Until(time.Unix(state.UntilUnix, 0))
	if ttl <= 0 {
		return nil // 已过期，不设置
	}

	ttlSeconds := int(ttl.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	_, err = tempUnschedSetScript.Run(ctx, c.rdb, []string{key}, state.UntilUnix, string(stateJSON), ttlSeconds).Result()
	return err
}

// GetTempUnsched 获取临时不可调度状态
func (c *tempUnschedCache) GetTempUnsched(ctx context.Context, accountID int64) (*service.TempUnschedState, error) {
	key := fmt.Sprintf("%s%d", tempUnschedPrefix, accountID)

	val, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var state service.TempUnschedState
	if err := json.Unmarshal([]byte(val), &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	return &state, nil
}

func (c *tempUnschedCache) GetTempUnschedBatch(ctx context.Context, accountIDs []int64) (map[int64]*service.TempUnschedState, error) {
	result := make(map[int64]*service.TempUnschedState, len(accountIDs))
	if len(accountIDs) == 0 {
		return result, nil
	}

	keys := make([]string, 0, len(accountIDs))
	ids := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			continue
		}
		if _, ok := seen[accountID]; ok {
			continue
		}
		seen[accountID] = struct{}{}
		ids = append(ids, accountID)
		keys = append(keys, fmt.Sprintf("%s%d", tempUnschedPrefix, accountID))
	}
	if len(keys) == 0 {
		return result, nil
	}

	values, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for i, value := range values {
		if value == nil {
			continue
		}
		raw, ok := value.(string)
		if !ok || raw == "" {
			continue
		}
		var state service.TempUnschedState
		if err := json.Unmarshal([]byte(raw), &state); err != nil {
			return nil, fmt.Errorf("unmarshal state for account %d: %w", ids[i], err)
		}
		result[ids[i]] = &state
	}
	return result, nil
}

// DeleteTempUnsched 删除临时不可调度状态
func (c *tempUnschedCache) DeleteTempUnsched(ctx context.Context, accountID int64) error {
	key := fmt.Sprintf("%s%d", tempUnschedPrefix, accountID)
	return c.rdb.Del(ctx, key).Err()
}

func (c *tempUnschedCache) NextAnthropicOpaque429Backoff(ctx context.Context, accountID int64) (int, error) {
	if accountID <= 0 {
		return 1, nil
	}
	key := fmt.Sprintf("%s%d", anthropicOpaque429BackoffKeyPrefix, accountID)
	streak, err := anthropicOpaque429BackoffScript.Run(
		ctx,
		c.rdb,
		[]string{key},
		int(anthropicOpaque429DedupeWindow.Seconds()),
		int(anthropicOpaque429BackoffTTL.Seconds()),
	).Int()
	if err != nil {
		return 0, fmt.Errorf("increment Anthropic opaque 429 backoff: %w", err)
	}
	if streak < 1 {
		streak = 1
	}
	return streak, nil
}

func (c *tempUnschedCache) ResetAnthropicOpaque429Backoff(ctx context.Context, accountID int64) error {
	if accountID <= 0 {
		return nil
	}
	key := fmt.Sprintf("%s%d", anthropicOpaque429BackoffKeyPrefix, accountID)
	return c.rdb.Del(ctx, key).Err()
}
