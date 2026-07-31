import type { RouterScrollBehavior } from "vue-router";

const ANCHOR_OFFSET = 72;
const ANCHOR_WAIT_TIMEOUT_MS = 2_000;

function anchorExists(hash: string): boolean {
  if (typeof document === "undefined") return false;
  const encodedID = hash.startsWith("#") ? hash.slice(1) : hash;
  try {
    return document.getElementById(decodeURIComponent(encodedID)) !== null;
  } catch {
    return document.getElementById(encodedID) !== null;
  }
}

function waitForAnchor(hash: string) {
  const target = { el: hash, top: ANCHOR_OFFSET };
  if (typeof document === "undefined" || anchorExists(hash)) return target;

  return new Promise<typeof target | { top: number }>((resolve) => {
    let settled = false;
    const finish = (position: typeof target | { top: number }) => {
      if (settled) return;
      settled = true;
      observer.disconnect();
      window.clearTimeout(timeout);
      resolve(position);
    };
    const observer = new MutationObserver(() => {
      if (anchorExists(hash)) finish(target);
    });
    const timeout = window.setTimeout(() => finish({ top: 0 }), ANCHOR_WAIT_TIMEOUT_MS);
    observer.observe(document.documentElement, { childList: true, subtree: true });
  });
}

export const appScrollBehavior: RouterScrollBehavior = (to, from, savedPosition) => {
  if (savedPosition) return savedPosition;
  // Zone navigation owns same-document scrolling. IntersectionObserver also
  // replaces the hash as the reader moves, which must not start a second
  // anchor scroll and pull the document away from its current position.
  if (to.hash && to.path === from.path) return false;
  if (to.hash) return waitForAnchor(to.hash);
  return { top: 0 };
};
