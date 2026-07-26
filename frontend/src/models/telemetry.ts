export interface VirtualWindow {
  start: number;
  end: number;
  offset: number;
  totalHeight: number;
}

export function virtualWindow(
  itemCount: number,
  scrollTop: number,
  viewportHeight: number,
  rowHeight: number,
  overscan = 6,
): VirtualWindow {
  if (itemCount <= 0 || viewportHeight <= 0 || rowHeight <= 0) {
    return { start: 0, end: 0, offset: 0, totalHeight: Math.max(0, itemCount * rowHeight) };
  }
  const visibleStart = Math.floor(Math.max(0, scrollTop) / rowHeight);
  const visibleEnd = Math.ceil((Math.max(0, scrollTop) + viewportHeight) / rowHeight);
  const start = Math.max(0, visibleStart - overscan);
  const end = Math.min(itemCount, visibleEnd + overscan);
  return { start, end, offset: start * rowHeight, totalHeight: itemCount * rowHeight };
}

export interface WaterfallPosition {
  left: number;
  width: number;
}

export function waterfallPosition(
  traceStart: string,
  traceDurationMS: number,
  spanStart: string,
  spanDurationMS: number,
): WaterfallPosition {
  const origin = new Date(traceStart).getTime();
  const start = new Date(spanStart).getTime();
  const duration = Math.max(traceDurationMS, spanDurationMS, 0.001);
  const left = Number.isFinite(origin) && Number.isFinite(start)
    ? Math.max(0, Math.min(100, ((start - origin) / duration) * 100))
    : 0;
  const width = Math.max(0.35, Math.min(100 - left, (Math.max(0, spanDurationMS) / duration) * 100));
  return { left, width };
}
