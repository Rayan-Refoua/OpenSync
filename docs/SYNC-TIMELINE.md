# Make a synced timeline with OpenSync

1. Add the original media for one event or production unit. Include the continuous recorder or camera scratch track that spans the most material.
2. Choose **Clips / takes** for repeats and independent takes, or **Chronological event** for a continuous event recorded in segments.
3. Synchronize into a new output folder. Review the resulting `.opensync` project before importing the Premiere XML.
4. Listen to suspicious placements, reject false relationships, and export the reviewed result with the unmatched-media and audio drift settings appropriate to the edit.

## Prepare inputs that can be synchronized

Use camera originals and original recorder files when available. Retain camera scratch audio, even if you’ll mute it in the edit. A long recorder or camera with usable sound can connect short takes that never overlap one another. If separate sessions share no audio, treat them as separate sync jobs or keep their relative placement unresolved.

**Add folders** scans recursively. Choose the folders that contain source media. If the tree also includes proxies, transcoded duplicates, previous exports, or alternate mixes, select the original folders explicitly. Preserve camera folder boundaries and original names when you intend to use chronology. Keep source paths stable from sync through Premiere import.

For a music video, include the exact playback master used on set, including any count-in or edits. Repeated verses, loops, or identical takes can still admit multiple valid acoustic offsets. Where exact take identity matters, include a slate or another source that captures a unique event.

## Choose the mode and run

| Mode | Use it for | Placement behavior |
| --- | --- | --- |
| **Clips / takes** | Music videos, repeated performances, interviews captured as independent takes | Uses acoustic relationships without chronological fallback. |
| **Chronological event** | Conferences, concerts, or documentaries with sequential files from the same recorder | Can additionally estimate a bounded placement from recording succession and calibrated timestamps when supported anchors make that inference defensible. Estimates remain provisional. |

Choose the mode before **Synchronize**. OpenSync saves the precise `.opensync` project and a Premiere XML. Keep the project as the master for re-export and review. The XML represents a particular export of that project.

During **Evaluating timing models**, the engine tests candidate offsets, fits timing across the overlap, and checks reserved audio windows. Watch the completed pair count and the filenames in the activity log, especially on long or repetitive material. **Solving independent timing anchors** is the subsequent graph step. On desktop builds with progressive peak saves, keep `Waveform Peaks` next to the project for faster waveform loading on reopen. A fresh acoustic sync still reads audio for evidence.

## Review placements before finishing

Use **Next attention** to find unresolved or provisional recordings. Also spot-check clips that are short, nearly silent, recorded far from the source, or in a section with repeated music.

- **Acoustically supported** means the engine found consistent evidence for the placement. Listen to more than one point in the overlap. For long recordings, check near both ends and around timing knots.
- **Provisional or chronological** means the placement depends on a hypothesis or metadata. Use it as a starting point for review. The inspector can show the timing basis and estimated uncertainty.
- **Unresolved** means the engine lacks a supported placement relative to the main group. A clip placed at the end for export remains unresolved.

Compare a sharp sound with a visible event when the distinction matters. In a large hall, the same sound can reach a distant camera later than a close microphone. A steady difference may be acoustic arrival delay. A difference that grows over the recording is a reason to inspect the rate, timing knots, and evidence for clock drift. A small fit residual describes how well the model fits its observations. It does not establish absolute capture-time alignment.

If you find a wrong match, **reject the individual relationship** in the inspector first. This lets other relationships still support the clip. **Reject this placement** when you want that entire recording kept unresolved. Review changes create revisions in `Solutions`. Use **Undo review change** or **Solution history** to compare versions, and keep the original project with the revisions.

Use **Re-evaluate saved evidence** to apply a different mode or recompute the placement from the saved evidence. Use **Resynchronize current recordings** for a fresh search. It carries forward the current revision’s manual rejections. Changing only export settings requires neither operation.

## Export for Premiere

Set **Unmatched media in Premiere** according to who will use the timeline:

- **Append** keeps every unresolved recording at the end of the main sequence.
- **Separate sequence** keeps the editing timeline focused while putting unresolved clips in their own sequence.
- **Append in recording order** orders unmatched groups using available succession evidence. It does not infer elapsed gaps.
- **Append disabled** keeps unresolved material in the main sequence without enabling those clips.
- **Omit from timeline** leaves unresolved clips out of the XML. The saved OpenSync project still retains them.

Set **Allowed audio lead** and **Allowed audio lag** in Workstation Settings. Defaults are **50 ms lead** and **70 ms lag**. A supported standalone recorder whose modeled timing exceeds the applicable tolerance can get a corrected 24-bit WAV beside the original, with a filename ending in `_driftCorrection.wav`. Premiere XML points to that file at normal playback speed. Keep the corrected WAV accessible with the other media. Its source folder must be writable and support hard links, as NTFS does. Video remains at native playback speed. The exporter may split a drifting video into segments to approximate its timing without changing its speed.

Use **Export XML** from the project bar to export the revision you reviewed. Import the resulting XML into Premiere and verify media links, the start and end of long recordings, any corrected WAVs, and the unresolved section. Structural validation checks the XML and source ranges. Playback in Premiere is the final check for the edit.

## Repeat the export from the CLI

Run sync from folders or re-export a saved project with explicit settings:

```powershell
opensync-cli.exe sync --folders "D:\Camera A" "E:\Camera B" "D:\Recorder" --mode chronological --output "D:\Sync\Concert"
opensync-cli.exe export "D:\Sync\Concert\OpenSync.opensync" --output "D:\Sync\Concert-Review" --unsynced separate-sequence --audio-early-ms 50 --audio-late-ms 70
```

For a reviewed revision, pass the revision’s `.opensync` path to `export`. OpenSync writes a timeline manifest with placement and export details so you can see which timeline was delivered.
