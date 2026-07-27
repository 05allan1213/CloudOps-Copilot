import { describe, expect, it } from "vitest";
import type { RouteLocationNormalized, RouterScrollBehavior } from "vue-router";

import { appScrollBehavior } from "./scrollBehavior";

function location(path: string, hash = ""): RouteLocationNormalized {
  return { path, hash } as RouteLocationNormalized;
}

function resolveScroll(
  to: RouteLocationNormalized,
  from: RouteLocationNormalized,
  savedPosition: Parameters<RouterScrollBehavior>[2],
) {
  return appScrollBehavior(to, from, savedPosition);
}

describe("document scroll ownership", () => {
  it("does not re-scroll when an Incident observer replaces the same-document hash", () => {
    expect(resolveScroll(
      location("/incidents/incident-1", "#recovery-zone"),
      location("/incidents/incident-1", "#decision-zone"),
      null,
    )).toBe(false);
  });

  it("still resolves an initial cross-route deep link to its anchor", () => {
    expect(resolveScroll(
      location("/incidents/incident-1", "#recovery-zone"),
      location("/incidents"),
      null,
    )).toEqual({ el: "#recovery-zone", top: 72 });
  });

  it("restores browser history positions before considering an anchor", () => {
    const savedPosition = { left: 0, top: 480 };
    expect(resolveScroll(
      location("/incidents/incident-1", "#decision-zone"),
      location("/incidents/incident-1", "#recovery-zone"),
      savedPosition,
    )).toBe(savedPosition);
  });
});
