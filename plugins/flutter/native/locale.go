package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed en-us.json
var englishJSON []byte

var english = func() map[string]string {
	var labels map[string]string
	if err := json.Unmarshal(englishJSON, &labels); err != nil {
		panic(err)
	}
	return labels
}()

func label(key string, args ...any) string {
	value, ok := english[key]
	if !ok {
		panic("missing embedded Flutter label: " + key)
	}
	if len(args) == 0 {
		return value
	}
	return fmt.Sprintf(value, args...)
}
