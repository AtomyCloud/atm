package main

import (
	_ "embed"
	"encoding/json"
)

//go:embed en-us.json
var englishJSON []byte
var english = func() map[string]string {
	var v map[string]string
	if err := json.Unmarshal(englishJSON, &v); err != nil {
		panic(err)
	}
	return v
}()

func label(key string) string {
	v, ok := english[key]
	if !ok {
		panic("missing REA label: " + key)
	}
	return v
}
