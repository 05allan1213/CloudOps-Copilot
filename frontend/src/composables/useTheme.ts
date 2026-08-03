import { computed, ref, watch } from "vue";

export type ThemeMode = "dark" | "light";

const STORAGE_KEY = "cloudops-theme";
export const THEME_CHANGE_EVENT = "cloudops:theme-change";
export const THEME_TRANSITION_MS = 240;
let transitionTimer: ReturnType<typeof setTimeout> | undefined;

export function resolveThemePreference(stored: string | null, prefersLight: boolean): ThemeMode {
  if (stored === "dark" || stored === "light") return stored;
  if (stored !== null) return prefersLight ? "light" : "dark";
  return "light";
}

export function oppositeTheme(mode: ThemeMode): ThemeMode {
  return mode === "dark" ? "light" : "dark";
}

function resolveInitialTheme(): ThemeMode {
  if (typeof window === "undefined") return "light";
  let stored: string | null = null;
  try {
    stored = window.localStorage.getItem(STORAGE_KEY);
  } catch {
    // Warm-gray light is the deterministic default when storage is blocked.
  }
  return resolveThemePreference(stored, window.matchMedia("(prefers-color-scheme: light)").matches);
}

const theme = ref<ThemeMode>(resolveInitialTheme());

function applyTheme(mode: ThemeMode, notify = true) {
  if (typeof document === "undefined") return;
  const html = document.documentElement;
  html.classList.remove("dark", "light");
  html.classList.add(mode);
  html.setAttribute("data-theme", mode);
  html.style.colorScheme = mode;
  const canvasColor = getComputedStyle(html).getPropertyValue("--co-bg-canvas").trim();
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute("content", canvasColor || (mode === "dark" ? "black" : "white"));
  if (notify && typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent<ThemeMode>(THEME_CHANGE_EVENT, { detail: mode }));
  }
}

export function initializeTheme() {
  applyTheme(theme.value, false);
}

watch(theme, (mode) => {
  applyTheme(mode);
  if (typeof window !== "undefined") {
    try {
      window.localStorage.setItem(STORAGE_KEY, mode);
    } catch {
      // The active theme remains valid for this browser lifecycle.
    }
  }
});

export function useTheme() {
  const isDark = computed(() => theme.value === "dark");

  function toggleTheme() {
    if (typeof document !== "undefined" && !window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      document.documentElement.classList.add("co-theme-transitioning");
      if (transitionTimer !== undefined) clearTimeout(transitionTimer);
      transitionTimer = setTimeout(() => {
        document.documentElement.classList.remove("co-theme-transitioning");
        transitionTimer = undefined;
      }, THEME_TRANSITION_MS);
    }
    theme.value = oppositeTheme(theme.value);
  }

  return {
    theme,
    isDark,
    toggleTheme,
  };
}
