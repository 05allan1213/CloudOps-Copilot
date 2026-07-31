import type { QuerySeries } from "../../api/monitoring";

export type MonitoringAlignedData = [number[], ...(number | null)[][]];

export interface MonitoringChartProjection {
  data: MonitoringAlignedData;
  timestamps: number[];
  sourceTimestampCount: number;
  renderedTimestampCount: number;
  downsampled: boolean;
}

export interface MonitoringChartTheme {
  background: string;
  border: string;
  cursor: string;
  grid: string;
  text: string;
  series: string[];
}

const semanticSeriesTokens = [
  "--co-action-primary",
  "--co-status-success-fg",
  "--co-status-warning-fg",
  "--co-status-inconclusive-fg",
  "--co-status-critical-fg",
  "--co-status-info-fg",
  "--co-status-neutral-fg",
];

function pointTimestamp(value: string): number | null {
  const timestamp = new Date(value).getTime();
  return Number.isFinite(timestamp) ? timestamp / 1000 : null;
}

function alignedRows(series: readonly QuerySeries[]): MonitoringAlignedData {
  const timestampSet = new Set<number>();
  const valuesBySeries = series.map((item) => {
    const values = new Map<number, number>();
    for (const point of item.points) {
      const timestamp = pointTimestamp(point.timestamp);
      if (timestamp === null || !Number.isFinite(point.value)) continue;
      timestampSet.add(timestamp);
      values.set(timestamp, point.value);
    }
    return values;
  });
  const timestamps = [...timestampSet].sort((left, right) => left - right);
  return [timestamps, ...valuesBySeries.map((values) => timestamps.map((timestamp) => values.get(timestamp) ?? null))];
}

function valueImportance(data: MonitoringAlignedData, index: number): number {
  let importance = 0;
  for (let seriesIndex = 1; seriesIndex < data.length; seriesIndex += 1) {
    const values = data[seriesIndex];
    const current = values[index];
    const previous = values[index - 1];
    const next = values[index + 1];
    if (current === null) {
      if (previous !== null || next !== null) importance = Math.max(importance, 1_000_000);
      continue;
    }
    if (previous === null || next === null) {
      importance = Math.max(importance, 500_000);
      continue;
    }
    const expected = previous + (next - previous) / 2;
    const scale = Math.max(1, Math.abs(previous), Math.abs(current), Math.abs(next));
    importance = Math.max(importance, Math.abs(current - expected) / scale);
  }
  return importance;
}

function downsampleIndices(data: MonitoringAlignedData, maxPoints: number): number[] {
  const count = data[0].length;
  if (count <= maxPoints || maxPoints < 3) return Array.from({ length: count }, (_, index) => index);
  const bucketCount = maxPoints - 2;
  const interiorCount = count - 2;
  const indices = [0];
  for (let bucket = 0; bucket < bucketCount; bucket += 1) {
    const start = 1 + Math.floor((bucket * interiorCount) / bucketCount);
    const end = Math.min(count - 1, 1 + Math.floor(((bucket + 1) * interiorCount) / bucketCount));
    let selected = start;
    let selectedImportance = -1;
    for (let index = start; index < Math.max(start + 1, end); index += 1) {
      const importance = valueImportance(data, index);
      if (importance > selectedImportance) {
        selected = index;
        selectedImportance = importance;
      }
    }
    if (indices[indices.length - 1] !== selected) indices.push(selected);
  }
  if (indices[indices.length - 1] !== count - 1) indices.push(count - 1);
  return indices.slice(0, maxPoints - 1).concat(count - 1).filter((value, index, list) => index === 0 || value !== list[index - 1]);
}

export function projectMonitoringSeries(
  series: readonly QuerySeries[],
  maxPoints = 2400,
): MonitoringChartProjection {
  const aligned = alignedRows(series);
  const indices = downsampleIndices(aligned, maxPoints);
  const projected = aligned.map((values) => indices.map((index) => values[index])) as MonitoringAlignedData;
  return {
    data: projected,
    timestamps: projected[0],
    sourceTimestampCount: aligned[0].length,
    renderedTimestampCount: projected[0].length,
    downsampled: projected[0].length < aligned[0].length,
  };
}

export function monitoringSeriesLabel(series: QuerySeries, index: number): string {
  const labels = Object.entries(series.labels)
    .filter(([key]) => key !== "__name__")
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([key, value]) => `${key}=${value}`);
  return labels.join(" · ") || series.labels.__name__ || `序列 ${index + 1}`;
}

export function monitoringValueAt(series: QuerySeries, timestampSeconds: number | null): { timestamp: string; value: number } | null {
  if (!series.points.length) return null;
  if (timestampSeconds === null) return series.points[series.points.length - 1] ?? null;
  const exact = series.points.find((point) => pointTimestamp(point.timestamp) === timestampSeconds);
  return exact ?? null;
}

export function monitoringChartTheme(readToken: (name: string) => string): MonitoringChartTheme {
  const read = (name: string, fallback: string) => readToken(name).trim() || fallback;
  return {
    background: read("--co-bg-canvas", "Canvas"),
    border: read("--co-border-default", "GrayText"),
    cursor: read("--co-focus-ring", "Highlight"),
    grid: read("--co-border-subtle", "GrayText"),
    text: read("--co-text-secondary", "CanvasText"),
    series: semanticSeriesTokens.map((token) => read(token, "LinkText")),
  };
}
