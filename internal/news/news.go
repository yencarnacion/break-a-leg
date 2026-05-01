package news

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Freshness string

const (
	FreshnessFresh  Freshness = "fresh"
	FreshnessRecent Freshness = "recent"
	FreshnessOld    Freshness = "old"
	FreshnessNone   Freshness = "none"
)

type Article struct {
	Ticker    string    `json:"ticker"`
	Exchange  string    `json:"exchange"`
	Title     string    `json:"title"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	Body      string    `json:"article_body"`
	URL       string    `json:"url"`
	Freshness Freshness `json:"freshness"`
}

type FreshnessConfig struct {
	FreshMinutes int
	RecentHours  int
}

type Provider interface {
	Lookup(ctx context.Context, ticker string, limit int) ([]Article, error)
}

func Classify(created, now time.Time, cfg FreshnessConfig) Freshness {
	if created.IsZero() {
		return FreshnessOld
	}
	fresh := time.Duration(cfg.FreshMinutes) * time.Minute
	if fresh <= 0 {
		fresh = 30 * time.Minute
	}
	recent := time.Duration(cfg.RecentHours) * time.Hour
	if recent <= 0 {
		recent = 4 * time.Hour
	}
	age := now.Sub(created)
	if age <= fresh {
		return FreshnessFresh
	}
	if age <= recent {
		return FreshnessRecent
	}
	return FreshnessOld
}

func PickNewestPreferred(articles []Article) *Article {
	if len(articles) == 0 {
		return nil
	}
	sort.SliceStable(articles, func(i, j int) bool {
		if articles[i].Freshness == FreshnessFresh && articles[j].Freshness != FreshnessFresh {
			return true
		}
		if articles[i].Freshness != FreshnessFresh && articles[j].Freshness == FreshnessFresh {
			return false
		}
		return articles[i].CreatedAt.After(articles[j].CreatedAt)
	})
	return &articles[0]
}

type RTPRProvider struct {
	APIKey    string
	BaseURL   string
	Client    *http.Client
	Freshness FreshnessConfig
}

func (p RTPRProvider) Lookup(ctx context.Context, ticker string, limit int) ([]Article, error) {
	if strings.TrimSpace(p.APIKey) == "" {
		return nil, fmt.Errorf("RTPR_API_KEY is empty")
	}
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		base = "https://api.rtpr.io"
	}
	if limit <= 0 {
		limit = 5
	}
	url := fmt.Sprintf("%s/articles/%s?limit=%d", base, strings.ToUpper(ticker), limit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("rtpr status %d", resp.StatusCode)
	}
	var raw any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	rows := articleRows(raw)
	out := make([]Article, 0, len(rows))
	now := time.Now()
	for _, row := range rows {
		a := Article{
			Ticker:    firstString(row, "ticker", "symbol"),
			Exchange:  firstString(row, "exchange"),
			Title:     firstString(row, "title", "headline"),
			Source:    firstString(row, "author", "source", "publisher"),
			Body:      firstString(row, "article_body", "body", "content", "text"),
			URL:       firstString(row, "url", "link"),
			CreatedAt: firstTime(row, "created", "created_at", "published_at", "timestamp"),
		}
		if a.Ticker == "" {
			a.Ticker = strings.ToUpper(ticker)
		}
		a.Freshness = Classify(a.CreatedAt, now, p.Freshness)
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func articleRows(v any) []map[string]any {
	switch x := v.(type) {
	case []any:
		return mapSlice(x)
	case map[string]any:
		for _, key := range []string{"articles", "data", "results"} {
			if arr, ok := x[key].([]any); ok {
				return mapSlice(arr)
			}
		}
		return []map[string]any{x}
	default:
		return nil
	}
}

func mapSlice(in []any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, item := range in {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := row[key]; ok {
			switch x := v.(type) {
			case string:
				if strings.TrimSpace(x) != "" {
					return strings.TrimSpace(x)
				}
			}
		}
	}
	return ""
}

func firstTime(row map[string]any, keys ...string) time.Time {
	for _, key := range keys {
		v, ok := row[key]
		if !ok {
			continue
		}
		switch x := v.(type) {
		case string:
			for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
				if ts, err := time.Parse(layout, x); err == nil {
					return ts
				}
			}
		case float64:
			if x > 1e12 {
				return time.UnixMilli(int64(x))
			}
			return time.Unix(int64(x), 0)
		}
	}
	return time.Time{}
}
