// Real-media regression: an obsolete in-flight seek must not block a later seek.
// Uses an isolated browser and the desktop-preview server; no user UI control.
import fs from "node:fs/promises";
import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
const { chromium } = await import(
  pathToFileURL(process.env.OPENSYNC_PLAYWRIGHT).href
);
const browser = await chromium.launch({
  executablePath: process.env.OPENSYNC_BROWSER,
  headless: true,
});
try {
  const page = await browser.newPage({
      viewport: { width: 1600, height: 1000 },
    }),
    errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const url = process.env.OPENSYNC_PREVIEW_URL;
  const project = await (await fetch(url + "/test/project")).json();
  assert.equal(
    project.clips.length,
    6,
    "use the six-original Zendegifakher project",
  );
  await page.goto(url + "/?test=1");
  await page.waitForFunction(() => window.opensyncTest);
  await page.evaluate((p) => {
    const t = window.opensyncTest,
      c = p.clips.find((c) => c.name.startsWith("Office_ch5"));
    t.setProject(p);
    t.workstation.monitor.tick(c.start + 10, false);
    t.workstation.monitor.maxFeeds = 1;
    t.workstation.monitor.pins = new Set([c.track]);
    t.state.tracks = new Set([c.track]);
    t.seek(c.start + 10, false);
  }, project);
  await page.waitForFunction(
    () => {
      const m = window.opensyncTest.workstation.monitor;
      return (
        m.tiles.length === 1 &&
        m.tiles[0].clip.name.startsWith("Office_ch5") &&
        m.ready(8)
      );
    },
    {},
    { timeout: 45000 },
  );
  const results = [];
  for (const [name, intermediate, target, feeds] of [
    ["forward", 1000, 4200, 1],
    ["backward_multicamera", 6500, 2400, 4],
  ]) {
    await page.evaluate(
      ({ intermediate, feeds }) => {
        const t = window.opensyncTest;
        t.stopPlayback();
        t.workstation.monitor.maxFeeds = feeds;
        t.seek(intermediate, false);
      },
      { intermediate, feeds },
    );
    await page.waitForTimeout(80);
    const started = Date.now();
    await page.evaluate((target) => {
      const t = window.opensyncTest;
      t.seek(target, false);
      t.startPlayback();
    }, target);
    await page.waitForFunction(
      () => window.opensyncTest.playbackStats().playing,
      {},
      { timeout: 30000 },
    );
    const startupMs = Date.now() - started;
    const sample = await page.evaluate(async () => {
      const t = window.opensyncTest,
        end = performance.now() + 20000;
      let minAudio = Infinity,
        minVideo = Infinity,
        buffering = 0,
        seeking = 0,
        catchingUp = 0,
        maxFeeds = 0,
        maxSkew = 0;
      while (performance.now() < end) {
        const s = t.playbackStats();
        if (s.buffering) buffering++;
        if (s.playing) {
          minAudio = Math.min(minAudio, s.audioAhead);
          maxFeeds = Math.max(maxFeeds, t.workstation.monitor.tiles.length);
          for (const tile of t.workstation.monitor.tiles) {
            const b = t.workstation.monitor.bufferInfo(tile);
            minVideo = Math.min(minVideo, b.ahead / b.speed);
            if (tile.video.seeking) seeking++;
            if (tile.overlay.textContent === "CATCHING UP") catchingUp++;
            maxSkew = Math.max(
              maxSkew,
              Math.abs(
                tile.video.currentTime - (tile.target - tile.previewStart),
              ),
            );
          }
        }
        await new Promise((r) => setTimeout(r, 25));
      }
      return {
        minAudio,
        minVideo,
        buffering,
        seeking,
        catchingUp,
        maxFeeds,
        maxSkew,
        position: t.state.playhead,
        playing: t.playbackStats().playing,
      };
    });
    console.log(name, JSON.stringify({ startupMs, ...sample }));
    results.push({ name, startupMs, ...sample });
    assert.ok(
      sample.playing && sample.position > target + 19,
      "timeline did not advance",
    );
    assert.equal(
      sample.buffering,
      0,
      "repeated buffering during steady playback",
    );
    assert.equal(sample.catchingUp, 0, "catching-up overlay flickered");
    assert.equal(sample.seeking, 0, "steady playback repeatedly sought");
    assert.ok(
      sample.minAudio >= 4.8 && sample.minVideo >= 4.8,
      "five-second reserve depleted",
    );
    if (feeds === 4)
      assert.equal(sample.maxFeeds, 2, "both available cameras must play");
  }
  assert.deepEqual(errors, []);
  await page.evaluate(() => window.opensyncTest.stopPlayback());
  if (process.env.OPENSYNC_SEEK_REPORT)
    await fs.writeFile(
      process.env.OPENSYNC_SEEK_REPORT,
      JSON.stringify({ project: project.path, results, errors }, null, 2),
    );
} finally {
  await browser.close();
}
