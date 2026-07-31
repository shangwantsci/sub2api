//go:build unit

package service

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

// 策略是 service 层硬约束而不是 UI 隐藏：供号商能直接构造请求，
// 前端少渲染一个单选框拦不住任何人。
func TestAssertProviderProxyModeAllowed(t *testing.T) {
	t.Run("both allows either source", func(t *testing.T) {
		require.NoError(t, AssertProviderProxyModeAllowed(ProviderProxyPolicyBoth, ProviderProxyModeAuto))
		require.NoError(t, AssertProviderProxyModeAllowed(ProviderProxyPolicyBoth, ProviderProxyModeManual))
	})

	t.Run("auto_only rejects a self supplied proxy", func(t *testing.T) {
		require.NoError(t, AssertProviderProxyModeAllowed(ProviderProxyPolicyAutoOnly, ProviderProxyModeAuto))
		require.ErrorIs(t,
			AssertProviderProxyModeAllowed(ProviderProxyPolicyAutoOnly, ProviderProxyModeManual),
			ErrProviderProxyModeNotAllowed)
	})

	t.Run("manual_only rejects the platform pool", func(t *testing.T) {
		require.NoError(t, AssertProviderProxyModeAllowed(ProviderProxyPolicyManualOnly, ProviderProxyModeManual))
		require.ErrorIs(t,
			AssertProviderProxyModeAllowed(ProviderProxyPolicyManualOnly, ProviderProxyModeAuto),
			ErrProviderProxyModeNotAllowed)
	})

	// settings 表被手改成未知值时按种子值（both）处理，不能把两种来源全锁死。
	t.Run("unknown policy falls back to both", func(t *testing.T) {
		require.NoError(t, AssertProviderProxyModeAllowed("platform_only", ProviderProxyModeAuto))
		require.NoError(t, AssertProviderProxyModeAllowed("", ProviderProxyModeManual))
	})

	t.Run("rejects an unknown mode", func(t *testing.T) {
		for _, mode := range []string{"", "both", "platform", "AUTO_ONLY"} {
			require.Error(t, AssertProviderProxyModeAllowed(ProviderProxyPolicyBoth, mode), mode)
		}
	})

	t.Run("is case and whitespace tolerant", func(t *testing.T) {
		require.NoError(t, AssertProviderProxyModeAllowed("  BOTH  ", "  AUTO  "))
	})
}

func TestProviderProxyName(t *testing.T) {
	require.Equal(t, "provider-7-1.2.3.4:1080", ProviderProxyName(7, "1.2.3.4", 1080))

	// proxies.name 列宽只有 100，而 host 列宽 255。长域名直接拼会写库失败，
	// 表现是上号在建代理这一步就报错。
	t.Run("truncates long hosts to the column width", func(t *testing.T) {
		name := ProviderProxyName(7, strings.Repeat("a", 200)+".example.com", 1080)
		require.LessOrEqual(t, len(name), proxyNameMaxLen)
		require.True(t, strings.HasPrefix(name, "provider-7-"), name)
		require.True(t, strings.HasSuffix(name, ":1080"), name)
	})

	t.Run("never emits invalid utf8", func(t *testing.T) {
		name := ProviderProxyName(7, strings.Repeat("代", 120)+".com", 1080)
		require.LessOrEqual(t, len(name), proxyNameMaxLen)
		require.True(t, utf8.ValidString(name))
	})
}

func TestParseProviderProxyURL(t *testing.T) {
	// socks5 必须被升级成 socks5h：否则 Go 的 SOCKS 拨号器在本机解析 DNS，
	// 目标域名会从服务器自己的出口漏出去，与整套伪装的目的相反。
	t.Run("upgrades socks5 to socks5h", func(t *testing.T) {
		out, err := ParseProviderProxyURL("socks5://1.2.3.4:1080")
		require.NoError(t, err)
		require.Equal(t, "socks5h", out.Protocol)
		require.Equal(t, "1.2.3.4", out.Host)
		require.Equal(t, 1080, out.Port)
		require.Empty(t, out.Username)
		require.Empty(t, out.Password)
	})

	t.Run("standard url with credentials", func(t *testing.T) {
		out, err := ParseProviderProxyURL("socks5h://alice:s3cret@proxy.example.com:1080")
		require.NoError(t, err)
		require.Equal(t, "socks5h", out.Protocol)
		require.Equal(t, "proxy.example.com", out.Host)
		require.Equal(t, 1080, out.Port)
		require.Equal(t, "alice", out.Username)
		require.Equal(t, "s3cret", out.Password)
	})

	t.Run("keeps http and https as written", func(t *testing.T) {
		for _, scheme := range []string{"http", "https"} {
			out, err := ParseProviderProxyURL(scheme + "://1.2.3.4:8080")
			require.NoError(t, err, scheme)
			require.Equal(t, scheme, out.Protocol)
		}
	})

	t.Run("colon form without credentials", func(t *testing.T) {
		out, err := ParseProviderProxyURL("1.2.3.4:1080")
		require.NoError(t, err)
		require.Equal(t, defaultProviderProxyProtocol, out.Protocol)
		require.Equal(t, "1.2.3.4", out.Host)
		require.Equal(t, 1080, out.Port)
	})

	t.Run("colon form with credentials", func(t *testing.T) {
		out, err := ParseProviderProxyURL("1.2.3.4:1080:alice:s3cret")
		require.NoError(t, err)
		require.Equal(t, defaultProviderProxyProtocol, out.Protocol)
		require.Equal(t, "1.2.3.4", out.Host)
		require.Equal(t, 1080, out.Port)
		require.Equal(t, "alice", out.Username)
		require.Equal(t, "s3cret", out.Password)
	})

	t.Run("credential form without scheme", func(t *testing.T) {
		out, err := ParseProviderProxyURL("alice:s3cret@1.2.3.4:1080")
		require.NoError(t, err)
		require.Equal(t, defaultProviderProxyProtocol, out.Protocol)
		require.Equal(t, "1.2.3.4", out.Host)
		require.Equal(t, 1080, out.Port)
		require.Equal(t, "alice", out.Username)
		require.Equal(t, "s3cret", out.Password)
	})

	// 代理密码里出现 @ 并不罕见。用第一个 @ 切会把密码截断成一个能连上但错误的凭据，
	// 表现是上号莫名其妙失败，所以必须从最后一个 @ 切。
	t.Run("password containing at sign is not truncated", func(t *testing.T) {
		out, err := ParseProviderProxyURL("alice:p@ss@1.2.3.4:1080")
		require.NoError(t, err)
		require.Equal(t, "alice", out.Username)
		require.Equal(t, "p@ss", out.Password)
		require.Equal(t, "1.2.3.4", out.Host)
	})

	t.Run("password containing colon survives the colon form", func(t *testing.T) {
		out, err := ParseProviderProxyURL("1.2.3.4:1080:alice:p:ss")
		require.NoError(t, err)
		require.Equal(t, "alice", out.Username)
		require.Equal(t, "p:ss", out.Password)
	})

	t.Run("ipv6 needs brackets", func(t *testing.T) {
		out, err := ParseProviderProxyURL("[2001:db8::1]:1080")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::1", out.Host)
		require.Equal(t, 1080, out.Port)

		fromURL, err := ParseProviderProxyURL("socks5h://[2001:db8::1]:1080")
		require.NoError(t, err)
		require.Equal(t, "2001:db8::1", fromURL.Host)
	})

	t.Run("trims surrounding whitespace", func(t *testing.T) {
		out, err := ParseProviderProxyURL("  socks5h://1.2.3.4:1080  ")
		require.NoError(t, err)
		require.Equal(t, "1.2.3.4", out.Host)
	})

	t.Run("rejects malformed input", func(t *testing.T) {
		for _, raw := range []string{
			"",                       // 空
			"   ",                    // 只有空白
			"1.2.3.4",                // 缺端口
			"1.2.3.4:1080:alice",     // 三段歧义
			"ftp://1.2.3.4:1080",     // 协议不在白名单
			"socks5://1.2.3.4",       // URL 缺端口
			"socks5://1.2.3.4:70000", // 端口越界
			"1.2.3.4:notaport",       // 端口非数字
			"socks5://:1080",         // 缺 host
			"@1.2.3.4:1080",          // 空凭据
			"2001:db8::1:1080",       // IPv6 没加方括号
		} {
			_, err := ParseProviderProxyURL(raw)
			require.Error(t, err, "expected %q to be rejected", raw)
		}
	})

	// 错误信息会进日志和前端 toast，绝不能回显原串——它可能带代理密码。
	t.Run("error never echoes the raw input", func(t *testing.T) {
		_, err := ParseProviderProxyURL("socks5://alice:s3cret@1.2.3.4")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "s3cret")
	})
}

func autoAssignCandidate(id int64, count int64) ProxyWithAccountCount {
	return ProxyWithAccountCount{
		Proxy: Proxy{
			ID:             id,
			Protocol:       "socks5h",
			Host:           "1.2.3.4",
			Port:           1080,
			Status:         StatusActive,
			AutoAssignable: true,
		},
		AccountCount: count,
	}
}

func TestSelectAutoAssignProxy(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	t.Run("picks the least loaded proxy", func(t *testing.T) {
		got, err := SelectAutoAssignProxy([]ProxyWithAccountCount{
			autoAssignCandidate(1, 2),
			autoAssignCandidate(2, 0),
			autoAssignCandidate(3, 1),
		}, 5, now)
		require.NoError(t, err)
		require.Equal(t, int64(2), got.ID)
	})

	t.Run("breaks ties by id for determinism", func(t *testing.T) {
		for i := 0; i < 20; i++ {
			got, err := SelectAutoAssignProxy([]ProxyWithAccountCount{
				autoAssignCandidate(7, 1),
				autoAssignCandidate(3, 1),
				autoAssignCandidate(5, 1),
			}, 5, now)
			require.NoError(t, err)
			require.Equal(t, int64(3), got.ID)
		}
	})

	// 这是本次改动最重要的一条边界：供号商自带的代理绝不能被分给别人。
	t.Run("never hands out a provider owned proxy", func(t *testing.T) {
		owner := int64(42)
		owned := autoAssignCandidate(1, 0)
		owned.ProviderUserID = &owner
		// 即便它被错误地标成了可分配，归属仍然一票否决。
		owned.AutoAssignable = true

		_, err := SelectAutoAssignProxy([]ProxyWithAccountCount{owned}, 5, now)
		require.ErrorIs(t, err, ErrNoAutoAssignableProxy)
	})

	t.Run("skips proxies the admin has not opened", func(t *testing.T) {
		closed := autoAssignCandidate(1, 0)
		closed.AutoAssignable = false
		_, err := SelectAutoAssignProxy([]ProxyWithAccountCount{closed}, 5, now)
		require.ErrorIs(t, err, ErrNoAutoAssignableProxy)
	})

	t.Run("skips inactive and expired proxies", func(t *testing.T) {
		inactive := autoAssignCandidate(1, 0)
		inactive.Status = "disabled"

		expired := autoAssignCandidate(2, 0)
		past := now.Add(-time.Hour)
		expired.ExpiresAt = &past

		_, err := SelectAutoAssignProxy([]ProxyWithAccountCount{inactive, expired}, 5, now)
		require.ErrorIs(t, err, ErrNoAutoAssignableProxy)
	})

	t.Run("honours the per proxy cap", func(t *testing.T) {
		full := autoAssignCandidate(1, 2)
		_, err := SelectAutoAssignProxy([]ProxyWithAccountCount{full}, 2, now)
		require.ErrorIs(t, err, ErrNoAutoAssignableProxy)

		room := autoAssignCandidate(2, 1)
		got, err := SelectAutoAssignProxy([]ProxyWithAccountCount{full, room}, 2, now)
		require.NoError(t, err)
		require.Equal(t, int64(2), got.ID)
	})

	// 上限传 0 不能退化成「不限」——那正是 4.14 里并发档位踩过的坑。
	t.Run("falls back to the seed cap when max is not set", func(t *testing.T) {
		atSeedCap := autoAssignCandidate(1, int64(DefaultProviderAutoProxyMaxAccounts))
		_, err := SelectAutoAssignProxy([]ProxyWithAccountCount{atSeedCap}, 0, now)
		require.ErrorIs(t, err, ErrNoAutoAssignableProxy)
	})

	t.Run("reports emptiness without leaking pool size", func(t *testing.T) {
		require.False(t, HasAutoAssignableProxy(nil, 5, now))
		require.True(t, HasAutoAssignableProxy([]ProxyWithAccountCount{autoAssignCandidate(1, 0)}, 5, now))
	})
}
