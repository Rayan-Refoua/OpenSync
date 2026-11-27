# Synchronization workflow

OpenSync works best when the input set contains the original camera files, scratch audio, and any continuous recorder or playback master that spans the event. Add files or folders in the desktop application. Folder selection searches subdirectories. Keep source paths stable through export and editor import.

Choose **Chronological event** for an event recorded as successive segments. Choose **Clips / takes** for repeated performances or independent takes. Chronological file-time placement is optional and produces an estimate with its own status.

Run synchronization, then inspect the timeline and evidence. Review unresolved, provisional, short, quiet, or repetitive clips. The Match Review workspace lets you compare waveforms and available video, audition audio, and save a human decision. A manual confirmation records your judgment.

Keep the `.opensync` project as the master record. Export Premiere XML using the desired unmatched and audio drift settings. Import into the target Premiere version and check media links, source ranges, tracks, and synchronization before delivery.

For detailed operating guidance, see [Make a synced timeline](../SYNC-TIMELINE.md). For cache and command-line examples, see [Native Go migration](../NATIVE-GO-MIGRATION.md).
