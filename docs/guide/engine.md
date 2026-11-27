# Engine stages

The public synchronization pipeline runs in Go. Its exact sequence depends on settings, source material, and available evidence.

1. **Inspect inputs.** Collect files, probe readable media, and record source identity and metadata.
2. **Prepare audio and fingerprints.** Decode or stream audio and extract acoustic features in bounded blocks. Visualization peaks can reuse audio read during synchronization.
3. **Discover candidates.** Compare fingerprints and rank possible pairs and offset regions.
4. **Verify candidates.** Examine acoustic windows and compare competing timing explanations.
5. **Search unresolved recordings.** Use recovery strategies to investigate candidates missed by the first pass.
6. **Evaluate timing models.** Fit supported timing relationships, assess residuals and reserved observations, and retain ambiguity where evidence is limited.
7. **Solve graph relationships.** Combine pairwise evidence into components and check conflicts and independent paths.
8. **Recover and refine.** Use accumulated context for further distributed searches and refine supported timing.
9. **Save and export.** Write the native project, construct the export timeline, and validate the Premiere XML.

Discovery, support, and validation describe different levels of evidence. A repeated sound can make several offsets attractive, while graph consistency can still reflect a shared error. Independent observations and competing explanations matter.

The implementation starts in `internal/autosync/native_pipeline.go`. Historical design notes and detailed algorithms are listed in the [documentation reference](./reference.md).
