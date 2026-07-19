ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS content_review_policy VARCHAR(16) NOT NULL DEFAULT 'inherit',
    ADD COLUMN IF NOT EXISTS claude_oauth_system_prompt_policy VARCHAR(16) NOT NULL DEFAULT 'inherit';

ALTER TABLE groups
    DROP CONSTRAINT IF EXISTS groups_content_review_policy_check,
    ADD CONSTRAINT groups_content_review_policy_check
        CHECK (content_review_policy IN ('inherit', 'enabled', 'disabled')),
    DROP CONSTRAINT IF EXISTS groups_claude_oauth_system_prompt_policy_check,
    ADD CONSTRAINT groups_claude_oauth_system_prompt_policy_check
        CHECK (claude_oauth_system_prompt_policy IN ('inherit', 'enabled', 'disabled'));

COMMENT ON COLUMN groups.content_review_policy IS
    'Anthropic group content review policy: inherit, enabled, disabled';
COMMENT ON COLUMN groups.claude_oauth_system_prompt_policy IS
    'Anthropic group Claude OAuth system prompt injection policy: inherit, enabled, disabled';
