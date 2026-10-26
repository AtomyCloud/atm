// Package data reads legacy assignments as data, never as executable shell code.
package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

const MaxFileSize = 1 << 20

func Read(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err == nil && len(b) > MaxFileSize {
		err = fmt.Errorf("file exceeds 1 MiB: %s", path)
	}
	return b, err
}

func JSON(path string, target any) error {
	b, err := Read(path)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%s: unexpected trailing JSON", path)
	}
	return nil
}

func Assignments(path string) (map[string]string, error) {
	b, err := Read(path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(bytes.NewReader(b), path)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, stmt := range f.Stmts {
		call, ok := stmt.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) != 0 || len(stmt.Redirs) != 0 || stmt.Background || stmt.Negated || stmt.Coprocess {
			return nil, fmt.Errorf("%s: only literal assignments are supported; shell code was not executed", path)
		}
		for _, a := range call.Assigns {
			if a.Name == nil || a.Append || a.Index != nil || a.Array != nil || a.Naked {
				return nil, fmt.Errorf("%s: unsupported assignment", path)
			}
			v := ""
			if a.Value != nil {
				v, err = literal(a.Value.Parts, false)
				if err != nil {
					return nil, fmt.Errorf("%s: %s: %w", path, a.Name.Value, err)
				}
			}
			values[a.Name.Value] = v
		}
	}
	return values, nil
}

func literal(parts []syntax.WordPart, quoted bool) (string, error) {
	var out strings.Builder
	for _, part := range parts {
		switch p := part.(type) {
		case *syntax.Lit:
			for i := 0; i < len(p.Value); i++ {
				c := p.Value[i]
				if c == '\\' && i+1 < len(p.Value) {
					n := p.Value[i+1]
					if !quoted || strings.ContainsRune("$`\"\\\n", rune(n)) {
						i++
						if n != '\n' {
							out.WriteByte(n)
						}
						continue
					}
				}
				out.WriteByte(c)
			}
		case *syntax.SglQuoted:
			if p.Dollar {
				return "", fmt.Errorf("ANSI-C quotes are not supported")
			}
			out.WriteString(p.Value)
		case *syntax.DblQuoted:
			if p.Dollar {
				return "", fmt.Errorf("shell translation is not supported")
			}
			v, err := literal(p.Parts, true)
			if err != nil {
				return "", err
			}
			out.WriteString(v)
		default:
			return "", fmt.Errorf("shell expansion is not supported; use a literal value")
		}
	}
	return out.String(), nil
}

func WriteJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".atm-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(b, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
