// Frontend wiring test. Native revision integrity/export is tested in Go.
import assert from "node:assert/strict";
import fs from "node:fs/promises";
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
  const url = process.env.OPENSYNC_PREVIEW_URL,
    project = await (await fetch(url + "/test/project")).json();
  await page.addInitScript((p) => {
    window.reviewCalls = [];
    const config = {
      mode: "clips",
      resources: {
        memoryMiB: 2048,
        workers: 4,
        background: "off",
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
            generation: 0,
            stage: "Project ready",
            inputs: 0,
            version: "review-test",
            visualization: await (await fetch("/test/stats")).json(),
          }),
          ReviewSolution: async (c) => {
            window.reviewCalls.push(c);
            const next = structuredClone(p);
            next.graph.solution_revision = { parent_project: p.path };
            next.path = p.path + ".revision";
            return next;
          },
          UndoReview: async () => {
            window.reviewCalls.push("undo");
            return structuredClone(p);
          },
          SolutionHistory: async () => [
            { path: p.path, label: "Original synchronization" },
            { path: p.path + ".revision", label: "Review revision" },
          ],
          OpenProject: async (path) => {
            window.reviewCalls.push({ open: path });
            return structuredClone(p);
          },
          ResynchronizeProject: async () => window.reviewCalls.push("resync"),
          ExportProjectXML: async () => {
            window.reviewCalls.push("export");
            return "test-export/OpenSync.xml";
          },
          OpenOutputFolder: async () => {},
        },
      },
    };
  }, project);
  await page.goto(url + "/?test=1");
  await page.waitForFunction(() => window.opensyncTest);
  await page.evaluate((p) => {
    const t = window.opensyncTest;
    t.setProject(p);
    t.workstation.monitor.enabled = false;
    t.inspect(p.clips[2]);
  }, project);
  await page.selectOption("#syncMode", "chronological");
  await page.waitForFunction(
    () => window.opensyncTest.workstation.config().mode === "chronological",
  );
  await page
    .getByRole("button", { name: "Reject this placement", exact: true })
    .click();
  await page.waitForFunction(() => window.reviewCalls.length === 1);
  assert.deepEqual(await page.evaluate(() => window.reviewCalls[0]), {
    kind: "reject_recording",
    recording: project.clips[2].id,
  });
  await page.locator(".projectActions summary").click();
  await page.locator("#undoReview").click();
  await page.waitForFunction(() => window.reviewCalls.includes("undo"));
  await page.locator("#rebuildSolution").click();
  await page.waitForFunction(() =>
    window.reviewCalls.some((c) => c.kind === "mode"),
  );
  assert.ok(
    await page.evaluate(() =>
      window.reviewCalls.some(
        (c) => c.kind === "mode" && c.mode === "chronological",
      ),
    ),
  );
  await page.locator("#solutionHistory").click();
  await page.waitForFunction(
    () => document.querySelector("#historyDialog").open,
  );
  assert.equal(await page.locator("#historyList option").count(), 2);
  await page.selectOption("#historyList", project.path);
  await page.locator("#openHistory").click();
  await page.waitForFunction(
    () => !document.querySelector("#historyDialog").open,
  );
  await page.locator("#exportXML").click();
  await page.locator("#resyncProject").click();
  await page.locator(".projectActions summary").click();
  await page.locator("#nextAttention").click();
  const selected = await page.evaluate(() => {
    const t = window.opensyncTest;
    return t.state.project.clips[t.state.selected].state;
  });
  assert.ok(["provisional", "unresolved"].includes(selected));
  const calls = await page.evaluate(() => window.reviewCalls);
  assert.ok(calls.includes("export") && calls.includes("resync"));
  assert.deepEqual(errors, []);
  assert.ok(
    !(await page.locator("#toast").textContent()).includes("Cannot read"),
    "caught UI error",
  );
  if (process.env.OPENSYNC_REVIEW_REPORT) {
    await fs.writeFile(
      process.env.OPENSYNC_REVIEW_REPORT,
      JSON.stringify(
        { calls, errors, nativeBackend: "covered separately by TestSolution" },
        null,
        2,
      ),
    );
    await page.screenshot({
      path: process.env.OPENSYNC_REVIEW_REPORT.replace(/\.json$/, ".png"),
    });
  }
  console.log("Review controls passed", JSON.stringify(calls));
} finally {
  await browser.close();
}
