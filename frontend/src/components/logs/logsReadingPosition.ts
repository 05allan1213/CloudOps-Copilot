const maximumPositions = 24;
const positions = new Map<string, number>();

export function appendedLogCount(
  previous: readonly { id: string }[],
  next: readonly { id: string }[],
): number {
  if (!previous.length || next.length <= previous.length) return 0;
  for (let index = 0; index < previous.length; index += 1) {
    if (previous[index]?.id !== next[index]?.id) return 0;
  }
  return next.length - previous.length;
}

export function rememberLogReadingPosition(queryIdentity: string, offset: number) {
  if (!queryIdentity || !Number.isFinite(offset)) return;
  positions.delete(queryIdentity);
  positions.set(queryIdentity, Math.max(0, offset));
  while (positions.size > maximumPositions) {
    const oldest = positions.keys().next().value as string | undefined;
    if (!oldest) break;
    positions.delete(oldest);
  }
}

export function readLogReadingPosition(queryIdentity: string): number {
  const offset = positions.get(queryIdentity) ?? 0;
  if (positions.has(queryIdentity)) {
    positions.delete(queryIdentity);
    positions.set(queryIdentity, offset);
  }
  return offset;
}

export function resetLogReadingPositionsForTests() {
  positions.clear();
}
