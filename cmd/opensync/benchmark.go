package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"opensync/internal/enginebench"
)

func benchmarkCommand(args []string) error {
	flags := flag.NewFlagSet("benchmark", flag.ContinueOnError)
	output := flags.String("output", "", "new output directory (required)")
	profile := flags.String("profile", "quick", "quick: 120s local timing; full: two-hour local timing (~1 GB fixtures)")
	manifest := flags.String("real-manifest", "", "optional version 1 JSON real-pair manifest")
	gate := flags.String("gate", "readiness", "readiness or regression; blockers fail readiness only")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *output == "" || flags.NArg() != 0 || (*gate != "readiness" && *gate != "regression") {
		return fmt.Errorf("usage: opensync benchmark --output NEW_DIR [--profile quick|full] [--real-manifest pairs.json] [--gate readiness|regression]")
	}
	report, err := enginebench.Execute(enginebench.Options{Output: *output, Profile: *profile, RealManifest: *manifest, Progress: os.Stdout})
	if err != nil {
		return err
	}
	fmt.Printf("REGRESSION: %s\nREADINESS: %s\nREPORT: %s\n", report.RegressionStatus, report.ReadinessStatus, filepath.Join(*output, "report.html"))
	status := report.ReadinessStatus
	if *gate == "regression" {
		status = report.RegressionStatus
	}
	if status != "pass" {
		return fmt.Errorf("%s gate is %s; full results were saved", *gate, status)
	}
	return nil
}
