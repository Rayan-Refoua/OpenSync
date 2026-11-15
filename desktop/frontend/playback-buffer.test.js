import test from "node:test";
import assert from "node:assert/strict";
import {
  bufferedAhead,
  hasBuffer,
  correctedRate,
  BUFFER_FLOOR,
  BUFFER_TARGET,
} from "./playback-buffer.js";
const ranges = (list) => ({
  length: list.length,
  start: (i) => list[i][0],
  end: (i) => list[i][1],
});
test("buffer reserve uses contiguous playable media, not disconnected ranges", () => {
  const r = ranges([
    [0, 3],
    [10, 30],
  ]);
  assert.equal(bufferedAhead(r, 2), 1);
  assert.equal(bufferedAhead(r, 6), 0);
  assert.equal(bufferedAhead(r, 12), 18);
  assert.equal(hasBuffer(1, 28, 1, BUFFER_FLOOR), false);
  assert.equal(hasBuffer(8, 28, 1, BUFFER_TARGET), true);
});
test("reserve adapts to speed and real end without an impossible EOF wait", () => {
  assert.equal(hasBuffer(8, 40, 2, BUFFER_FLOOR), false);
  assert.equal(hasBuffer(10, 40, 2, BUFFER_FLOOR), true);
  assert.equal(hasBuffer(1.9, 1.9, 1, BUFFER_TARGET), true);
  assert.equal(bufferedAhead(ranges([[2, 10]]), 8, -1), 6);
});
test("clock correction is gentle and bounded in the right direction", () => {
  assert.ok(correctedRate(1, 0.08) < 1);
  assert.ok(correctedRate(1, -0.08) > 1);
  assert.equal(correctedRate(1, 10), 0.96);
  assert.equal(correctedRate(1, -10), 1.04);
});
