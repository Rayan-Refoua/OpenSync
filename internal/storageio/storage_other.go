//go:build !windows

package storageio

import "path/filepath"

func resolveVolume(path string) volumeIdentity {
	root := filepath.VolumeName(path)
	if root == "" {
		root = string(filepath.Separator)
	}
	return volumeIdentity{key: root, root: root}
}

func classifyVolume(v volumeIdentity) Info {
	return unknownVolume(v, "Physical storage classification is implemented on Windows only; conservative source I/O serialization")
}
