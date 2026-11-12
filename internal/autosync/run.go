// Package autosync implements the synchronization and export pipeline in Go.
// FFmpeg and FFprobe are the only media runtime tools.
package autosync

import "context"

const BuildVersion = "2026.11.12-r7"

// Run retains the previous Go entry point for callers and saved workflows.
func Run(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "audit-import":
			return NativeAuditImportCommand(args[1:])
		case "export":
			return NativeExportCommand(args[1:])
		case "sync":
			args = args[1:]
		}
	}
	return NativeSyncCommand(ctx, args)
}
