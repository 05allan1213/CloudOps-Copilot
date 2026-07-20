export function formatJSON(value: unknown): string {
  if (value === undefined) return "Not projected";
  if (value === null) return "Not applicable";
  return JSON.stringify(value, null, 2);
}

export function formatDurationMS(value: number): string {
  if (!Number.isFinite(value) || value < 0) return "Unknown";
  if (value < 1000) return `${Math.round(value)} ms`;
  const seconds = value / 1000;
  if (seconds < 60) return `${trimFixed(seconds)} s`;
  const minutes = seconds / 60;
  if (minutes < 60) return `${trimFixed(minutes)} min`;
  return `${trimFixed(minutes / 60)} h`;
}

export function safeExternalURL(value: string | undefined): string {
  if (!value) return "";
  try {
    const parsed = new URL(value);
    return parsed.protocol === "https:" || parsed.protocol === "http:" ? parsed.toString() : "";
  } catch {
    return "";
  }
}

function trimFixed(value: number): string {
  return value.toFixed(value >= 10 ? 1 : 2).replace(/\.0+$/, "").replace(/(\.\d*[1-9])0+$/, "$1");
}
