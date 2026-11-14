// Stage progress describes pipeline position, never an invented ETA or work percentage.
export const stages = [
  "Reading media metadata",
  "Streaming audio and extracting fingerprints",
  "Discovering candidate pairs",
  "Verifying candidate pairs",
  "Searching unresolved recordings",
  "Verifying recovery candidates",
  "Evaluating timing models",
  "Distributed acoustic search",
  "Refining timing models",
  "Building project and Premiere XML",
];
export function placementLabel(c) {
  return (
    {
      supported: "Acoustically supported",
      validated: "Validated placement",
      provisional: "Provisional placement",
      unresolved: "Unresolved placement",
    }[c.state] || "Unclassified placement"
  );
}
export function currentReport(s, project) {
  if (s.running) {
    const i = stages.indexOf(s.stage),
      counts =
        s.stageTotal > 0 ? ` · ${s.stageCompleted}/${s.stageTotal} pairs` : "";
    return {
      tone: "working",
      title: s.stage || "Starting synchronization",
      detail:
        i < 0
          ? "Preparing the native engine"
          : `Stage ${i + 1}/${stages.length}${counts} · ${Math.floor(s.stageSeconds || 0)}s`,
      next:
        i < 0
          ? stages[0]
          : stages[i + 1] ||
            "Save project and Premiere XML, then review results",
      value: Math.max(0, i),
      max: stages.length,
      busy: true,
    };
  }
  if (s.outcome === "failed" || s.outcome === "cancelled")
    return {
      tone: s.outcome === "failed" ? "error" : "review",
      title:
        s.outcome === "failed"
          ? "Synchronization stopped with an error"
          : "Synchronization cancelled",
      detail: s.detail || "No completed result from this run",
      next: "Inspect the diagnostics before starting again",
      value: 0,
      max: stages.length,
      busy: false,
    };
  if (project) {
    const n = {
      supported: 0,
      validated: 0,
      provisional: 0,
      unresolved: 0,
      missing: 0,
    };
    for (const c of project.clips) {
      if (c.state in n) n[c.state]++;
      if (c.missing) n.missing++;
    }
    const review = n.provisional + n.unresolved;
    const complete = s.outcome === "complete" && project.path;
    return {
      tone: review || n.missing ? "review" : "complete",
      title: complete
        ? "Synchronization complete"
        : project.path
          ? "Project ready for review"
          : "Provisional timeline",
      detail: `${project.clips.length} recordings · ${n.supported} supported · ${n.validated} validated · ${n.provisional} provisional · ${n.unresolved} unresolved${n.missing ? ` · ${n.missing} offline` : ""}`,
      next: n.missing
        ? "Locate missing media, then review synchronization"
        : review
          ? "Review provisional and unresolved clips; compare the saved Premiere XML"
          : "Review playback or open the output folder for the Premiere XML",
      value: complete ? stages.length : 0,
      max: stages.length,
      busy: false,
    };
  }
  return {
    tone: "ready",
    title: s.inputs ? "Ready to synchronize" : "Ready",
    detail: s.inputs
      ? `${s.inputs} input files or folders selected`
      : "Native Go engine · media cache off by default",
    next: s.inputs
      ? "Synchronize to choose an output folder and begin"
      : "Add files or a folder, or open a saved project",
    value: 0,
    max: stages.length,
    busy: false,
  };
}
