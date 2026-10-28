package main

import (
	"fmt"

	"github.com/AtomyCloud/atm/internal/plugin"
)

// Compatibility entrypoints for the original Node build command and tests.
func nodeManifest() (plugin.Manifest, error) { return nativeManifest("node") }

func buildNode(targetOS, arch, stage string) ([]payload, error) {
	m, err := nodeManifest()
	if err != nil {
		return nil, err
	}
	return buildNativePlugin(m, targetOS, arch, stage)
}

func nodePayload(m plugin.Manifest, target, stage string) (payload, error) {
	return nativePayload(m, target, stage)
}

func publishNode(out string, file payload) error { return publishNative(out, file) }

func buildNodeOnly(targetOS, arch, out string) error {
	if out == "" {
		return fmt.Errorf("--out is required")
	}
	if targetOS != "linux" || (arch != "amd64" && arch != "arm64") {
		return fmt.Errorf("native Node backend pending for %s/%s", targetOS, arch)
	}
	m, err := nodeManifest()
	if err != nil {
		return err
	}
	return buildNativeOnly([]plugin.Manifest{m}, targetOS, arch, out)
}
