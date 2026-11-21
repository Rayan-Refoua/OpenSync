import test from "node:test";
import assert from "node:assert/strict";
import { chooseFeeds } from "./monitor.js";
import { validateShortcuts, keyChord } from "./shortcuts.js";
const clip = (index, track, start, end, videoDuration = end - start) => ({
  index,
  track,
  start,
  end,
  duration: end - start,
  video: true,
  missing: false,
  mapping: [
    [0, start],
    [end - start, end],
  ],
  metadata: { video: { duration: videoDuration } },
});
test("camera selection respects video end, pins and replaces completed clips", () => {
  const p = {
    clips: [
      clip(0, 0, 0, 20, 8),
      clip(1, 1, 0, 20),
      clip(2, 2, 0, 20),
      clip(3, 3, 9, 30),
    ],
  };
  const a = chooseFeeds(p, 2, new Set(), [], 3);
  assert.deepEqual(
    a.map((f) => f.clip.index),
    [0, 1, 2],
  );
  const b = chooseFeeds(p, 10, new Set(), a, 3);
  assert.deepEqual(
    b.map((f) => f.clip.index),
    [1, 2, 3],
  );
  const pinned = chooseFeeds(p, 10, new Set([0]), b, 3);
  assert.deepEqual(
    pinned.map((f) => f.clip.index),
    [1, 2, 3],
  );
  assert.equal(pinned.length, 3);
});
test("shortcut validation prevents ambiguous commands", () => {
  assert.throws(() => validateShortcuts({ forward: "J" }), /twice/);
  assert.equal(
    keyChord({ code: "KeyK", ctrlKey: true, altKey: true }),
    "Ctrl+Alt+K",
  );
  validateShortcuts({ forward: "Ctrl+L" });
});
