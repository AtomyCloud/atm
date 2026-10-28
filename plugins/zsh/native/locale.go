//go:build linux

package main

import (
	_ "embed"
	"encoding/json"
)

//go:embed en-us.json
var localeData []byte
var labels = func() map[string]string {
	m := map[string]string{}
	if e := json.Unmarshal(localeData, &m); e != nil {
		panic(e)
	}
	return m
}()

func label(k string) string {
	if s := labels[k]; s != "" {
		return s
	}
	return k
}
