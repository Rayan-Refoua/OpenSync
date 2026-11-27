import {
  shortcuts,
  keyChord,
  keyLabel,
  validateShortcuts,
} from "./shortcuts.js";
import { CameraMonitor } from "./monitor.js";
import { clamp } from "./core.js";
import { currentReport } from "./status.js";
import { addSettingHelp } from "./settings-help.js";
import { applySyncSetup, syncPresets, syncSetupAnswers } from "./sync-setup.js";
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
  let wasSyncing = false;
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
    $("syncMode").dispatchEvent(new Event("input"));
    $("fileTimePlacement").checked = config.mode === "chronological" && config.fileTimePlacement !== false;
    $("fileTimePlacement").disabled = config.mode !== "chronological" || $("syncMode").disabled;
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
  const reviewActions = $("reviewActions");
  reviewActions.append($("nextAttention"), $("findClip"), document.querySelector(".projectActions"));
  for (const label of $("settingsForm").querySelectorAll("label")) {
    const control = label.querySelector("input, select");
    if (!control) continue;
    const caption = label.cloneNode(true);
    caption.querySelector("input, select").remove();
    addSettingHelp(label, control, caption.textContent.trim());
  }
  addSettingHelp($("syncMode"), $("syncMode"), "Synchronization mode");
  function populateSettings(f, config) {
    $("fileTimePlacement").checked = config.fileTimePlacement !== false;
    for (const [k, v] of Object.entries(config.resources))
      f.elements[k].value = v;
    f.elements.sourceReview.value = config.sourceReview || "automatic";
    f.elements.searchShortcuts.checked = !config.analysis?.disableSearchShortcuts;
    f.elements.recovery.value = config.analysis?.recovery || "balanced";
    f.elements.readMode.value = config.analysis?.readMode || "auto";
    f.elements.timingProfiles.value = config.analysis?.timingProfiles || "auto";
    f.elements.globalTiming.checked = !!config.analysis?.globalTiming;
    f.elements.readers.value = config.analysis?.readers || "auto";
    f.elements.minWave.value = config.analysis?.verification?.minWave || 0.1;
    f.elements.minFlux.value = config.analysis?.verification?.minFlux || 0.25;
    f.elements.controlRatio.value = config.analysis?.verification?.controlRatio || 1.5;
    f.elements.controlMargin.value = config.analysis?.verification?.controlMargin || 0.035;
    f.elements.ambiguityRatio.value = config.analysis?.verification?.ambiguityRatio || 0.8;
    f.elements.eventToleranceMS.value = config.analysis?.verification?.eventToleranceMS || 150;
    f.elements.longReferenceHours.value = config.analysis?.longReferenceHours || 0;
    f.elements.recoveryReferenceHours.value = config.analysis?.recoveryReferenceHours || 0;
    f.elements.projectSummaryMarker.checked = !config.omitProjectMarker;
    f.elements.reverseTrackOrder.checked = !config.keepTrackOrder;
    f.elements.trackLayout.value = config.trackLayout || "family";
    f.elements.maxResidualMS.value = config.maxResidualMS || 150;
    f.elements.includeProvisional.checked = !!config.includeProvisional;
    f.elements.audioEarlyMS.value = config.audioEarlyMS || 50;
    f.elements.audioLateMS.value = config.audioLateMS || 70;
    f.elements.unmatchedPolicy.value = config.unmatchedPolicy || "append";
    f.elements.autoAudioFallback.checked = !!config.autoAudioFallback;
    f.elements.monitorPosition.value = config.layout.position;
    f.elements.monitorRows.value = config.layout.rows;
    f.elements.monitorColumns.value = config.layout.columns;
  }
  function watchSetting(control, value, restore) {
    const row = control.closest(".settingWithHelp");
    if (!row) return () => {};
    row.querySelector(".resetSetting")?.remove();
    const reset = document.createElement("button");
    reset.className = "resetSetting";
    reset.type = "button";
    reset.textContent = "↶";
    const name = row.querySelector(".settingHelpButton")?.getAttribute("aria-label")?.slice(6) || "setting";
    reset.title = `Reset ${name} to default`;
    reset.setAttribute("aria-label", reset.title);
    row.append(reset);
    const read = () => control.dataset.chord ?? (control.type === "checkbox" ? control.checked : control.value);
    const update = () => {
      const current = read();
      const changed = control.type === "number" && current !== ""
        ? Number(current) !== Number(value) : current !== value;
      row.classList.toggle("changedSetting", changed);
      reset.disabled = !changed || control.disabled;
    };
    control.oninput = update;
    reset.onclick = () => {
      restore(value);
      update();
    };
    update();
    return update;
  }
  watchSetting($("syncMode"), "clips", (value) => {
    $("syncMode").value = value;
    $("syncMode").dispatchEvent(new Event("change"));
  });
  async function openSettings(scope = "workstation") {
    const f = $("settingsForm");
    const titles = {
      workstation: "Workstation preferences",
      sync: "Synchronization options",
      export: "Premiere XML options",
    };
    const defaults = await h.api().DefaultSettings();
    $("settingsTitle").textContent = titles[scope];
    for (const panel of f.querySelectorAll("[data-settings]"))
      panel.hidden = panel.dataset.settings !== scope;
    $("resetShortcuts").hidden = scope !== "workstation";
    const controls = [...f.querySelectorAll("[data-settings] input, [data-settings] select")]
      .filter((control) => control.name || control.id === "fileTimePlacement");
    populateSettings(f, defaults);
    const read = (control) => control.type === "checkbox" ? control.checked : control.value;
    const values = controls.map(read);
    populateSettings(f, config);
    controls.forEach((control, index) => watchSetting(control, values[index], (value) => {
      if (control.type === "checkbox") control.checked = value;
      else control.value = value;
    }));
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
        input.dispatchEvent(new Event("input"));
      };
      row.append(document.createTextNode(label), input);
      list.append(row);
      addSettingHelp(row, input, label, [
        `Assigns a keyboard shortcut for ${label.toLowerCase()}.`,
        `The default is ${keyLabel(def)}. Press a new combination in the field, or Backspace to clear it. Avoid shortcuts used by other workstation commands. This changes the controls only.`,
      ]);
      watchSetting(input, def, (value) => {
        input.dataset.chord = value;
        input.value = keyLabel(value);
      });
    }
    $("settingsError").textContent = "";
    $("settingsDialog").showModal();
  }
  $("settingsButton").onclick = () => h.action(() => openSettings());
  $("syncSettings").onclick = () => h.action(() => openSettings("sync"));
  const setupForm = $("syncSetupForm"), setupDialog = $("syncSetupDialog");
  function showSetupAnswers(answers) {
    for (const [name, value] of Object.entries(answers)) setupForm.elements[name].value = value;
    updateSetupSummary();
  }
  function readSetupAnswers() {
    return Object.fromEntries(["structure", "confidence", "effort", "timestamps", "review", "referenceSource", "recorderOrder", "uncertain", "drift", "early", "late"].map(name => [name, setupForm.elements[name].value]));
  }
  function updateSetupSummary() {
    const answers = readSetupAnswers();
    $("setupRecorderOrder").hidden = answers.structure !== "chronological";
    $("setupTimestampField").hidden = answers.structure !== "chronological" || answers.recorderOrder === "independent";
    for (const label of setupForm.querySelectorAll("[data-drift-tolerance]")) {
      label.hidden = answers.drift === "original";
      label.querySelector("input").disabled = label.hidden;
    }
    const placement = answers.structure === "chronological" && answers.timestamps === "estimate" && answers.recorderOrder !== "independent"
      ? "Calibrated timestamp estimates are allowed and identified on the timeline."
      : "Placement uses acoustic evidence.";
    const confidence = answers.confidence === "loose" ? " Looser screening can admit more ambiguous leads. Independent checks still apply."
      : answers.confidence === "strict" ? " Stricter screening can leave faint overlaps unresolved." : "";
    const review = answers.review === "always" ? " Source review opens before synchronization." : "";
    $("syncSetupSummary").textContent = placement + confidence + review;
  }
  $("syncSetup").onclick = () => h.action(async () => {
    await load();
    setupForm.elements.preset.value = "";
    $("projectExceptions").open = false;
    for (const [name, value] of Object.entries({
      referenceSource: config.referenceSource || "", recorderOrder: config.analysis?.guideRecorderOrder ? "guided" : "", uncertain: "",
      drift: config.useOriginalAudio ? "original" : "corrected",
      early: config.audioEarlyMS ?? 50, late: config.audioLateMS ?? 70,
    })) setupForm.elements[name].value = value;
    showSetupAnswers(syncSetupAnswers(config));
    $("syncSetupError").textContent = "";
    setupDialog.showModal();
  });
  setupForm.onchange = event => {
    if (event.target.name === "preset") {
      showSetupAnswers(syncPresets[event.target.value] || syncSetupAnswers(config));
    } else {
      setupForm.elements.preset.value = "";
      updateSetupSummary();
    }
  };
  $("pickTimingReference").onclick = () => h.action(async () => {
    const path = await h.api().PickSyncReference();
    if (path) setupForm.elements.referenceSource.value = path;
  });
  $("clearTimingReference").onclick = () => { setupForm.elements.referenceSource.value = ""; };
  $("reviewSourceOverlap").onclick = () => {
    setupForm.elements.review.value = "always";
    setupForm.elements.preset.value = "";
    updateSetupSummary();
  };
  $("closeSyncSetup").onclick = () => setupDialog.close();
  setupForm.onsubmit = async event => {
    event.preventDefault();
    const submit = setupForm.querySelector('button[type="submit"]');
    submit.disabled = true;
    try {
      const next = applySyncSetup(config, readSetupAnswers());
      await h.api().UpdateSettings(next);
      config = next;
      apply();
      setupDialog.close();
    } catch (error) {
      $("syncSetupError").textContent = error.message || String(error);
    } finally {
      submit.disabled = false;
    }
  };
  $("exportSettings").onclick = () => h.action(() => openSettings("export"));
  $("resetWorkstation").onclick = () => h.action(async () => {
    if (!window.confirm("Clear the open timeline and input list, and restore all settings to defaults? Saved projects and source files stay on disk.")) return;
    config = await h.api().ResetWorkstation();
    apply();
    monitor.reset();
    h.clearProject();
  });
  $("closeSettings").onclick = () => $("settingsDialog").close();
  $("resetShortcuts").onclick = () => {
    for (const input of $("shortcutSettings").querySelectorAll("input")) {
      input.dataset.chord = shortcuts[input.dataset.action][0];
      input.value = keyLabel(input.dataset.chord);
      input.dispatchEvent(new Event("input"));
    }
  };
  $("restartDebugUI").onclick = () => h.action(() => h.api().RestartWithGUIDebug());
  $("settingsForm").onsubmit = async (e) => {
    e.preventDefault();
    try {
      const next = {
        referenceSource: config.referenceSource,
        useOriginalAudio: config.useOriginalAudio,
        queueUncertain: config.queueUncertain,
        mode: config.mode,
        sourceReview: e.target.elements.sourceReview.value,
        fileTimePlacement: config.mode === "chronological"
          ? $("fileTimePlacement").checked : config.fileTimePlacement !== false,
        analysis: {
          guideRecorderOrder: config.analysis?.guideRecorderOrder,
          recovery: e.target.elements.recovery.value,
          disableSearchShortcuts: !e.target.elements.searchShortcuts.checked,
          readMode: e.target.elements.readMode.value,
          timingProfiles: e.target.elements.timingProfiles.value,
          globalTiming: e.target.elements.globalTiming.checked,
          readers: e.target.elements.readers.value,
          verification: {
            minWave: Number(e.target.elements.minWave.value),
            minFlux: Number(e.target.elements.minFlux.value),
            controlRatio: Number(e.target.elements.controlRatio.value),
            controlMargin: Number(e.target.elements.controlMargin.value),
            ambiguityRatio: Number(e.target.elements.ambiguityRatio.value),
            eventToleranceMS: Number(e.target.elements.eventToleranceMS.value),
          },
          longReferenceHours: Number(e.target.elements.longReferenceHours.value),
          recoveryReferenceHours: Number(e.target.elements.recoveryReferenceHours.value),
        },
        trackLayout: e.target.elements.trackLayout.value,
        autoAudioFallback: e.target.elements.autoAudioFallback.checked,
        keepTrackOrder: !e.target.elements.reverseTrackOrder.checked,
        omitProjectMarker: !e.target.elements.projectSummaryMarker.checked,
        maxResidualMS: Number(e.target.elements.maxResidualMS.value),
        includeProvisional: e.target.elements.includeProvisional.checked,
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
      const restartAudio = !!config.autoAudioFallback !== next.autoAudioFallback;
      config = next;
      apply();
      $("settingsDialog").close();
      if (restartAudio) h.refreshAudition();
    } catch (e) {
      $("settingsError").textContent = e.message || String(e);
    }
  };
  $("outputFolder").onclick = () => h.action(() => h.api().OpenOutputFolder());
  const findDialog = $("findClipDialog"), findInput = $("findClipInput"), findResults = $("findClipResults");
  function updateMatches() {
    const query = findInput.value.trim().toLowerCase();
    const matches = (h.state.project?.clips || []).filter(c => c.name.toLowerCase().includes(query));
    findResults.replaceChildren(...matches.slice(0, 200).map(c => {
      const option = document.createElement("option");
      option.value = c.index;
      option.textContent = `${c.name} · lane ${c.track + 1}`;
      return option;
    }));
    findResults.selectedIndex = matches.length ? 0 : -1;
    $("jumpToClip").disabled = !matches.length;
  }
  function openFind() {
    if (!h.state.project || findDialog.open) return;
    findInput.value = "";
    updateMatches();
    findDialog.showModal();
    findInput.focus();
  }
  function jumpToMatch() {
    if (findResults.selectedIndex < 0) return;
    const clip = h.state.project?.clips[Number(findResults.value)];
    if (!clip) return;
    findDialog.close();
    h.stop();
    h.inspect(clip);
    const rowHeight = parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--row"));
    $("scroller").scrollTop = clip.track * rowHeight;
    h.pan(clip.start);
    h.seek(clip.start, false);
  }
  $("findClip").onclick = openFind;
  $("cancelFindClip").onclick = () => findDialog.close();
  $("jumpToClip").onclick = jumpToMatch;
  findInput.oninput = updateMatches;
  findResults.ondblclick = jumpToMatch;
  findDialog.addEventListener("keydown", e => {
    if (e.key === "Enter" && (e.target === findInput || e.target === findResults)) {
      e.preventDefault();
      jumpToMatch();
    }
  });
  document.addEventListener("keydown", e => {
    if (e.ctrlKey && e.key.toLowerCase() === "f" && !document.querySelector("dialog[open]") && h.state.project) {
      e.preventDefault();
      openFind();
    }
  });
  $("addChronological").onclick = () => h.action(() => h.reviewSolution({kind:"add_chronological", mode:config.mode}));
  $("rebuildSolution").onclick = () =>
    h.action(() => h.reviewSolution({ kind: "mode", mode: config.mode, fileTimePlacement: config.fileTimePlacement !== false }));
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
      h.setProject(await h.api().SelectHistoryRevision($("historyList").value), false);
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
    monitor.tick(h.state.playhead, transport.playing, transport.rate, transport.playing || transport.buffering);
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
    const estimate = (seconds) => {
      if (!Number.isFinite(seconds) || seconds < 0) return "ETA —";
      const n = Math.ceil(seconds);
      return n < 60 ? `~${n}s left` : `~${Math.floor(n / 60)}m ${String(n % 60).padStart(2, "0")}s left`;
    };
    const io = s.running ? s.ioProgress : null;
    const row = $("ioFiles");
    const files = (io?.files || []).map((f) =>
      `${f.name} · ${f.fraction >= 0 ? Math.round(f.fraction * 100) + "% · " : ""}${estimate(f.eta)}`,
    );
    row.textContent = io ? `Files left ${io.remaining}/${io.total} · ${estimate(io.eta)}${files.length ? "  |  " + files.join("  |  ") : ""}` : "";
    row.title = row.textContent;
    if (wasSyncing !== !!s.running) {
      wasSyncing = !!s.running;
      monitor.enabled = !s.running;
      if (s.running) { h.stop(); monitor.reset(); }
    }
    $("syncMode").disabled = !!s.running;
    $("syncMode").dispatchEvent(new Event("input"));
    $("resetWorkstation").disabled = !!s.running;
    $("syncSettings").disabled = !!s.running;
    $("syncSetup").disabled = !!s.running;
    $("exportSettings").disabled = !!s.running;
    const step = s.running
      ? s.stage === "Exporting Premiere XML" ? "export"
        : s.stage === "Re-evaluating saved evidence" ? "review" : "sync"
      : h.state.project?.path ? "review" : s.inputs ? "sync" : "load";
    for (const section of document.querySelectorAll(".workflowStep")) {
      if (section.dataset.step === step) section.setAttribute("aria-current", "step");
      else section.removeAttribute("aria-current");
    }
    $("fileTimePlacement").disabled = !!s.running || config.mode !== "chronological";
    for (const id of [
      "rebuildSolution",
      "solutionHistory",
      "exportXML",
      "resyncProject",
    ])
      $(id).disabled = !!s.running || !h.state.project?.path;
    $("addChronological").disabled = !!s.running || !h.state.project?.path || config.mode !== "chronological" || config.fileTimePlacement === false;
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
    if (s.running || config.resources.background === "off") {
      h.thumbs.suspended = true;
    } else h.thumbs.suspended = false;
  }
  apply();
  return { monitor, status, filterLogs, config: () => config };
}
