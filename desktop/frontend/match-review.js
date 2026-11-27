// Audition a bounded overlap, independently of the main timeline transport.
export function setupMatchReview(h) {
  const $ = (id) => document.getElementById(id);
  const dialog = $("matchReviewDialog"), list = $("matchCandidates");
  const position = $("matchPosition"), windowLength = $("matchDuration"), status = $("matchReviewStatus");
  const canvas = $("matchWaveforms"), plot = canvas.getContext("2d");
  const plotFrame = $("matchPlot");
  const videos = [$("matchVideoA"), $("matchVideoB")];
  const videoStates = [$("matchVideoStateA"), $("matchVideoStateB")];
  const previews = [null, null];
  let candidates = [], active, request, context, voices = [], audio, start, end;
  let listening = 0, lastSolo = 0, frame = 0, playing = false, saving = false;

  const sectionsByCandidate = new Map();
  const decisionFor = (id) => h.state.project?.graph?.manual_candidate_decisions?.[id];
  const reviewedLabels = {accepted: "Accepted placement", confirmed: "Manually confirmed", rejected: "Rejected"};
  windowLength.value = localStorage.getItem("matchReviewWindow") || "4";
  if (!windowLength.value) windowLength.value = "4";
  const length = () => Number(windowLength.value);
  const sectionKey = () => `${active.id}/${length()}`;

  function updateShuffle() {
    const remaining = sectionsByCandidate.get(sectionKey()).remaining.length;
    const shuffle = $("matchShuffle");
    shuffle.disabled = remaining === 0;
    shuffle.firstChild.textContent = remaining ? `Shuffle section (${remaining}) ` : "All sections visited ";
  }

  function setSections() {
    const key = sectionKey();
    position.min = start;
    position.max = Math.max(start, end - length());
    if (!sectionsByCandidate.has(key)) {
      const remaining = [];
      for (let at = start + length(); at < end; at += length()) {
        const section = Math.min(at, Number(position.max));
        if (section > start + 1e-6 && section - (remaining.at(-1) ?? start) > 1e-6) remaining.push(section);
      }
      for (let i = remaining.length - 1; i > 0; i--) {
        const j = Math.floor(Math.random() * (i + 1));
        [remaining[i], remaining[j]] = [remaining[j], remaining[i]];
      }
      sectionsByCandidate.set(key, {at: start, remaining});
    }
    position.value = sectionsByCandidate.get(key).at;
    updateShuffle();
  }

  function audioMode(mode) {
    listening = mode;
    if (mode !== 2) lastSolo = mode;
    $("matchAB").firstChild.textContent = mode === 2 ? "Solo A/B " : `Audio ${mode ? "B" : "A"} `;
    $("matchBoth").setAttribute("aria-pressed", String(mode === 2));
    voices.forEach(({gain}, i) => gain.gain.setTargetAtTime(mode === 2 ? .5 : +(mode === i), context.currentTime, .008));
  }

  function stop() {
    playing = false;
    cancelAnimationFrame(frame);
    for (const {voice} of voices) {
      try { voice.stop(); } catch {}
    }
    for (const video of videos) video.pause();
    voices = [];
    $("matchPlay").firstChild.textContent = "Play window ";
    plotFrame.style.setProperty("--match-playhead", "-1%");
  }
  function clearVideo(i) {
    const video = videos[i];
    video.onloadedmetadata = video.onloadeddata = video.onerror = null;
    video.removeAttribute("src");
    video.load();
    if (previews[i]) URL.revokeObjectURL(previews[i].url);
    previews[i] = null;
  }
  async function loadVideo(job, lane, clip, at, generation, duration) {
    clearVideo(lane);
    const video = videos[lane], label = videoStates[lane];
    const c = h.state.project.clips[clip];
    label.textContent = c.missing ? "Source offline" : c.video ? "Preparing picture…" : "Audio only";
    if (!c.video || c.missing) return;
    try {
      const query = new URLSearchParams({g: generation, clip, start: at, reviewDuration: duration});
      const response = await fetch(`/media/segment?${query}`, {signal: job.signal});
      if (!response.ok) throw Error(await response.text());
      const origin = Number(response.headers.get("X-OpenSync-Segment-Origin"));
      const blob = await response.blob();
      if (job.signal.aborted) return;
      const url = URL.createObjectURL(blob);
      previews[lane] = { url, start: Math.max(0, at - origin) };
      video.onloadedmetadata = () => { video.currentTime = previews[lane].start; };
      video.onloadeddata = () => { label.textContent = ""; };
      video.onerror = () => { label.textContent = "Picture unavailable · audio still works"; };
      video.src = url;
    } catch (error) {
      if (!job.signal.aborted) label.textContent = "Picture unavailable · audio still works";
    }
  }
  function available() {
    const project = h.state.project;
    const decisions = project?.graph?.manual_candidate_decisions || {};
    return (project?.manualCandidates || []).filter((c) => !c.auditOnly && !decisions[c.id]);
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
    videos.forEach((_, i) => { clearVideo(i); videoStates[i].textContent = "Waiting for audio…"; });
    $("matchPlay").disabled = true;
    draw();
    if (!active) return;
    const at = Number(position.value), duration = Math.min(length(), end - at);
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
      $("matchPlay").disabled = false;
      draw();
      status.textContent = "Ready to audition";
      options.forEach(([clip, source], lane) => void loadVideo(job, lane, clip, source, generation, duration));
    } catch (error) {
      if (!job.signal.aborted) status.textContent = error.message;
    }
  }
  function choose(id) {
    if (saving) return;
    active = candidates.find((candidate) => candidate.id === id);
    if (!active) {
      $("matchAccept").disabled = true;
      $("matchRejectNext").disabled = true;
      status.textContent = "No remaining candidates.";
      return;
    }
    const { reference, query, shift } = active;
    for (const card of list.children) {
      if (card.dataset.candidate === id) card.setAttribute("aria-current", "true");
      else card.removeAttribute("aria-current");
    }
    for (let i = 0; i < 2; i++) {
      clearVideo(i);
      videoStates[i].textContent = "";
    }
    const clips = h.state.project.clips, r = clips[reference], q = clips[query];
    start = Math.max(0, -shift);
    end = Math.min(q.duration, r.duration - shift);
    setSections();
    $("matchReference").textContent = `Reference · ${r.name}`;
    $("matchQuery").textContent = `Candidate · ${q.name}`;
    for (const [clip, id, label] of [[r, "matchMetaA", "A · Reference"], [q, "matchMetaB", "B · Candidate"]]) {
      const video = clip.metadata?.video;
      $(id).textContent = `${label}  /  ${clip.name}  ·  ${clip.duration.toFixed(2)}s${video?.width ? `  ·  ${video.width}×${video.height}  ·  ${video.codec_name || "video"}` : ""}`;
      $(id).title = clip.source;
    }
    const accept = $("matchAccept"), shortcut = document.createElement("kbd");
    shortcut.textContent = "F";
    const decision = decisionFor(id);
    accept.replaceChildren(!active.canAccept ? "Confirm match & next " :
      r.state === "unresolved" ? "Accept within group & next " : "Accept placement & next ", shortcut);
    accept.disabled = !!decision || active.auditOnly || r.missing || q.missing;
    $("matchAccept").title = !active.canAccept
      ? "Save human confirmation in project history. The existing placement stays in place."
      : r.state === "unresolved" ? "Save relative timing in a group separate from the main timeline." : "Place the query at this offset in a new revision";
    $("matchRejectNext").disabled = !!decision || active.auditOnly;
    if (active.auditOnly) {
      accept.textContent = "Verified placement · audition only";
      accept.title = "Review the chronology-guided acoustic match";
    }
    void load();
  }
  function open(index = h.state.selected, preferredID) {
    h.stop();
    candidates = h.state.project?.manualCandidates || [];
    if (!candidates.length) {
      h.message("No saved match candidates to review");
      return;
    }
    const clips = h.state.project.clips;
    const pending = available();
    $("verifyReviewMatches").disabled = !pending.some((c) => c.canAccept);
    $("matchQueueCount").textContent = `${pending.length} to review · ${candidates.filter((c) => c.auditOnly).length} chronology-verified`;
    $("matchUndo").disabled = !h.state.project.graph?.solution_revision;
    list.replaceChildren(...candidates.map((c) => {
      const reference = clips[c.reference];
      const position = reference.state === "unresolved" ? "Local group" : `Timeline ≈ ${(reference.start + c.shift * reference.rate).toFixed(3)}s`;
      const card = document.createElement("button");
      card.className = "matchCard";
      card.dataset.candidate = c.id;
      card.dataset.evidence = c.status;
      const decision = decisionFor(c.id);
      if (decision) card.dataset.reviewed = decision;
      for (const [tag, value] of [
        ["strong", clips[c.query].name], ["span", `↳ ${reference.name}`],
        ["small", `${position}  ·  Δ ${c.shift.toFixed(3)}s`],
        ["small", c.auditOnly ? "Chronology + acoustics verified" : c.status.replaceAll("_", " ")],
        ...(decision ? [["small", h.state.project.graph?.automatic_waveform_matches?.[c.id] ? "✓ Accepted by waveform" : `✓ ${reviewedLabels[decision]}`]] : []),
      ]) {
        const line = document.createElement(tag);
        line.textContent = value;
        card.append(line);
      }
      card.onclick = () => choose(c.id);
      return card;
    }));
    const selected = candidates.find((c) => c.id === preferredID) || pending.find((c) => c.query === index) || pending[0] || candidates[0];
    if (!dialog.open) dialog.showModal();
    choose(selected.id);
    list.querySelector('[aria-current="true"]')?.focus();
  }
  $("openMatchReview").onclick = () => open();
  $("closeMatchReview").onclick = () => dialog.close();
  dialog.addEventListener("close", () => { request?.abort(); stop(); videos.forEach((_, i) => clearVideo(i)); });
  position.onchange = () => {
    const sections = sectionsByCandidate.get(sectionKey());
    sections.at = Number(position.value);
    sections.remaining = sections.remaining.filter(at => Math.abs(at - sections.at) >= length() / 2);
    updateShuffle();
    void load();
  };
  windowLength.onchange = () => {
    localStorage.setItem("matchReviewWindow", windowLength.value);
    setSections();
    void load();
  };
  $("matchShuffle").onclick = () => {
    const sections = sectionsByCandidate.get(sectionKey());
    sections.at = sections.remaining.pop();
    position.value = sections.at;
    updateShuffle();
    void load();
    list.querySelector('[aria-current="true"]')?.focus();
  };
  $("matchPlay").onclick = async () => {
    if (playing) { stop(); return; }
    if (!audio) return;
    const bufferSet = audio;
    await context.resume();
    if (!dialog.open || bufferSet !== audio || playing) return;
    const when = context.currentTime + .04;
    playing = true;
    $("matchPlay").firstChild.textContent = "Stop audition ";
    for (let i = 0; i < 2; i++) {
      const voice = context.createBufferSource(), gain = context.createGain();
      voice.buffer = audio[i];
      gain.gain.value = listening === 2 ? .5 : +(listening === i);
      voice.connect(gain).connect(context.destination);
      voice.start(when);
      voices.push({voice, gain});
    }
    audioMode(listening);
    function tick() {
      if (!playing) return;
      const elapsed = context.currentTime - when;
      if (elapsed >= audio[0].duration) { stop(); return; }
      if (elapsed >= 0) {
        plotFrame.style.setProperty("--match-playhead", `${100 * elapsed / audio[0].duration}%`);
        videos.forEach((video, i) => {
          if (!previews[i] || video.readyState < 2) return;
          const expected = previews[i].start + elapsed;
          if (expected >= video.duration) { video.pause(); return; }
          if (Math.abs(video.currentTime - expected) > .12 && !video.seeking) video.currentTime = expected;
          if (video.paused && !video.seeking) video.play().catch(() => {});
        });
      }
      frame = requestAnimationFrame(tick);
    }
    frame = requestAnimationFrame(tick);
  };
  $("matchStop").onclick = stop;
  $("matchAB").onclick = () => audioMode(lastSolo === 0 ? 1 : 0);
  $("matchBoth").onclick = () => audioMode(listening === 2 ? lastSolo : 2);
  dialog.addEventListener("keydown", (event) => {
    const key = event.key.toLowerCase();
    if (event.ctrlKey && key === "z" && !event.shiftKey && !event.repeat) {
      event.preventDefault();
      event.stopPropagation();
      $("matchUndo").click();
      return;
    }
    if (event.ctrlKey || event.altKey || event.metaKey || event.shiftKey || event.repeat || saving || event.target === windowLength) return;
    const buttons = {" ": "matchPlay", s: "matchAB", w: "matchBoth", e: "matchShuffle", f: "matchAccept", x: "matchRejectNext"};
    if (buttons[key]) {
      event.preventDefault();
      event.stopPropagation();
      $(buttons[key]).click();
    } else if (["a","d","arrowup","arrowdown"].includes(key) && !(event.target === position && key.startsWith("arrow"))) {
      event.preventDefault();
      const index = candidates.indexOf(active) + (["a","arrowup"].includes(key) ? -1 : 1);
      if (candidates[index]) {
        choose(candidates[index].id);
        list.children[index]?.focus();
      }
    }
  });
  async function decide(kind) {
    if (!active || saving) return;
    const candidate = active;
    const oldIndex = candidates.indexOf(candidate);
    const priorPending = new Set(available().map(c => c.id));
    request?.abort();
    stop();
    saving = true;
    list.inert = true;
    for (const id of ["matchAccept", "matchUndo", "matchRejectNext"]) $(id).disabled = true;
    try {
      const p = kind === "undo" ? await h.api().UndoReview() : await h.api().ReviewSolution({kind, candidate: candidate.id});
      h.setProject(p, false);
      const clip = h.state.project.clips[candidate.query];
      if (clip) { h.inspect(clip); h.pan(clip.start); }
      const remaining = available();
      saving = false;
      list.inert = false;
      if (dialog.open) {
        const byID = new Map(remaining.map(c => [c.id, c]));
        const previousOrder = [...candidates.slice(oldIndex + 1), ...candidates.slice(0, oldIndex)];
        const restored = kind === "undo" && remaining.find(c => !priorPending.has(c.id));
        const preferred = restored || remaining.find(c => c.query === candidate.query)
          || previousOrder.map(c => byID.get(c.id)).find(Boolean) || remaining[0];
        open(candidate.query, preferred?.id || candidate.id);
      }
    } catch (error) {
      saving = false;
      $("matchUndo").disabled = !h.state.project.graph?.solution_revision;
      list.inert = false;
      choose(candidate.id);
      list.querySelector('[aria-current="true"]')?.focus();
      status.textContent = error.message || String(error);
    } finally { saving = false; list.inert = false; }
  }
  $("verifyReviewMatches").onclick = async () => {
    if (saving) return;
    request?.abort();
    stop();
    saving = true;
    dialog.close();
    try {
      const result = await h.api().VerifyReviewMatches();
      if (result.project) h.setProject(result.project, false);
      h.message(result.added ? `Placed ${result.added} verified recordings in an undoable revision` : "No pending matches passed automatic waveform verification");
    } catch (error) {
      h.message(String(error.message || error), true);
    } finally {
      saving = false;
      open();
    }
  };
  $("matchAccept").onclick = () => decide("accept_candidate");
  $("matchUndo").onclick = () => decide("undo");
  $("matchRejectNext").onclick = () => decide("reject_candidate");
  return { open, available };
}
