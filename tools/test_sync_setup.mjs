import assert from "node:assert/strict";
import test from "node:test";
import { applySyncSetup, syncPresets, syncSetupAnswers } from "../desktop/frontend/sync-setup.js";

test("event and takes presets choose distinct placement policies", () => {
  const config = { mode: "clips", analysis: { readMode: "buffered", timingProfiles: "dense" }, unmatchedPolicy: "append" };
  const event = applySyncSetup(config, syncPresets.event);
  const takes = applySyncSetup(config, { ...syncPresets.takes, timestamps: "estimate" });
  assert.equal(event.mode, "chronological");
  assert.equal(event.fileTimePlacement, true);
  assert.equal(takes.fileTimePlacement, false);
  assert.equal(takes.analysis.readMode, "buffered");
  assert.equal(takes.analysis.timingProfiles, "dense");
  assert.equal(takes.unmatchedPolicy, "append");
  assert.equal(syncSetupAnswers(takes).confidence, "strict");
});

test("setup preserves user verification controls until a preset replaces them", () => {
  const config = { mode: "clips", analysis: { verification: { minWave: .41 } } };
  const current = syncSetupAnswers(config);
  assert.equal(current.confidence, "custom");
  assert.deepEqual(applySyncSetup(config, current).analysis.verification, { minWave: .41 });
  const difficult = applySyncSetup(config, syncPresets.difficult);
  assert.equal(difficult.analysis.recovery, "deep");
  assert.equal(syncSetupAnswers(difficult).confidence, "loose");
  assert.equal(difficult.analysis.verification.controlRatio, 1.5);
});

test("project exceptions route uncertain matches and preserve original audio", () => {
  const config = { audioEarlyMS: 40, audioLateMS: 60, includeProvisional: true, analysis: { guideRecorderOrder: true } };
  const answers = { ...syncPresets.event, referenceSource: "master.wav", recorderOrder: "independent", uncertain: "separate", drift: "original" };
  const next = applySyncSetup(config, answers);
  assert.equal(next.referenceSource, "master.wav");
  assert.equal(next.fileTimePlacement, false);
  assert.equal(next.analysis.disableSearchShortcuts, true);
  assert.equal(next.analysis.guideRecorderOrder, false);
  assert.equal(next.unmatchedPolicy, "separate-sequence");
  assert.equal(next.includeProvisional, false);
  assert.equal(next.useOriginalAudio, true);
  assert.equal(next.audioEarlyMS, 40);
  assert.equal(applySyncSetup(next, { ...answers, uncertain: "queue" }).queueUncertain, true);
});
