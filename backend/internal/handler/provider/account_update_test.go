//go:build unit

package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func strptr(s string) *string { return &s }

// 改名改备注时，除这两项外的任何字段都不能被填上。
//
// UpdateAccount 对几个字段的语义是「传了就写」而不是「传了才有意义」，踩中任意一个
// 后果都是静默的：
//   - Extra 非 nil = 整体覆盖，伪装开关与 account_uuid 一起没；账号照跑，没有报错，
//     只是伪装不再生效；
//   - Credentials 非空 = 非敏感键由入参说了算，回填不全就丢键；
//   - Priority 传 0 = 最高优先级，该账号独占整个分组，把自有账号全挤出候选；
//   - GroupIDs 非 nil = 全量替换绑定，传空切片直接解绑所有分组。
//
// 所以这里逐个断言它们仍是零值。将来有人为了「顺手把当前值也带上」而回填，
// 这个测试会立刻炸掉。
func TestBuildProfileUpdateInputTouchesNothingElse(t *testing.T) {
	input, err := buildProfileUpdateInput(UpdateAccountRequest{
		Name:  strptr("新名字"),
		Notes: strptr("备注"),
	})
	require.NoError(t, err)

	require.Equal(t, "新名字", input.Name)
	require.Equal(t, "备注", *input.Notes)

	require.Nil(t, input.Extra, "Extra 非 nil 会整体覆盖并清掉伪装开关与身份字段")
	require.Nil(t, input.Credentials, "Credentials 非空会让未回填的非敏感键丢失")
	require.Nil(t, input.Priority, "Priority 传 0 会让该账号独占整个分组")
	require.Nil(t, input.GroupIDs, "GroupIDs 非 nil 会全量替换分组绑定")
	require.Nil(t, input.ProxyID, "ProxyID 传 0 会把账号改成直连")
	require.Nil(t, input.Concurrency)
	require.Nil(t, input.LoadFactor)
	require.Nil(t, input.RateMultiplier)
	require.Nil(t, input.ExpiresAt)
	require.Empty(t, input.Type, "改名不该动账号类型")
	require.Empty(t, input.Status, "改名不该动账号状态")
}

// 只改备注时 Name 必须留空 —— 空串在 UpdateAccountInput 里正是「不改」。
func TestBuildProfileUpdateInputNotesOnly(t *testing.T) {
	input, err := buildProfileUpdateInput(UpdateAccountRequest{Notes: strptr("只改备注")})
	require.NoError(t, err)
	require.Empty(t, input.Name)
	require.Equal(t, "只改备注", *input.Notes)
}

// 备注传空串是「清空备注」，与不传不是一回事，必须原样透传下去。
func TestBuildProfileUpdateInputClearsNotesWithEmptyString(t *testing.T) {
	input, err := buildProfileUpdateInput(UpdateAccountRequest{Notes: strptr("")})
	require.NoError(t, err)
	require.NotNil(t, input.Notes)
	require.Equal(t, "", *input.Notes)
}

// 只改名时不能顺手把备注清掉：Notes 为 nil 才是「不动它」。
func TestBuildProfileUpdateInputKeepsNotesUntouched(t *testing.T) {
	input, err := buildProfileUpdateInput(UpdateAccountRequest{Name: strptr("新名字")})
	require.NoError(t, err)
	require.Nil(t, input.Notes)
}

// 显式传了空名字必须报错。
//
// 直接放行的话它会退化成 UpdateAccountInput 里的「不改」，供号商以为改成功了、
// 实际名字纹丝未动 —— 一个没有任何提示的静默失败。
func TestBuildProfileUpdateInputRejectsBlankName(t *testing.T) {
	for _, name := range []string{"", "   ", "\t\n"} {
		_, err := buildProfileUpdateInput(UpdateAccountRequest{Name: strptr(name)})
		require.Error(t, err, "空名字必须被拒绝，输入=%q", name)
	}
}

func TestBuildProfileUpdateInputTrimsName(t *testing.T) {
	input, err := buildProfileUpdateInput(UpdateAccountRequest{Name: strptr("  带空格  ")})
	require.NoError(t, err)
	require.Equal(t, "带空格", input.Name)
}
