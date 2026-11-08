package main

import (
	"encoding/json"
	"os"

	"opensync/internal/quality"
)

func showPreset(name string) error {
	preset, err := quality.Parse(name)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(preset)
}
