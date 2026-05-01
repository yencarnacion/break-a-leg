package risk

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"break-a-leg/internal/broker"
	"break-a-leg/internal/config"
	"break-a-leg/internal/marketdata"
)

type Decision struct {
	Allowed bool      `json:"allowed"`
	Reasons []string  `json:"reasons"`
	At      time.Time `json:"at"`
}

type AlertContext struct {
	ID          string
	Ticker      string
	CreatedAt   time.Time
	Watchlisted bool
	Snapshot    marketdata.Snapshot
}

type Engine struct {
	cfg            config.RiskConfig
	tradingEnabled bool
	now            func() time.Time
	mu             sync.Mutex
	recent         map[string]time.Time
	counts         map[string]int
}

func NewEngine(cfg config.RiskConfig, tradingEnabled bool) *Engine {
	return &Engine{cfg: cfg, tradingEnabled: tradingEnabled, now: time.Now, recent: map[string]time.Time{}, counts: map[string]int{}}
}

func (e *Engine) SetNow(fn func() time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = fn
}

func (e *Engine) Evaluate(intent broker.OrderIntent, alert AlertContext) Decision {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := e.now()
	var reasons []string
	if !e.tradingEnabled {
		reasons = append(reasons, "broker.trading_enabled is false")
	}
	if e.cfg.RequireManualClick && intent.UserSelectedButton == "" {
		reasons = append(reasons, "manual click is required")
	}
	if e.cfg.BlockIfTickerNotInWatchlist && !alert.Watchlisted {
		reasons = append(reasons, "ticker is not in watchlist")
	}
	if e.cfg.BlockIfNoMarketData && alert.Snapshot.LastPrice <= 0 {
		reasons = append(reasons, "no market data snapshot")
	}
	if e.cfg.BlockIfAlertStaleSeconds > 0 && !alert.CreatedAt.IsZero() && now.Sub(alert.CreatedAt) > time.Duration(e.cfg.BlockIfAlertStaleSeconds)*time.Second {
		reasons = append(reasons, "alert is stale")
	}
	if intent.Quantity <= 0 {
		reasons = append(reasons, "quantity must be positive")
	}
	if e.cfg.MaxSharesPerOrder > 0 && intent.Quantity > e.cfg.MaxSharesPerOrder {
		reasons = append(reasons, fmt.Sprintf("quantity exceeds max shares per order (%d)", e.cfg.MaxSharesPerOrder))
	}
	price := intent.LimitPrice
	if price <= 0 {
		price = alert.Snapshot.LastPrice
	}
	if e.cfg.MaxNotionalPerOrder > 0 && float64(intent.Quantity)*price > e.cfg.MaxNotionalPerOrder {
		reasons = append(reasons, fmt.Sprintf("notional exceeds max %.2f", e.cfg.MaxNotionalPerOrder))
	}
	if len(e.cfg.AllowedOrderTypes) > 0 && !containsFold(e.cfg.AllowedOrderTypes, intent.OrderType) {
		reasons = append(reasons, "order type is not allowed")
	}
	if e.cfg.MaxLimitPriceDeviationPercentFromLast > 0 && intent.LimitPrice > 0 && alert.Snapshot.LastPrice > 0 {
		dev := ((intent.LimitPrice - alert.Snapshot.LastPrice) / alert.Snapshot.LastPrice) * 100
		if dev < 0 {
			dev = -dev
		}
		if dev > e.cfg.MaxLimitPriceDeviationPercentFromLast {
			reasons = append(reasons, "limit price deviates too far from last price")
		}
	}
	if e.cfg.MaxSpreadPercent > 0 && alert.Snapshot.Bid > 0 && alert.Snapshot.Ask > 0 && alert.Snapshot.LastPrice > 0 {
		spread := ((alert.Snapshot.Ask - alert.Snapshot.Bid) / alert.Snapshot.LastPrice) * 100
		if spread > e.cfg.MaxSpreadPercent {
			reasons = append(reasons, "spread exceeds configured maximum")
		}
	}
	dupKey := intent.IdempotencyKey
	if dupKey == "" {
		dupKey = fmt.Sprintf("%s:%s:%d:%s", intent.SourceAlertID, intent.UserSelectedButton, intent.Quantity, strings.ToLower(intent.Side))
	}
	if cooldown := time.Duration(e.cfg.DuplicateOrderCooldownSeconds) * time.Second; cooldown > 0 {
		if last, ok := e.recent[dupKey]; ok && now.Sub(last) < cooldown {
			reasons = append(reasons, "duplicate order cooldown active")
		}
	}
	dayKey := alert.Ticker + ":" + now.Format("20060102")
	if e.cfg.MaxOrdersPerTickerPerDay > 0 && e.counts[dayKey] >= e.cfg.MaxOrdersPerTickerPerDay {
		reasons = append(reasons, "max orders per ticker per day reached")
	}

	allowed := len(reasons) == 0
	e.recent[dupKey] = now
	if allowed {
		e.counts[dayKey]++
	}
	return Decision{Allowed: allowed, Reasons: reasons, At: now}
}

func containsFold(list []string, needle string) bool {
	for _, item := range list {
		if strings.EqualFold(item, needle) {
			return true
		}
	}
	return false
}
