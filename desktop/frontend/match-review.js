// Audition a bounded overlap, independently of the main timeline transport.
export function setupMatchReview(h) {
  const $ = (id) => document.getElementById(id);
  const dialog = $("matchReviewDialog"), list = $("matchCandidates");
  const position = $("matchPosition"), status = $("matchReviewStatus");
  const canvas = $("matchWaveforms"), plot = canvas.getContext("2d");
  let candidates = [], active, request, context, voices = [], audio, start, end;

  function stop() {
    for (const voice of voices) {
      try { voice.stop(); } catch {}
    }
    voices = [];
  }
  function available() {
    const project = h.state.project;
    const decisions = project?.graph?.manual_candidate_decisions || {};
    return (project?.manualCandidates || []).filter((c) => !decisions[c.id]);
  }
  function draw() {
    const width = canvas.width, height = canvas.height;
    plot.fillStyle = "#141a20";
    plot.fillRect(0, 0, width, height);
    for (let lane = 0; lane < 2; lane++) {
      const middle = height * (lane ? .75 : .25);
      plot.strokeStyle = lane ? "#d9a570" : "#7bc8d8";
      plot.beginPath();
      const pcm = audio?.[lane]?.getChannelData(0);
      for (let x = 0; x < width; x++) {
        let lo = 0, hi = 0;
        if (pcm) {
          for (let i = Math.floor(x * pcm.length / width); i < (x + 1) * pcm.length / width; i++) {
            lo = Math.min(lo, pcm[i]);
            hi = Math.max(hi, pcm[i]);
          }
        }
        plot.moveTo(x, middle - hi * height * .21);
        plot.lineTo(x, middle - lo * height * .21 + 1);
      }
      plot.stroke();
    }
  }
  async function load() {
    stop();
    request?.abort();
    const job = new AbortController();
    request = job;
    audio = null;
    $("matchAccept").disabled = true;
    draw();
    if (!active) return;
    const at = Number(position.value), duration = Math.min(4, end - at);
    $("matchWindow").textContent = `Query ${at.toFixed(3)}s  ·  Reference ${(at + active.shift).toFixed(3)}s  ·  ${duration.toFixed(2)}s window`;
    status.textContent = "Reading audio…";
    try {
      context ??= new AudioContext({ sampleRate: 48000 });
      const generation = h.state.generation;
      const options = [[active.reference, Math.max(0, at + active.shift)], [active.query, at]];
      const result = await Promise.all(options.map(async ([clip, start]) => {
        const query = new URLSearchParams({g: generation, clip, start, duration});
        const response = await fetch(`/media/review-audio?${query}`, {signal: job.signal});
        if (!response.ok) throw Error(await response.text());
        const pcm = new Float32Array(await response.arrayBuffer());
        const buffer = context.createBuffer(2, pcm.length / 2, 48000);
        for (let channel = 0; channel < 2; channel++) {
          const output = buffer.getChannelData(channel);
          for (let i = 0; i < output.length; i++) output[i] = pcm[i * 2 + channel];
        }
        return buffer;
      }));
      if (request !== job || job.signal.aborted) return;
      audio = result;
      const { reference, query } = active;
      const r = h.state.project.clips[reference], q = h.state.project.clips[query];
      $("matchAccept").disabled = !active.canAccept || r.missing || q.missing;
      draw();
      status.textContent = "Ready to audition";
    } catch (error) {
      if (!job.signal.aborted) status.textContent = error.message;
    }
  }
  function choose() {
    active = candidates.find((candidate) => candidate.id === list.value);
    if (!active) {
      $("matchAccept").disabled = true;
      $("matchReject").disabled = $("matchRejectNext").disabled = true;
      status.textContent = "No remaining candidates.";
      return;
    }
    const { reference, query, shift } = active;
    const clips = h.state.project.clips, r = clips[reference], q = clips[query];
    start = Math.max(0, -shift);
    end = Math.min(q.duration, r.duration - shift);
    position.min = start;
    position.max = Math.max(start, end - 4);
    position.value = start;
    $("matchReference").textContent = `Reference · ${r.name}`;
    $("matchQuery").textContent = `Candidate · ${q.name}`;
    $("matchAccept").textContent = r.state === "unresolved" ? "Accept within group" : "Accept placement";
    $("matchAccept").title = !active.canAccept
      ? "This candidate can be auditioned. Accepting it would change an existing recording group or depend on a provisional reference."
      : r.state === "unresolved" ? "Save relative timing in a group separate from the main timeline." : "Place the query at this offset in a new revision";
    $("matchReject").disabled = $("matchRejectNext").disabled = false;
    void load();
  }
  function open(index = h.state.selected) {
    h.stop();
    candidates = available();
    if (!candidates.length) {
      h.message("No saved match candidates to review");
      return;
    }
    const clips = h.state.project.clips;
    list.replaceChildren(...candidates.map((c) => {
      const reference = clips[c.reference];
      const position = reference.state === "unresolved" ? "reference only" : `timeline ~${(reference.start + c.shift * reference.rate).toFixed(3)}s`;
      return new Option(`${clips[c.query].name} ↔ ${reference.name} · ${position} · shift ${c.shift.toFixed(3)}s · ${c.status.replaceAll("_", " ")}`, c.id);
    }));
    const selected = candidates.find((c) => c.query === index);
    if (selected) list.value = selected.id;
    dialog.showModal();
    choose();
  }
  $("openMatchReview").onclick = () => open();
  $("closeMatchReview").onclick = () => dialog.close();
  dialog.addEventListener("close", () => { request?.abort(); stop(); });
  list.onchange = choose;
  position.onchange = load;
  $("matchPlay").onclick = async () => {
    if (!audio) return;
    stop();
    await context.resume();
    if (!dialog.open || !audio) return;
    const mode = $("matchListen").value, when = context.currentTime + .04;
    for (let i = 0; i < 2; i++) {
      if (mode !== "both" && mode !== String(i)) continue;
      const voice = context.createBufferSource(), gain = context.createGain();
      voice.buffer = audio[i];
      gain.gain.value = mode === "both" ? .5 : 1;
      voice.connect(gain).connect(context.destination);
      voice.start(when);
      voices.push(voice);
    }
  };
  $("matchStop").onclick = stop;
  async function decide(kind, next) {
    if (!active) return;
    const candidate = active;
    const oldIndex = candidates.indexOf(candidate);
    request?.abort();
    stop();
    list.disabled = true;
    for (const id of ["matchAccept", "matchReject", "matchRejectNext"]) $(id).disabled = true;
    try {
      const p = await h.api().ReviewSolution({kind, candidate: candidate.id});
      h.setProject(p, false);
      dialog.close();
      const clip = h.state.project.clips[candidate.query];
      if (clip) { h.inspect(clip); h.pan(clip.start); }
      if (next && available().length) {
        open(candidate.query);
        const preferred = candidates.slice(oldIndex).find((c) => c.query === candidate.query)
          || candidates.find((c) => c.query === candidate.query) || candidates[Math.min(oldIndex, candidates.length - 1)];
        list.value = preferred.id;
        choose();
      }
    } catch (error) {
      choose();
      h.message(String(error), true);
    } finally { list.disabled = false; }
  }
  $("matchAccept").onclick = () => decide("accept_candidate", false);
  $("matchReject").onclick = () => decide("reject_candidate", false);
  $("matchRejectNext").onclick = () => decide("reject_candidate", true);
  return { open, available };
}
