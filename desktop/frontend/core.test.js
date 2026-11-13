import test from "node:test";
import assert from "node:assert/strict";
import {
  mapTime,
  visibleClips,
  formatTime,
  waveformLevel,
  tileSeconds,
  tileBins,
  TileCache,
} from "./core.js";
test("precise piecewise timing and inverse survive fractional positions", () => {
  const p = [
    [0, 13.0000001],
    [4, 17.0004],
    [10, 23.0011],
  ];
  for (const t of [0, 0.012345678, 3.9999, 6.1234567, 10])
    assert.ok(Math.abs(mapTime(p, mapTime(p, t), true) - t) < 1e-12);
});
test("5000 clip viewport queries preserve boundaries and visit only intersections", () => {
  const clips = Array.from({ length: 5000 }, (_, i) => ({
    start: i * 20 + 0.123,
    end: i * 20 + 10.123,
    index: i,
  }));
  const p = { clips },
    track = { clips: clips.map((c) => c.index) };
  assert.deepEqual(
    visibleClips(p, track, 20000, 20030).map((c) => c.index),
    [1000, 1001],
  );
  assert.deepEqual(
    visibleClips(p, track, clips[1000].end, clips[1001].start),
    [],
  );
});
test("waveform levels remain bounded and sample anchored", () => {
  for (const spp of [0.000001, 0.001, 0.1, 1, 100]) {
    const l = waveformLevel(spp);
    assert.ok(l >= 0 && l <= 20);
    assert.equal(tileSeconds(l) * 16000, 128 * 2 ** l * tileBins(l));
    assert.ok(tileSeconds(l) <= 120);
  }
  assert.equal(formatTime(430.56), "00:07:10.560");
});
test("abandoned waveform jobs are cancelled and memory stays bounded", async () => {
  const cache = new TileCache(
    12,
    1,
    () => {},
    async (url, signal) => {
      await new Promise((r) => setTimeout(r, 5));
      if (signal.aborted) throw new DOMException("cancelled", "AbortError");
      return { value: new Float32Array(2), bytes: 8 };
    },
  );
  cache.begin();
  cache.get("a", "a");
  cache.end();
  cache.begin();
  cache.get("b", "b");
  cache.end();
  await new Promise((r) => setTimeout(r, 30));
  assert.equal(cache.cache.has("a"), false);
  assert.ok(cache.bytes <= 12);
  cache.clear();
  assert.equal(cache.bytes, 0);
});
