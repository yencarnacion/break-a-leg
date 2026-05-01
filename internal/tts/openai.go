package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Provider interface {
	Speak(ctx context.Context, text string) ([]byte, error)
	Extension() string
}

type OpenAIProvider struct {
	APIKey       string
	Model        string
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
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is empty")
	}
	model := p.Model
	if model == "" {
		model = "gpt-4o-mini-tts"
	}
	voice := p.Voice
	if voice == "" {
		voice = "alloy"
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/audio/speech", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
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
