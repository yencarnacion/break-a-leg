package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadWatchlistsSupportsTickerList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watchlist.yaml")
	if err := os.WriteFile(path, []byte("tickers:\n  - abcd\n  - XYZ\n  - abcd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	symbols, seen, names, err := LoadWatchlists([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 2 || symbols[0] != "ABCD" || symbols[1] != "XYZ" {
		t.Fatalf("unexpected symbols: %#v", symbols)
	}
	if !seen["ABCD"] || !seen["XYZ"] {
		t.Fatalf("missing seen entries: %#v", seen)
	}
	if len(names) != 0 {
		t.Fatalf("unexpected names: %#v", names)
	}
}

func TestLoadWatchlistsSupportsAlertcatStyleSymbolEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "watchlist.yaml")
	body := "watchlist:\n - symbol: \"AACG\"\n   name: \"ATA Creativity Global\"\n - symbol: \"abts\"\n - symbol: \"AACG\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	symbols, seen, names, err := LoadWatchlists([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 2 || symbols[0] != "AACG" || symbols[1] != "ABTS" {
		t.Fatalf("unexpected symbols: %#v", symbols)
	}
	if !seen["AACG"] || !seen["ABTS"] {
		t.Fatalf("missing seen entries: %#v", seen)
	}
	if names["AACG"] != "ATA Creativity Global" {
		t.Fatalf("missing company name: %#v", names)
	}
}

func TestLoadLeavesOmittedTTSVoiceEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := "tts:\n  base_url: \"http://marvin:9084/v1/audio/speech\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TTS.Voice != "" {
		t.Fatalf("expected omitted TTS voice to stay empty, got %q", cfg.TTS.Voice)
	}
}
