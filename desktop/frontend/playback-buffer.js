export const BUFFER_FLOOR = 5;
export const BUFFER_TARGET = 8;

// Count only the contiguous media range at the requested time, never the sum
// of disconnected ranges after a seek. All returned values are source seconds.
export function bufferedAhead(ranges, time, direction = 1) {
  for (let i = 0; i < ranges.length; i++) {
    if (ranges.start(i) <= time + 0.025 && ranges.end(i) >= time - 0.025)
      return Math.max(
        0,
        direction > 0 ? ranges.end(i) - time : time - ranges.start(i),
      );
  }
  return 0;
}
export function hasBuffer(ahead, remaining, speed, seconds) {
  return (
    ahead + 0.05 >=
    Math.min(Math.max(0, remaining), seconds * Math.max(0.1, Math.abs(speed)))
  );
}
// Small clock errors use a bounded phase correction. Re-seeking every 80 ms
// used to flush useful decoded frames even when the storage was fast.
export function correctedRate(nominal, error) {
  return Math.max(
    0.1,
    Math.min(16, nominal * (1 - Math.max(-0.04, Math.min(0.04, error * 0.25)))),
  );
}
