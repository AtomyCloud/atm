package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed en-us.json
var englishJSON []byte
var english = func() map[string]string {
	var m map[string]string
	if err := json.Unmarshal(englishJSON, &m); err != nil {
		panic(err)
	}
	return m
}()

func label(key string, args ...any) string {
	s, ok := english[key]
	if !ok {
		panic("missing Android SDK label: " + key)
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}
