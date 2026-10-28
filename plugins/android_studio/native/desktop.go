package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const desktopReceiptName = ".atm-desktop.json"
const desktopFileName = "atm-android-studio-native.desktop"

type desktopReceipt struct {
	Schema int    `json:"schema"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func desktopScalar(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(s)
}
func desktopExec(s string) (string, error) {
	if !safeText(s, 4096) || strings.Contains(s, "=") {
		return "", fmt.Errorf("desktop executable path contains unsupported characters (including '=')")
	}
	quoted := strings.NewReplacer("%", "%%", "\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$").Replace(s)
	return "\"" + desktopScalar(quoted) + "\"", nil
}
func (b *backend) desktopContent(id identity) ([]byte, error) {
	command, err := desktopExec(filepath.Join(b.root, "current", id.Launcher))
	if err != nil {
		return nil, err
	}
	text := "[Desktop Entry]\nType=Application\nName=Android Studio (ATM)\nComment=Android Studio selected in ATM\nExec=" + command + "\nTerminal=false\nCategories=Development;IDE;\n"
	if id.Icon != "" {
		text += "Icon=" + desktopScalar(filepath.Join(b.root, "current", id.Icon)) + "\n"
	}
	return []byte(text), nil
}
func (b *backend) integrationPlans(remove bool) ([]profilePlan, error) {
	return b.integrationPlansFor(remove, "", true)
}
func (b *backend) selectionDesktopPlans(target string) ([]profilePlan, error) {
	if _, err := os.Lstat(filepath.Join(b.root, desktopReceiptName)); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return b.integrationPlansFor(false, target, false)
}
func (b *backend) integrationPlansFor(remove bool, target string, profiles bool) ([]profilePlan, error) {
	var plans []profilePlan
	var err error
	if profiles {
		plans, err = b.profilePlans(remove)
		if err != nil {
			return nil, err
		}
	}
	directory := filepath.Join(b.home, ".local/share/applications")
	if err = realDirs(directory, !remove && profiles); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	file := filepath.Join(directory, desktopFileName)
	state := filepath.Join(b.root, desktopReceiptName)
	before, mode, exists, err := readProfile(file)
	if err != nil {
		return nil, err
	}
	stateBefore, stateMode, stateExists, err := readProfile(state)
	if err != nil {
		return nil, err
	}
	if stateExists {
		var rec desktopReceipt
		dec := json.NewDecoder(strings.NewReader(string(stateBefore)))
		dec.DisallowUnknownFields()
		if err = dec.Decode(&rec); err != nil {
			return nil, err
		}
		if dec.Decode(new(any)) != io.EOF || rec.Schema != 1 || rec.Path != file || len(rec.SHA256) != 64 || strings.Trim(rec.SHA256, "0123456789abcdef") != "" {
			return nil, fmt.Errorf("invalid desktop ownership receipt")
		}
		if exists {
			hash := sha256.Sum256(before)
			if mode != 0644 || hex.EncodeToString(hash[:]) != rec.SHA256 {
				return nil, fmt.Errorf("desktop entry modified; preserving it")
			}
		}
	} else if exists {
		if remove {
			return plans, nil
		}
		return nil, fmt.Errorf("desktop entry is unowned; preserving it")
	}
	if !profiles && (!exists || !stateExists) {
		return nil, fmt.Errorf("configured desktop is missing; run explicit integrate to repair it")
	}
	if remove {
		if !stateExists {
			return plans, nil
		}
		if exists {
			plans = append(plans, profilePlan{path: file, before: before, after: nil, mode: mode, exists: true})
		}
		return append(plans, profilePlan{path: state, before: stateBefore, after: nil, mode: stateMode, exists: true}), nil
	}
	current := target
	if current == "" {
		current, err = b.current()
		if err != nil {
			return nil, err
		}
	}
	if current == "" {
		return nil, fmt.Errorf("no Android Studio selected")
	}
	if err = b.validInstalled(current); err != nil {
		return nil, err
	}
	a, err := b.spec(current)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Join(b.root, current))
	if err != nil {
		return nil, err
	}
	id, err := validatePayload(root, a)
	root.Close()
	if err != nil {
		return nil, err
	}
	after, err := b.desktopContent(id)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(after)
	stateAfter, err := json.Marshal(desktopReceipt{1, file, hex.EncodeToString(hash[:])})
	if err != nil {
		return nil, err
	}
	if !exists {
		mode = 0644
	}
	// One rollback sequence covers profiles, desktop content and its receipt.
	return append(plans, profilePlan{file, before, after, mode, exists}, profilePlan{state, stateBefore, stateAfter, stateMode, stateExists}), nil
}
func (b *backend) integrate(ctx context.Context) error {
	plans, err := b.integrationPlans(false)
	if err != nil {
		return err
	}
	for i := range plans {
		if filepath.Base(plans[i].path) == desktopFileName && !plans[i].exists {
			plans[i].mode = 0644
		}
	}
	_, err = applyProfiles(ctx, plans)
	return err
}
