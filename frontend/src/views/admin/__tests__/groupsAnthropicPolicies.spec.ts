import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

import { describe, expect, it } from "vitest";

const currentDir = dirname(fileURLToPath(import.meta.url));
const groupsViewSource = readFileSync(
  resolve(currentDir, "../GroupsView.vue"),
  "utf8",
);

describe("groups Anthropic policies", () => {
  it("renders create and edit controls only for Anthropic groups", () => {
    expect(groupsViewSource).toContain(
      `v-if="createForm.platform === 'anthropic'"`,
    );
    expect(groupsViewSource).toContain(
      `v-if="editForm.platform === 'anthropic'"`,
    );
    expect(groupsViewSource).toContain(
      'v-model="createForm.content_review_policy"',
    );
    expect(groupsViewSource).toContain(
      'v-model="editForm.content_review_policy"',
    );
    expect(groupsViewSource).toContain(
      'v-model="createForm.claude_oauth_system_prompt_policy"',
    );
    expect(groupsViewSource).toContain(
      'v-model="editForm.claude_oauth_system_prompt_policy"',
    );
  });

  it("defaults and resets both policies to inherit", () => {
    expect(groupsViewSource).toContain(
      'content_review_policy: "inherit" as GroupPolicy',
    );
    expect(groupsViewSource).toContain(
      'claude_oauth_system_prompt_policy: "inherit" as ClaudeOAuthSystemPromptPolicy',
    );
    expect(groupsViewSource).toContain(
      'createForm.content_review_policy = "inherit"',
    );
    expect(groupsViewSource).toContain(
      'editForm.content_review_policy = "inherit"',
    );
  });

  it("offers identity-only mode only for Claude OAuth system injection", () => {
    expect(
      groupsViewSource.match(/<option value="identity_only">/g),
    ).toHaveLength(2);
    expect(groupsViewSource).toContain(
      'admin.groups.anthropicPolicies.identityOnly',
    );
  });
});
