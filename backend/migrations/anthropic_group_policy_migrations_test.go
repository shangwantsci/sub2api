package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration179AddsIdentityOnlyWithoutChangingContentReviewPolicy(t *testing.T) {
	content, err := FS.ReadFile("179_expand_claude_oauth_system_prompt_policy.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "groups_claude_oauth_system_prompt_policy_check")
	require.Contains(t, sql, "'identity_only'")
	require.NotContains(t, sql, "groups_content_review_policy_check")
}
