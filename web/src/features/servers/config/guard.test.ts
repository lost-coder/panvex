import { describe, expect, it } from "vitest";

import { isProcessOwned, PROCESS_OWNED_PATHS } from "./guard";

describe("isProcessOwned", () => {
  it("flags the three process-owned paths", () => {
    expect(isProcessOwned("general.data_path")).toBe(true);
    expect(isProcessOwned("general.quota_state_path")).toBe(true);
    expect(isProcessOwned("general.disable_colors")).toBe(true);
  });

  it("does not flag a normal editable path", () => {
    expect(isProcessOwned("general.log_level")).toBe(false);
  });

  it("exposes exactly the three paths", () => {
    expect(PROCESS_OWNED_PATHS).toHaveLength(3);
  });
});
