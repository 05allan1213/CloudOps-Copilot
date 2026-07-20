import { computed, onMounted, ref, watch } from "vue";

export type ThemeMode = "dark" | "light";

const STORAGE_KEY = "server-monitor-theme";

const theme = ref<ThemeMode>(
  (localStorage.getItem(STORAGE_KEY) as ThemeMode) || "dark",
);

function applyTheme(mode: ThemeMode) {
  const html = document.documentElement;
  html.classList.remove("dark", "light");
  html.classList.add(mode);
  html.setAttribute("data-theme", mode);
}

export function getEChartsTheme(isDark: boolean) {
  return {
    backgroundColor: "transparent",
    textStyle: {
      color: isDark ? "#cbd5e1" : "#334155",
    },
    legend: {
      textStyle: {
        color: isDark ? "#94a3b8" : "#64748b",
      },
    },
    tooltip: {
      backgroundColor: isDark ? "#1e293b" : "#ffffff",
      borderColor: isDark ? "#334155" : "#e2e8f0",
      textStyle: {
        color: isDark ? "#f1f5f9" : "#1e293b",
      },
    },
    xAxis: {
      axisLine: { lineStyle: { color: isDark ? "#334155" : "#e2e8f0" } },
      axisLabel: { color: isDark ? "#94a3b8" : "#64748b" },
      splitLine: { lineStyle: { color: isDark ? "#1e293b" : "#f1f5f9" } },
    },
    yAxis: {
      axisLine: { lineStyle: { color: isDark ? "#334155" : "#e2e8f0" } },
      axisLabel: { color: isDark ? "#94a3b8" : "#64748b" },
      splitLine: { lineStyle: { color: isDark ? "#1e293b" : "#f1f5f9" } },
    },
    chartColors: ["#3b82f6", "#22c55e", "#f59e0b", "#ef4444", "#8b5cf6"],
    series: [
      { itemStyle: { color: "#3b82f6" } },
      { itemStyle: { color: "#22c55e" } },
      { itemStyle: { color: "#f59e0b" } },
      { itemStyle: { color: "#ef4444" } },
      { itemStyle: { color: "#8b5cf6" } },
    ],
  };
}

export function useTheme() {
  const isDark = computed(() => theme.value === "dark");

  function toggleTheme() {
    theme.value = theme.value === "dark" ? "light" : "dark";
  }

  watch(theme, (mode) => {
    applyTheme(mode);
    localStorage.setItem(STORAGE_KEY, mode);
  });

  onMounted(() => {
    applyTheme(theme.value);
  });

  return {
    theme,
    isDark,
    toggleTheme,
    getEChartsTheme,
  };
}
