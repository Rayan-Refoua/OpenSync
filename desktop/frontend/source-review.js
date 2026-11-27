const basisLabels = {
  device: "Device identifier",
  filename_profile: "Filename family and media format",
  profile: "Folder and media format",
  transcoded: "Transcoded media"
};

export function setupSourceReview(api) {
  const element = (id) => document.getElementById(id);
  const dialog = element("sourceReviewDialog");
  const groups = element("sourceReviewGroups");
  const destination = element("sourceReviewDestination");
  const error = element("sourceReviewError");
  const merge = element("sourceReviewMerge");
  const split = element("sourceReviewSplit");
  const proceed = element("sourceReviewContinue");
  let pending = null;
  let clips = [];
  let splitCount = 0;
  let submitted = false;
  const selected = new Set();

  function render() {
    const byGroup = new Map();
    for (const clip of clips) {
      const id = clip.assignment.group;
      if (!byGroup.has(id)) byGroup.set(id, []);
      byGroup.get(id).push(clip);
    }
    const previousDestination = destination.value;
    groups.replaceChildren();
    destination.replaceChildren();
    let number = 0;
    for (const [id, members] of byGroup) {
      number++;
      const assignment = members[0].assignment;
      const title = `Source ${number} · ${assignment.label} · ${members[0].mediaType === "video" ? "Video" : "Audio"}`;
      destination.add(new Option(title, id));

      const section = document.createElement("details");
      section.open = true;
      const name = document.createElement("summary");
      name.textContent = `${title} (${members.length})`;
      section.append(name);
      const policy = document.createElement("label");
      const exclusive = document.createElement("input");
      exclusive.type = "checkbox";
      exclusive.checked = !!assignment.exclusive;
      exclusive.onchange = () => {
        for (const clip of members) clip.assignment.exclusive = exclusive.checked;
      };
      policy.append(exclusive, " Single recording stream · clips cannot overlap");
      section.append(policy);

      for (const clip of members) {
        const row = document.createElement("label");
        row.className = "source-review-clip";
        const check = document.createElement("input");
        check.type = "checkbox";
        check.checked = selected.has(clip.id);
        check.onchange = () => {
          if (check.checked) selected.add(clip.id);
          else selected.delete(clip.id);
          updateActions();
        };
        const description = document.createElement("span");
        const filename = document.createElement("strong");
        filename.textContent = clip.name;
        const metadata = document.createElement("small");
        metadata.textContent = [clip.format, basisLabels[clip.basis]].filter(Boolean).join(" · ");
        description.append(filename, metadata);
        row.title = clip.path;
        row.append(check, description);
        section.append(row);
      }
      groups.append(section);
    }
    if (byGroup.has(previousDestination)) destination.value = previousDestination;
    updateActions();
  }

  function updateActions() {
    const moving = clips.filter((clip) => selected.has(clip.id));
    const types = new Set(moving.map((clip) => clip.mediaType));
    const target = clips.find((clip) => clip.assignment.group === destination.value);
    merge.disabled = types.size !== 1 || !types.has(target?.mediaType) ||
      !moving.some((clip) => clip.assignment.group !== destination.value);
    split.disabled = types.size !== 1;
  }

  function move(assignment) {
    for (const clip of clips) {
      if (selected.has(clip.id)) clip.assignment = { ...assignment };
    }
    selected.clear();
    render();
  }

  destination.onchange = updateActions;
  merge.onclick = () => {
    const target = clips.find((clip) => clip.assignment.group === destination.value);
    if (target) move(target.assignment);
  };
  split.onclick = () => {
    const first = clips.find((clip) => selected.has(clip.id));
    if (!first) return;
    let group;
    do {
      group = `manual-${first.id}-${++splitCount}`;
    } while (clips.some((clip) => clip.assignment.group === group));
    move({group, label: `Split ${splitCount}`, exclusive: false});
  };
  proceed.onclick = async () => {
    if (!pending || submitted) return;
    submitted = true;
    proceed.disabled = true;
    error.textContent = "";
    try {
      await api().SubmitSourceReview(pending.id, {version: pending.plan.version, clips});
    } catch (reason) {
      submitted = false;
      proceed.disabled = false;
      error.textContent = String(reason.message || reason);
    }
  };
  element("sourceReviewCancel").onclick = async () => {
    try {
      await api().CancelSync();
    } catch (reason) {
      error.textContent = String(reason.message || reason);
    }
  };
  dialog.addEventListener("cancel", (event) => event.preventDefault());

  return (review) => {
    if (!review) {
      pending = null;
      if (dialog.open) dialog.close();
      return;
    }
    if (review.id === pending?.id) return;
    pending = review;
    clips = review.plan.clips;
    selected.clear();
    splitCount = 0;
    submitted = false;
    proceed.disabled = false;
    error.textContent = "";
    element("sourceReviewWarnings").textContent = (review.plan.warnings || []).join(" ");
    render();
    if (!dialog.open) dialog.showModal();
  };
}
