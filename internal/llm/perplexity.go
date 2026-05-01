package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type PerplexityAnalyzer struct {
	APIKey     string
	BaseURL    string
	SearchMode string
	Client     *http.Client
}

func (a PerplexityAnalyzer) Analyze(ctx context.Context, req Request) (string, error) {
	if strings.TrimSpace(a.APIKey) == "" {
		return "", fmt.Errorf("PPLX_API_KEY is empty")
	}
	prompt, err := os.ReadFile(req.PromptPath)
	if err != nil {
		return "", err
	}
	baseURL := strings.TrimSpace(a.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.perplexity.ai/chat/completions"
	}
	searchMode := strings.TrimSpace(a.SearchMode)
	if searchMode == "" {
		searchMode = "sec"
	}
	input := perplexityContext(string(prompt), req)
	body := map[string]any{
		"model": req.Model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": input,
			},
		},
		"search_mode": searchMode,
		"stream":      true,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Accept", "application/json")
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
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("perplexity status %d: %s", resp.StatusCode, string(data))
	}
	return readPerplexityStream(resp.Body)
}

func perplexityContext(prompt string, req Request) string {
	return prompt + `

---

Use Perplexity SEC search mode for OUTSIDE CONTEXT. Prefer SEC filings, offering/prospectus documents, recent 8-K/10-Q/10-K/S-1 filings, exhibit agreements, and company filing history when relevant. Use that outside context to evaluate dilution risk, promotional timing, cash/runway clues, financing support, filing proximity, and whether the press release is meaningfully new.

If SEC/outside context is unavailable or not found, say "outside context unavailable" rather than guessing.

` + marketContext(req)
}

func readPerplexityStream(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var out strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data: "))
		if payload == "[DONE]" {
			break
		}
		var ev struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return "", err
		}
		if len(ev.Choices) > 0 {
			out.WriteString(ev.Choices[0].Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	text := strings.TrimSpace(out.String())
	if text == "" {
		return "", fmt.Errorf("perplexity response contained no output text")
	}
	return text, nil
}
