import type { RouterScrollBehavior } from "vue-router";

export const appScrollBehavior: RouterScrollBehavior = (to, from, savedPosition) => {
  if (savedPosition) return savedPosition;
  // Zone navigation owns same-document scrolling. IntersectionObserver also
  // replaces the hash as the reader moves, which must not start a second
  // anchor scroll and pull the document away from its current position.
  if (to.hash && to.path === from.path) return false;
  if (to.hash) return { el: to.hash, top: 72 };
  return { top: 0 };
};
