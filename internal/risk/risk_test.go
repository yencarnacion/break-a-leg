package risk

import (
	"testing"
	"time"

	"break-a-leg/internal/broker"
	"break-a-leg/internal/config"
	"break-a-leg/internal/marketdata"
)

func riskConfig() config.RiskConfig {
	return config.RiskConfig{
		MaxSharesPerOrder:             1000,
		MaxNotionalPerOrder:           2500,
		MaxOrdersPerTickerPerDay:      3,
		DuplicateOrderCooldownSeconds: 10,
		AllowedOrderTypes:             []string{"limit"},
		BlockIfNoMarketData:           true,
		BlockIfTickerNotInWatchlist:   true,
		BlockIfAlertStaleSeconds:      300,
	}
}

func TestRiskBlocksDuplicateOrder(t *testing.T) {
	e := NewEngine(riskConfig(), true)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	e.SetNow(func() time.Time { return now })
	alert := AlertContext{ID: "a1", Ticker: "ABCD", CreatedAt: now, Watchlisted: true, Snapshot: marketdata.Snapshot{Ticker: "ABCD", LastPrice: 2}}
	intent := broker.OrderIntent{IdempotencyKey: "same", Ticker: "ABCD", Side: "buy", Quantity: 100, OrderType: "limit", LimitPrice: 2.01, SourceAlertID: "a1", UserSelectedButton: "buy-small"}
	if d := e.Evaluate(intent, alert); !d.Allowed {
		t.Fatalf("first order should be allowed: %+v", d)
	}
	if d := e.Evaluate(intent, alert); d.Allowed {
		t.Fatalf("duplicate order should be blocked: %+v", d)
	}
}

func TestRiskBlocksOverMaxNotional(t *testing.T) {
	e := NewEngine(riskConfig(), true)
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	e.SetNow(func() time.Time { return now })
	alert := AlertContext{ID: "a1", Ticker: "ABCD", CreatedAt: now, Watchlisted: true, Snapshot: marketdata.Snapshot{Ticker: "ABCD", LastPrice: 10}}
	intent := broker.OrderIntent{IdempotencyKey: "notional", Ticker: "ABCD", Side: "buy", Quantity: 300, OrderType: "limit", LimitPrice: 10, SourceAlertID: "a1", UserSelectedButton: "buy-medium"}
	if d := e.Evaluate(intent, alert); d.Allowed {
		t.Fatalf("over-notional order should be blocked: %+v", d)
	}
}
