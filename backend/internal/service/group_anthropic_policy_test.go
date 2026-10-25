//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveGroupPolicy(t *testing.T) {
	require.True(t, ResolveGroupPolicy(true, GroupPolicyInherit))
	require.False(t, ResolveGroupPolicy(false, GroupPolicyInherit))
	require.True(t, ResolveGroupPolicy(false, GroupPolicyEnabled))
	require.False(t, ResolveGroupPolicy(true, GroupPolicyDisabled))
	require.True(t, ResolveGroupPolicy(true, "unknown"))
}
