export const shortcuts = {
  play: ["Space", "Play / pause"],
  stop: ["K", "Stop shuttle"],
  reverse: ["J", "Shuttle reverse"],
  forward: ["L", "Shuttle forward"],
  frameBack: ["ArrowLeft", "Previous frame"],
  frameNext: ["ArrowRight", "Next frame"],
  fiveBack: ["Shift+ArrowLeft", "Back five frames"],
  fiveNext: ["Shift+ArrowRight", "Forward five frames"],
  previous: ["ArrowUp", "Previous edit"],
  next: ["ArrowDown", "Next edit"],
  start: ["Home", "Sequence start"],
  end: ["End", "Sequence end"],
  fit: ["Backslash", "Fit sequence"],
  zoomIn: ["Equal", "Zoom in"],
  zoomOut: ["Minus", "Zoom out"],
  hand: ["H", "Hand tool"],
  select: ["V", "Selection tool"],
  open: ["Ctrl+O", "Open project"],
  settings: ["Ctrl+Alt+K", "Keyboard shortcuts / settings"],
  focus: ["F", "Focus selected recording"],
};
export function keyChord(e) {
  const parts = [];
  if (e.ctrlKey || e.metaKey) parts.push("Ctrl");
  if (e.altKey) parts.push("Alt");
  if (e.shiftKey) parts.push("Shift");
  parts.push((e.code || e.key).replace(/^Key/, "").replace(/^Digit/, ""));
  return parts.join("+");
}
export function keyLabel(s) {
  return s
    .replace("Backslash", "\\")
    .replace("Equal", "=")
    .replace("Minus", "−")
    .replace("Arrow", "");
}
export function validateShortcuts(map) {
  const seen = new Map();
  for (const [id, [def]] of Object.entries(shortcuts)) {
    const key = map[id] ?? def;
    if (!key) continue;
    if (seen.has(key))
      throw Error(`Shortcut ${keyLabel(key)} is assigned twice.`);
    seen.set(key, id);
  }
}
