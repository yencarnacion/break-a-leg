package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Store struct {
	BaseDir string
}

func New(base string) Store {
	if strings.TrimSpace(base) == "" {
		base = "data"
	}
	return Store{BaseDir: base}
}

func (s Store) Ensure() error {
	for _, d := range []string{"alerts", "audio", "logs", "news", "llm", "trades", "risk"} {
		if err := os.MkdirAll(filepath.Join(s.BaseDir, d), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) WriteJSON(kind, name string, v any) (string, error) {
	path := filepath.Join(s.BaseDir, kind, safeName(name)+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, b, 0o644)
}

func (s Store) WriteText(kind, name, text string) (string, error) {
	path := filepath.Join(s.BaseDir, kind, safeName(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, []byte(text), 0o644)
}

func (s Store) WriteBytes(kind, name string, data []byte) (string, error) {
	path := filepath.Join(s.BaseDir, kind, safeName(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

func Name(prefix, id string) string {
	return time.Now().Format("20060102_150405") + "_" + safeName(prefix) + "_" + safeName(id)
}

func safeName(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "unnamed"
	}
	repl := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "\t", "_", "\n", "_")
	return repl.Replace(v)
}
