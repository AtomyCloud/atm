package main

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/AtomyCloud/atm/internal/protocol"
)

func TestVendorAliasesAndDefaults(t *testing.T) {
	for input, want := range map[string]string{
		"jdk": "openjdk", "OpenJDK": "openjdk", "amazon": "corretto",
		"AMAZON-CORRETTO": "corretto", "amazon_corretto": "corretto",
		"eclipse": "temurin", "eclipse-temurin": "temurin", "eclipse_temurin": "temurin",
		"ms": "microsoft", "microsoft-openjdk": "microsoft", "microsoft_openjdk": "microsoft",
		"graal": "graalvm", "custom": "custom",
	} {
		if got, err := vendorSlug(input); err != nil || got != want {
			t.Fatalf("vendor %q: %q %v", input, got, err)
		}
	}
	for vendor, version := range map[string]string{"openjdk": "26.0.1", "corretto": "26", "temurin": "26", "microsoft": "25", "graalvm": "25"} {
		o, err := parseOptions([]string{"--vendor", vendor})
		if err != nil || o.vendor != vendor || o.version != version {
			t.Fatalf("default %s: %+v %v", vendor, o, err)
		}
	}
}

func TestCustomMenuCarriesLiteralArguments(t *testing.T) {
	// No client, filesystem or logger is supplied: these pages only collect input.
	b := &backend{}
	ctx := context.Background()
	root, err := b.menu(ctx, nil)
	if err != nil || protocol.ValidateChoices(root) != nil {
		t.Fatal(root, err)
	}
	label := "custom-27-ea"
	url := "https://example.invalid/jdk.tar.gz?label=a;b"
	for _, args := range [][]string{{"--custom", label}, {"--custom", label, url}} {
		choices, err := b.menu(ctx, args)
		if err != nil || len(choices) != 2 || protocol.ValidateChoices(choices) != nil {
			t.Fatalf("custom step %q: %+v %v", args, choices, err)
		}
		if choices[1].Operation != "menu" || len(choices[1].Arguments) != 0 {
			t.Fatal("custom flow lacks root-menu escape")
		}
		if len(args) == 3 {
			want := []string{"--vendor", "custom", "--version", label, "--url", url, "--sha256"}
			if choices[0].Operation != "install" || choices[0].Prompt == "" || !reflect.DeepEqual(choices[0].Arguments, want) {
				t.Fatalf("custom URL/label changed: %+v", choices[0])
			}
		}
	}
	for _, args := range [][]string{{"--custom"}, {"--custom", "../bad"}, {"--custom", label, "http://example.invalid/jdk"}, {"--custom", label, url, "extra"}} {
		if _, err := b.menu(ctx, args); err == nil {
			t.Fatalf("invalid custom step accepted: %q", args)
		}
	}
}

func TestVersionAndTargetBoundaries(t *testing.T) {
	for _, prefix := range []string{"", "jdk-", "jdk", "java-", "java"} {
		if got, err := normalizeVersion(prefix + "26.0.2.1"); err != nil || got != "26.0.2.1" {
			t.Fatalf("four-component version: %q %v", got, err)
		}
	}
	for _, input := range []string{"", "0", "026", "26..1", "26/1", "../26", "26.0.2.1.1", "26\n"} {
		if _, err := normalizeVersion(input); err == nil {
			t.Fatalf("invalid numeric version accepted: %q", input)
		}
	}
	for _, input := range []string{"../version", "openjdk/../26", "openjdk/26/extra", "/openjdk/26", "unknown/26", "custom/..", "custom/version\n", "custom/version."} {
		if _, err := normalizeTarget(input); err == nil {
			t.Fatalf("unsafe target accepted: %q", input)
		}
	}
	if got, err := normalizeTarget("jdk/26.0.1"); err != nil || got != "openjdk/26.0.1" {
		t.Fatalf("target alias: %q %v", got, err)
	}
	if !compatible("26.0.2.1", "26") || compatible("26.0.2.1", "26.0.2") || compatible("260.0.1", "26") {
		t.Fatal("major request and exact version boundaries conflated")
	}
}

func TestCustomDownloadRequiresExplicitHashAndLabel(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	args := []string{"--vendor=custom", "--version=my-jdk", "--url=https://example.invalid/jdk.tar.gz", "--sha256=" + strings.ToUpper(hash)}
	o, err := parseOptions(args)
	if err != nil || o.vendor != "custom" || o.version != "my-jdk" || o.hash != hash {
		t.Fatalf("custom exact install: %+v %v", o, err)
	}
	for _, args := range [][]string{
		{"--vendor", "custom"},
		{"--vendor", "custom", "--version", "my-jdk"},
		{"--url", "https://example.invalid/jdk.tar.gz"},
		{"--sha256", hash},
		{"--url", "https://example.invalid/jdk.tar.gz", "--sha256", "bad"},
		{"--version"}, {"--version="}, {"--unknown=value"},
		{"--version", "26", "--version=25"},
		{"--vendor", "openjdk", "--vendor=custom"},
		{"--vendor", "custom", "--version", "../escape", "--url", "https://example.invalid/x", "--sha256", hash},
	} {
		if _, err := parseOptions(args); err == nil {
			t.Fatalf("invalid arguments accepted: %q", args)
		}
	}
	for _, raw := range []string{"http://example.invalid/jdk.tar.gz", "file:///tmp/jdk.tar.gz", "https://user:password@example.invalid/jdk.tar.gz", "https://example.invalid/jdk.tar.gz#fragment", "https:///jdk.tar.gz", "https://example.invalid/\n"} {
		if _, err := parseOptions([]string{"--url", raw, "--sha256", hash}); err == nil {
			t.Fatalf("unsafe URL accepted: %q", raw)
		}
	}
}
