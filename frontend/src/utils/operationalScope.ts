import type { LocationQueryRaw } from "vue-router";

import type { OperationalScope } from "../api/platform";

export const OPERATIONAL_SCOPE_CHANGED_EVENT = "cloudops:scope-changed";

const scopeContextKeys = [
  "cluster",
  "namespace",
  "resource",
  "resource_ref",
  "cursor",
  "kind",
  "search",
  "from",
  "to",
] as const;

export function queryForScopeChange(current: LocationQueryRaw, clusterID: string): LocationQueryRaw {
  const next = { ...current };
  for (const key of scopeContextKeys) delete next[key];
  if (clusterID) next.cluster = clusterID;
  return next;
}

export function dispatchOperationalScopeChange(scope: OperationalScope) {
  window.dispatchEvent(new CustomEvent<OperationalScope>(OPERATIONAL_SCOPE_CHANGED_EVENT, { detail: scope }));
}
