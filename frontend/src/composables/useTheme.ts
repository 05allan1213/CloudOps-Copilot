import { computed, ref, watch } from "vue";

export type ThemeMode = "dark" | "light";

const STORAGE_KEY = "server-monitor-theme";

function resolveInitialTheme(): ThemeMode {
  if (typeof window === "undefined") return "dark";
  const stored = window.localStorage.getItem(STORAGE_KEY);
  if (stored === "dark" || stored === "light") return stored;
  return window.matchMedia("(prefers-color-scheme: light)").matches ? "light" : "dark";
}

const theme = ref<ThemeMode>(resolveInitialTheme());

function applyTheme(mode: ThemeMode) {
  if (typeof document === "undefined") return;
  const html = document.documentElement;
  html.classList.remove("dark", "light");
  html.classList.add(mode);
  html.setAttribute("data-theme", mode);
  html.style.colorScheme = mode;
  document
    .querySelector('meta[name="theme-color"]')
    ?.setAttribute("content", mode === "dark" ? "#0B0F14" : "#F4F6F8");
}

export function initializeTheme() {
  applyTheme(theme.value);
}

watch(theme, (mode) => {
  applyTheme(mode);
  if (typeof window !== "undefined") window.localStorage.setItem(STORAGE_KEY, mode);
});

export function useTheme() {
  const isDark = computed(() => theme.value === "dark");

  function toggleTheme() {
    theme.value = theme.value === "dark" ? "light" : "dark";
  }

  return {
    theme,
    isDark,
    toggleTheme,
  };
}
