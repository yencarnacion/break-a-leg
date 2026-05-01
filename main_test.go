package main

import (
	"testing"
	"time"

	"break-a-leg/internal/news"
)

func TestDurationUntilSecond(t *testing.T) {
	now := time.Date(2026, 5, 1, 10, 15, 10, 0, time.UTC)
	if got := durationUntilSecond(now, 30); got != 20*time.Second {
		t.Fatalf("expected 20s, got %s", got)
	}
	now = time.Date(2026, 5, 1, 10, 15, 31, 0, time.UTC)
	if got := durationUntilSecond(now, 30); got != 59*time.Second {
		t.Fatalf("expected 59s, got %s", got)
	}
}

func TestPickNewestAfter(t *testing.T) {
	alertAt := time.Date(2026, 5, 1, 10, 15, 0, 0, time.UTC)
	articles := []news.Article{
		{Title: "before", CreatedAt: alertAt.Add(-time.Minute)},
		{Title: "first after", CreatedAt: alertAt.Add(30 * time.Second)},
		{Title: "newest after", CreatedAt: alertAt.Add(time.Minute)},
	}
	got := pickNewestAfter(articles, alertAt)
	if got == nil || got.Title != "newest after" {
		t.Fatalf("unexpected article: %#v", got)
	}
}

func TestPickNewestAfterIgnoresEarlierArticles(t *testing.T) {
	alertAt := time.Date(2026, 5, 1, 10, 15, 0, 0, time.UTC)
	articles := []news.Article{{Title: "before", CreatedAt: alertAt.Add(-time.Second)}}
	if got := pickNewestAfter(articles, alertAt); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}
