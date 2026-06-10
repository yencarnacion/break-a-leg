package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

type Provider interface {
	Speak(ctx context.Context, text string) ([]byte, error)
	Extension() string
}

type OpenAIProvider struct {
	APIKey       string
	Model        string
	BaseURL      string
	Voice        string
	OutputFormat string
	Client       *http.Client
}

func (p OpenAIProvider) Extension() string {
	if strings.TrimSpace(p.OutputFormat) == "" {
		return "mp3"
	}
	return strings.TrimPrefix(strings.ToLower(p.OutputFormat), ".")
}

func (p OpenAIProvider) Speak(ctx context.Context, text string) ([]byte, error) {
	endpoint := strings.TrimSpace(p.BaseURL)
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/audio/speech"
	}
	apiKey := strings.TrimSpace(p.APIKey)
	if apiKey == "" && isOpenAIEndpoint(endpoint) {
		return nil, fmt.Errorf("OPENAI_API_KEY is empty")
	}
	model := p.Model
	if model == "" {
		model = "gpt-4o-mini-tts"
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	voice := strings.TrimSpace(p.Voice)
	if voice == "" {
		if isOpenAIEndpoint(endpoint) {
			voice = "alloy"
		} else {
			var err error
			voice, err = p.defaultLocalVoice(ctx, endpoint, client, apiKey)
			if err != nil {
				return nil, err
			}
		}
	}
	body := map[string]any{
		"model":           model,
		"voice":           voice,
		"input":           text,
		"response_format": p.Extension(),
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai speech status %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func isOpenAIEndpoint(endpoint string) bool {
	return strings.Contains(strings.ToLower(endpoint), "api.openai.com")
}

func (p OpenAIProvider) defaultLocalVoice(ctx context.Context, speechEndpoint string, client *http.Client, apiKey string) (string, error) {
	voicesEndpoint, err := voiceListEndpoint(speechEndpoint)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, voicesEndpoint, nil)
	if err != nil {
		return "", err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("list local TTS voices: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("list local TTS voices status %d: %s", resp.StatusCode, string(data))
	}
	voice, err := firstVoice(data)
	if err != nil {
		return "", fmt.Errorf("list local TTS voices: %w", err)
	}
	return voice, nil
}

func voiceListEndpoint(speechEndpoint string) (string, error) {
	u, err := url.Parse(speechEndpoint)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(u.Path, "/")
	switch {
	case strings.HasSuffix(basePath, "/audio/speech"):
		u.Path = strings.TrimSuffix(basePath, "/speech") + "/voices"
	case strings.HasSuffix(basePath, "/v1"):
		u.Path = basePath + "/audio/voices"
	default:
		u.Path = basePath + "/audio/voices"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func firstVoice(data []byte) (string, error) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", err
	}
	voice := findFirstVoice(raw)
	if voice == "" {
		return "", fmt.Errorf("no voices returned; pass --voice SomeVoice.wav or set tts.voice")
	}
	return voice, nil
}

func findFirstVoice(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		for _, item := range v {
			if voice := findFirstVoice(item); voice != "" {
				return voice
			}
		}
	case map[string]any:
		for _, key := range []string{"voices", "data", "items", "results"} {
			if voice := findFirstVoice(v[key]); voice != "" {
				return voice
			}
		}
		for _, key := range []string{"filename", "file", "name", "voice", "id", "path"} {
			if voice := findFirstVoice(v[key]); voice != "" {
				return voice
			}
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if voice := findFirstVoice(v[key]); voice != "" {
				return voice
			}
		}
	}
	return ""
}
