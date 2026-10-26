package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssignments(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
		fail             bool
	}{
		{"literal", "X=\"hello world\"\nY='value'\n", "hello world", false},
		{"escaped", "X=\"quote\\\" slash\\\\ dollar\\$\"\n", "quote\" slash\\ dollar$", false},
		{"command", "X=$(touch sentinel)\n", "", true},
		{"backticks", "X=`id`\n", "", true},
		{"parameter", "X=\"$HOME/Apps\"\n", "", true},
		{"redirect", "X=x >sentinel\n", "", true},
		{"code", "touch sentinel\n", "", true},
		{"array", "X=(one two)\n", "", true},
		{"append", "X+=x\n", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(tc.text), 0600); err != nil {
				t.Fatal(err)
			}
			values, err := Assignments(path)
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v", err)
			}
			if !tc.fail && values["X"] != tc.want {
				t.Fatalf("got %q", values["X"])
			}
		})
	}
}

func TestStrictJSON(t *testing.T) {
	for _, text := range []string{`{"a":1,"extra":2}`, `{"a":1} {"a":2}`, `{"a":`} {
		path := filepath.Join(t.TempDir(), "input.json")
		os.WriteFile(path, []byte(text), 0600)
		var target struct {
			A int `json:"a"`
		}
		if JSON(path, &target) == nil {
			t.Fatal("accepted", text)
		}
	}
}

func TestAtomicJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "prefs.json")
	for _, value := range []string{"first", "second"} {
		if err := WriteJSON(path, map[string]string{"value": value}); err != nil {
			t.Fatal(err)
		}
		var got map[string]string
		if err := JSON(path, &got); err != nil || got["value"] != value {
			t.Fatalf("%v %v", got, err)
		}
	}
}
