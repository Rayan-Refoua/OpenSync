export const HEADER = 176,
  ROW = 112,
  RULER = 28;
export const clamp = (v, a, b) => Math.max(a, Math.min(b, v));
export function mapTime(points, t, inverse = false) {
  const x = inverse ? 1 : 0,
    y = 1 - x;
  let lo = 0,
    hi = points.length;
  while (lo < hi) {
    const m = (lo + hi) >>> 1;
    if (points[m][x] <= t) lo = m + 1;
    else hi = m;
  }
  const i = clamp(lo - 1, 0, points.length - 2),
    a = points[i],
    b = points[i + 1];
  return a[y] + ((t - a[x]) * (b[y] - a[y])) / (b[x] - a[x]);
}
export function visibleClips(project, track, left, right) {
  const ids = track.clips;
  let lo = 0,
    hi = ids.length;
  while (lo < hi) {
    const m = (lo + hi) >>> 1;
    if (project.clips[ids[m]].end <= left) lo = m + 1;
    else hi = m;
  }
  const out = [];
  for (let i = lo; i < ids.length; i++) {
    const c = project.clips[ids[i]];
    if (c.start >= right) break;
    out.push(c);
  }
  return out;
}
export function formatTime(t) {
  const ms = Math.max(0, Math.round(t * 1000)),
    s = Math.floor(ms / 1000);
  return `${String(Math.floor(s / 3600)).padStart(2, "0")}:${String(Math.floor(s / 60) % 60).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}.${String(ms % 1000).padStart(3, "0")}`;
}
export function waveformLevel(secondsPerPixel) {
  return clamp(
    Math.floor(Math.log2(Math.max(128, secondsPerPixel * 16000) / 128)),
    0,
    20,
  );
}
export function tileBins(level) {
  let bins = 512;
  while (bins > 1 && 128 * 2 ** level * bins > 16000 * 120) bins /= 2;
  return bins;
}
export function tileSeconds(level) {
  return (128 * 2 ** level * tileBins(level)) / 16000;
}
export function rulerStep(pps) {
  const desired = 90 / pps,
    power = 10 ** Math.floor(Math.log10(desired));
  return (
    [1, 2, 5, 10].map((v) => v * power).find((v) => v >= desired) || 10 * power
  );
}

// Memory-bounded visible-request queue. Abandoned viewport work is cancelled;
// no full recording peak file or unbounded image cache is created.
export class TileCache {
  constructor(limit, concurrency, onChange, load) {
    this.limit = limit;
    this.concurrency = concurrency;
    this.onChange = onChange;
    this.load = load;
    this.cache = new Map();
    this.pending = new Map();
    this.wanted = new Set();
    this.bytes = 0;
    this.errors = 0;
  }
  begin() {
    this.wanted.clear();
  }
  get(key, url) {
    this.wanted.add(key);
    const v = this.cache.get(key);
    if (v) {
      this.cache.delete(key);
      this.cache.set(key, v);
      return v.value;
    }
    if (!this.pending.has(key))
      this.pending.set(key, { key, url, controller: null });
    return null;
  }
  end() {
    for (const [k, v] of this.pending) {
      if (!this.wanted.has(k)) {
        v.controller?.abort();
        this.pending.delete(k);
      }
    }
    this.pump();
  }
  pump() {
    if(this.suspended)return;
    let n = [...this.pending.values()].filter((v) => v.controller).length;
    for (const v of this.pending.values()) {
      if (n >= this.concurrency) break;
      if (v.controller) continue;
      n++;
      v.controller = new AbortController();
      this.load(v.url, v.controller.signal)
        .then(({ value, bytes }) => {
          if (v.controller.signal.aborted) {
            value?.close?.();
            return;
          }
          this.cache.set(v.key, { value, bytes });
          this.bytes += bytes;
          while (this.bytes > this.limit && this.cache.size) {
            const k = this.cache.keys().next().value,
              e = this.cache.get(k);
            this.bytes -= e.bytes;
            e.value?.close?.();
            this.cache.delete(k);
          }
        })
        .catch((e) => {
          if (e.name !== "AbortError") {
            this.errors++;
            this.lastError = e.message;
            this.cache.set(v.key, { value: { error: e.message }, bytes: 0 });
            if (this.cache.size > 10000) {
              this.clear();
            }
          }
        })
        .finally(() => {
          if (this.pending.get(v.key) === v) this.pending.delete(v.key);
          this.onChange();
          this.pump();
        });
    }
  }
  clear() {
    for (const p of this.pending.values()) p.controller?.abort();
    this.pending.clear();
    for (const v of this.cache.values()) v.value?.close?.();
    this.cache.clear();
    this.bytes = 0;
  }
}
