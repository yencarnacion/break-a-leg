package events

import (
	"time"

	"break-a-leg/internal/broker"
	"break-a-leg/internal/marketdata"
	"break-a-leg/internal/news"
	"break-a-leg/internal/risk"
)

type AlertStatus string

const (
	AlertStatusSoft     AlertStatus = "soft"
	AlertStatusFull     AlertStatus = "full"
	AlertStatusUpdating AlertStatus = "updating"
	AlertStatusCooldown AlertStatus = "cooldown"
)

type MarketTick struct {
	Ticker string    `json:"ticker"`
	Price  float64   `json:"price"`
	Size   int64     `json:"size"`
	At     time.Time `json:"at"`
}

type HODEvent struct {
	Ticker string    `json:"ticker"`
	Price  float64   `json:"price"`
	At     time.Time `json:"at"`
}

type LODEvent = HODEvent

type BurstEvent struct {
	AlertID string `json:"alert_id"`
	Ticker  string `json:"ticker"`
	Kind    string `json:"kind"`
}

type NewsLookupRequested struct{ AlertID, Ticker string }
type NewsLookupCompleted struct{ AlertID, Ticker, Status, Error string }
type LLMAnalysisRequested struct{ AlertID, Ticker string }
type LLMAnalysisCompleted struct{ AlertID, Ticker, Error string }
type TTSRequested struct{ AlertID, Ticker string }
type TTSCompleted struct{ AlertID, Ticker, AudioPath, Error string }
type AlertCardUpdated struct{ AlertID, Ticker string }
type TradeIntentRequested struct{ AlertID, Ticker, ButtonID string }
type RiskCheckCompleted struct {
	AlertID, Ticker string
	Allowed         bool
}
type BrokerOrderIntentCompleted struct {
	AlertID, Ticker string
	Accepted        bool
}

type Alert struct {
	ID             string              `json:"id"`
	Ticker         string              `json:"ticker"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	Snapshot       marketdata.Snapshot `json:"snapshot"`
	BurstStatus    AlertStatus         `json:"burst_status"`
	HODCount       int                 `json:"hod_count"`
	FirstHODTime   time.Time           `json:"first_hod_time"`
	LatestHODTime  time.Time           `json:"latest_hod_time"`
	CooldownActive bool                `json:"cooldown_active"`
	NewsStatus     string              `json:"news_status"`
	NewsError      string              `json:"news_error,omitempty"`
	Article        *news.Article       `json:"article,omitempty"`
	LLMStatus      string              `json:"llm_status"`
	LLMError       string              `json:"llm_error,omitempty"`
	LLMMarkdown    string              `json:"llm_markdown,omitempty"`
	TTSStatus      string              `json:"tts_status"`
	TTSError       string              `json:"tts_error,omitempty"`
	AudioPath      string              `json:"audio_path,omitempty"`
	RiskStatus     string              `json:"risk_status"`
	BrokerMode     string              `json:"broker_mode"`
	TradeResults   []TradeResultRecord `json:"trade_results"`
	ProviderErrors []string            `json:"provider_errors,omitempty"`
}

type TradeResultRecord struct {
	At     time.Time          `json:"at"`
	Intent broker.OrderIntent `json:"intent"`
	Risk   risk.Decision      `json:"risk"`
	Broker broker.OrderResult `json:"broker"`
	Error  string             `json:"error,omitempty"`
}

type Health struct {
	OK                    bool      `json:"ok"`
	Now                   time.Time `json:"now"`
	MarketProvider        string    `json:"market_provider"`
	NewsProvider          string    `json:"news_provider"`
	LLMProvider           string    `json:"llm_provider"`
	TTSProvider           string    `json:"tts_provider"`
	BrokerProvider        string    `json:"broker_provider"`
	DummyMode             bool      `json:"dummy_mode"`
	TradingEnabled        bool      `json:"trading_enabled"`
	WatchlistCount        int       `json:"watchlist_count"`
	ChartOpenerBaseURL    string    `json:"chart_opener_base_url"`
	GappersRefreshSeconds int       `json:"gappers_refresh_seconds"`
}
