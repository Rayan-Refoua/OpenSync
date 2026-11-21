import test from "node:test";
import assert from "node:assert/strict";
import { currentReport, placementLabel, stages } from "./status.js";
test("completion reports evidence separately from completion", () => {
  const r = currentReport(
    { outcome: "complete" },
    {
      path: "test.opensync",
      clips: [{ state: "supported" }, { state: "unresolved" }],
    },
  );
  assert.equal(r.title, "Synchronization complete");
  assert.equal(r.tone, "review");
  assert.match(r.detail, /1 supported.*1 unresolved/);
  assert.doesNotMatch(r.detail, /certified/);
  assert.equal(r.value, stages.length);
});
test("running reports known stage and next step without time estimates", () => {
  const r = currentReport({
    running: true,
    stage: stages[3],
    stageCompleted: 7,
    stageTotal: 20,
    stageSeconds: 12,
  });
  assert.equal(r.value, 3);
  assert.match(r.detail, /Stage 4\/11 · 7\/20/);
  assert.equal(r.next, stages[4]);
});
test("failed run cannot show old project as successful completion", () => {
  assert.equal(
    currentReport({ outcome: "failed" }, { path: "old", clips: [] }).tone,
    "error",
  );
  assert.equal(
    placementLabel({ state: "supported" }),
    "Acoustically supported",
  );
});
