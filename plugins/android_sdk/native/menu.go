package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func option(id, operation string, args ...string) choice {
	return choice{ID: id, Label: label(id), Operation: operation, Arguments: args}
}
func (b *backend) menu(ctx context.Context, args []string) ([]choice, error) {
	back := option("back", "menu")
	if len(args) == 0 {
		return []choice{option("stacks", "menu", "stacks"), option("custom", "menu", "custom"), option("review_update", "menu", "review-update"), option("status", "status"), option("remove", "menu", "remove"), option("uninstall", "uninstall"), option("integrate", "integrate"), option("java", "menu", "java")}, nil
	}
	switch args[0] {
	case "stacks":
		if len(args) != 1 {
			break
		}
		var out []choice
		for _, s := range []string{"latest", "api36", "api35", "api35_legacy", "api34"} {
			out = append(out, choice{ID: s, Label: label("stack", s), Operation: "menu", Arguments: []string{"review", "--stack", s}})
		}
		return append(out, back), nil
	case "java":
		if len(args) != 1 {
			break
		}
		b.javaStatus()
		return []choice{back}, nil
	case "custom":
		if len(args) > 5 {
			break
		}
		names := []string{"platform", "build_tools", "cmake", "ndk"}
		if len(args) < 5 {
			return []choice{{ID: "next", Label: label("custom"), Operation: "menu", Arguments: args, Prompt: label(names[len(args)-1])}, back}, nil
		}
		selected := []string{"--stack", "custom"}
		for i, key := range []string{"--platform", "--build-tools", "--cmake", "--ndk"} {
			if args[i+1] != "-" {
				selected = append(selected, key, args[i+1])
			}
		}
		return b.review(ctx, selected, "install")
	case "review":
		return b.review(ctx, args[1:], "install")
	case "review-update":
		if len(args) != 1 {
			break
		}
		return b.review(ctx, nil, "update")
	case "remove":
		if len(args) != 1 {
			break
		}
		records, err := b.receipts()
		if err != nil {
			return nil, err
		}
		if len(records) > 90 {
			return nil, fmt.Errorf("too many packages; use explicit remove ID")
		}
		var out []choice
		for i, r := range records {
			if r.Spec.ID != toolsID {
				out = append(out, choice{ID: fmt.Sprint(i), Label: label("remove_package", r.Spec.ID), Operation: "remove", Arguments: []string{r.Spec.ID}})
			}
		}
		return append(out, back), nil
	}
	return nil, fmt.Errorf("unknown Android SDK submenu")
}
func (b *backend) review(ctx context.Context, args []string, operation string) ([]choice, error) {
	o, err := parseOptions(args, operation == "update")
	if err != nil {
		return nil, err
	}
	if o.Accept != "" {
		return nil, fmt.Errorf("review does not accept a prior acknowledgement")
	}
	if _, err = b.ownedRoot(operation == "update"); err != nil {
		return nil, err
	}
	plan, err := b.plan(ctx, o, operation)
	if err != nil {
		return nil, err
	}
	logs, err := reviewOutput(plan)
	if err != nil {
		return nil, err
	}
	for _, line := range logs {
		b.log(line)
	}
	acknowledged := append(append([]string(nil), args...), "--accept-licenses", plan.digest())
	return []choice{{ID: "accept", Label: label("accept", operation), Operation: operation, Arguments: acknowledged}, option("back", "menu")}, nil
}
func reviewOutput(plan installPlan) ([]string, error) {
	licenses := map[string]string{}
	total := 0
	var out strings.Builder
	out.WriteString(label("review_header") + "\nOperation: " + plan.Operation + "\nSDK root: " + plan.Root + "\nReview digest: " + plan.digest() + "\nPackages (dependencies first):\n")
	for _, p := range plan.Packages {
		fmt.Fprintf(&out, "%s revision %s, %d bytes, published %s %s\n", p.ID, p.Revision.String(), p.Size, p.ChecksumType, p.Checksum)
		if old, ok := licenses[p.License.ID]; ok {
			if old != p.License.Text {
				return nil, fmt.Errorf("conflicting license bodies")
			}
		} else {
			licenses[p.License.ID] = p.License.Text
			total += len(p.License.Text)
		}
	}
	if total > 128<<10 || len(licenses) > 8 {
		return nil, fmt.Errorf("full license review exceeds presentation limit; cannot offer acceptance")
	}
	ids := make([]string, 0, len(licenses))
	for id := range licenses {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(&out, "\nBEGIN FULL LICENSE %s\n%s\nEND FULL LICENSE %s\n", id, licenses[id], id)
	}
	text := out.String()
	if !utf8.ValidString(text) || len(text) > 240<<10 {
		return nil, fmt.Errorf("review output too large/invalid")
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return nil, fmt.Errorf("unprintable license content; refusing acceptance")
		}
	}
	var chunks []string
	for len(text) > 0 {
		n := 7000
		if len(text) < n {
			n = len(text)
		}
		for n < len(text) && !utf8.RuneStart(text[n]) {
			n--
		}
		chunks = append(chunks, text[:n])
		text = text[n:]
	}
	if len(chunks) > 40 {
		return nil, fmt.Errorf("review event limit")
	}
	return chunks, nil
}
func (b *backend) javaStatus() {
	candidates := []string{filepath.Join(b.req.AppsDir, "Java/current/bin/java")}
	if home := os.Getenv("JAVA_HOME"); filepath.IsAbs(home) {
		candidates = append(candidates, filepath.Join(home, "bin/java"))
	}
	if p, err := exec.LookPath("java"); err == nil && filepath.IsAbs(p) {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			b.log("Java found (not executed/version not verified): " + p)
			return
		}
	}
	b.log("Java not found. Native downloads do not require Java; SDK tools do. Install a compatible JDK explicitly with: atm plugin java")
}
func (b *backend) status() error {
	b.log("SDK root: " + b.root)
	if err := realDirs(b.root, false); os.IsNotExist(err) {
		b.log("Android SDK not installed")
		b.javaStatus()
		return nil
	} else if err != nil {
		return err
	}
	owned, err := b.ownedRoot(false)
	if err != nil {
		b.log("Unmanaged or invalid SDK preserved: " + err.Error())
		b.javaStatus()
		return nil
	}
	if !owned {
		b.log("Empty unmanaged SDK directory")
		return nil
	}
	records, err := b.receipts()
	if err != nil {
		return err
	}
	b.log(fmt.Sprintf("Native package receipts: %d (status is not a full integrity audit)", len(records)))
	for i, r := range records {
		if i == 24 {
			b.log("Additional packages omitted from summary; use the package menu")
			break
		}
		b.log(r.Spec.ID + " revision " + r.Spec.Revision.String())
	}
	b.javaStatus()
	return nil
}
