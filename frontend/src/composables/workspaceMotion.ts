const visitedRoutes = new Set<string>();
const revealedSections = new Set<string>();

export const WORKSPACE_ROUTE_MOTION_MS = 360;

export function consumeWorkspaceRouteEntrance(routeIdentity: string): boolean {
  const normalized = routeIdentity.trim() || "/";
  if (visitedRoutes.has(normalized)) return false;
  visitedRoutes.add(normalized);
  return true;
}

export function prefersReducedWorkspaceMotion(): boolean {
  return typeof window !== "undefined"
    && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

export function activateWorkspaceSectionReveals(
  root: HTMLElement,
  routeIdentity: string,
  animate = true,
): () => void {
  const page = root.firstElementChild;
  if (!(page instanceof HTMLElement)) return () => undefined;
  const candidates = [...page.children].filter((element): element is HTMLElement => (
    element instanceof HTMLElement
    && ["HEADER", "MAIN", "SECTION", "ARTICLE"].includes(element.tagName)
  ));
  const reducedMotion = prefersReducedWorkspaceMotion();
  const pending = candidates.filter((element, index) => {
    const key = `${routeIdentity}:${index}`;
    element.dataset.motionSection = key;
    if (revealedSections.has(key)) return false;
    if (!animate || reducedMotion) {
      revealedSections.add(key);
      return false;
    }
    element.classList.add("co-section-reveal-pending");
    return true;
  });
  if (!pending.length) return () => undefined;

  const reveal = (element: HTMLElement) => {
    const key = element.dataset.motionSection;
    if (key) revealedSections.add(key);
    element.classList.remove("co-section-reveal-pending");
    element.classList.add("co-section-reveal-visible");
  };
  if (typeof IntersectionObserver === "undefined") {
    pending.forEach(reveal);
    return () => undefined;
  }
  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting || !(entry.target instanceof HTMLElement)) continue;
      reveal(entry.target);
      observer.unobserve(entry.target);
    }
  }, { rootMargin: "80px 0px", threshold: 0.08 });
  pending.forEach((element) => observer.observe(element));
  return () => observer.disconnect();
}

export function resetWorkspaceMotionForTests() {
  visitedRoutes.clear();
  revealedSections.clear();
}
