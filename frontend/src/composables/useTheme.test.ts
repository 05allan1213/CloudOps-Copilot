import { describe, expect, it } from "vitest";

import { oppositeTheme, resolveThemePreference } from "./useTheme";

describe("theme preference", () => {
  it("gives a persisted explicit choice priority over the system", () => {
    expect(resolveThemePreference("light", false)).toBe("light");
    expect(resolveThemePreference("dark", true)).toBe("dark");
  });

  it("uses the system only when no valid choice exists", () => {
    expect(resolveThemePreference(null, true)).toBe("light");
    expect(resolveThemePreference("unexpected", false)).toBe("dark");
  });

  it("toggles between the two canonical modes", () => {
    expect(oppositeTheme("dark")).toBe("light");
    expect(oppositeTheme("light")).toBe("dark");
  });
});
