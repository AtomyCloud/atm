// Package protocol defines the bounded native plugin wire format, independent of UI.
package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const Version = 1
const MaxFrame = 64 * 1024
const MaxMessage = 8192

type Request struct {
	Protocol  int      `json:"protocol"`
	RequestID string   `json:"request_id"`
	PluginID  string   `json:"plugin_id"`
	Operation string   `json:"operation"`
	Arguments []string `json:"arguments"`
	Home      string   `json:"home"`
	AppsDir   string   `json:"apps_dir"`
	Language  string   `json:"language"`
}

type Event struct {
	Protocol  int      `json:"protocol"`
	RequestID string   `json:"request_id"`
	Type      string   `json:"type"`
	Message   string   `json:"message,omitempty"`
	Progress  *float64 `json:"progress,omitempty"`
	Status    string   `json:"status,omitempty"`
	Code      string   `json:"code,omitempty"`
}

func Decode(line []byte, value any) error {
	if len(line) > MaxFrame || !utf8.Valid(line) {
		return fmt.Errorf("invalid UTF-8 or oversized protocol frame")
	}
	// Reject duplicate top-level keys as well as unknown fields and trailing data.
	d := json.NewDecoder(bytes.NewReader(line))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("protocol frame must be an object")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return fmt.Errorf("duplicate/invalid protocol field")
		}
		seen[key] = true
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return err
		}
	}
	if _, err = d.Token(); err != nil {
		return err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing protocol data")
	}
	d = json.NewDecoder(bytes.NewReader(line))
	d.DisallowUnknownFields()
	return d.Decode(value)
}

func ReadRequest(r io.Reader) (Request, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxFrame+1))
	if err != nil {
		return Request{}, err
	}
	var request Request
	err = Decode(b, &request)
	if err == nil && (request.Protocol != Version || len(request.RequestID) != 32 || request.PluginID == "" || request.Operation == "") {
		err = fmt.Errorf("invalid native request")
	}
	return request, err
}

func SafeText(s string) string {
	s = strings.ToValidUTF8(s, "?")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

type Stream struct {
	RequestID string
	Started   bool
	Result    *Event
}

func (s *Stream) Accept(line []byte) (Event, error) {
	var e Event
	if err := Decode(line, &e); err != nil {
		return e, err
	}
	if e.Protocol != Version || e.RequestID != s.RequestID || len(e.Message) > MaxMessage || len(e.Code) > 64 {
		return e, fmt.Errorf("invalid event version, identity or size")
	}
	if s.Result != nil {
		return e, fmt.Errorf("event after terminal result")
	}
	if e.Type != "started" && !s.Started {
		return e, fmt.Errorf("event before started")
	}
	if e.Type != "result" && (e.Status != "" || e.Code != "") {
		return e, fmt.Errorf("result fields on nonterminal event")
	}
	if e.Type != "progress" && e.Progress != nil {
		return e, fmt.Errorf("progress field on wrong event")
	}
	switch e.Type {
	case "started":
		if s.Started {
			return e, fmt.Errorf("duplicate started event")
		}
		s.Started = true
	case "progress":
		if e.Progress == nil || *e.Progress < 0 || *e.Progress > 1 {
			return e, fmt.Errorf("invalid progress value")
		}
	case "log":
	case "result":
		if e.Status != "success" && e.Status != "error" && e.Status != "cancelled" {
			return e, fmt.Errorf("invalid result status")
		}
		s.Result = &e
	default:
		return e, fmt.Errorf("unknown event type")
	}
	return e, nil
}
