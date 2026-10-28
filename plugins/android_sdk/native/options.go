package main

import (
	"fmt"
	"regexp"
	"strings"
)

const toolsBuild = "14742923"
const toolsDirectory = "20.0"
const toolsID = "cmdline-tools;20.0"
const toolsArchive = "commandlinetools-linux-14742923_latest.zip"
const toolsSHA1 = "48833c34b761c10cb20bcd16582129395d121b27"
const toolsSize int64 = 172789259

var sha256RE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var sha1RE = regexp.MustCompile(`^[0-9a-f]{40}$`)
var numericRE = regexp.MustCompile(`^[0-9]{1,8}(\.[0-9]{1,8}){0,3}$`)
var apiRE = regexp.MustCompile(`^android-[0-9]{1,4}$`)

type options struct{ Stack, Platform, BuildTools, CMake, NDK, Accept string }

func parseOptions(args []string, update bool) (options, error) {
	o := options{Stack: "latest"}
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		key, value, has := strings.Cut(args[i], "=")
		if !has {
			i++
			if i >= len(args) {
				return o, fmt.Errorf("missing option value")
			}
			value = args[i]
		}
		if value == "" || seen[key] {
			return o, fmt.Errorf("missing/duplicate option")
		}
		seen[key] = true
		if update && key != "--accept-licenses" {
			return o, fmt.Errorf("update only accepts --accept-licenses; it retains installed IDs")
		}
		switch key {
		case "--stack":
			o.Stack = value
		case "--platform":
			o.Platform = value
		case "--build-tools":
			o.BuildTools = value
		case "--cmake":
			o.CMake = value
		case "--ndk":
			o.NDK = value
		case "--accept-licenses":
			o.Accept = value
		default:
			return o, fmt.Errorf("unknown option: %s", key)
		}
	}
	if o.Accept != "" && !sha256RE.MatchString(o.Accept) {
		return o, fmt.Errorf("invalid license acceptance digest")
	}
	if o.Platform != "" && !apiRE.MatchString(o.Platform) {
		return o, fmt.Errorf("platform must be android-N")
	}
	for _, v := range []string{o.BuildTools, o.CMake, o.NDK} {
		if v != "" && !numericRE.MatchString(v) {
			return o, fmt.Errorf("invalid numeric package version")
		}
	}
	if o.Platform != "" || o.BuildTools != "" || o.CMake != "" || o.NDK != "" {
		o.Stack = "custom"
	}
	switch o.Stack {
	case "latest", "api36", "api35", "api35_legacy", "api34", "custom":
	default:
		return o, fmt.Errorf("unsupported stack (api37 has no legacy mapping)")
	}
	return o, nil
}
func (o options) ids() []string {
	ids := []string{toolsID, "platform-tools", "emulator"}
	fields := []string{o.Platform, o.BuildTools, o.CMake, o.NDK}
	switch o.Stack {
	case "latest", "api36":
		fields = []string{"android-36", "36.0.0", "3.31.6", "29.0.14206865"}
	case "api35":
		fields = []string{"android-35", "35.0.1", "3.31.6", "29.0.14206865"}
	case "api35_legacy":
		fields = []string{"android-35", "35.0.0", "3.22.1", "28.2.13676358"}
	case "api34":
		fields = []string{"android-34", "34.0.0", "3.22.1", "27.3.13750724"}
	}
	for i, prefix := range []string{"platforms;", "build-tools;", "cmake;", "ndk;"} {
		if fields[i] != "" {
			ids = append(ids, prefix+fields[i])
		}
	}
	return ids
}
func packageDir(id string) (string, error) {
	if id == toolsID {
		return "cmdline-tools/" + toolsDirectory, nil
	}
	if id == "platform-tools" || id == "emulator" {
		return id, nil
	}
	family, v, ok := strings.Cut(id, ";")
	if !ok {
		return "", fmt.Errorf("unsupported package ID")
	}
	switch family {
	case "platforms":
		if apiRE.MatchString(v) {
			return family + "/" + v, nil
		}
	case "build-tools", "cmake", "ndk":
		if numericRE.MatchString(v) {
			return family + "/" + v, nil
		}
	case "patcher":
		if v == "v4" {
			return "patcher/v4", nil
		}
	}
	return "", fmt.Errorf("unsupported package ID: %s", id)
}
