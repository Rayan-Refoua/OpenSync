const descriptions = {
  syncMode: [
    "Choose whether recording order and timestamps can assist acoustic matching and placement.",
    "Use Clips / takes for a music video with repeated performances. Choose Chronological event for a continuous concert split into camera files. Chronology can recover missing placements during a continuous event, but can put repeated takes in misleading positions.",
  ],
  fileTimePlacement: [
    "In Chronological event mode, calibrated file dates and recording order can guide the audio search and provide provisional placements. Eligible estimates can be refined using audio.",
    "Enable it when a camera recorded sequentially and its clock was consistently offset. Disable it when copied files share rewritten dates or the recorder clock changed during the event. Review magenta placements before export.",
  ],
  recovery: [
    "Controls how broadly OpenSync searches after its initial acoustic passes leave clips unresolved.",
    "Try Deep for faint scratch audio against several long references. Quick reduces runtime when the initial results are already good. Off skips distributed recovery and can miss weak matches. Changing search depth leaves the verification thresholds in place.",
  ],
  timingProfiles: [
    "Controls how many acoustic windows are measured to check a timing model. Both options use the same audio sampling and separate model fitting, selection, and audit regions.",
    "Adaptive starts with a smaller set on strong, long, single-offset relationships. Weak evidence, suspected drift, and competing offsets use the dense schedule.",
  ],
  globalTiming: [
    "Allows a joint clock and offset fit to refine the final timeline when independent audio windows show lower timing error.",
    "Enable for a controlled comparison on overlapping continuous recordings. The reference clock, nonlinear timing, and conflicting evidence are protected by placement guards. Leave off for the established production solver.",
  ],
  readMode: [
    "Automatic can use direct audio reads for supported MP4/MOV files on confirmed SSDs. Buffered uses the standard FFmpeg input path.",
    "Leave Automatic on for SSD camera media. Try Buffered if you suspect a container or storage compatibility issue. Buffered can read more data. Both modes aim to produce the same analysis input.",
  ],
  readers: [
    "Sets the number of concurrent input readers during fingerprint extraction with caching off. Physical disk scheduling still applies.",
    "Try One reader on a busy external drive. Up to two may improve throughput on fast SSDs or separate drives, but can slow shared storage. Automatic measures throughput. This controls speed rather than matching criteria.",
  ],
  longReferenceHours: [
    "Caps the total duration of reference material examined in extra long-reference searches. Zero uses the recovery preset.",
    "Increase the budget if short clips may overlap late sections of several long recordings. Lower it to finish sooner when extra searches rarely help. Material outside the budget may go unexamined.",
  ],
  recoveryReferenceHours: [
    "Caps the sum of reference durations scheduled for distributed acoustic recovery. Earlier acoustic searches use their usual coverage. Zero leaves the preset in control.",
    "For example, one reference hour can cover six ten-minute reference comparisons. Raising the cap can recover more candidates. Deferred pairs are recorded as unsearched.",
  ],
  minWave: [
    "Sets the minimum waveform correlation for verification paths that use waveform evidence.",
    "Try 0.15 instead of 0.10 if unrelated recordings are getting weak matches. Lowering it can help clips recorded through different microphones, but makes coincidental peaks easier to admit. Compare known positives and negatives before keeping the change.",
  ],
  minFlux: [
    "Sets the minimum agreement in changes to spectral energy. Those changes help align events whose waveforms differ across microphones.",
    "Try 0.30 instead of 0.25 if noisy or repetitive material passes too easily. A higher minimum can also reject real distant recordings. A lower value is more forgiving and raises the risk of coincidences.",
  ],
  controlRatio: [
    "Requires the proposed alignment to beat wrong-offset comparisons by a minimum ratio.",
    "Raise 1.5 to 2.0 when repeated beats produce similar scores at many offsets. A higher ratio rejects more ambiguous candidates, including genuine overlaps with few distinctive sounds.",
  ],
  controlMargin: [
    "Requires an absolute correlation lead over wrong-offset comparisons, alongside the ratio check.",
    "Raise 0.035 to 0.05 when tiny score differences are admitting false matches. Lowering the margin can keep faint genuine matches in play, but makes noise-level score differences more influential.",
  ],
  ambiguityRatio: [
    "Marks a pair ambiguous when the runner-up confidence is this fraction of the leading candidate or higher.",
    "At 0.80, a runner-up with 85% of the leading confidence can trigger ambiguity. At 0.90, that comparison alone would no longer trigger it. Lower values flag more close contests and can leave more clips unresolved.",
  ],
  eventToleranceMS: [
    "Controls how close alternative timeline positions must be to count as the same event during hypothesis comparison.",
    "Reduce 150 ms to 75 ms to keep closely spaced events distinct. Increase it when echoes or distant microphones produce several nearby offsets for the same event. Too large a tolerance can merge different events.",
  ],
  trackLayout: [
    "Controls how Premiere tracks group sources. Overlapping clips can still require extra tracks.",
    "Use Folder and filename family when several camera naming patterns share one folder. Use Folder only when each recorder has its own folder and you want fewer tracks. This changes layout while preserving solved positions.",
  ],
  maxResidualMS: [
    "Sets the allowable presentation residual for Premiere export. It does not tune acoustic verification.",
    "Try 40 ms when reviewing tighter timing. A stricter budget can make more placements unsuitable for export. A larger budget accepts more presentation error, which can be visible on dialogue or sharp transients.",
  ],
  includeProvisional: [
    "Allows eligible provisional placements to appear in the exported XML for comparison.",
    "Enable it for an editor who wants to inspect file-time estimates against camera scratch audio. Leave it off for delivery when you want to exclude uncertain positions. The project retains their evidence and uncertainty either way.",
  ],
  unmatchedPolicy: [
    "Controls where Premiere XML puts recordings without a supported placement relative to the main group.",
    "Use Separate sequence for a clean edit with unresolved material available for review. Append keeps everything in one sequence, and Omit removes unmatched clips from the XML while retaining them in the project. Dedicated local tracks can keep smaller groups together.",
  ],
  audioEarlyMS: [
    "Sets how far audio may lead video before eligible audio-only clock drift can trigger correction.",
    "Reduce 50 ms to 30 ms if lip sync is visibly early. A tighter tolerance can generate more corrected WAVs. A wider tolerance can leave audible drift in the export.",
  ],
  audioLateMS: [
    "Sets how far audio may lag video before eligible audio-only clock drift can trigger correction.",
    "Reduce 70 ms to 50 ms for a tighter dialogue edit. Increase it if slight lateness is acceptable and you want fewer corrected WAVs. Corrections preserve the original recording and video speed.",
  ],
  memoryMiB: [
    "Controls managed analysis and visualization memory budgets. Browser, GPU, and decoder memory also contribute to total usage.",
    "Try 4096 MiB on a workstation with RAM to spare if long jobs repeatedly need audio windows. Lower it when editing software is open. Too low can cause extra reads, while too high can make Windows page memory to disk.",
  ],
  workers: [
    "Caps CPU concurrency for engine work. Disk reads have their own limits.",
    "Try 8 workers while editing on a 16-thread machine. Increase it when other apps are idle. Too many workers can compete for memory bandwidth or make the workstation less responsive.",
  ],
  background: [
    "Controls how aggressively the workstation prepares visualization data away from the foreground.",
    "Choose Foreground only when an HDD is busy. Balanced can make later navigation faster by doing more work upfront, at the cost of CPU and disk contention. This does not change synchronization decisions.",
  ],
  decodeWorkers: [
    "Limits concurrency in media decode pools.",
    "Try two workers on a shared HDD. Increase the limit on a fast SSD if multiple cameras buffer slowly and there is CPU headroom. More decoders can worsen seeks on slow disks.",
  ],
  thumbnailWorkers: [
    "Limits simultaneous thumbnail jobs.",
    "Use one on HDD media to reduce competing seeks. Increase it when thumbnails lag on an SSD and CPU usage is low. This changes thumbnail responsiveness without changing alignment.",
  ],
  maxVideoFeeds: [
    "Limits how many active cameras the monitor can show and buffer at once.",
    "Use two feeds for a slow disk or four for a multicamera review. Raising the limit helps compare more angles, but can increase decode load and buffering delays.",
  ],
  autoAudioFallback: [
    "When neither selected audition track has audio at the playhead, temporarily play up to two other tracks. Your selection resumes automatically as soon as either selected track has audio.",
    "Enable for continuous listening across camera or recorder gaps. Leave off to hear those gaps as silence when inspecting specific tracks. Turning every audition track off always mutes playback.",
  ],
  monitorPosition: [
    "Moves the multicamera monitor without changing the timeline or synchronization.",
    "Choose Right of timeline for a tall portrait grid. Choose Above timeline when you want a full-width timeline. The tradeoff is horizontal versus vertical editing space.",
  ],
  monitorRows: [
    "Sets the planned number of rows in the camera monitor. The grid can add room for active feeds.",
    "Use four rows for a tall side panel, or two rows for larger images in a wide monitor. More rows can make individual previews smaller.",
  ],
  monitorColumns: [
    "Sets the camera monitor column count, or lets the grid choose based on active feeds and rows.",
    "Use one column for a narrow side panel or two for four feeds in a wide panel. Auto allows the grid to grow as feeds become active. Layout does not change sync timing.",
  ],
};

// Key option guidance by the actual values saved by the form.
const optionGuides = {
  syncMode: {
    clips: "Use for music videos, repeated takes, or independent sessions. Keeps recording order from driving placement when the same sound occurs in several takes.",
    chronological: "Use for a continuous event split across camera files. With file-time placement enabled, reliable recorder chronology can guide recovery and estimate gaps. Review provisional placements when files were copied or clocks reset.",
  },
  recovery: {
    balanced: "Start here for normal production jobs with mixed clip lengths and scratch audio quality.",
    quick: "Use for a fast pass when references are clear and extra searching is unlikely to help. Some weak overlaps may remain unresolved.",
    deep: "Use when short or distant clips remain unresolved and several references could contain the same event. Expect more processing time. Verification criteria still apply.",
    forensic: "Use when coverage matters more than runtime. Searches every eligible reference for unresolved short clips and includes occupied offsets. The explicit project cap still applies.",
    off: "Use to isolate the standard matching passes or skip distributed recovery on well-recorded material. This can miss weak matches that need a broader search.",
  },
  timingProfiles: {
    auto: "Use routinely. Automatic uses dense timing for small workloads and tries sparse timing on expensive workloads only where independent holdout evidence already supports the offset.",
    dense: "Use for production, critical reviews, and projects with intermittent timing changes. Measures the complete set of timing windows.",
    adaptive: "Try on long, continuous recordings with clear shared audio. A sparse pass can miss a brief timing change between sampled windows. Use Dense if that risk matters.",
  },
  readMode: {
    auto: "Use routinely. Compatible MP4/MOV files on confirmed SSDs can use direct audio reads, with the standard input path available for other sources.",
    buffered: "Use to diagnose a container or storage issue or compare with the regular FFmpeg path. It can increase disk traffic.",
  },
  readers: {
    auto: "Use for mixed storage or when you do not know which approach is faster. OpenSync measures throughput and adapts.",
    serial: "Use if simultaneous reads slow down a busy external drive. One reader can underuse a fast SSD.",
    parallel: "Try on fast SSDs or independent drives when reading is the bottleneck. More concurrent reads can hurt throughput on shared storage. Physical disk scheduling still applies.",
  },
  trackLayout: {
    family: "Use when one folder mixes several recorder filename patterns. Preserves recognizable camera families on separate tracks.",
    folder: "Use when the folders already identify the recorders. Filenames within each folder can share tracks where clips do not overlap.",
  },
  unmatchedPolicy: {
    append: "Use for a single Premiere sequence with every unresolved recording parked after the synced material.",
    "local-tracks": "Use when smaller groups are internally synchronized and you want to inspect them together on dedicated tracks. Their position relative to the main group remains uncertain.",
    "separate-sequence": "Use for a clean editing sequence plus a separate sequence for unresolved sources.",
    order_only: "Use when recorder succession matters during review. Preserves available recording order without estimating gaps.",
    disabled: "Use to keep unmatched clips in the delivered sequence while preventing them from playing until an editor enables them.",
    remove: "Use when the deliverable should contain only included placements. Unmatched recordings stay in the OpenSync project.",
  },
  background: {
    off: "Use while syncing, working from a busy HDD, or sharing CPU with an editor. Background visualization work is minimized.",
    low: "Use for light waveform and thumbnail preparation when storage or CPU is shared.",
    balanced: "Use when you have spare I/O and CPU and want more visualization work ready before you navigate. Background jobs can compete with other apps.",
  },
  monitorPosition: {
    right: "Use on a wide display or for a tall stack of camera tiles. Leaves less horizontal room for the timeline.",
    top: "Use for a full-width timeline or a wide camera grid. Leaves less vertical room for timeline tracks.",
  },
  monitorColumns: {
    "0": "Use when the number of active cameras changes often. The grid adds columns according to the row count and active feeds.",
    "1": "Use for a narrow side panel or stacked portrait feeds.",
    "2": "Use for a two-by-two view when comparing four feeds with two rows.",
    "3": "Use for six feeds in two rows on a wide display. Tiles get narrower.",
    "4": "Use on a wide monitor when comparing many angles. Smaller tiles make detail harder to judge.",
  },
};

const numericGuides = {
  longReferenceHours: ["Search more long-reference material when missed overlaps may be far from the start.", "Finish sooner by limiting how much reference audio the extra searches can examine. Zero restores the preset."],
  minWave: ["Require clearer waveform agreement when weak false matches pass.", "Give quiet or mismatched microphones more latitude. Coincidental peaks can pass more easily."],
  minFlux: ["Require stronger agreement in spectral changes when repetitive or noisy material passes too easily.", "Allow more variation across microphones. This can help distant recordings and admit more coincidences."],
  controlRatio: ["Demand a larger relative lead over wrong offsets when beats or repeated phrases produce several close peaks.", "Allow modest relative leads when shared sound is faint. Compare against known false matches."],
  controlMargin: ["Require a larger absolute score gap so tiny differences carry less weight.", "Keep weak candidates with smaller score gaps eligible. Those gaps can be sensitive to noise."],
  ambiguityRatio: ["Flag fewer contests. This can recover placements when the best offset is persuasive, but can hide a plausible alternative.", "Flag more contests for review and expect more uncertain or unresolved placements."],
  eventToleranceMS: ["Group more nearby hypotheses as the same event. Useful for echoes or distant microphones, with a risk of combining distinct events.", "Keep close events separate. One real event may produce several competing hypotheses."],
  maxResidualMS: ["Permit more presentation error in the XML when compatibility or coverage matters.", "Apply a tighter presentation budget for dialogue or sharp transients. More placements may fall outside the budget."],
  audioEarlyMS: ["Tolerate more audio lead before generating corrected recorder audio.", "Correct smaller audio leads. This can produce more corrected WAVs."],
  audioLateMS: ["Tolerate more audio lag before generating corrected recorder audio.", "Correct smaller audio lags. This can produce more corrected WAVs."],
  memoryMiB: ["Retain more working data and potentially avoid rereads. Leave RAM for Windows and other apps.", "Reduce managed memory use when editing software is open. More data may have to be read again."],
  workers: ["Use more CPU parallelism when the job is compute-bound and the machine has headroom.", "Leave CPU capacity for other apps or reduce contention when throughput stops improving."],
  decodeWorkers: ["Prepare more media in parallel when storage and CPU have headroom.", "Reduce competing disk reads and CPU load, especially on shared HDDs."],
  thumbnailWorkers: ["Generate more thumbnails in parallel on fast storage.", "Reduce seek pressure while playing or reading footage from an HDD."],
  maxVideoFeeds: ["Compare more angles at once, with more decoding and buffering load.", "Reduce decoder demand when reviewing a few angles or playing from slow storage."],
  monitorRows: ["Stack more tiles in a tall panel, with less height per tile.", "Use larger rows in a wide monitor. The layout can expand to accommodate active feeds."],
};

export function addSettingHelp(label, control, name = label.textContent.trim(), help = descriptions[control.name || control.id]) {
  if (!help) return;
  const row = document.createElement("div");
  row.className = "settingWithHelp";
  label.before(row);
  row.append(label);
  const button = document.createElement("button");
  button.type = "button";
  button.className = "settingHelpButton";
  button.setAttribute("aria-label", `About ${name}`);
  button.setAttribute("aria-haspopup", "dialog");
  button.title = `About ${name}`;
  button.innerHTML = '<svg viewBox="0 0 24 24" width="16" height="16" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M12 11v5m0-8h.01"/></svg>';
  button.onclick = () => {
    const dialog = document.getElementById("settingHelpDialog");
    document.getElementById("settingHelpTitle").textContent = name;
    document.getElementById("settingHelpDescription").textContent = help[0];
    const key = control.name || control.id;
    const choices = optionGuides[key];
    const directions = numericGuides[key];
    const entries = choices
      ? Array.from(control.options, (option) => [option.textContent.trim(), choices[option.value]])
      : [
          ...(directions ? [["Raise it", directions[0]], ["Lower it", directions[1]]] : []),
          ["Example", help[1]],
        ];
    document.getElementById("settingHelpGuide").textContent = choices ? "When to use each option" : "How to use it";
    const list = document.getElementById("settingHelpList");
    list.replaceChildren(...entries.filter(([, detail]) => detail).map(([caption, detail]) => {
      const item = document.createElement("li");
      const heading = document.createElement("strong");
      heading.textContent = caption;
      item.append(heading, document.createTextNode(detail));
      return item;
    }));
    dialog.showModal();
  };
  row.append(button);
}
