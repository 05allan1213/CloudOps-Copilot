import { describe, expect, it } from "vitest";

import {
  filterSettingsSearchEntries,
  resolveSettingsViewSection,
  shouldBlockSettingsLeave,
  type SettingsSearchEntry,
} from "./settingsWorkspace";

const entries: SettingsSearchEntry[] = [
  { key: "system", label: "Telemetry 保留", text: "数据保留天数", field: "system.telemetry_retention_days" },
  { key: "providers", label: "Endpoint", text: "Provider 连接地址", field: "providers.llm.endpoint" },
];

describe("Settings workspace navigation", () => {
  it("keeps stable legacy hashes and defaults unknown anchors to system", () => {
    expect(resolveSettingsViewSection("#providers")).toBe("providers");
    expect(resolveSettingsViewSection("#revision-history")).toBe("revisions");
    expect(resolveSettingsViewSection("#unknown")).toBe("system");
  });

  it("searches human labels and field descriptions case-insensitively", () => {
    expect(filterSettingsSearchEntries("provider", entries)).toEqual([entries[1]]);
    expect(filterSettingsSearchEntries("保留", entries)).toEqual([entries[0]]);
    expect(filterSettingsSearchEntries("  ", entries)).toEqual([]);
  });

  it("blocks navigation only while an independent draft is dirty", () => {
    expect(shouldBlockSettingsLeave(true)).toBe(true);
    expect(shouldBlockSettingsLeave(false)).toBe(false);
  });
});
