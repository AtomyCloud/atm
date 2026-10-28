package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStrictFramesAndState(t *testing.T) {
	for _, line := range []string{`null`, `[]`, `{"type":"log","type":"log"}`, `{"unknown":true}`, `{} {}`, string([]byte{255}), strings.Repeat("x", MaxFrame+1)} {
		var event Event
		if Decode([]byte(line), &event) == nil {
			t.Fatalf("accepted %q", line[:min(len(line), 80)])
		}
	}
	id := strings.Repeat("a", 32)
	accept := func(s *Stream, e Event) error {
		e.Protocol = 1
		e.RequestID = id
		b, _ := json.Marshal(e)
		_, err := s.Accept(b)
		return err
	}
	for _, e := range []Event{{Type: "log"}, {Type: "result", Status: "success"}, {Type: "progress"}} {
		if accept(&Stream{RequestID: id}, e) == nil {
			t.Fatal("accepted event before started")
		}
	}
	s := Stream{RequestID: id}
	if err := accept(&s, Event{Type: "started"}); err != nil {
		t.Fatal(err)
	}
	bad := 1.1
	for _, e := range []Event{{Type: "started"}, {Type: "unknown"}, {Type: "progress", Progress: &bad}, {Type: "log", Status: "success"}, {Type: "log", Message: strings.Repeat("x", MaxMessage+1)}, {Type: "result", Status: "maybe"}} {
		if accept(&s, e) == nil {
			t.Fatal("invalid event accepted", e.Type)
		}
	}
	if err := accept(&s, Event{Type: "result", Status: "success"}); err != nil {
		t.Fatal(err)
	}
	if accept(&s, Event{Type: "log"}) == nil {
		t.Fatal("post-result event accepted")
	}
}

func TestSafeTextAndRequest(t *testing.T) {
	if text := SafeText("hi\x1b[2J\n\u009b31m"); strings.ContainsAny(text, "\x1b\n\u009b") {
		t.Fatal(text)
	}
	r := Request{Protocol: 1, RequestID: strings.Repeat("1", 32), PluginID: "fixture", Operation: "status"}
	b, _ := json.Marshal(r)
	if _, err := ReadRequest(strings.NewReader(string(b) + "\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRequest(strings.NewReader(strings.Repeat("x", MaxFrame+1))); err == nil {
		t.Fatal("oversized request accepted")
	}
}

func TestChoicesValidation(t *testing.T) {
	good := Choice{ID: "custom", Label: "Custom version", Operation: "install", Arguments: []string{"--version"}, Prompt: "Version"}
	if err := ValidateChoices([]Choice{good}); err != nil {
		t.Fatal(err)
	}
	for _, choices := range [][]Choice{
		{good, good}, make([]Choice, 101),
		{{ID: "a", Label: "x", Operation: "shell"}},
		{{ID: "a", Label: "x\x1b[2J", Operation: "install"}},
		{{ID: "a", Label: "x", Operation: "install", Arguments: []string{"v1\x00"}}},
		{{ID: "a", Label: " ", Operation: "menu"}},
	} {
		if ValidateChoices(choices) == nil {
			t.Fatalf("unsafe choices accepted: %+v", choices)
		}
	}
	for _, kind := range []string{"started", "log", "result"} {
		s := Stream{RequestID: "id", Started: true}
		e := Event{Protocol: 1, RequestID: "id", Type: kind, Choices: []Choice{good}}
		if kind == "result" {
			e.Status = "error"
		}
		b, _ := json.Marshal(e)
		if _, err := s.Accept(b); err == nil {
			t.Fatal("choices accepted outside successful result")
		}
	}
}
