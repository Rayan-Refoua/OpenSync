import {
  shortcuts,
  keyChord,
  keyLabel,
  validateShortcuts,
} from "./shortcuts.js";
import { CameraMonitor } from "./monitor.js";
import { clamp } from "./core.js";
import { currentReport } from "./status.js";
const defaultLayout = { position: "right", rows: 4, columns: 0 };
const $ = (id) => document.getElementById(id);
export function setupWorkstation(h) {
  const monitor = new CameraMonitor(
    $("cameraGrid"),
    () => h.state,
    (c) => {
      h.inspect(c);
      h.pan(c.start);
    },
  );
  let config = {
      mode: "clips",
      resources: {
        memoryMiB: 4096,
        workers: 8,
        background: "balanced",
        decodeWorkers: 2,
        thumbnailWorkers: 1,
        maxVideoFeeds: 4,
      },
      shortcuts: {},
      layout: { ...defaultLayout },
    },
    loaded = false,
    lastTime = 0,
    lastSample = 0,
    history = [],
    shuttle = 0;
  const bindings = () =>
    Object.fromEntries(
      Object.entries(shortcuts).map(([id, [key]]) => [
        id,
        config.shortcuts[id] ?? key,
      ]),
    );
  function reference() {
    const el = $("shortcutReference");
    el.replaceChildren();
    for (const [id, [def, label]] of Object.entries(shortcuts)) {
      const dt = document.createElement("dt"),
        dd = document.createElement("dd");
      dt.textContent = keyLabel(config.shortcuts[id] ?? def);
      dd.textContent = label;
      el.append(dt, dd);
    }
    for (const [id, action] of [
      ["play", "play"],
      ["stop", "stop"],
      ["fit", "fit"],
      ["open", "open"],
      ["zoomIn", "zoomIn"],
      ["zoomOut", "zoomOut"],
    ])
      $(id).title = `${shortcuts[action][1]} · ${keyLabel(bindings()[action])}`;
  }
  function apply() {
    config.mode ||= "clips";
    $("syncMode").value = config.mode;
    config.layout = { ...defaultLayout, ...config.layout };
    monitor.rows = config.layout.rows;
    monitor.columns = config.layout.columns;
    const right = config.layout.position === "right",
      panel = $("monitorPanel"),
      grip = $("monitorResize");
    document.body.classList.toggle("monitor-right", right);
    if (right) {
      $("inspectorResize").before(grip, panel);
    } else {
      $("workspace").prepend(panel, grip);
    }
    monitor.maxFeeds = config.resources.maxVideoFeeds;
    h.waves.limit =
      Math.min(64, Math.max(8, config.resources.memoryMiB / 32)) * 1048576;
    h.thumbs.limit = h.waves.limit;
    h.waves.concurrency = config.resources.background === "balanced" ? 4 : 1;
    h.thumbs.concurrency = config.resources.background === "balanced" ? 2 : 1;
    reference();
  }
  async function load() {
    if (loaded || !h.api()?.GetSettings) return;
    try {
      config = await h.api().GetSettings();
      loaded = true;
      apply();
    } catch (e) {
      h.message(String(e), true);
    }
  }
  function openSettings() {
    const f = $("settingsForm");
    for (const [k, v] of Object.entries(config.resources))
      f.elements[k].value = v;
    f.elements.audioEarlyMS.value = config.audioEarlyMS || 50;
    f.elements.audioLateMS.value = config.audioLateMS || 70;
    f.elements.unmatchedPolicy.value = config.unmatchedPolicy || "append";
    f.elements.monitorPosition.value = config.layout.position;
    f.elements.monitorRows.value = config.layout.rows;
    f.elements.monitorColumns.value = config.layout.columns;
    const list = $("shortcutSettings");
    list.replaceChildren();
    for (const [id, [def, label]] of Object.entries(shortcuts)) {
      const row = document.createElement("label"),
        input = document.createElement("input");
      input.readOnly = true;
      input.dataset.action = id;
      input.dataset.chord = config.shortcuts[id] ?? def;
      input.value = keyLabel(input.dataset.chord);
      input.title = "Press a key combination; Backspace clears this shortcut";
      input.onkeydown = (e) => {
        e.preventDefault();
        e.stopPropagation();
        if (["Control", "Shift", "Alt", "Meta"].includes(e.key)) return;
        input.dataset.chord = e.code === "Backspace" ? "" : keyChord(e);
        input.value = keyLabel(input.dataset.chord);
      };
      row.append(document.createTextNode(label), input);
      list.append(row);
    }
    $("settingsError").textContent = "";
    $("settingsDialog").showModal();
  }
  $("settingsButton").onclick = openSettings;
  $("closeSettings").onclick = () => $("settingsDialog").close();
  $("resetShortcuts").onclick = () => {
    for (const input of $("shortcutSettings").querySelectorAll("input")) {
      input.dataset.chord = shortcuts[input.dataset.action][0];
      input.value = keyLabel(input.dataset.chord);
    }
  };
  $("settingsForm").onsubmit = async (e) => {
    e.preventDefault();
    try {
      const next = {
        mode: config.mode,
        unmatchedPolicy: e.target.elements.unmatchedPolicy.value,
        audioEarlyMS: Number(e.target.elements.audioEarlyMS.value),
        audioLateMS: Number(e.target.elements.audioLateMS.value),
        resources: {},
        shortcuts: {},
        layout: {
          position: e.target.elements.monitorPosition.value,
          rows: Number(e.target.elements.monitorRows.value),
          columns: Number(e.target.elements.monitorColumns.value),
        },
      };
      for (const k of Object.keys(config.resources)) {
        const value = e.target.elements[k].value;
        next.resources[k] = k === "background" ? value : Number(value);
      }
      for (const input of $("shortcutSettings").querySelectorAll("input"))
        next.shortcuts[input.dataset.action] = input.dataset.chord;
      validateShortcuts(next.shortcuts);
      if (!h.api()?.UpdateSettings)
        throw Error("Native settings API is unavailable");
      await h.api().UpdateSettings(next);
      config = next;
      apply();
      $("settingsDialog").close();
    } catch (e) {
      $("settingsError").textContent = e.message || String(e);
    }
  };
  $("outputFolder").onclick = () => h.action(() => h.api().OpenOutputFolder());
  $("nextAttention").onclick = () => {
    const p = h.state.project;
    if (!p) return;
    const clips = p.clips
      .filter((c) => c.state === "unresolved" || c.state === "provisional")
      .sort((a, b) => a.start - b.start || a.index - b.index);
    if (!clips.length) {
      h.message("No unresolved or provisional recordings");
      return;
    }
    const at = clips.findIndex((c) => c.index === h.state.selected),
      c = clips[(at + 1) % clips.length];
    h.stop();
    h.inspect(c);
    h.pan(c.start);
    h.seek(c.start, false);
  };
  $("rebuildSolution").onclick = () =>
    h.action(() => h.reviewSolution({ kind: "mode", mode: config.mode }));
  $("undoReview").onclick = () =>
    h.action(async () => {
      h.stop();
      h.setProject(await h.api().UndoReview(), false);
    });
  $("resyncProject").onclick = () =>
    h.action(() => h.api().ResynchronizeProject());
  $("exportXML").onclick = () =>
    h.action(async () => {
      const path = await h.api().ExportProjectXML();
      if (path) h.message("Premiere XML saved: " + path);
    });
  $("solutionHistory").onclick = () =>
    h.action(async () => {
      const list = $("historyList");
      list.replaceChildren();
      for (const item of await h.api().SolutionHistory()) {
        const option = document.createElement("option");
        option.value = item.path;
        option.textContent =
          (item.created
            ? new Date(item.created).toLocaleString() + " · "
            : "") + item.label;
        option.selected = item.path === h.state.project?.path;
        list.append(option);
      }
      $("historyDialog").showModal();
    });
  $("closeHistory").onclick = () => $("historyDialog").close();
  $("openHistory").onclick = () =>
    h.action(async () => {
      if (!$("historyList").value) return;
      h.stop();
      h.setProject(await h.api().OpenProject($("historyList").value), false);
      $("historyDialog").close();
    });
  $("syncMode").onchange = () =>
    h.action(async () => {
      const next = { ...config, mode: $("syncMode").value };
      try {
        await h.api().UpdateSettings(next);
        config = next;
      } finally {
        apply();
      }
    });
  $("monitorToggle").onclick = () => {
    monitor.toggle();
    $("monitorToggle").textContent = monitor.visible ? "Collapse" : "Expand";
  };
  $("activityToggle").onclick = () =>
    $("activity").classList.toggle("collapsed");
  $("inspectorToggle").onclick = () =>
    $("inspectorPanel").classList.toggle("collapsed");
  function resizeGrip(id, move) {
    $(id).onpointerdown = (e) => {
      e.preventDefault();
      const x = e.clientX,
        y = e.clientY;
      const begin = {
        monitor: $("monitorPanel").offsetHeight,
        monitorWidth: $("monitorPanel").offsetWidth,
        activity: $("activity").offsetHeight,
        inspector: $("inspectorPanel").offsetWidth,
      };
      const fn = (ev) => move(begin, ev.clientX - x, ev.clientY - y);
      const up = () => {
        document.removeEventListener("pointermove", fn);
        document.removeEventListener("pointerup", up);
      };
      document.addEventListener("pointermove", fn);
      document.addEventListener("pointerup", up, { once: true });
    };
  }
  resizeGrip("monitorResize", (b, x, y) => {
    if (config.layout.position === "right")
      document.documentElement.style.setProperty(
        "--monitor-width",
        `${clamp(b.monitorWidth - x, 220, innerWidth * 0.6)}px`,
      );
    else
      document.documentElement.style.setProperty(
        "--monitor-height",
        `${clamp(b.monitor + y, 110, innerHeight - 390)}px`,
      );
  });
  resizeGrip("activityResize", (b, x, y) => {
    $("activity").classList.remove("collapsed");
    $("activity").style.height =
      `${clamp(b.activity - y, 60, innerHeight * 0.45)}px`;
  });
  resizeGrip("inspectorResize", (b, x) =>
    document.documentElement.style.setProperty(
      "--inspector-width",
      `${clamp(b.inspector - x, 220, 560)}px`,
    ),
  );
  function filterLogs() {
    const value = $("logFilter").value;
    for (const row of $("log").children) {
      const kind = row.dataset.kind || "info";
      row.hidden =
        value !== "all" &&
        (value === "warning"
          ? !["warning", "error"].includes(kind)
          : value === "stage"
            ? !["stage", "complete", "sync"].includes(kind)
            : value === "pair"
              ? !["pair", "evidence"].includes(kind)
              : kind !== value);
    }
  }
  $("logFilter").onchange = filterLogs;
  function setTool(hand) {
    h.state.hand = hand;
    $("handTool").classList.toggle("tool-active", hand);
    $("selectTool").classList.toggle("tool-active", !hand);
    $("canvas").style.cursor = hand ? "grab" : "default";
  }
  setTool(false);
  $("handTool").onclick = () => setTool(true);
  $("selectTool").onclick = () => setTool(false);
  const commands = {
    play: () => {
      shuttle = 0;
      h.togglePlay();
    },
    stop: () => {
      shuttle = 0;
      h.stop();
    },
    forward: () => {
      shuttle = shuttle > 0 ? Math.min(4, shuttle * 2) : 1;
      h.action(() => h.play(shuttle));
    },
    reverse: () => {
      shuttle = shuttle < 0 ? Math.max(-4, shuttle * 2) : -1;
      h.action(() => h.play(shuttle));
    },
    frameBack: () => h.seek(h.state.playhead - 1 / Number($("fps").value)),
    frameNext: () => h.seek(h.state.playhead + 1 / Number($("fps").value)),
    fiveBack: () => h.seek(h.state.playhead - 5 / Number($("fps").value)),
    fiveNext: () => h.seek(h.state.playhead + 5 / Number($("fps").value)),
    start: () => h.seek(0),
    end: () => h.seek(h.state.project?.duration || 0),
    fit: h.fit,
    zoomIn: () =>
      h.zoom(
        h.state.pps * 1.5,
        (h.state.playhead - h.state.left) * h.state.pps,
      ),
    zoomOut: () =>
      h.zoom(
        h.state.pps / 1.5,
        (h.state.playhead - h.state.left) * h.state.pps,
      ),
    open: () => $("open").click(),
    settings: openSettings,
    hand: () => setTool(true),
    select: () => setTool(false),
    focus: () => {
      const c = h.state.project?.clips[h.state.selected];
      if (c) {
        h.zoom(h.state.width / (c.end - c.start + 2));
        h.pan(c.start - 1);
      }
    },
  };
  for (const [id, direction] of [
    ["next", 1],
    ["previous", -1],
  ])
    commands[id] = () => {
      const cuts = [
        ...new Set(
          (h.state.project?.clips || []).flatMap((c) => [c.start, c.end]),
        ),
      ].sort((a, b) => a - b);
      const target =
        direction > 0
          ? cuts.find((t) => t > h.state.playhead + 0.0001)
          : cuts.findLast((t) => t < h.state.playhead - 0.0001);
      if (target !== undefined) h.seek(target);
    };
  document.addEventListener("keydown", (e) => {
    if (
      e.target.matches("input,select,textarea") ||
      document.querySelector("dialog[open]")
    )
      return;
    if (e.key === "Escape") {
      h.stop();
      return;
    }
    const chord = keyChord(e),
      entry = Object.entries(bindings()).find(([id, key]) => key === chord);
    if (entry) {
      e.preventDefault();
      commands[entry[0]]?.();
    }
  });
  const timer = setInterval(() => {
    const transport = h.transport();
    monitor.tick(h.state.playhead, transport.playing, transport.rate);
    const buffers = monitor.visible
      ? monitor.tiles.map((t) => monitor.bufferInfo(t))
      : [];
    const video = buffers.length
      ? Math.min(...buffers.map((b) => b.ahead / b.speed))
      : Infinity;
    const reserve = Math.max(0, Math.min(video, transport.audioAhead || 0));
    $("playbackBuffer").value = Math.min(8, reserve);
    $("playbackBuffer").hidden = !(transport.playing || transport.buffering);
    $("bufferDetail").textContent =
      transport.playing || transport.buffering
        ? `A ${(transport.audioAhead || 0).toFixed(1)}s · V ${Number.isFinite(video) ? video.toFixed(1) + "s" : "—"}`
        : "";
    if (transport.playing && transport.rate > 0 && monitor.needsBuffer())
      h.action(() => h.play(transport.rate));
  }, 50);
  window.addEventListener("beforeunload", () => {
    clearInterval(timer);
    monitor.reset();
  });
  function status(s) {
    $("syncMode").disabled = !!s.running;
    for (const id of [
      "rebuildSolution",
      "solutionHistory",
      "exportXML",
      "resyncProject",
    ])
      $(id).disabled = !!s.running || !h.state.project?.path;
    $("undoReview").disabled =
      !!s.running || !h.state.project?.graph?.solution_revision;
    if (s.diagnostic && h.api()?.ReportPreviewState)
      h.api()
        .ReportPreviewState(
          monitor.tiles.length,
          monitor.ready(8) ? monitor.tiles.length : 0,
        )
        .catch(() => {});
    load();
    filterLogs();
    $("outputFolder").disabled = !h.state.project?.path;
    const report = currentReport(s, h.state.project);
    $("currentState").dataset.tone = report.tone;
    $("stateTitle").textContent = report.title;
    $("stateDetail").textContent = report.detail;
    $("stateNext").textContent = `Next: ${report.next}`;
    $("stageProgress").value = report.value;
    $("stageProgress").max = report.max;
    $("stageProgress").title =
      "Pipeline stages completed; stages have different processing times";
    $("stageProgress").setAttribute("aria-valuetext", report.detail);
    $("stageProgress").hidden = false;
    $("stageProgress").classList.toggle("inactive", !report.busy);
    for (const id of ["stateTitle", "stateDetail", "stateNext"]) $(id).title = $(id).textContent;
    const volumes = s.storageVolumes || [],
      disks = s.storage || [],
      waiting = disks.reduce((n, d) => n + d.waiting, 0);
    $("storageState").textContent = disks.length
      ? `I/O ${disks.length} disks · ${waiting} queued`
      : volumes.some((v) => v.serialized)
        ? "I/O disk-aware"
        : volumes.length
          ? "I/O SSD parallel"
          : "I/O —";
    $("storageState").title = volumes
      .map((v) => `${v.volume} · ${v.kind.toUpperCase()} · ${v.reason}`)
      .join("\n");
    const r = s.resources;
    if (r?.available) {
      if (r.timestampMS !== lastSample) {
        lastSample = r.timestampMS;
        history.push(r.treeCPUPercent);
        if (history.length > 60) history.shift();
      }
      $("resources").textContent =
        `APP CPU ${r.cpuReady ? r.treeCPUPercent.toFixed(1) + "%" : "—"} · RAM ${(r.treeWorkingSetBytes / 1073741824).toFixed(2)} GB`;
      $("resources").title =
        `OpenSync + ${r.processCount - 1} child processes. Main: ${(r.appWorkingSetBytes / 1048576).toFixed(0)} MiB; WebView: ${(r.webviewWorkingSetBytes / 1048576).toFixed(0)} MiB; media: ${(r.decoderWorkingSetBytes / 1048576).toFixed(0)} MiB. System CPU ${r.systemCPUPercent.toFixed(1)}%. Working-set sums can include shared pages.`;
      const c = $("resourceGraph"),
        ctx = c.getContext("2d");
      ctx.clearRect(0, 0, c.width, c.height);
      ctx.strokeStyle = "#88b5c6";
      ctx.beginPath();
      history.forEach((v, i) => {
        const x = (i / 59) * c.width,
          y = c.height - (v / 100) * c.height;
        i ? ctx.lineTo(x, y) : ctx.moveTo(x, y);
      });
      ctx.stroke();
    }
    if (s.running)
      $("engineStage").textContent =
        `${s.stage} · ${Math.floor(s.stageSeconds || 0)}s`;
    if (config.resources.background === "off") {
      h.thumbs.suspended = true;
    } else h.thumbs.suspended = false;
  }
  apply();
  return { monitor, status, filterLogs, config: () => config };
}
