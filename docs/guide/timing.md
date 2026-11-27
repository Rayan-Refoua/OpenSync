# Timing and chronology

OpenSync keeps acoustic evidence, recording succession, and timeline layout as distinct concerns. Acoustic observations contribute timing relationships. Filename patterns, directories, timecode, and file dates can suggest order and estimated placement. Track allocation decides how clips appear in an editor.

In a continuous event, neighboring anchored recordings can help estimate where a missing segment belongs. Compare their durations, gaps, naming patterns, and acquisition times. Wrong device clocks, copied files, and contradictory timestamps can undermine an estimate. Chronological placements remain visibly identified and should be reviewed.

Audio and video clocks can drift. OpenSync evaluates timing observations over an overlap and may use supported piecewise mappings. For eligible audio recordings, export can create a new corrected audio file when drift exceeds the configured early and late tolerances. Source video is not stretched by this correction.

See [Component and chronology](../COMPONENT-AND-CHRONOLOGY.md), [Adaptive timing](../ADAPTIVE-TIMING.md), and [Acoustic propagation](../ACOUSTIC-PROPAGATION.md).
