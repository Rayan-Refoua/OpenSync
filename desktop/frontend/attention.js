export function setupAttention(h) {
  const $ = (id) => document.getElementById(id);
  const dialog = $("attentionDialog");
  const list = $("attentionList");
  const reference = $("retryReference");
  const selected = new Set();

  function rejected(c) {
    return h.state.project?.graph?.review_ledger?.rejected_recordings?.includes(c.id);
  }

  function eligible(c) {
    return !rejected(c) && c.audio && !c.missing && ["unresolved", "provisional"].includes(c.state);
  }

  function update() {
    $("attentionRetry").disabled = !selected.size || !reference.options.length;
    $("attentionRetry").textContent = selected.size ? `Search ${selected.size} selected` : "Search selected clips";
  }

  function goTo(clip) {
    h.stop();
    h.inspect(clip);
    h.pan(clip.start);
    h.seek(clip.start, false);
    h.scrollToTrack(clip.track);
  }

  function open(preselect) {
    const p = h.state.project;
    if (!p?.path) return;
    selected.clear();
    const requested = h.state.retrySelection?.size ? h.state.retrySelection : new Set([h.state.selected]);
    const decisions = p.graph?.manual_candidate_decisions || {};
    const candidates = new Map(), confirmations = new Set(), audits = new Map();
    for (const pair of p.manualCandidates || []) {
      if (pair.auditOnly) { audits.set(pair.query, pair.id); continue; }
      if (decisions[pair.id] === "confirmed") confirmations.add(pair.query);
      if (!decisions[pair.id] && !candidates.has(pair.query)) candidates.set(pair.query, pair.id);
    }
    list.replaceChildren();
    reference.replaceChildren();
    const refs = p.clips.filter((c) => c.audio && !c.missing && !rejected(c) && ["supported", "validated"].includes(c.state));
    if (refs.length) reference.add(new Option("All placed audio references", ""));
    for (const c of refs) reference.add(new Option(c.name, String(c.index)));

    const rows = p.clips.filter((c) => c.missing || eligible(c) || c.state === "unresolved" || c.state === "provisional" || confirmations.has(c.index) || audits.has(c.index));
    rows.sort((a, b) => a.start - b.start || a.index - b.index);
    for (const c of rows) {
      const row = document.createElement("div");
      row.className = "attention-row";
      const choice = document.createElement("input");
      choice.type = "checkbox";
      choice.setAttribute("aria-label", `Retry ${c.name}`);
      choice.disabled = !eligible(c);
      choice.checked = preselect && !choice.disabled && requested.has(c.index);
      if (choice.checked) selected.add(c.index);
      choice.onchange = () => {
        if (choice.checked) selected.add(c.index);
        else selected.delete(c.index);
        update();
      };
      const label = document.createElement("div");
      const title = document.createElement("strong");
      title.textContent = c.name;
      const reason = document.createElement("small");
      reason.textContent = c.missing ? "Source media is offline"
        : audits.has(c.index) ? "Chronology guided the search and acoustic checks verified the match"
        : rejected(c) ? "Recording rejected in review"
        : confirmations.has(c.index) ? "Match confirmed by hand, placement still needs review"
        : !c.audio ? "No usable audio stream"
        : candidates.has(c.index) ? (c.reviewCount > 1 ? "Competing timing candidates" : "A timing candidate is ready to audition")
        : (c.timingBasis || "").includes("chronolog") ? "Placement estimated from file chronology"
        : c.state === "provisional" ? "Placement needs independent audio evidence"
        : "No supported timing match";
      label.append(title, reason);
      const next = document.createElement("button");
      next.textContent = c.missing ? "Locate media" : (candidates.has(c.index) || audits.has(c.index)) ? "A/B review" : eligible(c) ? "Select for retry" : "Show on timeline";
      next.onclick = () => {
        if (c.missing) {
          dialog.close();
          $("locate").click();
        } else if (candidates.has(c.index) || audits.has(c.index)) {
          dialog.close();
          goTo(c);
          h.matchReview.open(c.index, candidates.get(c.index) || audits.get(c.index));
        } else if (eligible(c)) {
          choice.checked = true;
          selected.add(c.index);
          update();
          reference.focus();
        } else {
          dialog.close();
          goTo(c);
        }
      };
      row.append(choice, label, next);
      list.append(row);
    }
    if (!rows.length) list.textContent = "No recordings need attention.";
    update();
    dialog.showModal();
  }

  $("nextAttention").onclick = () => open(false);
  $("retrySelected").onclick = () => open(true);
  $("attentionClose").onclick = () => dialog.close();
  $("attentionRetry").onclick = () => {
    const clips = [...selected];
    const ref = reference.value === "" ? null : Number(reference.value);
    const broader = $("retryBroader").checked;
    dialog.close();
    h.stop();
    h.action(async () => {
      const result = await h.api().RetryClips({clips, reference: ref, broader});
      if (!result.added) {
        h.message("No new supported candidates found. Existing results are available in Match Review.");
        return;
      }
      h.setProject(result.project, false);
      const proposal = result.project.manualCandidates.find((c) => clips.includes(c.query) && c.status === "retry_acoustic_candidate" && !result.project.graph?.manual_candidate_decisions?.[c.id]);
      if (proposal) h.matchReview.open(proposal.query, proposal.id);
      h.message(`Saved ${result.added} new match candidates for review`);
    });
  };
}
