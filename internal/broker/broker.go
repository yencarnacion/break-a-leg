package broker

import (
	"context"
	"fmt"
	"log"
	"time"
)

type Broker interface {
	PlaceOrderIntent(ctx context.Context, intent OrderIntent) (OrderResult, error)
}

type RiskCheckResult struct {
	Allowed bool     `json:"allowed"`
	Reasons []string `json:"reasons"`
}

type OrderIntent struct {
	ID                         string          `json:"id"`
	IdempotencyKey             string          `json:"idempotency_key"`
	Ticker                     string          `json:"ticker"`
	Side                       string          `json:"side"`
	Quantity                   int64           `json:"quantity"`
	OrderType                  string          `json:"order_type"`
	LimitPrice                 float64         `json:"limit_price"`
	TimeInForce                string          `json:"time_in_force"`
	SourceAlertID              string          `json:"source_alert_id"`
	CreatedAt                  time.Time       `json:"created_at"`
	UserSelectedButton         string          `json:"user_selected_button"`
	CurrentMarketPriceSnapshot float64         `json:"current_market_price_snapshot"`
	Bid                        float64         `json:"bid,omitempty"`
	Ask                        float64         `json:"ask,omitempty"`
	RiskCheckResult            RiskCheckResult `json:"risk_check_result"`
}

type OrderResult struct {
	Accepted       bool      `json:"accepted"`
	Broker         string    `json:"broker"`
	DummyMode      bool      `json:"dummy_mode"`
	OrderID        string    `json:"order_id,omitempty"`
	Reason         string    `json:"reason,omitempty"`
	ReceivedAt     time.Time `json:"received_at"`
	IdempotencyKey string    `json:"idempotency_key"`
}

type DummyBroker struct {
	TradingEnabled bool
	DummyMode      bool
}

func (b DummyBroker) PlaceOrderIntent(ctx context.Context, intent OrderIntent) (OrderResult, error) {
	select {
	case <-ctx.Done():
		return OrderResult{}, ctx.Err()
	default:
	}
	log.Printf("[broker] dummy intent ticker=%s side=%s qty=%d type=%s limit=%.4f key=%s", intent.Ticker, intent.Side, intent.Quantity, intent.OrderType, intent.LimitPrice, intent.IdempotencyKey)
	if !b.TradingEnabled {
		return OrderResult{
			Accepted:       false,
			Broker:         "dummy",
			DummyMode:      true,
			Reason:         "blocked by config: broker.trading_enabled is false",
			ReceivedAt:     time.Now(),
			IdempotencyKey: intent.IdempotencyKey,
		}, nil
	}
	return OrderResult{
		Accepted:       true,
		Broker:         "dummy",
		DummyMode:      true,
		OrderID:        fmt.Sprintf("DUMMY-%d", time.Now().UnixNano()),
		ReceivedAt:     time.Now(),
		IdempotencyKey: intent.IdempotencyKey,
	}, nil
}
