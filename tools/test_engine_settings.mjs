import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

const { chromium } = await import(pathToFileURL(process.env.OPENSYNC_PLAYWRIGHT).href);
const browser = await chromium.launch({ executablePath: process.env.OPENSYNC_BROWSER, headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1450, height: 850 } });
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.addInitScript(() => {
    window.go = { main: { App: {
      GetSettings: async () => ({
        mode: "clips", shortcuts: {},
        resources: { memoryMiB: 1024, workers: 8, background: "balanced",
          decodeWorkers: 2, thumbnailWorkers: 1, maxVideoFeeds: 4 },
      }),
      Status: async () => ({ events: [], running: false, visualization: { active: 0, cacheBytes: 0 } }),
      UpdateSettings: async (settings) => { window.savedSettings = settings; },
    }}};
  });
  await page.goto(process.env.OPENSYNC_PREVIEW_URL + "/?test=1");
  await page.waitForFunction(() => window.opensyncTest?.workstation.config().resources.memoryMiB === 1024);
  await page.locator("#settingsButton").click();
  await page.locator(".engineSettings summary").click();
  assert.equal(await page.locator("[name=recovery]").inputValue(), "balanced");
  for (const [key, value] of [["recovery", "deep"], ["readMode", "buffered"], ["readers", "serial"], ["trackLayout", "folder"]]) {
    await page.selectOption(`[name=${key}]`, value);
  }
  await page.locator("[name=longReferenceHours]").fill("66");
  await page.locator("[name=maxResidualMS]").fill("100");
  await page.locator("[name=includeProvisional]").check();
  await page.getByRole("button", { name: "Apply", exact: true }).click();
  await page.waitForFunction(() => window.savedSettings);
  const settings = await page.evaluate(() => window.savedSettings);
  assert.deepEqual(settings.analysis, { recovery: "deep", readMode: "buffered", readers: "serial", longReferenceHours: 66 });
  assert.equal(settings.trackLayout, "folder");
  assert.equal(settings.maxResidualMS, 100);
  assert.equal(settings.includeProvisional, true);
  assert.equal(settings.resources.workers, 8);
  assert.deepEqual(errors, []);
  console.log("Advanced settings defaults and manual selection passed");
} finally {
  await browser.close();
}
