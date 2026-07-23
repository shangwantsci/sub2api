ALTER TABLE groups
    DROP CONSTRAINT IF EXISTS groups_claude_oauth_system_prompt_policy_check,
    ADD CONSTRAINT groups_claude_oauth_system_prompt_policy_check
        CHECK (claude_oauth_system_prompt_policy IN ('inherit', 'enabled', 'identity_only', 'disabled'));

COMMENT ON COLUMN groups.claude_oauth_system_prompt_policy IS
    'Anthropic group Claude OAuth system prompt injection policy: inherit, enabled, identity_only, disabled';
