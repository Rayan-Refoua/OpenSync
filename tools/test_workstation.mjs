import fs from "node:fs/promises";
import path from "node:path";
import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
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
try {
  const page = await browser.newPage({
      viewport: { width: 1700, height: 1100 },
    }),
    errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const project = await (await fetch(base + "/test/project")).json();
  await page.addInitScript(() => {
    const config = {
      resources: {
        memoryMiB: 2048,
        workers: 4,
        background: "balanced",
        decodeWorkers: 2,
        thumbnailWorkers: 1,
        maxVideoFeeds: 4,
      },
      shortcuts: {},
    };
    window.go = {
      main: {
        App: {
          GetSettings: async () => structuredClone(config),
          UpdateSettings: async (s) => Object.assign(config, s),
          Status: async () => ({
            events: [],
            running: false,
            stage: "Read-only media validation",
            detail: "Artificial test layout; synchronization not asserted",
            generation: 0,
            inputs: 0,
            version: "workstation-test",
            visualization: await (await fetch("/test/stats")).json(),
          }),
          OpenOutputFolder: async () => {},
        },
      },
    };
  });
  await page.goto(base + "/?test=1");
  await page.waitForFunction(() => window.opensyncTest);
  await page.evaluate((p) => {
    const t = window.opensyncTest;
    t.setProject(p);
    t.zoom(16);
    t.pan(0);
    t.seek(1, false);
    t.inspect(p.clips[1]);
  }, project);
  await page.waitForFunction(
    () =>
      [...document.querySelectorAll("#cameraGrid video")].filter(
        (v) => v.readyState >= 2,
      ).length === 4,
    {},
    { timeout: 45000 },
  );
  await page.screenshot({ path: path.join(out, "workstation-four-feeds.png") });
  const initial = await page.evaluate(() =>
    window.opensyncTest.workstation.monitor.tiles.map((t) => ({
      index: t.clip.index,
      source: t.clip.name,
      ready: t.video.readyState,
      proxy: t.proxy,
      time: t.video.currentTime,
      frameTime: t.frameTime,
    })),
  );
  assert.equal(initial.length, 4);
  const layout = await page.evaluate(() => ({
    right: document.body.classList.contains("monitor-right"),
    columns: document.getElementById("cameraGrid").style.gridTemplateColumns,
    width: window.opensyncTest.state.width,
    actual: document.getElementById("canvas").getBoundingClientRect().width,
  }));
  assert.ok(layout.right);
  assert.match(layout.columns, /repeat\(1,/);
  assert.ok(layout.width > 200 && Math.abs(layout.width - layout.actual) < 1);
  const handle = await page.locator("#playhead").boundingBox();
  await page.mouse.move(handle.x + 5, handle.y + 5);
  await page.mouse.down();
  await page.mouse.move(handle.x + 85, handle.y + 5, { steps: 8 });
  await page.mouse.up();
  assert.ok(
    (await page.evaluate(() => window.opensyncTest.state.playhead)) > 5,
  );
  await page.evaluate(() => window.opensyncTest.seek(1, false));

  await page.keyboard.press("ArrowRight");
  const stepped = await page.evaluate(() => window.opensyncTest.state.playhead);
  assert.ok(Math.abs(stepped - 1.02) < 0.0001);
  await page.keyboard.press("Control+Alt+k");
  await page.waitForSelector("#settingsDialog[open]");
  await page.locator("input[name=maxVideoFeeds]").fill("6");
  await page.locator("select[name=monitorPosition]").selectOption("top");
  await page.locator("input[name=monitorRows]").fill("2");
  await page.locator("select[name=monitorColumns]").selectOption("2");
  await page.locator("#settingsForm button[type=submit]").click();
  await page.waitForFunction(
    () => window.opensyncTest.workstation.monitor.maxFeeds === 6,
  );
  await page.waitForFunction(
    () => document.querySelectorAll("#cameraGrid video").length === 6,
  );
  assert.equal(
    await page.evaluate(() =>
      document.body.classList.contains("monitor-right"),
    ),
    false,
  );
  await page.evaluate(() => {
    const t = window.opensyncTest;
    t.workstation.monitor.maxFeeds = 4;
    t.seek(1, false);
  });
  await page.keyboard.press("Space");
  await page.waitForFunction(
    () => document.getElementById("playState").textContent === "PLAYING",
    {},
    { timeout: 30000 },
  );
  try {
    await page.waitForFunction(
      () => window.opensyncTest.state.playhead > 6,
      {},
      { timeout: 30000 },
    );
  } catch (e) {
    console.log(
      "STALLED",
      JSON.stringify(
        await page.evaluate(() => ({
          time: window.opensyncTest.state.playhead,
          label: document.getElementById("playState").textContent,
          toast: document.getElementById("toast").textContent,
          feeds: window.opensyncTest.workstation.monitor.tiles.map((t) => ({
            index: t.clip.index,
            time: t.video.currentTime,
            target: t.target,
            ready: t.video.readyState,
            seeking: t.video.seeking,
            paused: t.video.paused,
            proxy: t.proxy,
            failed: t.failed,
            frame: t.frameTime,
          })),
        })),
      ),
    );
    throw e;
  }
  const playback = await page.evaluate(() => {
    const t = window.opensyncTest;
    return {
      playhead: t.state.playhead,
      feeds: t.workstation.monitor.tiles.map((tile) => ({
        index: tile.clip?.index,
        name: tile.clip?.name,
        ready: tile.video.readyState,
        proxy: tile.proxy,
        driftMS:
          (tile.video.currentTime - (tile.target - tile.previewStart)) * 1000,
        displayedDriftMS:
          (tile.frameTime - (tile.target - tile.previewStart)) * 1000,
        error: tile.failed,
      })),
    };
  });
  console.log("Playback sample", JSON.stringify(playback));
  await page.keyboard.press("k");
  assert.ok(
    playback.feeds.every((t) => t.index !== 0),
    "finished video was not replaced",
  );
  assert.ok(
    playback.feeds.every((t) => t.ready >= 2 && !t.error),
    "feed playback failed",
  );
  await page.evaluate(() => window.opensyncTest.seek(12, false));
  await page.keyboard.press("j");
  await page.waitForFunction(
    () => window.opensyncTest.state.playhead < 10,
    {},
    { timeout: 30000 },
  );
  await page.keyboard.press("k");
  // Exercise source seeking and decoding for each real video without moving files.
  const sources = [];
  for (const c of project.clips.filter((c) => c.video)) {
    const result = await page.evaluate(async (c) => {
      const v = document.createElement("video");
      v.muted = true;
      v.src = `/media/source?g=${window.opensyncTest.state.generation}&clip=${c.index}`;
      document.body.append(v);
      let native = false,
        error = "";
      try {
        await new Promise((resolve, reject) => {
          const timer = setTimeout(
            () => reject(Error("native metadata timeout")),
            6000,
          );
          v.onloadedmetadata = () => {
            clearTimeout(timer);
            resolve();
          };
          v.onerror = () => {
            clearTimeout(timer);
            reject(Error(v.error?.message || "native decoder rejected source"));
          };
        });
        v.currentTime = Math.min(1, c.duration / 2);
        await new Promise((resolve, reject) => {
          const timer = setTimeout(
            () => reject(Error("native seek timeout")),
            6000,
          );
          v.onseeked = () => {
            clearTimeout(timer);
            resolve();
          };
        });
        native = v.readyState >= 2;
      } catch (e) {
        error = e.message;
      }
      v.pause();
      v.removeAttribute("src");
      v.load();
      v.remove();
      return { name: c.name, native, error };
    }, c);
    sources.push(result);
  }
  // Reopening the same project reuses source identities in the backend peak store.
  await page.evaluate((p) => {
    const t = window.opensyncTest;
    t.workstation.monitor.enabled = false;
    t.setProject(p);
    t.zoom(16);
    t.pan(0);
  }, project);
  await page.waitForTimeout(600);
  assert.deepEqual(errors, []);
  const report = {
    layoutAndPlayheadDragPassed: true,
    realFiles: project.clips.length,
    initialFourFeeds: initial,
    playback,
    individualVideoNativeSeeking: sources,
    pageErrors: errors,
    notes:
      "Isolated headless Chromium; fixture placements explicitly provisional and NOT synchronization evidence. Native WebView2 hardware behavior requires separate launch check.",
  };
  await fs.writeFile(
    path.join(out, "workstation-results.json"),
    JSON.stringify(report, null, 2),
  );
  console.log(JSON.stringify(report, null, 2));
} finally {
  await browser.close();
}
