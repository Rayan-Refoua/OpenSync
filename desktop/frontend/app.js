import { setupAttention } from "./attention.js";
import { setupWorkstation } from "./workstation.js";
import { setupSourceReview } from "./source-review.js";
import { setupMatchReview } from "./match-review.js";
import { placementLabel } from "./status.js";
import { BUFFER_FLOOR, BUFFER_TARGET } from "./playback-buffer.js";
import {
  HEADER,
  ROW,
  RULER,
  clamp,
  mapTime,
  visibleClips,
  formatTime,
  waveformLevel,
  tileSeconds,
  tileBins,
  rulerStep,
  TileCache,
} from "./core.js";
const $ = (id) => document.getElementById(id),
  api = () => window.go?.main?.App;
const canvas = $("canvas"),
  ctx = canvas.getContext("2d", { alpha: false }),
  ruler = $("ruler"),
  rc = ruler.getContext("2d", { alpha: false });
const state = {
  project: null,
  left: 0,
  pps: 8,
  width: 1,
  height: 1,
  scroll: 0,
  selected: null,
  tracks: new Set(),
  trackOrder: [],
  playhead: 0,
  dirty: false,
  frameTimes: [],
  drawn: 0,
  lastSeq: 0,
  generation: 0,
};
const colors = {
  ambiguous: ["#453453", "#e5b3fc"],
  manual: ["#284838", "#b8e4a6"],
  supported: ["#24454a", "#8bbfc5"],
  validated: ["#294635", "#98c4a0"],
  provisional: ["#51442c", "#ddbe7b"],
  unresolved: ["#453844", "#b79aac"],
};
let headerStamp = "",
  scrollGuard = false,
  toastTimer,
  seekTimer,
  drag = null;
function message(text, error = false) {
  $("status").textContent = text;
  if (error) {
    $("toast").textContent = text;
    $("toast").hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => ($("toast").hidden = true), 12000);
  }
}
function log(
  kind,
  text,
  time = new Date().toLocaleTimeString(),
  recording = null,
) {
  const row = document.createElement("div");
  row.className = kind === "error" ? "error" : "";
  row.dataset.kind = kind;
  if (recording !== null) {
    row.classList.add("recording-event");
    row.title = "Locate this recording on the timeline";
    row.onclick = () => {
      const c = state.project?.clips[recording];
      if (c) {
        inspect(c);
        pan(c.start);
        $("scroller").scrollTop = trackRow(c.track) * ROW;
      }
    };
  }
  const stamp = document.createElement("span");
  stamp.className = "stamp";
  stamp.textContent = time;
  row.append(stamp, document.createTextNode(text));
  const el = $("log"),
    bottom = el.scrollTop + el.clientHeight >= el.scrollHeight - 30;
  el.append(row);
  while (el.children.length > 200) el.firstChild.remove();
  if (bottom) el.scrollTop = el.scrollHeight;
}
async function action(fn) {
  try {
    return await fn();
  } catch (e) {
    message(String(e.message || e), true);
    log("error", String(e.message || e));
    return null;
  }
}
async function fetchBytes(url, signal) {
  const r = await fetch(url, { signal });
  if (r.status === 503) throw Object.assign(Error("Preview paused"), { retry: true });
  if (!r.ok) throw Error(await r.text());
  return r.arrayBuffer();
}
const waves = new TileCache(24 << 20, 4, requestDraw, async (url, signal) => {
  const b = await fetchBytes(url, signal);
  return { value: new Float32Array(b), bytes: b.byteLength };
});
const thumbs = new TileCache(24 << 20, 2, requestDraw, async (url, signal) => {
  const r = await fetch(url, { signal });
  if (r.status === 503) throw Object.assign(Error("Preview paused"), { retry: true });
  if (!r.ok) throw Error(await r.text());
  const im = await createImageBitmap(await r.blob());
  return { value: im, bytes: im.width * im.height * 4 };
});
function url(kind, params) {
  return `/media/${kind}?${new URLSearchParams({ g: state.generation, ...params })}`;
}
function requestDraw() {
  if (!state.dirty) {
    state.dirty = true;
    requestAnimationFrame(draw);
  }
}
function resize() {
  const box = $("timeline").getBoundingClientRect();
  state.width = Math.max(1, box.width - HEADER - 12);
  state.height = Math.max(1, box.height - RULER);
  const dpr = window.devicePixelRatio || 1;
  for (const [c, h] of [
    [canvas, state.height],
    [ruler, RULER],
  ]) {
    c.width = Math.round(state.width * dpr);
    c.height = Math.round(h * dpr);
    c.style.width = `${state.width}px`;
    c.style.height = `${h}px`;
    c.getContext("2d").setTransform(dpr, 0, 0, dpr, 0, 0);
  }
  pan(state.left);
  headerStamp = "";
  requestDraw();
}
new ResizeObserver(resize).observe($("timeline"));
function limitLeft() {
  return Math.max(
    0,
    (state.project?.duration || 0) + 2 - state.width / state.pps,
  );
}
function updateScroll() {
  scrollGuard = true;
  const s = $("hscroll");
  s.scrollLeft =
    limitLeft() > 0
      ? (state.left / limitLeft()) * (s.scrollWidth - s.clientWidth)
      : 0;
  queueMicrotask(() => (scrollGuard = false));
}
function pan(left) {
  state.left = clamp(left, 0, limitLeft());
  updateScroll();
  requestDraw();
}
function zoom(pps, anchor = state.width / 2) {
  const t = state.left + anchor / state.pps;
  state.pps = clamp(
    pps,
    Math.max(
      0.001,
      state.width / Math.max(1, state.project?.duration || 100) / 4,
    ),
    4096,
  );
  $("zoom").value = Math.log2(state.pps);
  pan(t - anchor / state.pps);
}
function fit() {
  if (!state.project) return;
  state.pps = Math.max(
    0.001,
    (state.width - 24) / Math.max(1, state.project.duration),
  );
  state.left = 0;
  $("zoom").min = Math.min(-3, Math.log2(state.pps) - 1);
  $("zoom").value = Math.log2(state.pps);
  updateScroll();
  requestDraw();
}
const trackIcons = {
  video: '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="6" width="12" height="12" rx="2"/><path d="m15 10 6-4v12l-6-4"/></svg>',
  audio: '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 11v2m4-6v10m5-13v16m5-13v10m4-6v2"/></svg>',
};

function trackOrderKey(project) {
  const path = project.graph?.solution_revision?.base_project || project.path;
  return path ? "opensync.trackOrder:" + path : "";
}

function loadTrackOrder(project) {
  const defaults = project.tracks.map((_, index) => index);
  const key = trackOrderKey(project);
  if (!key) return defaults;
  try {
    const saved = JSON.parse(localStorage.getItem(key) || "[]");
    const index = new Map(project.tracks.map((track, i) => [track.id, i]));
    const ordered = saved.map((id) => index.get(id)).filter((i) => i != null);
    return [...new Set([...ordered, ...defaults])];
  } catch {
    return defaults;
  }
}

function trackRow(track) {
  return Math.max(0, state.trackOrder.indexOf(track));
}

function moveTrack(from, to) {
  if (from === to || to < 0 || to >= state.trackOrder.length) return;
  const [track] = state.trackOrder.splice(from, 1);
  state.trackOrder.splice(to, 0, track);
  const key = trackOrderKey(state.project);
  if (key) {
    try {
      localStorage.setItem(key, JSON.stringify(state.trackOrder.map((i) => state.project.tracks[i].id)));
    } catch (error) {
      log("warning", "Could not save this project's visual track order");
    }
  }
  headerStamp = "";
  requestDraw();
}

function setProject(p, fitView = true) {
  if (!p) return;
  stopPlayback();
  state.project = p;
  state.generation = p.generation;
  state.trackOrder = loadTrackOrder(p);
  const fps = $("fps");
  fps.querySelector("option[data-project-rate]")?.remove();
  const matchingRate = Array.from(fps.options).find((o) => Math.abs(Number(o.value) - p.fps) < 0.00001);
  if (matchingRate) fps.value = matchingRate.value;
  else if (p.fps > 0) {
    const option = new Option(`${Number(p.fps.toFixed(3))} fps`, p.fps, false, true);
    option.dataset.projectRate = "";
    fps.add(option);
  }
  state.selected = null;
  state.retrySelection = new Set();
  state.tracks = new Set(p.tracks.length ? [0] : []);
  waves.clear();
  thumbs.clear();
  headerStamp = "";
  $("spacer").style.height = `${p.tracks.length * ROW}px`;
  $("empty").hidden = p.clips.length > 0;
  $("projectName").textContent = p.name;
  $("projectName").title = p.path || "Live, provisional view";
  const ambiguous = p.clips.filter((c) => c.reviewCount).length,
    unresolved = p.clips.filter((c) => c.state === "unresolved" && !c.reviewCount).length,
    review = p.clips.filter((c) => c.state === "provisional").length,
    missing = p.clips.filter((c) => c.missing).length;
  $("counts").textContent =
    `${p.clips.length} recordings · ${p.tracks.length} lanes · ${unresolved} unresolved${ambiguous ? ` · ${ambiguous} with match candidates` : ""}${review ? ` · ${review} to review` : ""}${missing ? ` · ${missing} missing` : ""}`;
  const sourceNotice = $("singleSourceNotice");
  sourceNotice.hidden = p.sourceGroups !== 1 || p.clips.length < 2;
  $("inspector").replaceChildren();
  const hint = document.createElement("p");
  hint.className = "muted";
  hint.textContent =
    "Select a recording to inspect precise timing and evidence.";
  $("inspector").append(hint);
  // Measure after inserting the project, before computing fit. Also refit once
  // the browser has applied panel layout; no manual window resize is required.
  resize();
  if (fitView) {
    fit();
    const fittedScale = state.pps;
    requestAnimationFrame(() => {
      if (state.project === p) {
        const unchanged = state.pps === fittedScale;
        resize();
        if (unchanged) fit();
      }
    });
  } else pan(state.left);
  seek(0, false);
  requestDraw();
}
function clearProject() {
  setProject({ name: "No project open", generation: 0, fps: 50, clips: [], tracks: [], duration: 0, graph: {} });
  state.project = null;
  state.left = state.playhead = state.scroll = 0;
  $("counts").textContent = "Open a project to inspect its synchronization";
  $("projectName").title = "";
  $("log").replaceChildren();
  $("toast").hidden = true;
  message("Ready");
  updatePlayhead();
  requestDraw();
}
function draw() {
  state.dirty = false;
  const begin = performance.now(),
    p = state.project,
    w = state.width,
    h = state.height;
  ctx.fillStyle = "#15191e";
  ctx.fillRect(0, 0, w, h);
  rc.fillStyle = "#252b32";
  rc.fillRect(0, 0, w, RULER);
  rc.font = "10px Consolas,monospace";
  rc.fillStyle = "#aebdca";
  const step = rulerStep(state.pps);
  for (
    let t = Math.floor(state.left / step) * step;
    t < state.left + w / state.pps + step;
    t += step
  ) {
    const x = (t - state.left) * state.pps;
    rc.fillText(
      formatTime(t).slice(step >= 1 ? 0 : 3, step >= 1 ? 8 : 12),
      x + 5,
      14,
    );
    rc.fillStyle = "#5b6875";
    rc.fillRect(Math.round(x), 19, 1, 9);
    rc.fillStyle = "#aebdca";
    ctx.fillStyle = "#202730";
    ctx.fillRect(Math.round(x), 0, 1, h);
  }
  waves.begin();
  thumbs.begin();
  state.drawn = 0;
  if (p) {
    const first = Math.floor(state.scroll / ROW),
      last = Math.min(p.tracks.length, Math.ceil((state.scroll + h) / ROW));
    for (let i = first; i < last; i++) {
      const y = i * ROW - state.scroll;
      const track = p.tracks[state.trackOrder[i]];
      ctx.fillStyle = track.kind === "audio"
        ? (i % 2 ? "#191d2b" : "#1d2232")
        : (i % 2 ? "#162128" : "#19252c");
      ctx.fillRect(0, y, w, ROW - 1);
      ctx.fillStyle = "#303944";
      ctx.fillRect(0, y + ROW - 1, w, 1);
      for (const c of visibleClips(
        p,
        track,
        state.left,
        state.left + w / state.pps,
      )) {
        drawClip(c, y);
        state.drawn++;
      }
    }
    drawHeaders(first, last);
  }
  waves.end();
  thumbs.end();
  updatePlayhead();
  $("scale").textContent = `${(1 / state.pps).toFixed(3)} s/px`;
  $("visibleRange").textContent =
    `${formatTime(state.left).slice(0, 8)} — ${formatTime(state.left + w / state.pps).slice(0, 8)}`;
  const ms = performance.now() - begin;
  state.frameTimes.push(ms);
  if (state.frameTimes.length > 240) state.frameTimes.shift();
  $("performance").textContent =
    `${state.drawn} visible · draw ${ms.toFixed(1)} ms`;
}
function drawClip(c, row) {
  const left = (c.start - state.left) * state.pps,
    right = (c.end - state.left) * state.pps,
    x = Math.max(0, left),
    end = Math.min(state.width, right),
    width = end - x,
    y = row + 5,
    height = ROW - 10;
  if (width <= 0) return;
  const chronological = c.status === "provisional_chronological_estimate";
  const [fill, ink] = chronological ? ["#64164e", "#ff95db"] : colors[c.reviewCount ? "ambiguous" : c.state] || colors.provisional;
  // At overview scale a clip may occupy only two pixels. Its waveform and
  // status remain visible; expensive text clipping has no useful pixels there.
  const clipped = width > 24;
  if (clipped) {
    ctx.save();
    ctx.beginPath();
    ctx.rect(x, y, width, height);
    ctx.clip();
  }
  ctx.fillStyle = c.missing ? "#342f35" : fill;
  ctx.fillRect(x, y, width, height);
  ctx.fillStyle = "#ffffff08";
  ctx.fillRect(x, y, width, 21);
  ctx.fillStyle = ink;
  if (clipped) {
    ctx.font = "11px Segoe UI,sans-serif";
    ctx.fillText(c.name, x + 6, y + 14);
    ctx.font = "9px Consolas,monospace";
  }
  if (width > 170) {
    const label = c.missing
      ? "MISSING SOURCE"
      : c.reviewCount
        ? `${c.reviewCount} MATCH CANDIDATES`
      : c.status === "manually_accepted"
        ? "MANUALLY ACCEPTED"
      : chronological ? (c.timingBasis === "chronological_estimate_refined_by_audio" ? "AUDIO REFINED" : "CHRONOLOGICAL ESTIMATE")
      : c.status === "unresolved_disconnected_group"
        ? `GROUP ${c.component} · LOCAL SYNC`
        : c.state === "provisional"
          ? (c.timingBasis?.startsWith("local_") ? `GROUP ${c.component} · REVIEW` : "REVIEW")
        : c.state.toUpperCase();
    const tw = ctx.measureText(label).width;
    ctx.fillStyle = fill;
    ctx.fillRect(end - tw - 12, y + 2, tw + 10, 16);
    ctx.fillStyle = ink;
    ctx.fillText(label, end - tw - 6, y + 13);
  } else if (c.state === "provisional" && width >= 12 && !c.missing) {
    ctx.fillStyle = ink;
    ctx.beginPath();
    ctx.moveTo(end - 11, y + 3);
    ctx.lineTo(end - 3, y + 3);
    ctx.lineTo(end - 3, y + 11);
    ctx.fill();
  }
  let waveTop = y + 24,
    waveH = height - 28;
  if (c.video && $("thumbs").checked && width > 45 && !c.missing) {
    const frameH = 34;
    waveTop += frameH;
    waveH -= frameH;
    const sourceLo = clamp(
        mapTime(c.mapping, state.left + x / state.pps, true),
        0,
        c.duration,
      ),
      sourceHi = clamp(
        mapTime(c.mapping, state.left + end / state.pps, true),
        0,
        c.duration,
      );
    const interval =
      2 ** Math.ceil(Math.log2(Math.max(0.125, 110 / state.pps / c.rate)));
    let t = Math.floor(sourceLo / interval) * interval;
    let count = 0;
    for (; t < sourceHi && count < 24; t += interval, count++) {
      const tx = (mapTime(c.mapping, t) - state.left) * state.pps;
      const time = clamp(t + interval / 2, 0, Math.max(0, c.duration - 0.05));
      const key = `${state.generation}:${c.index}:thumb:${time.toFixed(4)}`,
        im = thumbs.get(
          key,
          url("thumb", { clip: c.index, time: time.toFixed(4) }),
        );
      if (im && !im.error) {
        ctx.globalAlpha = 0.85;
        ctx.drawImage(im, tx, y + 23, 60, 34);
        ctx.globalAlpha = 1;
      } else {
        ctx.fillStyle = "#10182066";
        ctx.fillRect(tx, y + 23, 60, 34);
      }
    }
  }
  const mid = waveTop + waveH / 2;
  ctx.strokeStyle = ink;
  ctx.lineWidth = 1;
  ctx.globalAlpha = 0.2;
  ctx.beginPath();
  ctx.moveTo(x, mid);
  ctx.lineTo(end, mid);
  ctx.stroke();
  ctx.globalAlpha = 1;
  let loaded = false,
    failed = "";
  if (c.audio && !c.missing && waveH > 0) {
    const sourceLo = clamp(
        mapTime(c.mapping, state.left + x / state.pps, true),
        0,
        c.duration,
      ),
      sourceHi = clamp(
        mapTime(c.mapping, state.left + end / state.pps, true),
        0,
        c.duration,
      );
    const level = waveformLevel((sourceHi - sourceLo) / Math.max(1, width)),
      span = tileSeconds(level),
      bins = tileBins(level),
      bin = span / bins;
    for (
      let tile = Math.floor(sourceLo / span);
      tile <= Math.floor(sourceHi / span) && tile * span < c.duration;
      tile++
    ) {
      const key = `${state.generation}:${c.index}:wave:${level}:${tile}`,
        data = waves.get(
          key,
          waves.cache.has(key)
            ? ""
            : url("wave", { clip: c.index, level, tile }),
        );
      if (!data) continue;
      if (data.error) {
        failed = data.error;
        continue;
      }
      loaded = true;
      ctx.beginPath();
      const first = clamp(
          Math.floor((sourceLo - tile * span) / bin),
          0,
          bins - 1,
        ),
        last = clamp(Math.ceil((sourceHi - tile * span) / bin), 0, bins);
      for (let i = first; i < last; i++) {
        const t = tile * span + i * bin,
          px = clamp((mapTime(c.mapping, t) - state.left) * state.pps, x, end);
        ctx.moveTo(px, mid - data[i * 2 + 1] * waveH * 0.46);
        ctx.lineTo(px, mid - data[i * 2] * waveH * 0.46);
      }
      ctx.stroke();
    }
  }
  if (width > 70 && (c.missing || failed || (!loaded && c.audio))) {
    ctx.fillStyle = "#b6bfc9";
    ctx.font = "10px Consolas,monospace";
    ctx.fillText(
      c.missing
        ? "Media offline · Locate media"
        : failed
          ? "Waveform unavailable · inspect activity"
          : "Loading waveform…",
      x + 7,
      mid + 4,
    );
  }
  if (!c.audio && width > 70) {
    ctx.fillStyle = "#aab7c3";
    ctx.fillText("No audio stream", x + 7, mid + 4);
  }
  if (c.activity && width > 250) {
    ctx.fillStyle = "#e4c383";
    ctx.font = "9px Consolas,monospace";
    ctx.fillText(c.activity, x + 7, y + height - 3);
  }
  if (clipped) ctx.restore();
  ctx.strokeStyle = (state.selected === c.index || state.retrySelection?.has(c.index)) ? "#e3eff6" : ink;
  ctx.globalAlpha = (state.selected === c.index || state.retrySelection?.has(c.index)) ? 1 : 0.55;
  ctx.lineWidth = (state.selected === c.index || state.retrySelection?.has(c.index)) ? 2 : 1;
  ctx.strokeRect(
    Math.max(-2, left) + 0.5,
    y + 0.5,
    Math.min(state.width + 4, right) - Math.max(-2, left) - 1,
    height - 1,
  );
  ctx.globalAlpha = 1;
}
function drawHeaders(first, last) {
  const stamp = `${first}:${last}:${state.generation}:${[...state.tracks]}`;
  const el = $("headers");
  if (headerStamp !== stamp) {
    headerStamp = stamp;
    el.replaceChildren();
    for (let i = first; i < last; i++) {
      const trackIndex = state.trackOrder[i];
      const track = state.project.tracks[trackIndex];
      const lane = document.createElement("div");
      lane.className = "track";
      lane.dataset.kind = track.kind;
      lane.dataset.index = i;
      const name = document.createElement("div");
      name.className = "name";
      name.innerHTML = trackIcons[track.kind] || "";
      const trackLabel = document.createElement("span");
      trackLabel.textContent = track.name.replace(/^\d+\s+/, "");
      name.append(trackLabel);
      name.title = trackLabel.textContent;
      const btn = document.createElement("button");
      btn.textContent = state.tracks.has(trackIndex) ? "ON" : "OFF";
      btn.className = state.tracks.has(trackIndex) ? "active" : "";
      btn.title = "Include this lane in audio audition (maximum two)";
      btn.onclick = () => {
        if (state.tracks.has(trackIndex)) state.tracks.delete(trackIndex);
        else if (state.tracks.size < 2) state.tracks.add(trackIndex);
        else {
          message(
            "Select at most two audition lanes. Turn one off first.",
            true,
          );
          return;
        }
        stopPlayback();
        headerStamp = "";
        requestDraw();
      };
      const count = document.createElement("small");
      count.textContent = `${track.clips.length} recordings`;
      const hint = document.createElement("div");
      hint.className = "lanehint";
      hint.textContent = track.kind === "audio" ? "AUDIO" : "VIDEO";
      const move = document.createElement("span");
      move.className = "track-order-controls";
      for (const [direction, sign] of [[-1, "↑"], [1, "↓"]]) {
        const button = document.createElement("button");
        button.textContent = sign;
        button.title = direction < 0 ? "Move track up" : "Move track down";
        button.setAttribute("aria-label", button.title + ": " + trackLabel.textContent);
        button.disabled = i + direction < 0 || i + direction >= state.trackOrder.length;
        button.onclick = () => moveTrack(i, i + direction);
        move.append(button);
      }
      lane.append(name, btn, count, hint, move);
      el.append(lane);
    }
  }
  for (const e of el.children)
    e.style.top = `${Number(e.dataset.index) * ROW - state.scroll}px`;
}
function updatePlayhead() {
  const x = (state.playhead - state.left) * state.pps;
  $("playhead").hidden = x < 0 || x > state.width || !state.project;
  $("playhead").style.left = `${HEADER + x}px`;
  $("time").textContent = formatTime(state.playhead);
}
function hit(e) {
  const rect = canvas.getBoundingClientRect(),
    x = e.clientX - rect.left,
    y = e.clientY - rect.top;
  const track = Math.floor((y + state.scroll) / ROW),
    t = state.left + x / state.pps;
  if (!state.project || track < 0 || track >= state.project.tracks.length)
    return null;
  return (
    visibleClips(
      state.project,
      state.project.tracks[state.trackOrder[track]],
      t,
      t + 0.000001,
    )[0] || null
  );
}
function inspect(c) {
  state.selected = c.index;
  const el = $("inspector");
  el.replaceChildren();
  const title = document.createElement("h2");
  title.textContent = c.name;
  const badge = document.createElement("span");
  badge.className = "badge";
  badge.textContent = placementLabel(c);
  badge.title = c.status;
  el.append(title, badge);
  const dl = document.createElement("dl");
  for (const [k, v] of [
    ["Timeline in", `${c.start.toFixed(6)} s`],
    ["Timeline out", `${c.end.toFixed(6)} s`],
    ["Source duration", `${c.duration.toFixed(6)} s`],
    ["Model rate", c.rate.toPrecision(12)],
    ["Clock difference", `${((c.rate - 1) * 1e6).toFixed(3)} ppm`],
    [
      "Fit residual",
      c.placementUncertainty || (c.state === "provisional" && !c.residual)
        ? "Not acoustically measured"
        : `${(c.residual * 1000).toFixed(3)} ms`,
    ],
    ...(c.placementUncertainty
      ? [
          ["Placement basis", c.timingBasis],
          [
            "Estimated uncertainty",
            `±${c.placementUncertainty.toFixed(3)} s (conditional)`,
          ],
        ]
      : []),
    ["Component", c.component],
    ["Timing knots", c.mapping.length],
    ["Engine status", c.status],
    ["Media", c.missing ? "OFFLINE" : c.video ? "Video + audio" : "Audio"],
    ["Source frames", c.metadata.video?.avg_frame_rate || "—"],
    [
      "Audio format",
      c.metadata.audio
        ? `${c.metadata.audio.sample_rate} Hz / ${c.metadata.audio.channels} ch`
        : "None",
    ],
  ]) {
    const dt = document.createElement("dt"),
      dd = document.createElement("dd");
    dt.textContent = k;
    dd.textContent = v;
    dl.append(dt, dd);
  }
  el.append(dl);
  const note = document.createElement("p");
  note.className = "muted";
  note.textContent =
    c.reviewCount
      ? "Saved acoustic candidates are available. Compare the two recordings and accept a placement or reject individual offsets."
    : c.status === "manually_accepted"
      ? "A reviewer accepted this offset by audition. The decision is stored in this solution revision."
    : c.status === "unresolved_disconnected_group"
      ? "This recording has supported timing within its group. The group has no established offset to the main timeline, so it is presented separately."
      : c.state === "unresolved"
        ? "No supported placement relative to the main group. This recording is preserved at the timeline end."
        : c.status.startsWith("provisional_") && c.status.endsWith("review_required")
          ? (c.timingBasis?.startsWith("local_")
            ? "Candidate placement beside its reference in a local group. The group's offset to the main timeline is unresolved. Check the clip's timing before using it."
            : "Candidate placement from acoustic evidence. Check the clip's timing before using it.")
      : c.placementUncertainty
        ? "Provisional chronological estimate from recording order or calibrated timestamps. Its uncertainty is conditional on that metadata; this is not an acoustic match."
        : c.state === "supported"
          ? "The engine found consistent acoustic evidence for this placement. This is a completed synchronization result, not independent certification of physical capture time. Sound travel in a large venue can introduce a camera-dependent delay."
          : "Placement follows acoustic arrival time. Physical capture-time alignment is not independently established.";
  el.append(note);
  if (c.reviewCount && state.project.path) {
    const review = document.createElement("button");
    review.textContent = `Inspect ${c.reviewCount} match candidate${c.reviewCount === 1 ? "" : "s"}`;
    review.onclick = () => matchReview.open(c.index);
    el.append(review);
  }
  const ledger = state.project.graph?.review_ledger || {},
    rejected = (ledger.rejected_recordings || []).includes(c.id);
  const correction = document.createElement("button");
  correction.textContent = rejected
    ? "Restore placement evidence"
    : "Reject this placement";
  correction.title =
    "Save a reversible revision. Rejected recordings stay unresolved during re-evaluation; original measurements remain intact.";
  correction.disabled = !api()?.ReviewSolution || !state.project.path;
  correction.onclick = () =>
    action(() =>
      reviewSolution({
        kind: rejected ? "restore_recording" : "reject_recording",
        recording: c.id,
      }),
    );
  el.append(correction);
  const evidence = (state.project.evidence || []).filter(
    (e) => e.reference === c.index || e.query === c.index,
  );
  if (evidence.length) {
    const relations = document.createElement("details"),
      summary = document.createElement("summary");
    summary.textContent = "Review individual relationships";
    relations.append(summary);
    for (const e of evidence) {
      const a = state.project.clips[e.reference],
        b = state.project.clips[e.query];
      if (!a || !b) continue;
      const rejected = (ledger.rejected_pairs || []).some(
        (p) => (p.a === a.id && p.b === b.id) || (p.a === b.id && p.b === a.id),
      );
      const row = document.createElement("div"),
        label = document.createElement("span"),
        button = document.createElement("button");
      row.className = "relationReview";
      label.textContent =
        (a.index === c.index ? b.name : a.name) +
        (rejected ? " · rejected" : "");
      button.textContent = rejected ? "Restore" : "Reject";
      button.disabled = !api()?.ReviewSolution || !state.project.path;
      button.title =
        "Reject or restore this pair only, then re-solve the remaining saved relationships";
      button.onclick = () =>
        action(() =>
          reviewSolution({
            kind: rejected ? "restore_pair" : "reject_pair",
            reference: a.id,
            query: b.id,
          }),
        );
      row.append(label, button);
      relations.append(row);
    }
    el.append(relations);
  }
  for (const [label, value] of [
    [`Pair evidence (${evidence.length})`, evidence],
    [
      "Provisional placement hypotheses",
      (state.project.graph?.provisional_placements || []).filter(
        (p) => p.recording === c.index,
      ),
    ],
    ["Exact timing mapping", c.mapping],
    ["Source metadata", c.metadata],
  ]) {
    const details = document.createElement("details"),
      summary = document.createElement("summary"),
      pre = document.createElement("pre");
    summary.textContent = label;
    pre.textContent = JSON.stringify(value, null, 2);
    details.append(summary, pre);
    el.append(details);
  }
  const path = document.createElement("div");
  path.className = "path";
  path.textContent = c.source;
  el.append(path);
  requestDraw();
}

async function reviewSolution(change) {
  const previousPath = state.project?.path;
  const selected = state.project?.clips[state.selected]?.id;
  stopPlayback();
  const p = await api().ReviewSolution(change);
  setProject(p, false);
  const c = p?.clips.find((c) => c.id === selected);
  if (c) {
    inspect(c);
    pan(c.start);
  }
  message(change.kind === "add_chronological" ? (p.path === previousPath ? "No additional chronological placements found" : "Chronological placements added · inspect the magenta clips") : "Review revision saved; original evidence retained");
}

// Two-lane audition with one output clock, bounded four-second PCM blocks.
let audioCtx = null,
  playToken = 0,
  playing = false,
  buffering = false,
  baseClock = 0,
  baseTime = 0,
  nextTime = 0,
  scheduledEnd = 0,
  pumping = false,
  playRate = 1,
  playController = null,
  sources = new Set();
function stopPlayback() {
  clearTimeout(seekTimer);
  if (playing && audioCtx)
    state.playhead = clamp(
      baseTime + Math.max(0, audioCtx.currentTime - baseClock) * playRate,
      0,
      state.project?.duration || 0,
    );
  playToken++;
  playing = false;
  buffering = false;
  pumping = false;
  playController?.abort();
  for (const s of sources) {
    try {
      s.stop();
    } catch {}
  }
  sources.clear();
  $("play").textContent = "▶";
  $("playState").textContent = "STOPPED";
  updatePlayhead();
}
async function startPlayback(speed = 1) {
  if (!state.project) return;
  stopPlayback();
  playRate = clamp(speed, -4, 4) || 1;
  if (playRate > 0 && state.playhead >= state.project.duration)
    state.playhead = 0;
  if (playRate < 0 && state.playhead <= 0)
    state.playhead = state.project.duration;
  const token = ++playToken;
  playController = new AbortController();
  buffering = true;
  $("playState").textContent = "BUFFERING";
  audioCtx ??= new AudioContext({ sampleRate: 48000 });
  await audioCtx.resume();
  if (token !== playToken) return;
  baseTime = state.playhead;
  nextTime = baseTime;
  scheduledEnd = 0;
  try {
    await workstation.monitor.prepare(
      baseTime,
      playRate,
      playController.signal,
    );
    if (token !== playToken) return;
    await pumpAudio(token, true);
    if (token !== playToken) return;
    playing = true;
    buffering = false;
    $("play").textContent = "Ⅱ";
    $("playState").textContent =
      playRate === 1 ? "PLAYING" : `PLAY ${playRate}×`;
    requestAnimationFrame(playTick);
  } catch (e) {
    if (token === playToken) {
      stopPlayback();
      message(e.message || String(e), true);
    }
  }
}
async function decodeAudioBlock(token, at, duration) {
  const bytes = state.tracks.size
    ? await fetchBytes(
        url("audio", {
          start: at.toFixed(9),
          duration: duration.toFixed(9),
          tracks: JSON.stringify([...state.tracks]),
          fallback: workstation.config().autoAudioFallback ? 1 : 0,
        }),
        playController.signal,
      )
    : new ArrayBuffer(Math.round(duration * 48000) * 2 * 4);
  if (token !== playToken) return null;
  const data = new Float32Array(bytes),
    buffer = audioCtx.createBuffer(2, data.length / 2, 48000);
  for (let ch = 0; ch < 2; ch++) {
    const dest = buffer.getChannelData(ch);
    for (let i = 0; i < dest.length; i++)
      dest[i] = data[(playRate < 0 ? dest.length - 1 - i : i) * 2 + ch];
  }
  return buffer;
}
function scheduleAudioBlock(buffer, at) {
  const source = audioCtx.createBufferSource();
  source.buffer = buffer;
  source.playbackRate.value = Math.abs(playRate);
  source.connect(audioCtx.destination);
  sources.add(source);
  source.onended = () => sources.delete(source);
  const when = baseClock + Math.abs(at - baseTime) / Math.abs(playRate);
  source.start(when);
  scheduledEnd = when + buffer.duration / Math.abs(playRate);
}
async function pumpAudio(token, first = false) {
  if (pumping === token || token !== playToken) return;
  pumping = token;
  try {
    if (first) {
      const prepared = [];
      let cursor = baseTime,
        seconds = 0;
      while (
        seconds < BUFFER_TARGET &&
        (playRate > 0 ? cursor < state.project.duration : cursor > 0)
      ) {
        const duration = Math.min(
            4,
            playRate > 0 ? state.project.duration - cursor : cursor,
          ),
          at = playRate > 0 ? cursor : cursor - duration;
        const buffer = await decodeAudioBlock(token, at, duration);
        if (!buffer || token !== playToken) return;
        prepared.push({ buffer, at: cursor });
        cursor += duration * Math.sign(playRate);
        seconds += duration / Math.abs(playRate);
        $("playState").textContent =
          "BUFFERING · audio " +
          seconds.toFixed(1) +
          "s / " +
          BUFFER_TARGET +
          "s";
      }
      // Nothing is scheduled until BOTH video and all startup PCM are ready.
      await workstation.monitor.prepare(
        baseTime,
        playRate,
        playController.signal,
      );
      if (token !== playToken) return;
      baseClock = audioCtx.currentTime + 0.08;
      for (const item of prepared) scheduleAudioBlock(item.buffer, item.at);
      nextTime = cursor;
      return;
    }
    while (
      token === playToken &&
      (playRate > 0 ? nextTime < state.project.duration : nextTime > 0) &&
      scheduledEnd - audioCtx.currentTime < BUFFER_TARGET
    ) {
      const duration = Math.min(
          4,
          playRate > 0 ? state.project.duration - nextTime : nextTime,
        ),
        at = playRate > 0 ? nextTime : nextTime - duration;
      const buffer = await decodeAudioBlock(token, at, duration);
      if (!buffer || token !== playToken) return;
      if (
        baseClock + Math.abs(nextTime - baseTime) / Math.abs(playRate) <
        audioCtx.currentTime + 0.005
      )
        throw Error(
          "Audio buffer exhausted; playback stopped to preserve synchronization.",
        );
      scheduleAudioBlock(buffer, nextTime);
      nextTime += duration * Math.sign(playRate);
    }
  } finally {
    if (pumping === token) pumping = false;
  }
}
function playTick() {
  if (!playing) return;
  state.playhead = clamp(
    baseTime + Math.max(0, audioCtx.currentTime - baseClock) * playRate,
    0,
    state.project.duration,
  );
  if (
    playRate > 0
      ? state.playhead >= state.project.duration
      : state.playhead <= 0
  ) {
    stopPlayback();
    return;
  }
  updatePlayhead();
  if (
    (playRate > 0 ? nextTime < state.project.duration : nextTime > 0) &&
    scheduledEnd - audioCtx.currentTime < BUFFER_FLOOR + 0.25
  ) {
    action(() => startPlayback(playRate));
    return;
  }
  if (
    $("follow").checked &&
    (state.playhead < state.left ||
      state.playhead > state.left + (state.width / state.pps) * 0.85)
  )
    pan(state.playhead - (state.width / state.pps) * 0.2);
  const token = playToken;
  pumpAudio(token).catch((e) => {
    if (token === playToken && e.name !== "AbortError") {
      stopPlayback();
      message(e.message || String(e), true);
    }
  });
  requestAnimationFrame(playTick);
}
function seek(t, restart = true) {
  const resume = restart && (playing || buffering);
  stopPlayback();
  state.playhead = clamp(t, 0, state.project?.duration || 0);
  updatePlayhead();
  clearTimeout(seekTimer);
  if (resume)
    seekTimer = setTimeout(() => action(() => startPlayback(playRate)), 50);
}

canvas.addEventListener("pointerdown", (e) => {
  canvas.focus();
  if (e.button === 1 || e.altKey || state.hand) {
    drag = {
      x: e.clientX,
      y: e.clientY,
      left: state.left,
      scroll: state.scroll,
    };
    canvas.setPointerCapture(e.pointerId);
    e.preventDefault();
    return;
  }
  if (e.button !== 0) return;
  const c = hit(e);
  if (c) {
    state.retrySelection ??= new Set();
    if (e.ctrlKey) {
      if (!state.retrySelection.size && state.selected != null && state.selected !== c.index) {
        state.retrySelection.add(state.selected);
      }
      if (state.retrySelection.has(c.index)) state.retrySelection.delete(c.index);
      else state.retrySelection.add(c.index);
    } else {
      state.retrySelection.clear();
    }
    inspect(c);
  }
  const x = e.clientX - canvas.getBoundingClientRect().left;
  seek(state.left + x / state.pps);
});
canvas.addEventListener("pointermove", (e) => {
  if (drag) {
    pan(drag.left - (e.clientX - drag.x) / state.pps);
    $("scroller").scrollTop = drag.scroll - (e.clientY - drag.y);
    return;
  }
  const c = hit(e),
    tip = $("tooltip");
  if (!c) {
    tip.hidden = true;
    return;
  }
  tip.textContent = `${c.name}\n${c.status}\nIn ${formatTime(c.start)}  •  ${c.duration.toFixed(3)} s\n${c.placementUncertainty ? `Chronological estimate ±${c.placementUncertainty.toFixed(3)} s` : `Residual ${(c.residual * 1000).toFixed(3)} ms`}${c.missing ? "\nSOURCE MISSING" : ""}`;
  tip.hidden = false;
  tip.style.left = `${Math.min(e.clientX + 15, innerWidth - 430)}px`;
  tip.style.top = `${Math.min(e.clientY + 17, innerHeight - 130)}px`;
});
canvas.addEventListener("pointerup", () => (drag = null));
canvas.addEventListener("pointercancel", () => (drag = null));
canvas.addEventListener("pointerleave", () => ($("tooltip").hidden = true));
canvas.addEventListener("dblclick", (e) => {
  const c = hit(e);
  if (c) {
    state.pps = clamp((state.width - 70) / (c.end - c.start), 0.001, 4096);
    pan(c.start - 30 / state.pps);
  }
});
for (const handle of [ruler, $("playhead")]) {
  handle.title = "Drag to scrub the timeline";
  handle.addEventListener("pointerdown", (e) => {
    if (e.button !== 0 || !state.project) return;
    e.preventDefault();
    e.stopPropagation();
    const resume = playing || buffering,
      speed = playRate;
    clearTimeout(seekTimer);
    stopPlayback();
    handle.setPointerCapture(e.pointerId);
    const move = (ev) => {
      state.playhead = clamp(
        state.left +
          (ev.clientX - ruler.getBoundingClientRect().left) / state.pps,
        0,
        state.project.duration,
      );
      updatePlayhead();
    };
    const end = (ev) => {
      if (ev.type === "pointerup") move(ev);
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", end);
      handle.removeEventListener("pointercancel", end);
      if (handle.hasPointerCapture(e.pointerId))
        handle.releasePointerCapture(e.pointerId);
      if (resume && ev.type === "pointerup") action(() => startPlayback(speed));
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", end);
    handle.addEventListener("pointercancel", end);
    move(e);
  });
}
$("timeline").addEventListener(
  "wheel",
  (e) => {
    e.preventDefault();
    if (e.ctrlKey || e.metaKey) {
      zoom(
        state.pps * Math.exp(-e.deltaY * 0.002),
        clamp(e.clientX - canvas.getBoundingClientRect().left, 0, state.width),
      );
    } else if (e.shiftKey || Math.abs(e.deltaX) > Math.abs(e.deltaY)) {
      pan(state.left + (e.deltaX || e.deltaY) / state.pps);
    } else {
      $("scroller").scrollTop += e.deltaY;
    }
  },
  { passive: false },
);
$("scroller").addEventListener("scroll", () => {
  state.scroll = $("scroller").scrollTop;
  requestDraw();
});
$("hscroll").addEventListener("scroll", () => {
  if (scrollGuard) return;
  const s = $("hscroll");
  state.left =
    (s.scrollLeft / Math.max(1, s.scrollWidth - s.clientWidth)) * limitLeft();
  requestDraw();
});
$("open").onclick = () =>
  action(async () => setProject(await api().ChooseProject()));
$("locate").onclick = () =>
  action(async () => setProject(await api().LocateMedia(), false));
$("add").onclick = () => action(() => api().SelectInputs(false));
$("folder").onclick = () => action(() => api().SelectInputs(true));
$("clear").onclick = () => action(() => api().ClearInputs());
$("sync").onclick = () => action(() => api().StartSync());
$("cancel").onclick = () => action(() => api().CancelSync());
$("play").onclick = () =>
  playing || buffering ? stopPlayback() : action(startPlayback);
$("stop").onclick = stopPlayback;
$("fit").onclick = fit;
$("zoomIn").onclick = () => zoom(state.pps * 1.5);
$("zoomOut").onclick = () => zoom(state.pps / 1.5);
$("zoom").oninput = (e) => zoom(2 ** Number(e.target.value));
$("thumbs").onchange = requestDraw;
const updateSourceReview = setupSourceReview(api);
let polling = false;
async function poll() {
  if (polling || !api()) return;
  polling = true;
  try {
    const s = await api().Status(state.lastSeq);
    updateSourceReview(s.sourceReview);
    for (const e of s.events) {
      state.lastSeq = Math.max(state.lastSeq, e.seq);
      log(e.kind, e.message, e.time, e.recording ?? null);
    }
    $("engineStage").textContent = s.stage;
    $("activityDetail").textContent = s.detail;
    $("sync").textContent = s.running
      ? "Synchronizing…"
      : s.inputs
        ? `Synchronize (${s.inputs})`
        : "Synchronize";
    $("sync").disabled = s.running || !s.inputs;
    $("cancel").disabled = !s.running;
    for (const id of ["open", "add", "folder", "clear", "locate"])
      $(id).disabled = s.running;
    const mem = s.visualization;
    $("memory").textContent =
      `Visual RAM ${Math.round((mem.cacheBytes + waves.bytes + thumbs.bytes) / 1048576)} MB · ${mem.active} media workers · disk OFF`;
    if (s.generation && s.generation !== state.generation) {
      const p = await api().GetProject();
      setProject(p, !state.project || !s.running);
    }
    $("retrySelected").disabled = s.running || !state.project?.path;
    $("nextAttention").disabled = s.running || !state.project?.path;
    $("openMatchReview").disabled = s.running || !state.project?.path || !state.project?.manualCandidates?.length;
    let changed = false;
    if (s.running && state.project) {
      for (const [i, text] of Object.entries(s.clipActivity || {})) {
        const c = state.project.clips[Number(i)];
        if (c && c.activity !== text) {
          c.activity = text;
          changed = true;
        }
      }
    }
    if (changed) requestDraw();
    if (waves.lastError) {
      log("error", waves.lastError);
      waves.lastError = null;
    }
    if (thumbs.lastError) {
      log("error", thumbs.lastError);
      thumbs.lastError = null;
    }
    workstation.status(s);
  } catch (e) {
    message(String(e.message || e), true);
  } finally {
    polling = false;
  }
}
const workstation = setupWorkstation({
  setProject,
  clearProject,
  reviewSolution,
  state,
  api,
  action,
  message,
  inspect,
  pan,
  zoom,
  fit,
  seek,
  waves,
  thumbs,
  play: startPlayback,
  refreshAudition: () => {
    if (playing || buffering) action(() => startPlayback(playRate));
  },
  stop: stopPlayback,
  togglePlay: () =>
    playing || buffering ? stopPlayback() : action(() => startPlayback()),
  transport: () => ({
    playing: playing && audioCtx.currentTime >= baseClock,
    buffering,
    audioAhead: audioCtx ? Math.max(0, scheduledEnd - audioCtx.currentTime) : 0,
    rate: playRate,
  }),
});
const matchReview = setupMatchReview({state, api, stop: stopPlayback, setProject, inspect, pan, message});
setupAttention({state, api, action, setProject, inspect, pan, seek, message, stop: stopPlayback, matchReview, scrollToTrack: (track) => { $("scroller").scrollTop = trackRow(track) * ROW; }});
window.runtime?.OnFileDrop?.((_x, _y, paths) => {
  action(async () => setProject(await api().OpenDroppedProject(paths)));
}, false);
setInterval(poll, 750);
poll();
resize();
// Explicit test surface, independent of native UI automation. Used only by the
// development harness to inject fixtures and measure actual Canvas rendering.
export const testAPI = {
  workstation,
  inspect,
  setProject,
  state,
  pan,
  zoom,
  draw,
  fit,
  seek,
  startPlayback,
  stopPlayback,
  playbackStats: () => ({
    playing,
    buffering,
    audioAhead: audioCtx ? Math.max(0, scheduledEnd - audioCtx.currentTime) : 0,
  }),
  waves,
  thumbs,
  visibleClips,
};
if (new URLSearchParams(location.search).has("test"))
  window.opensyncTest = testAPI;
