import fs from "node:fs/promises";
import path from "node:path";
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
  let delayed = false,
    delayNext = false;
  await page.route("**/media/audio?*", async (route) => {
    if (delayNext) {
      delayNext = false;
      delayed = true;
      await new Promise((r) => setTimeout(r, 6500));
    }
    try {
      await route.continue();
    } catch {}
  });
  let delayVideoNext=false,videoDelayed=false;
  await page.route('**/media/segment?*',async route=>{if(delayVideoNext){delayVideoNext=false;videoDelayed=true;await new Promise(r=>setTimeout(r,9000));}try{await route.continue();}catch{}});
  const project = await (
    await fetch(process.env.OPENSYNC_PREVIEW_URL + "/test/project")
  ).json();
  await page.goto(process.env.OPENSYNC_PREVIEW_URL + "/?test=1");
  await page.waitForFunction(() => window.opensyncTest);
  await page.evaluate((p) => {
    const t = window.opensyncTest;
    t.setProject(p);
    t.zoom(10);
    t.pan(9);
    t.seek(10, false);
    t.state.tracks = new Set([1]);
  }, project);
  await page.evaluate(() =>
    Promise.race([
      window.opensyncTest.startPlayback(),
      new Promise((r) => setTimeout(r, 30000)),
    ]),
  );
  const startup = await page.evaluate(() => {
    const t = window.opensyncTest;
    return {
      audio: t.playbackStats(),
      cameras: t.workstation.monitor.tiles.map((x) =>
        t.workstation.monitor.bufferInfo(x),
      ),
    };
  });
  assert.ok(startup.audio.playing, "startup did not finish");
  assert.ok(
    startup.audio.audioAhead >= 7.8,
    "audio started before eight seconds were prepared",
  );
  assert.ok(
    startup.cameras.every(
      (b) => b.ahead + 0.05 >= Math.min(b.remaining, 8 * b.speed),
    ),
    "video started before eight seconds were buffered",
  );
  async function sample(ms) {
    return page.evaluate(async (ms) => {
      const t = window.opensyncTest;
      let minimumAudio = Infinity,
        minimumVideo = Infinity,
        bufferTransitions = 0,
        lateSamples = 0,
        seekingSamples = 0,
        prior = false;
      const end = performance.now() + ms;
      while (performance.now() < end) {
        const s = t.playbackStats();
        if (s.buffering && !prior) bufferTransitions++;
        prior = s.buffering;
        if (s.playing) {
          minimumAudio = Math.min(minimumAudio, s.audioAhead);
          for (const tile of t.workstation.monitor.tiles) {
            const b = t.workstation.monitor.bufferInfo(tile);
            if (b.remaining > 8 * b.speed)
              minimumVideo = Math.min(minimumVideo, b.ahead / b.speed);
            if (tile.video.seeking) seekingSamples++;
            if (tile.overlay.textContent === "CATCHING UP") lateSamples++;
          }
        }
        await new Promise((r) => setTimeout(r, 25));
      }
      return {
        minimumAudio,
        minimumVideo,
        bufferTransitions,
        lateSamples,
        seekingSamples,
        position: t.state.playhead,
        playing: t.playbackStats().playing,
        toast: document.getElementById("toast").textContent,
      };
    }, ms);
  }
  const steady = await sample(16000);
  console.log("STEADY", JSON.stringify({ startup, steady }));
  assert.equal(
    steady.bufferTransitions,
    0,
    "fast-storage playback repeatedly rebuffered",
  );
  assert.equal(steady.lateSamples, 0, "catching-up overlay flickered");
  assert.equal(
    steady.seekingSamples,
    0,
    "steady playback must not flush decoders with corrective seeks",
  );
  assert.ok(
    steady.minimumAudio > 4.8 && steady.minimumVideo > 4.8,
    "five-second reserve not maintained",
  );
  delayNext = true;
  const stalled = await sample(13000);
  assert.ok(
    delayed && stalled.bufferTransitions >= 1,
    "injected slow read did not trigger shared buffering",
  );
  assert.ok(stalled.playing, "playback did not recover after slow read");
  assert.equal(stalled.lateSamples, 0);
  assert.deepEqual(errors, []);
  delayVideoNext=true;const videoStall=await sample(23000);assert.ok(videoDelayed,'video delay was not exercised');assert.ok(videoStall.playing,'video did not resume after delayed source segment');assert.equal(videoStall.lateSamples,0);assert.ok(videoStall.minimumVideo>=5,'video reserve drained below five seconds');
  await page.evaluate(() => window.opensyncTest.stopPlayback());
  const report = {
    startup,
    steady,
    simulatedSlowAudioRead: stalled,
    delayMS: 6500,
    simulatedSlowVideoRead:videoStall,
    videoDelayMS:9000,
    pageErrors: errors,
    note: "Isolated Chromium, real original media. Injected I/O delay is a buffer regression, not an HDD throughput benchmark.",
  };
  await fs.mkdir(process.env.OPENSYNC_UI_RESULTS, { recursive: true });
  await fs.writeFile(
    path.join(process.env.OPENSYNC_UI_RESULTS, "playback-buffer-results.json"),
    JSON.stringify(report, null, 2),
  );
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser.close();
}
