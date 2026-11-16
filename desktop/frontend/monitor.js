import { mapTime, clamp, formatTime } from "./core.js";
import { BufferedVideo } from "./buffered-video.js";
import {
  BUFFER_FLOOR,
  BUFFER_TARGET,
  bufferedAhead,
  hasBuffer,
  correctedRate,
} from "./playback-buffer.js";

function videoRange(c) {
  const stream = c.metadata?.video || {},
    d = Number(stream.duration);
  return [
    c.start,
    Math.min(
      c.end,
      mapTime(c.mapping, Number.isFinite(d) && d > 0 ? d : c.duration),
    ),
  ];
}
export function chooseFeeds(project, t, pins, current, limit) {
  if (!project) return [];
  const active = [];
  for (const c of project.clips) {
    const [a, b] = videoRange(c);
    if (c.video && !c.missing && t >= a && t < b) active.push(c);
  }
  const result = [],
    used = new Set();
  for (const track of pins) {
    if (result.length >= limit) break;
    const c = active.find((c) => c.track === track);
    if (c) {
      result.push({ track, clip: c, pinned: true });
      used.add(c.index);
    }
  }
  for (const old of current) {
    if (result.length >= limit) break;
    const c = active.find((c) => c.index === old?.clip?.index);
    if (c && !used.has(c.index) && !pins.has(c.track)) {
      result.push({ track: c.track, clip: c, pinned: false });
      used.add(c.index);
    }
  }
  for (const c of active) {
    if (result.length >= limit) break;
    if (!used.has(c.index) && !pins.has(c.track)) {
      result.push({ track: c.track, clip: c, pinned: false });
      used.add(c.index);
    }
  }
  return result;
}
export class CameraMonitor {
  bufferInfo(t) {
    const v = t.video,
      expected = Math.max(0, t.target - t.previewStart),
      speed = t.sourceSpeed || 1;
    const end = Math.min(
      Number.isFinite(v.duration) ? v.duration : Infinity,
      t.sourceEnd - t.previewStart,
    );
    const remaining = Math.max(0, end - expected);
    return { ahead: bufferedAhead(v.buffered, expected), remaining, speed };
  }
  ready(seconds = 0) {
    return (
      !this.visible ||
      !this.enabled ||
      this.tiles.every(
        (t) =>
          !t.failed &&
          t.video.readyState >= 2 &&
          !t.video.seeking &&
          Math.abs(t.video.currentTime - (t.target - t.previewStart)) < 0.12 &&
          (seconds === 0 ||
            hasBuffer(
              this.bufferInfo(t).ahead,
              this.bufferInfo(t).remaining,
              this.bufferInfo(t).speed,
              seconds,
            )),
      )
    );
  }
  needsBuffer() {
    if (!this.visible || !this.enabled) return false;
    return this.tiles.some((t) => {
      const b = this.bufferInfo(t);
      return (
        t.failed ||
        t.video.readyState < 2 ||
        !hasBuffer(b.ahead, b.remaining, b.speed, BUFFER_FLOOR + 0.25) ||
        t.hardCorrection
      );
    });
  }
  async prepare(time, rate, signal) {
    const deadline = performance.now() + 60000;
    try {
      while (!signal.aborted) {
        this.tick(time, false, rate);
        if (this.ready(rate > 0 ? BUFFER_TARGET : 0)) return;
        const minimum = this.tiles.length
          ? Math.min(
              ...this.tiles.map(
                (t) => this.bufferInfo(t).ahead / this.bufferInfo(t).speed,
              ),
            )
          : 0;
        document.getElementById("playState").textContent =
          `BUFFERING · video ${minimum.toFixed(1)}s / ${BUFFER_TARGET}s`;
        if (this.tiles.some((t) => t.failed))
          throw Error(
            "A selected camera could not decode. Playback stopped to preserve the shared timeline; collapse the monitor to audition audio only.",
          );
        if (performance.now() > deadline)
          throw Error(
            "Camera buffering timed out. Playback stopped rather than let cameras drift; reduce active feeds or collapse the monitor for audio-only review.",
          );
        await new Promise((resolve) => setTimeout(resolve, 50));
      }
      throw new DOMException("Playback cancelled", "AbortError");
    } finally {
      if (!signal.aborted) for (const tile of this.tiles) tile.video.pause();
    }
  }
  constructor(container, getState, onSelect) {
    this.container = container;
    this.getState = getState;
    this.onSelect = onSelect;
    this.pins = new Set();
    this.tiles = [];
    this.maxFeeds = 4;
    this.visible = true;
    this.lastTick = 0;
    this.failures = 0;
    this.generation = 0;
    this.enabled = true;
  }
  release(tile) {
    tile.key = "";
    tile.buffered?.close();
    tile.buffered = null;
    tile.video.pause();
    tile.video.removeAttribute("src");
    tile.video.load();
    tile.el.remove();
  }
  reset() {
    for (const t of this.tiles) this.release(t);
    this.tiles = [];
    this.pins.clear();
  }
  toggle() {
    this.visible = !this.visible;
    document
      .getElementById("monitorPanel")
      .classList.toggle("collapsed", !this.visible);
    if (!this.visible) {
      for (const t of this.tiles) {
        t.buffered?.close();
        t.buffered = null;
        t.video.pause();
        t.video.removeAttribute("src");
        t.video.load();
        t.key = "";
      }
    }
  }
  tick(time, playing, rate = 1) {
    if (!this.visible || !this.enabled) return;
    const state = this.getState(),
      p = state.project;
    if (!p) return;
    if (this.generation !== state.generation) {
      this.reset();
      this.generation = state.generation;
    }
    const feeds = chooseFeeds(p, time, this.pins, this.tiles, this.maxFeeds),
      count = Math.max(1, feeds.length);
    const rows = this.rows || 4,
      cols = this.columns || Math.ceil(count / rows);
    this.container.style.gridTemplateColumns = `repeat(${cols},minmax(0,1fr))`;
    this.container.style.gridTemplateRows = `repeat(${Math.max(rows, Math.ceil(count / cols))},minmax(0,1fr))`;
    // Preserve each active video's element/decoder when another feed ends.
    // Changing the grid order must not reload all the surviving cameras.
    const used = new Set(),
      ordered = feeds.map((f) => {
        const t = this.tiles.find(
          (t) =>
            !used.has(t) &&
            (f.clip
              ? t.clip?.index === f.clip.index
              : t.feed?.track === f.track && !t.clip),
        );
        if (t) used.add(t);
        return t;
      });
    const spare = this.tiles.filter((t) => !used.has(t));
    for (let i = 0; i < ordered.length; i++) {
      if (!ordered[i]) ordered[i] = spare.shift() || this.makeTile();
      ordered[i].el.style.order = i;
    }
    for (const t of spare) this.release(t);
    this.tiles = ordered;
    for (let i = 0; i < feeds.length; i++) {
      const feed = feeds[i],
        tile = this.tiles[i];
      tile.feed = feed;
      tile.clip = feed.clip;
      tile.pin.textContent = feed.pinned ? "Pinned" : "Pin";
      tile.pin.classList.toggle("active", feed.pinned);
      tile.title.textContent = feed.clip
        ? `${p.tracks[feed.track].name} · ${feed.clip.name}`
        : `${p.tracks[feed.track]?.name || "Camera"} · waiting for next recording`;
      const c = feed.clip;
      if (!c) {
        if (tile.key) {
          tile.video.pause();
          tile.video.removeAttribute("src");
          tile.video.load();
          tile.key = "";
        }
        tile.detail.textContent = "Pinned track is outside video bounds";
        tile.overlay.textContent = "NO ACTIVE VIDEO";
        continue;
      }
      const source = clamp(mapTime(c.mapping, time, true), 0, c.duration),
        key = `${state.generation}:${c.index}`;
      if (tile.key !== key) {
        tile.key = key;
        tile.proxy = false;
        tile.failed = false;
        tile.previewStart = 0;
        tile.target = source;
        this.load(tile, source, false);
      }
      tile.target = source;
      tile.sourceEnd = mapTime(c.mapping, videoRange(c)[1], true);
      tile.sourceSpeed = Math.abs(rate) / (c.rate || 1);
      tile.buffered?.ensure(source, tile.sourceSpeed);
      if (
        tile.proxy &&
        (source < tile.previewStart || source >= tile.previewStart + 19.5)
      ) {
        this.load(tile, source, true);
      }
      const expected = source - (tile.proxy ? tile.previewStart : 0),
        v = tile.video;
      // A new seek supersedes an unfinished seek. Waiting for HAVE_CURRENT_DATA
      // or !seeking deadlocks when the old target was evicted while the new
      // target is already buffered. Metadata is enough to move the seek target.
      if (
        (!playing || rate < 0) &&
        v.readyState >= 1 &&
        Math.abs(v.currentTime - expected) > 0.008
      ) {
        v.pause();
        v.currentTime = Math.max(0, expected);
      }
      if (v.readyState >= 2 && !tile.failed) {
        const error = v.currentTime - expected,
          dt =
            mapTime(c.mapping, Math.min(c.duration, source + 0.1)) -
            mapTime(c.mapping, source);
        const modelRate = dt > 0 ? dt / 0.1 : c.rate;
        const desired = clamp(Math.abs(rate) / modelRate, 0.1, 16);
        tile.sourceSpeed = desired;
        if (!playing || rate < 0) {
          tile.hardCorrection = false;
          tile.badClockSince = 0;
          v.pause();
          if (Math.abs(error) > 0.008 && !v.seeking)
            v.currentTime = Math.max(0, expected);
        } else {
          v.playbackRate = correctedRate(desired, error);
          const now = performance.now();
          if (Math.abs(error) > 0.25) {
            tile.badClockSince ||= now;
            tile.hardCorrection = now - tile.badClockSince > 400;
          } else {
            tile.badClockSince = 0;
            tile.hardCorrection = false;
          }
          if (v.paused) v.play().catch(() => {});
        }
        const displayed = Number.isFinite(tile.frameTime)
          ? tile.frameTime
          : v.currentTime;
        const drift = (displayed - expected) * 1000;
        const buffer = this.bufferInfo(tile);
        tile.detail.textContent = `${c.state.toUpperCase()} · source ${formatTime(source)} · frame Δ ${drift.toFixed(0)} ms · buffer ${(buffer.ahead / desired).toFixed(1)}s · ${tile.buffered ? "BUFFERED PREVIEW 640×360" : tile.proxy ? "SOFTWARE PREVIEW 25 fps" : "NATIVE VIDEO"}`;
        const now = performance.now();
        if (playing && Math.abs(drift) > 150) tile.lateSince ||= now;
        else tile.lateSince = 0;
        const late = !!tile.lateSince && now - tile.lateSince > 600;
        tile.el.classList.toggle("late", late);
        tile.overlay.textContent = tile.failed
          ? "PREVIEW ERROR"
          : v.seeking
            ? "SEEKING"
            : late
              ? "CATCHING UP"
              : "";
      } else {
        tile.detail.textContent = `${c.state.toUpperCase()} · ${tile.buffered ? "Buffered preview" : tile.proxy ? "Software preview" : "Native decoder"} · loading`;
        if (!tile.failed) tile.overlay.textContent = "BUFFERING";
      }
    }
    document.getElementById("monitorCount").textContent =
      `${feeds.length}/${this.maxFeeds} feeds · common timeline clock`;
  }
  load(tile, source, proxy) {
    const { generation } = this.getState();
    tile.buffered?.close();
    tile.buffered = null;
    tile.proxy = proxy;
    tile.previewStart = proxy ? Math.max(0, source - 1) : 0;
    tile.frameTime = NaN;
    tile.lateSince = 0;
    tile.badClockSince = 0;
    tile.hardCorrection = false;
    tile.video.pause();
    if (!proxy && BufferedVideo.supported()) {
      const end = mapTime(tile.clip.mapping, videoRange(tile.clip)[1], true);
      tile.buffered = new BufferedVideo(
        tile.video,
        `/media/segment?g=${generation}&clip=${tile.clip.index}`,
        end,
        (e) => {
          tile.failed = true;
          tile.overlay.textContent = "VIDEO PREVIEW UNAVAILABLE";
          tile.detail.textContent = e.message || String(e);
        },
      );
      tile.buffered.ensure(source);
      return;
    }
    tile.failed = true;
    tile.overlay.textContent = "BUFFERED PREVIEW UNAVAILABLE";
    tile.detail.textContent =
      "Update WebView2 to enable MediaSource H.264 playback.";
  }

  makeTile() {
    const el = document.createElement("article");
    el.className = "cameraTile";
    const video = document.createElement("video");
    video.muted = true;
    video.playsInline = true;
    video.preload = "auto";
    const title = document.createElement("div");
    title.className = "cameraTitle";
    const detail = document.createElement("div");
    detail.className = "cameraDetail";
    const overlay = document.createElement("div");
    overlay.className = "cameraOverlay";
    const pin = document.createElement("button");
    pin.className = "pin";
    pin.title =
      "Pin this track; keep its slot while waiting for its next recording";
    el.append(video, title, detail, overlay, pin);
    this.container.append(el);
    const tile = {
      el,
      video,
      title,
      detail,
      overlay,
      pin,
      key: "",
      clip: null,
      frameTime: NaN,
      target: 0,
    };
    pin.onclick = () => {
      if (!tile.feed) return;
      const track = tile.feed.track;
      if (this.pins.has(track)) this.pins.delete(track);
      else if (this.pins.size < this.maxFeeds) this.pins.add(track);
    };
    el.ondblclick = () => tile.clip && this.onSelect(tile.clip);
    video.onloadedmetadata = () => {
      video.currentTime = Math.max(0, tile.target - tile.previewStart);
    };
    video.onerror = () => {
      if (!tile.key || !video.error) return;
      if (tile.buffered) {
        tile.buffered.fail(
          Error(video.error.message || "Buffered video decoding failed"),
        );
        tile.failed = true;
        tile.overlay.textContent = "VIDEO PREVIEW UNAVAILABLE";
        tile.detail.textContent =
          video.error?.message || "Buffered video decoding failed";
        return;
      }
      tile.failed = true;
      tile.overlay.textContent = "VIDEO PREVIEW UNAVAILABLE";
      this.failures++;
    };
    if (video.requestVideoFrameCallback) {
      const frame = (_, meta) => {
        tile.frameTime = meta.mediaTime;
        if (el.isConnected) video.requestVideoFrameCallback(frame);
      };
      video.requestVideoFrameCallback(frame);
    }
    return tile;
  }
}
