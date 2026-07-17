import { describe, expect, it } from "vitest";

import { normalizeClaudeCalibratedProfileStatus } from "@/api/admin/settings";

describe("normalizeClaudeCalibratedProfileStatus", () => {
  it("returns a render-safe unpublished status for missing data", () => {
    expect(normalizeClaudeCalibratedProfileStatus(undefined)).toEqual({
      published: false,
      valid: false,
      cli_version: "",
      captured_at: "",
      source: "",
      user_agent: "",
      salt_verified: false,
      guard_checked: 0,
      guard_ok: 0,
      beta_rule_keys: [],
      absent_headers: [],
      raw: "",
    });
  });

  it("normalizes nullable collection fields without losing valid status data", () => {
    expect(normalizeClaudeCalibratedProfileStatus({
      published: true,
      valid: true,
      cli_version: "2.1.211",
      source: "cc-calibrate",
      beta_rule_keys: null as unknown as string[],
      absent_headers: null as unknown as string[],
    })).toMatchObject({
      published: true,
      valid: true,
      cli_version: "2.1.211",
      source: "cc-calibrate",
      beta_rule_keys: [],
      absent_headers: [],
    });
  });
});
