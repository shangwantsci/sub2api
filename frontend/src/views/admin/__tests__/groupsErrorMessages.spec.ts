import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";

import { describe, expect, it } from "vitest";

const currentDir = dirname(fileURLToPath(import.meta.url));
const groupsViewSource = readFileSync(
  resolve(currentDir, "../GroupsView.vue"),
  "utf8",
);

describe("groups error messages", () => {
  it("shows the API-provided message when updating a group fails", () => {
    expect(groupsViewSource).toContain(
      'import { extractApiErrorMessage } from "@/utils/apiError";',
    );
    expect(groupsViewSource).toContain(
      'extractApiErrorMessage(error, t("admin.groups.failedToUpdate"))',
    );
    expect(groupsViewSource).not.toContain(
      'error.response?.data?.detail || t("admin.groups.failedToUpdate")',
    );
  });
});
