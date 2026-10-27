package setup

import (
	"context"
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

type registryPath struct{ key registry.Key }

func (r registryPath) Close() error { return r.key.Close() }
func (r registryPath) Read() (pathValue, error) {
	v, typ, err := r.key.GetStringValue("Path")
	if err == registry.ErrNotExist {
		return pathValue{}, nil
	}
	if err != nil {
		return pathValue{}, err
	}
	return pathValue{v, typ, true}, nil
}
func (r registryPath) Write(v pathValue) error {
	if v.Type == registry.EXPAND_SZ {
		return r.key.SetExpandStringValue("Path", v.Value)
	}
	if v.Type == registry.SZ {
		return r.key.SetStringValue("Path", v.Value)
	}
	return fmt.Errorf("unsupported registry PATH type")
}

func integrateWindows(ctx context.Context, home *os.Root, o Options, result Result) (Result, error) {
	if o.LinkCLI || o.shell() != "windows" {
		return result, fmt.Errorf("%w: Windows uses user registry PATH, not Unix profiles or links", ErrUnavailable)
	}
	access := uint32(registry.QUERY_VALUE)
	if !o.DryRun {
		access |= registry.SET_VALUE
	}
	// Existing HKCU Environment is opened, never a system hive or an admin API.
	key, err := registry.OpenKey(registry.CURRENT_USER, "Environment", access)
	if err != nil {
		return result, err
	}
	return integrateUserPath(ctx, home, o, result, registryPath{key})
}
