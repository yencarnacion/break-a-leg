package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"break-a-leg/internal/marketdata"
	"break-a-leg/internal/news"
)

type Request struct {
	PromptPath      string
	Model           string
	ReasoningEffort string
	Snapshot        marketdata.Snapshot
	Article         news.Article
}

type Analyzer interface {
	Analyze(ctx context.Context, req Request) (string, error)
}

type OpenAIAnalyzer struct {
	APIKey string
	Client *http.Client
}

func (a OpenAIAnalyzer) Analyze(ctx context.Context, req Request) (string, error) {
	if strings.TrimSpace(a.APIKey) == "" {
		return "", fmt.Errorf("OPENAI_API_KEY is empty")
	}
	prompt, err := os.ReadFile(req.PromptPath)
	if err != nil {
		return "", err
	}
	input := string(prompt) + "\n\n---\n\n" + marketContext(req)
	body := map[string]any{
		"model": req.Model,
		"input": input,
		"reasoning": map[string]any{
			"effort": req.ReasoningEffort,
		},
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai responses status %d: %s", resp.StatusCode, string(data))
	}
	var parsed responsePayload
	if err := json.Unmarshal(data, &parsed); err != nil {
		return "", err
	}
	if parsed.OutputText != "" {
		return parsed.OutputText, nil
	}
	var parts []string
	for _, item := range parsed.Output {
		for _, c := range item.Content {
			if c.Type == "output_text" && strings.TrimSpace(c.Text) != "" {
				parts = append(parts, c.Text)
			}
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("openai response contained no output text")
	}
	return strings.Join(parts, "\n\n"), nil
}

type responsePayload struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func marketContext(req Request) string {
	s := req.Snapshot
	a := req.Article
	return fmt.Sprintf(`Market context:
- ticker: %s
- current price: %.4f
- percent change: %.2f%%
- premarket volume: %d
- number of HOD events in burst window: %d
- first HOD timestamp: %s
- latest HOD timestamp: %s
- article title: %s
- article source: %s
- article created time: %s

Article body:
%s
`, s.Ticker, s.LastPrice, s.PercentChange, s.PremarketCumulativeVolume, len(s.HODEventTimes), firstTime(s.HODEventTimes), lastTime(s.HODEventTimes), a.Title, a.Source, a.CreatedAt.Format(time.RFC3339), a.Body)
}

func firstTime(times []time.Time) string {
	if len(times) == 0 {
		return ""
	}
	return times[0].Format(time.RFC3339)
}

func lastTime(times []time.Time) string {
	if len(times) == 0 {
		return ""
	}
	return times[len(times)-1].Format(time.RFC3339)
}
