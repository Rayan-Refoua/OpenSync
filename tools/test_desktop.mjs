// Run against tools/desktop-preview with Playwright supplied as a development
// dependency (OPENSYNC_PLAYWRIGHT points to playwright-core's index.mjs).
import fs from "node:fs/promises";
import path from "node:path";
import { pathToFileURL } from "node:url";
import assert from "node:assert/strict";
const { chromium } = await import(
  pathToFileURL(process.env.OPENSYNC_PLAYWRIGHT).href
);
const base = process.env.OPENSYNC_PREVIEW_URL,
  out = process.env.OPENSYNC_UI_RESULTS;
await fs.mkdir(out, { recursive: true });
const browser = await chromium.launch({
  executablePath: process.env.OPENSYNC_BROWSER,
  headless: true,
});
const page = await browser.newPage({
  viewport: { width: 1600, height: 1000 },
  deviceScaleFactor: 1,
});
const errors = [];
page.on("pageerror", (e) => errors.push(e.message));
await page.goto(base + "/?test=1");
await page.waitForFunction(() => window.opensyncTest);
const project = await (await fetch(base + "/test/project")).json();
await page.evaluate((p) => window.opensyncTest.setProject(p), project);
// Start at an overlapping region of the actual project, not a fabricated UI.
const focus = project.clips.find(
  (c) => c.state === "supported" && c.audio && !c.missing,
);
assert.ok(focus, "real source project unavailable");
await page.evaluate((start) => {
  const t = window.opensyncTest;
  t.zoom(12);
  t.pan(start);
}, focus.start);
await page.waitForFunction(
  () => window.opensyncTest.waves.cache.size > 2,
  {},
  { timeout: 60000 },
);
await page.screenshot({ path: path.join(out, "real-project.png") });
const bytes = await (
  await fetch(
    base +
      `/media/audio?g=${project.generation}&start=${focus.start + 2}&duration=1&tracks=[${focus.track}]`,
  )
).arrayBuffer();
assert.equal(bytes.byteLength, 48000 * 2 * 4);
const samples = new Float32Array(bytes);
let peak = 0;
for (const v of samples) {
  assert.ok(Number.isFinite(v));
  peak = Math.max(peak, Math.abs(v));
}
assert.ok(peak > 0.00001, "real audition block is silent");
await page.evaluate(async () => {
  const t = window.opensyncTest;
  t.seek(t.state.project.clips.find((c) => c.state === "supported").start + 2);
  await t.startPlayback();
});
await page.waitForFunction(
  () => document.getElementById("playState").textContent === "PLAYING",
  {},
  { timeout: 30000 },
);
await page.waitForFunction(
  () => window.opensyncTest.state.playhead > 6.2,
  {},
  { timeout: 30000 },
);
const playback = await page.evaluate(() => ({
  label: document.getElementById("playState").textContent,
  position: window.opensyncTest.state.playhead,
}));
await page.evaluate(() => window.opensyncTest.stopPlayback());
assert.equal(playback.label, "PLAYING");
const fixture = (count) => {
  const p = {
    name: `Stress / ${count} recordings`,
    generation: 9000 + count,
    duration: 0,
    path: "generated renderer benchmark",
    fps: 50,
    clips: [],
    tracks: [],
    evidence: [],
    graph: {},
  };
  const lanes = 20;
  for (let j = 0; j < lanes; j++)
    p.tracks.push({ name: `Camera ${j + 1}`, clips: [] });
  for (let i = 0; i < count; i++) {
    const track = i % lanes,
      start = Math.floor(i / lanes) * 18 + track * 0.04,
      duration = 15 + (i % 3) * 0.2;
    const c = {
      index: i,
      id: String(i),
      name: `Camera-${track + 1} / Clip-${i + 1}.mp4`,
      start,
      end: start + duration,
      duration,
      track,
      state: i % 11 === 0 ? "unresolved" : "supported",
      status: "renderer_fixture",
      rate: 1,
      residual: 0.001,
      component: 1,
      mapping: [
        [0, start],
        [duration, start + duration],
      ],
      audio: true,
      video: true,
      missing: false,
      source: "renderer-fixture",
      metadata: {},
    };
    p.clips.push(c);
    p.tracks[track].clips.push(i);
    p.duration = Math.max(p.duration, c.end);
  }
  return p;
};
const data = new Float32Array(1024);
for (let i = 0; i < 512; i++) {
  const v = 0.1 + Math.abs(Math.sin(i * 0.19) * Math.cos(i * 0.041)) * 0.7;
  data[i * 2] = -v;
  data[i * 2 + 1] = v;
}
await page.route("**/media/**", (route) =>
  route.fulfill({
    contentType: "application/octet-stream",
    body: Buffer.from(data.buffer),
  }),
);
const results = [];
for (const count of [500, 5000]) {
  await page.evaluate((p) => {
    document.getElementById("thumbs").checked = false;
    window.opensyncTest.setProject(p);
  }, fixture(count));
  await page.waitForTimeout(700);
  for (const mode of ["overview", "detail"]) {
    const result = await page.evaluate(async (mode) => {
      const t = window.opensyncTest;
      if (mode === "detail") t.zoom(4);
      else t.fit();
      await new Promise((r) => setTimeout(r, 200));
      t.state.frameTimes = [];
      const frames = [];
      let prev = performance.now();
      for (let i = 0; i < 120; i++) {
        await new Promise((r) => requestAnimationFrame(r));
        const now = performance.now();
        frames.push(now - prev);
        prev = now;
        t.pan(
          (Math.sin(i * 0.04) + 1) *
            Math.min(50, t.state.project.duration * 0.1),
        );
      }
      const times = [...t.state.frameTimes].sort((a, b) => a - b),
        deltas = frames.slice(1).sort((a, b) => a - b);
      return {
        mode,
        clips: t.state.project.clips.length,
        visible: t.state.drawn,
        drawMedianMS: times[Math.floor(times.length * 0.5)],
        drawP95MS: times[Math.floor(times.length * 0.95)],
        frameMedianMS: deltas[Math.floor(deltas.length * 0.5)],
        frameP95MS: deltas[Math.floor(deltas.length * 0.95)],
        waveCacheBytes: t.waves.bytes,
      };
    }, mode);
    results.push(result);
  }
}
await page.screenshot({ path: path.join(out, "stress-5000.png") });
assert.deepEqual(errors, []);
const report = {
  realProject: {
    name: project.name,
    recordings: project.clips.length,
    tracks: project.tracks.length,
    audioPeak: peak,
    playback,
  },
  renderer: results,
  pageErrors: errors,
  notes:
    "Headless Chromium Canvas timings; synthetic peaks for scale test. Not a native WebView2 FPS or audible speaker certification.",
};
await fs.writeFile(
  path.join(out, "results.json"),
  JSON.stringify(report, null, 2),
);
console.log(JSON.stringify(report, null, 2));
await browser.close();
