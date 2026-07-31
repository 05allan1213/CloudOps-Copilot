import type { ResourceHealthState, ResourceLayer } from "../api/infrastructure";

export interface AtlasSemanticTheme {
  background: string;
  edge: string;
  light: string;
  selection: string;
  layer: Record<ResourceLayer, string>;
  health: Record<ResourceHealthState, string>;
}

type StyleReader = Pick<CSSStyleDeclaration, "getPropertyValue">;

function token(styles: StyleReader, name: string, fallback: string): string {
  return styles.getPropertyValue(name).trim() || fallback;
}

export function atlasSemanticTheme(styles: StyleReader): AtlasSemanticTheme {
  const action = token(styles, "--co-action-primary", "#1769aa");
  const info = token(styles, "--co-status-info-fg", "#175cd3");
  const success = token(styles, "--co-status-success-fg", "#18794e");
  const warning = token(styles, "--co-status-warning-fg", "#8a4b00");
  const inconclusive = token(styles, "--co-status-inconclusive-fg", "#6941c6");
  const neutral = token(styles, "--co-status-neutral-fg", "#617180");
  return {
    background: token(styles, "--co-bg-canvas", "#f4f6f8"),
    edge: token(styles, "--co-border-strong", "#aebac5"),
    light: token(styles, "--co-text-primary", "#17212b"),
    selection: token(styles, "--co-focus-ring", "#0b72e7"),
    layer: {
      namespace: neutral,
      service: info,
      workload: inconclusive,
      pod: success,
      node: warning,
      gateway: action,
    },
    health: {
      healthy: success,
      warning,
      critical: token(styles, "--co-status-critical-fg", "#b42318"),
      unknown: neutral,
    },
  };
}

export function currentAtlasSemanticTheme(): AtlasSemanticTheme {
  return atlasSemanticTheme(getComputedStyle(document.documentElement));
}
