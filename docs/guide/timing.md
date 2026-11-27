# Timing and chronology

OpenSync keeps acoustic evidence, recording succession, and timeline layout as distinct concerns. Acoustic observations contribute timing relationships. Filename patterns, directories, timecode, and file dates can suggest order and estimated placement. Track allocation decides how clips appear in an editor.

In a continuous event, neighboring anchored recordings can help estimate where a missing segment belongs. Compare their durations, gaps, naming patterns, and acquisition times. Wrong device clocks, copied files, and contradictory timestamps can undermine an estimate. Chronological placements remain visibly identified and should be reviewed.

Audio and video clocks can drift. OpenSync evaluates timing observations over an overlap and may use supported piecewise mappings. For eligible audio recordings, export can create a new corrected audio file when drift exceeds the configured early and late tolerances. Source video is not stretched by this correction.

See [Component and chronology](../COMPONENT-AND-CHRONOLOGY.md), [Adaptive timing](../ADAPTIVE-TIMING.md), and [Acoustic propagation](../ACOUSTIC-PROPAGATION.md).

## Source detection and review

The desktop and default Premiere track layout use a shared detector. An original-device serial with a make or model can link clips across folders and recording modes. Other clips use folder, filename family when available, frame dimensions, container and video properties, and the full audio stream layout. Known transcoder output can provide filename continuity, while shared encoder properties carry no recorder identity.

In Synchronize settings, choose **Automatic**, **Pause on ambiguity**, or **Always review**. Review runs after probing and before synchronization starts. Select recordings to move them to an existing source or split them into a new source. Mark a source as a single recording stream if its clips cannot overlap. This opts the source into eligible occupied-range exclusions during chronological recovery. Reviewed assignments are saved with the recordings and reused by re-sync when the source files still match.

The command line can write a metadata-only plan and accept a reviewed plan on the next run.

```powershell
opensync sync --folders CameraFiles --stage sources --output SourceReview
opensync sync --folders CameraFiles --source-plan SourceReview/source-plan.json --output SyncedProject
```

Edit the clips' assignment groups and labels in the plan. Use the same assignment settings for all members of a group. OpenSync checks the source paths and file identities before applying assignments.

In chronological mode, the audit counts pairs with corroborating creation and modification times and separated recording intervals. The audit estimates potential savings from timestamps. Distributed recovery can also skip occupied offsets for explicitly reviewed single recording streams using independently validated timing anchors and default verification settings. The recovery report records excluded ranges on its 100 Hz offset grid.


### Chronological search shortcuts

In Chronological mode with file-time placement enabled, OpenSync can check a predicted acoustic match before broad recovery. The clips need consecutive filename counters within one detected source, two validated timing neighbors, and agreement between recording dates and file modification times. The match must pass separate audio holdout checks and fit the timing graph before it can join the main component early.

Turn off **Use chronological search shortcuts** in Synchronize settings or pass `--no-search-shortcuts` to `opensync sync` to run the usual recovery search throughout. This also turns off occupied-range exclusions. Custom verification thresholds automatically bypass both shortcuts.
