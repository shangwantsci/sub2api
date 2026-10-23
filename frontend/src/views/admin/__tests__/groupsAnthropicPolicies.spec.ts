import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

import { describe, expect, it } from "vitest";

const currentDir = dirname(fileURLToPath(import.meta.url));
const groupsViewSource = readFileSync(
  resolve(currentDir, "../GroupsView.vue"),
  "utf8",
);

// GroupsView 里每个 <GroupClaudePolicyCard ... /> 的完整标签（创建在前、编辑在后）
const cardTags =
  groupsViewSource.match(/<GroupClaudePolicyCard[\s\S]*?\/>/g) ?? [];

/** 取出一个标签里全部 v-model:xxx="yyy" 绑定：{ "claude-code-only": "createForm.claude_code_only", ... } */
const modelBindings = (tag: string) =>
  Object.fromEntries(
    [...tag.matchAll(/v-model:([a-zA-Z-]+)="([^"]+)"/g)].map((match) => [
      match[1],
      match[2].trim(),
    ]),
  );

/** 取出以 trigger 开头的那个 watch 的代码块（到下一个 watch( 为止）。 */
const watchBlocks = (trigger: string) =>
  [...groupsViewSource.matchAll(new RegExp(escapeRegExp(trigger), "g"))].map(
    (match) => {
      const start = match.index ?? 0;
      const next = groupsViewSource.indexOf("\nwatch(", start);
      return groupsViewSource.slice(start, next === -1 ? undefined : next);
    },
  );
const escapeRegExp = (text: string) =>
  text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

describe("groups Anthropic policies", () => {
  it("renders exactly two policy cards (create and edit), each only for Anthropic groups", () => {
    expect(groupsViewSource.match(/<GroupClaudePolicyCard/g)).toHaveLength(2);
    expect(cardTags).toHaveLength(2);
    expect(cardTags[0]).toMatch(
      /^<GroupClaudePolicyCard\s+v-if="createForm\.platform === 'anthropic'"/,
    );
    expect(cardTags[1]).toMatch(
      /^<GroupClaudePolicyCard\s+v-if="editForm\.platform === 'anthropic'"/,
    );
  });

  it.each([
    ["create", "createForm", 0],
    ["edit", "editForm", 1],
  ])("binds the four %s form fields to the card", (_name, form, index) => {
    expect(modelBindings(cardTags[index])).toEqual({
      "claude-code-only": `${form}.claude_code_only`,
      "fallback-group-id": `${form}.fallback_group_id`,
      "system-prompt-policy": `${form}.claude_oauth_system_prompt_policy`,
      "content-review-policy": `${form}.content_review_policy`,
    });
  });

  it("feeds each card its own candidates, shares the loading flag and name resolver, and only the edit card gets referrers", () => {
    expect(cardTags[0]).toContain(
      ':fallback-options="createFallbackCandidates"',
    );
    expect(cardTags[1]).toContain(':fallback-options="editFallbackCandidates"');
    for (const tag of cardTags) {
      expect(tag).toContain(':fallback-loading="claudeGroupsLoading"');
      expect(tag).toContain(':resolve-group-name="resolveClaudeGroupName"');
    }
    expect(cardTags[0]).not.toContain(":referrers");
    expect(cardTags[1]).toContain(':referrers="editFallbackReferrers"');
    expect(cardTags[0]).toContain('id-prefix="create-group-policy"');
    expect(cardTags[1]).toContain('id-prefix="edit-group-policy"');
  });

  it("does not bring back the inline Claude Code switch, fallback select or policy selects", () => {
    expect(groupsViewSource).not.toContain(
      'v-model="createForm.content_review_policy"',
    );
    expect(groupsViewSource).not.toContain(
      'v-model="editForm.content_review_policy"',
    );
    expect(groupsViewSource).not.toMatch(
      /v-model="(create|edit)Form\.claude_oauth_system_prompt_policy"/,
    );
    expect(groupsViewSource).not.toMatch(
      /v-model="(create|edit)Form\.fallback_group_id"/,
    );
    expect(groupsViewSource).not.toMatch(
      /(create|edit)Form\.claude_code_only\s*=\s*!\s*(create|edit)Form\.claude_code_only/,
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

  it("resets the Claude-only fields when the create platform leaves Anthropic (a cheap guard; the behaviour is covered by groupsClaudePolicyWiring.spec.ts)", () => {
    const blocks = watchBlocks("() => createForm.platform,");
    expect(blocks).toHaveLength(1);
    expect(blocks[0]).toContain("createForm.claude_code_only = false;");
    expect(blocks[0]).toContain("createForm.fallback_group_id = null;");
  });

  it("does not touch Claude-only fields in the edit platform watchers (the platform is locked there)", () => {
    const blocks = watchBlocks("() => editForm.platform,");
    expect(blocks.length).toBeGreaterThan(0);
    for (const block of blocks) {
      expect(block).not.toContain("editForm.claude_code_only");
      expect(block).not.toContain("editForm.fallback_group_id =");
    }
  });

  it("no longer offers the identity-only option inline (the card owns it now)", () => {
    expect(groupsViewSource).not.toContain('<option value="identity_only">');
    expect(groupsViewSource).not.toContain(
      "admin.groups.anthropicPolicies.identityOnly",
    );
  });
});
