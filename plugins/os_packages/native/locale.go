package main

import (
	_ "embed"
	"encoding/json"
)

//go:embed en-us.json
var localeData []byte
var labels = func() map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(localeData, &m); err != nil {
		panic(err)
	}
	return m
}()

func label(key string) string {
	if s := labels[key]; s != "" {
		return s
	}
	return key
}
