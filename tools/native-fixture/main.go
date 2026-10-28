// native-fixture exercises the protocol; it never installs applications.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/AtomyCloud/atm/internal/protocol"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--child" {
		for {
			if err := os.WriteFile(os.Args[2], []byte(time.Now().String()), 0600); err != nil {
				os.Exit(1)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	r, err := protocol.ReadRequest(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	emit := func(e protocol.Event) {
		e.Protocol = 1
		e.RequestID = r.RequestID
		if err := json.NewEncoder(os.Stdout).Encode(e); err != nil {
			os.Exit(3)
		}
	}
	emit(protocol.Event{Type: "started", Message: "Fixture started"})
	mode := "success"
	if r.Operation == "update" {
		mode = "hang"
	}
	if r.Operation == "menu" {
		mode = "choices"
	}
	if len(r.Arguments) > 0 {
		mode = r.Arguments[0]
	}
	switch mode {
	case "malformed":
		fmt.Println("not JSON")
		return
	case "oversized":
		fmt.Println(strings.Repeat("x", protocol.MaxFrame+1))
		return
	case "truncated":
		fmt.Print(`{"protocol":1`)
		return
	case "missing":
		return
	case "wrong-id":
		fmt.Printf("{\"protocol\":1,\"request_id\":\"other\",\"type\":\"log\"}\n")
		return
	case "duplicate":
		emit(protocol.Event{Type: "started"})
		return
	case "stderr-flood":
		fmt.Fprint(os.Stderr, strings.Repeat("x", 1024*1024+1))
		return
	case "error":
		emit(protocol.Event{Type: "result", Status: "error", Code: "fixture_error", Message: "Expected fixture error"})
		os.Exit(7)
	case "error-zero":
		emit(protocol.Event{Type: "result", Status: "error", Message: "Error with exit zero"})
		return
	case "cancelled":
		emit(protocol.Event{Type: "result", Status: "cancelled"})
		return
	case "hang":
		time.Sleep(time.Hour)
		return
	case "tree", "tree-exit":
		if len(r.Arguments) != 2 {
			os.Exit(4)
		}
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "--child", r.Arguments[1])
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(4)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(r.Arguments[1]); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(5)
			}
			time.Sleep(10 * time.Millisecond)
		}
		emit(protocol.Event{Type: "log", Message: "Child ready"})
		if mode == "tree" {
			time.Sleep(time.Hour)
			return
		}
	case "echo":
		b, _ := json.Marshal(r)
		emit(protocol.Event{Type: "log", Message: string(b)})
	case "choices":
		emit(protocol.Event{Type: "result", Status: "success", Message: "Choose a version", Choices: []protocol.Choice{{ID: "custom", Label: "Custom version", Operation: "install", Arguments: []string{"--version"}, Prompt: "Version"}}})
		return
	case "log-flood":
		for i := 0; i < 1000; i++ {
			emit(protocol.Event{Type: "log", Message: fmt.Sprintf("log %d", i)})
		}
	}
	if mode == "success" {
		for i := 1; i <= 5; i++ {
			v := float64(i) / 5
			emit(protocol.Event{Type: "progress", Progress: &v, Message: fmt.Sprintf("Step %d", i)})
			time.Sleep(100 * time.Millisecond)
		}
	}
	emit(protocol.Event{Type: "result", Status: "success", Message: "Fixture completed"})
	if mode == "late-event" {
		emit(protocol.Event{Type: "log", Message: "Too late"})
	}
	if mode == "success-nonzero" {
		os.Exit(7)
	}
}
