package main

import (
	"encoding/json"
	"fmt"
	"os"

	"opensync/internal/premierexml"
	"opensync/internal/projectstate"
)

func auditPremiere(sourcePath, projectPath, outputPath string) error {
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read Premiere XML: %w", err)
	}
	p, err := projectstate.Load(projectPath)
	if err != nil {
		return fmt.Errorf("load OpenSync project: %w", err)
	}
	audit, err := premierexml.AuditTimeline(source, p)
	if err != nil {
		return fmt.Errorf("inspect Premiere timeline: %w", err)
	}
	data, err := json.MarshalIndent(audit, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(outputPath)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(outputPath)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println("STATUS:", audit.Status)
	fmt.Printf("PROJECT_RECORDINGS: %d\n", audit.ProjectRecordings)
	fmt.Printf("MATCHED_RECORDINGS: %d\n", audit.MatchedRecordings)
	fmt.Printf("VIDEO_INSTANCES: %d\n", audit.VideoClipInstances)
	fmt.Printf("AUDIO_INSTANCES: %d\n", audit.AudioClipInstances)
	fmt.Printf("VIDEO_TRACKS: %d\n", audit.VideoTracks)
	fmt.Printf("AUDIO_TRACKS: %d\n", audit.AudioTracks)
	fmt.Printf("MISSING_RECORDINGS: %d\n", len(audit.MissingRecordingIDs))
	fmt.Printf("AMBIGUOUS_IDENTITIES: %d\n", len(audit.AmbiguousRecordingIDs))
	fmt.Printf("UNMATCHED_VIDEO_CLIPS: %d\n", len(audit.UnmatchedVideoClipIDs))
	fmt.Printf("TRACK_COLLISIONS: %d\n", len(audit.OverlappingTrackPairs))
	fmt.Println("REPORT:", outputPath)
	return nil
}
