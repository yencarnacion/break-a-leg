package main

import (
	"math"
	"testing"
	"time"

	"break-a-leg/internal/marketdata"
	"break-a-leg/internal/news"
	"break-a-leg/internal/storage"
	"break-a-leg/internal/ui"
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

func TestSimSellCanCloseWithShortPL(t *testing.T) {
	a := &app{
		store:     storage.New(t.TempDir()),
		tracker:   marketdata.NewTracker(marketdata.SessionClock{}),
		simOrders: map[string]*simOrder{},
	}
	a.tracker.Update(marketdata.Tick{Ticker: "ABCD", Price: 10, Bid: 9.90, Ask: 10.10, At: time.Now()})

	open, err := a.simBuy(ui.SimOrderRequest{Ticker: "ABCD", Side: "sell", Quantity: 100})
	if err != nil {
		t.Fatalf("sim sell: %v", err)
	}
	if open.Side != "sell" {
		t.Fatalf("expected sell side, got %q", open.Side)
	}
	if !near(open.OpenPrice, 9.80) {
		t.Fatalf("expected open price 9.80, got %.4f", open.OpenPrice)
	}

	a.tracker.Update(marketdata.Tick{Ticker: "ABCD", Price: 9, Bid: 8.90, Ask: 9.10, At: time.Now()})
	closed, err := a.simClose(open.ID)
	if err != nil {
		t.Fatalf("sim close: %v", err)
	}
	if closed.Status != "closed" {
		t.Fatalf("expected closed status, got %q", closed.Status)
	}
	if !near(closed.ClosePrice, 9.20) {
		t.Fatalf("expected close price 9.20, got %.4f", closed.ClosePrice)
	}
	if !near(closed.RealizedPL, 60) {
		t.Fatalf("expected realized P/L 60, got %.4f", closed.RealizedPL)
	}
}

func near(got, want float64) bool {
	return math.Abs(got-want) < 0.000001
}
