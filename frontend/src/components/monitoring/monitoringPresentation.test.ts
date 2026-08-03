import { describe, expect, it } from "vitest";

import type { QuerySeries } from "../../api/monitoring";
import {
  monitoringChartTheme,
  monitoringValueAt,
  projectMonitoringSeries,
} from "./monitoringChart";
import { buildMonitoringRouteQuery, parseMonitoringRoute } from "./monitoringRoute";

function series(values: Array<[string, number]>, labels: Record<string, string> = {}): QuerySeries {
  return { labels, points: values.map(([timestamp, value]) => ({ timestamp, value })) };
}

describe("Monitoring route codec", () => {
  it("normalizes route arrays, invalid time and expert PromQL", () => {
    const parsed = parseMonitoringRoute({
      namespace: ["platform", "ignored"],
      mode: "expert",
      query: " rate(http_requests_total[5m]) ",
      from: "invalid",
      to: "2026-07-31T08:00:00+08:00",
    });
    expect(parsed.namespace).toBe("platform");
    expect(parsed.mode).toBe("expert");
    expect(parsed.from).toBe("");
    expect(parsed.to).toBe("2026-07-31T00:00:00.000Z");
    expect(buildMonitoringRouteQuery(parsed)).toMatchObject({
      mode: "expert",
      query: "rate(http_requests_total[5m])",
      to: "2026-07-31T00:00:00.000Z",
    });
  });

  it("emits only the active guided query identity", () => {
    const parsed = parseMonitoringRoute({ mode: "guided", metric: "cpu", query: "ignored" });
    expect(buildMonitoringRouteQuery(parsed)).toEqual({ mode: "guided", metric: "cpu" });
  });
});
describe("Monitoring uPlot projection", () => {
  it("aligns multiple series and retains explicit null gaps", () => {
    const projection = projectMonitoringSeries([
      series([["2026-07-31T00:00:00Z", 1], ["2026-07-31T00:02:00Z", 3]]),
      series([["2026-07-31T00:00:00Z", 10], ["2026-07-31T00:01:00Z", 20], ["2026-07-31T00:02:00Z", 30]]),
    ]);
    expect(projection.data[0]).toEqual([1785456000, 1785456060, 1785456120]);
    expect(projection.data[1]).toEqual([1, null, 3]);
    expect(projection.data[2]).toEqual([10, 20, 30]);
  });

  it("bounds dense projections while preserving endpoints and a gap", () => {
    const start = Date.parse("2026-07-31T00:00:00Z");
    const dense = series(Array.from({ length: 100 }, (_, index) => [
      new Date(start + index * 1000).toISOString(),
      index === 50 ? 1000 : index,
    ]));
    const sparse = series(Array.from({ length: 100 }, (_, index) => index === 49 ? null : [
      new Date(start + index * 1000).toISOString(),
      index,
    ]).filter((value): value is [string, number] => value !== null));
    const projection = projectMonitoringSeries([dense, sparse], 12);
    expect(projection.downsampled).toBe(true);
    expect(projection.renderedTimestampCount).toBeLessThanOrEqual(12);
    expect(projection.data[0][0]).toBe(start / 1000);
    expect(projection.data[0].at(-1)).toBe((start + 99_000) / 1000);
    expect(projection.data[0]).toContain((start + 49_000) / 1000);
  });

  it("projects synchronized values and semantic theme tokens", () => {
    const item = series([["2026-07-31T00:00:00Z", 7]]);
    expect(monitoringValueAt(item, 1785456000)?.value).toBe(7);
    expect(monitoringValueAt(item, 1785456060)).toBeNull();
    const theme = monitoringChartTheme((name) => name === "--co-action-primary" ? " rgb(1 2 3) " : "");
    expect(theme.series[0]).toBe("rgb(1 2 3)");
    expect(theme.series).toHaveLength(7);
  });
});
