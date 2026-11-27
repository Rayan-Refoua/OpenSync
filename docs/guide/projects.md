# Projects and export

The `.opensync` project holds source information, timing models, graph evidence, and project metadata. It is the master record for inspection and re-export. Manual decisions create solution revisions under the project's `Solutions` folder. Keep that folder and the active-review pointer with the base project so selected history and undo remain available after moving it.

Premiere-compatible FCP7 XML is an interchange timeline. Export translates the project into track placement, source ranges, frame positions, and any applicable corrected audio assets. Verify the result in the intended Premiere version, including source links, trims, clip duration, overlapping tracks, and synchronization.

Analysis caching is disabled by default. Explicit cache mode saves reusable analysis and checkpoints for later runs. It does not copy original media. A completed project can be opened independently of a transient in-memory analysis run.

Other NLE interchange formats are planned. Consult the current [delivery policy](../DELIVERY-POLICY.md) and [native Go migration notes](../NATIVE-GO-MIGRATION.md) for supported options and limitations.
