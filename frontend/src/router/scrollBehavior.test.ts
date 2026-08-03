import { afterEach, describe, expect, it, vi } from "vitest";
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
  afterEach(() => vi.unstubAllGlobals());

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

  it("waits for an asynchronously rendered cross-route anchor", async () => {
    let anchorAvailable = false;
    let notifyMutation = () => {};
    const disconnect = vi.fn();
    vi.stubGlobal("document", {
      documentElement: {},
      getElementById: (id: string) => anchorAvailable && id === "providers" ? {} : null,
    });
    vi.stubGlobal("window", {
      setTimeout: vi.fn(() => 1),
      clearTimeout: vi.fn(),
    });
    vi.stubGlobal("MutationObserver", class {
      constructor(callback: MutationCallback) {
        notifyMutation = () => callback([], this as unknown as MutationObserver);
      }

      observe() {}
      disconnect() { disconnect(); }
    });

    const pending = resolveScroll(
      location("/settings", "#providers"),
      location("/incidents"),
      null,
    ) as Promise<unknown>;
    anchorAvailable = true;
    notifyMutation();

    await expect(pending).resolves.toEqual({ el: "#providers", top: 72 });
    expect(disconnect).toHaveBeenCalledOnce();
  });

  it("restores browser history positions before considering an anchor", () => {
    const savedPosition = { left: 0, top: 480 };
    expect(resolveScroll(
      location("/incidents/incident-1", "#decision-zone"),
      location("/incidents/incident-1", "#recovery-zone"),
      savedPosition,
    )).toBe(savedPosition);
  });

  it("keeps the reader in place for same-Workspace Query changes", () => {
    expect(resolveScroll(
      location("/incidents"),
      location("/incidents"),
      null,
    )).toBe(false);
  });
});
