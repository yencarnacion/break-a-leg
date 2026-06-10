package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIProviderUsesCustomSpeechEndpointWithoutAPIKey(t *testing.T) {
	var gotAuth string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/audio/speech" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte("wav-bytes"))
	}))
	defer server.Close()

	provider := OpenAIProvider{
		BaseURL:      server.URL + "/v1/audio/speech",
		Model:        "tts-1",
		Voice:        "default",
		OutputFormat: "wav",
		Client:       server.Client(),
	}
	audio, err := provider.Speak(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "wav-bytes" {
		t.Fatalf("unexpected audio: %q", string(audio))
	}
	if gotAuth != "" {
		t.Fatalf("expected no authorization header, got %q", gotAuth)
	}
	if gotBody["model"] != "tts-1" || gotBody["voice"] != "default" || gotBody["input"] != "hello" || gotBody["response_format"] != "wav" {
		t.Fatalf("unexpected body: %#v", gotBody)
	}
}

func TestOpenAIProviderDiscoversDefaultVoiceForLocalEndpoint(t *testing.T) {
	var sawVoices bool
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/voices":
			if r.Method != http.MethodGet {
				t.Fatalf("expected GET, got %s", r.Method)
			}
			sawVoices = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`["Alice.wav","Bob.wav"]`))
		case "/v1/audio/speech":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			_, _ = w.Write([]byte("wav-bytes"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := OpenAIProvider{
		BaseURL:      server.URL + "/v1/audio/speech",
		Model:        "tts-1",
		OutputFormat: "wav",
		Client:       server.Client(),
	}
	audio, err := provider.Speak(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if string(audio) != "wav-bytes" {
		t.Fatalf("unexpected audio: %q", string(audio))
	}
	if !sawVoices {
		t.Fatal("expected voice list request")
	}
	if gotBody["voice"] != "Alice.wav" {
		t.Fatalf("expected discovered voice, got body: %#v", gotBody)
	}
}

func TestOpenAIProviderFailsClearlyWhenLocalEndpointHasNoVoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/voices" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	provider := OpenAIProvider{
		BaseURL: server.URL + "/v1/audio/speech",
		Client:  server.Client(),
	}
	_, err := provider.Speak(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--voice SomeVoice.wav") {
		t.Fatalf("expected actionable voice error, got: %v", err)
	}
}

func TestOpenAIProviderRequiresAPIKeyForDefaultOpenAIEndpoint(t *testing.T) {
	_, err := OpenAIProvider{}.Speak(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected missing API key error")
	}
}
