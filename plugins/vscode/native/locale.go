package main

import (
	"embed"
	"encoding/json"
)

//go:embed en-us.json
var locales embed.FS

func label(key string) string {
	data, _ := locales.ReadFile("en-us.json")
	var labels map[string]string
	_ = json.Unmarshal(data, &labels)
	if s := labels[key]; s != "" {
		return s
	}
	return key
}
