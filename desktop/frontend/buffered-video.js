import { bufferedAhead, BUFFER_TARGET } from "./playback-buffer.js";

// At most one in-flight 12-second segment per feed. Remove old ranges after
// append; the queue targets 12 wall-clock seconds, allowing five seconds of
// disk/decoder jitter without depending on browser preload heuristics.
export class BufferedVideo {
  static supported() {
    return !!globalThis.MediaSource?.isTypeSupported(
      'video/mp4; codecs="avc1.42c01f"',
    );
  }
  constructor(video, url, duration, onError) {
    this.video = video;
    this.url = url;
    this.duration = duration;
    this.onError = onError;
    this.target = 0;
    this.speed = 1;
    this.dead = false;
    this.busy = false;
    this.controller = null;
    this.fetches = 0;
    this.media = new MediaSource();
    this.objectURL = URL.createObjectURL(this.media);
    video.src = this.objectURL;
    this.media.addEventListener(
      "sourceopen",
      () => {
        if (this.dead) return;
        try {
          this.buffer = this.media.addSourceBuffer(
            'video/mp4; codecs="avc1.42c01f"',
          );
          this.media.duration = duration;
          this.ensure(this.target, this.speed);
        } catch (e) {
          this.fail(e);
        }
      },
      { once: true },
    );
  }
  ensure(time, speed = 1) {
    this.target = time;
    this.speed = Math.max(0.1, Math.abs(speed));
    if (this.busy) {
      if (
        this.controller &&
        bufferedAhead(this.video.buffered, time) === 0 &&
        (time < this.pendingStart || time >= this.pendingStart + 12)
      )
        this.controller.abort();
      return;
    }
    if (this.dead || !this.buffer || this.media.readyState !== "open") return;
    this.pump().catch((e) => {
      if (e.name !== "AbortError" && !this.dead) this.fail(e);
    });
  }
  operation(fn) {
    return new Promise((resolve, reject) => {
      const b = this.buffer;
      const end = () => {
          cleanup();
          resolve();
        },
        error = () => {
          cleanup();
          reject(Error("Video segment append failed"));
        };
      const cleanup = () => {
        b.removeEventListener("updateend", end);
        b.removeEventListener("error", error);
        b.removeEventListener("abort", error);
      };
      b.addEventListener("updateend", end, { once: true });
      b.addEventListener("error", error, { once: true });
      b.addEventListener("abort", error, { once: true });
      try {
        fn();
      } catch (e) {
        cleanup();
        reject(e);
      }
    });
  }
  async pump() {
    this.busy = true;
    try {
      const ahead = bufferedAhead(this.video.buffered, this.target),
        remaining = this.duration - this.target;
      if (ahead + 0.05 >= Math.min(remaining, (BUFFER_TARGET + 4) * this.speed))
        return;
      // Aligned segments make seeks and sequential fills share identical ranges.
      const start = Math.floor((this.target + ahead + 0.05) / 12) * 12;
      if (start >= this.duration - 0.025) return;
      this.pendingStart = start;
      this.controller = new AbortController();
      this.fetches++;
      const response = await fetch(this.url + "&start=" + start, {
        signal: this.controller.signal,
      });
      if (!response.ok) throw Error(await response.text());
      const bytes = await response.arrayBuffer();
      if (this.dead) return;
      const originHeader = response.headers.get("X-OpenSync-Segment-Origin"),
        origin = Number(originHeader);
      if (originHeader === null || !Number.isFinite(origin))
        throw Error("Video segment source timestamp missing");
      this.buffer.timestampOffset = origin;
      await this.operation(() => this.buffer.appendBuffer(bytes));
      if (this.dead) return;
      // Keep a little history for frame stepping, bounded forward data for seek.
      const behind = Math.max(0, this.target - 4),
        aheadLimit =
          this.target + 12 + Math.max(12, (BUFFER_TARGET + 4) * this.speed);
      if (
        behind > 0 &&
        this.buffer.buffered.length &&
        this.buffer.buffered.start(0) < behind
      )
        await this.operation(() => this.buffer.remove(0, behind));
      if (
        this.buffer.buffered.length &&
        this.buffer.buffered.end(this.buffer.buffered.length - 1) > aheadLimit
      )
        await this.operation(() =>
          this.buffer.remove(aheadLimit, this.duration),
        );
    } finally {
      this.busy = false;
      this.controller = null;
    }
  }
  fail(e) {
    this.dead = true;
    this.controller?.abort();
    this.onError(e);
  }
  close() {
    this.dead = true;
    this.controller?.abort();
    if (this.buffer?.updating) {
      try {
        this.buffer.abort();
      } catch {}
    }
    this.video.pause();
    this.video.removeAttribute("src");
    this.video.load();
    URL.revokeObjectURL(this.objectURL);
  }
}
