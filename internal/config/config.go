package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig        `yaml:"server"`
	Session      SessionConfig       `yaml:"session"`
	Watchlists   WatchlistsConfig    `yaml:"watchlists"`
	MarketData   MarketDataConfig    `yaml:"market_data"`
	Burst        BurstConfig         `yaml:"burst"`
	Gappers      GappersConfig       `yaml:"gappers"`
	News         NewsConfig          `yaml:"news"`
	LLM          LLMConfig           `yaml:"llm"`
	TTS          TTSConfig           `yaml:"tts"`
	UI           UIConfig            `yaml:"ui"`
	Broker       BrokerConfig        `yaml:"broker"`
	Risk         RiskConfig          `yaml:"risk"`
	TradeButtons []TradeButtonConfig `yaml:"trade_buttons"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type SessionConfig struct {
	Timezone       string `yaml:"timezone"`
	PremarketStart string `yaml:"premarket_start"`
	RegularOpen    string `yaml:"regular_open"`
	RegularClose   string `yaml:"regular_close"`
}

type WatchlistsConfig struct {
	Files []string `yaml:"files"`
}

type MarketDataConfig struct {
	Provider         string `yaml:"provider"`
	RestBaseURL      string `yaml:"rest_base_url"`
	WebSocketURL     string `yaml:"websocket_url"`
	ReconnectSeconds int    `yaml:"reconnect_seconds"`
	BackfillWorkers  int    `yaml:"backfill_workers"`
}

type BurstConfig struct {
	Enabled                     bool    `yaml:"enabled"`
	MinPremarketVolume          int64   `yaml:"min_premarket_volume"`
	FullTriggerMinPercentChange float64 `yaml:"full_trigger_min_percent_change"`
	SoftTriggerEnabled          bool    `yaml:"soft_trigger_enabled"`
	SoftTriggerMinPercentChange float64 `yaml:"soft_trigger_min_percent_change"`
	HODCountRequired            int     `yaml:"hod_count_required"`
	HODWindowSeconds            int     `yaml:"hod_window_seconds"`
	WorkflowCooldownSeconds     int     `yaml:"workflow_cooldown_seconds"`
}

type GappersConfig struct {
	MinPercentChange   float64 `yaml:"min_percent_change"`
	MinPremarketVolume int64   `yaml:"min_premarket_volume"`
	RefreshSeconds     int     `yaml:"refresh_seconds"`
}

type NewsConfig struct {
	Provider       string `yaml:"provider"`
	RestBaseURL    string `yaml:"rest_base_url"`
	LookupLimit    int    `yaml:"lookup_limit"`
	FreshMinutes   int    `yaml:"fresh_minutes"`
	RecentHours    int    `yaml:"recent_hours"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	RunLLMIfNoNews bool   `yaml:"run_llm_if_no_news"`
}

type LLMConfig struct {
	Provider        string `yaml:"provider"`
	Model           string `yaml:"model"`
	BaseURL         string `yaml:"base_url"`
	SearchMode      string `yaml:"search_mode"`
	ReasoningEffort string `yaml:"reasoning_effort"`
	PromptFile      string `yaml:"prompt_file"`
	TimeoutSeconds  int    `yaml:"timeout_seconds"`
	Enabled         bool   `yaml:"enabled"`
}

type TTSConfig struct {
	Provider     string `yaml:"provider"`
	Model        string `yaml:"model"`
	Voice        string `yaml:"voice"`
	OutputFormat string `yaml:"output_format"`
	Enabled      bool   `yaml:"enabled"`
	AutoplayInUI bool   `yaml:"autoplay_in_ui"`
}

type UIConfig struct {
	ChartOpenerBaseURL string `yaml:"chart_opener_base_url"`
	MaxAlertCards      int    `yaml:"max_alert_cards"`
}

type BrokerConfig struct {
	Provider       string `yaml:"provider"`
	DummyMode      bool   `yaml:"dummy_mode"`
	TradingEnabled bool   `yaml:"trading_enabled"`
}

type RiskConfig struct {
	MaxSharesPerOrder                     int64    `yaml:"max_shares_per_order"`
	MaxNotionalPerOrder                   float64  `yaml:"max_notional_per_order"`
	MaxOrdersPerTickerPerDay              int      `yaml:"max_orders_per_ticker_per_day"`
	DuplicateOrderCooldownSeconds         int      `yaml:"duplicate_order_cooldown_seconds"`
	AllowedOrderTypes                     []string `yaml:"allowed_order_types"`
	MaxLimitPriceDeviationPercentFromLast float64  `yaml:"max_limit_price_deviation_percent_from_last"`
	RequireManualClick                    bool     `yaml:"require_manual_click"`
	BlockIfNoMarketData                   bool     `yaml:"block_if_no_market_data"`
	BlockIfTickerNotInWatchlist           bool     `yaml:"block_if_ticker_not_in_watchlist"`
	BlockIfAlertStaleSeconds              int      `yaml:"block_if_alert_stale_seconds"`
	MaxSpreadPercent                      float64  `yaml:"max_spread_percent"`
}

type TradeButtonConfig struct {
	ID                      string  `yaml:"id" json:"id"`
	Label                   string  `yaml:"label" json:"label"`
	Side                    string  `yaml:"side" json:"side"`
	Quantity                int64   `yaml:"quantity" json:"quantity"`
	OrderType               string  `yaml:"order_type" json:"order_type"`
	LimitPriceMode          string  `yaml:"limit_price_mode" json:"limit_price_mode"`
	LimitPriceOffsetPercent float64 `yaml:"limit_price_offset_percent" json:"limit_price_offset_percent"`
	TimeInForce             string  `yaml:"time_in_force" json:"time_in_force"`
}

type Watchlist struct {
	Tickers   []string         `yaml:"tickers"`
	Watchlist []WatchlistEntry `yaml:"watchlist"`
}

type WatchlistEntry struct {
	Symbol string `yaml:"symbol"`
	Name   string `yaml:"name,omitempty"`
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, err
	}
	cfg.applyDefaults()
	return cfg, nil
}

func Defaults() Config {
	cfg := Config{}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 8087
	cfg.Session.Timezone = "America/New_York"
	cfg.Session.PremarketStart = "04:00"
	cfg.Session.RegularOpen = "09:30"
	cfg.Session.RegularClose = "16:00"
	cfg.Watchlists.Files = []string{"watchlist.yaml"}
	cfg.MarketData.Provider = "massive"
	cfg.MarketData.RestBaseURL = "https://api.massive.com"
	cfg.MarketData.WebSocketURL = "wss://socket.massive.com/stocks"
	cfg.MarketData.ReconnectSeconds = 5
	cfg.MarketData.BackfillWorkers = 6
	cfg.Burst.Enabled = true
	cfg.Burst.MinPremarketVolume = 20000
	cfg.Burst.FullTriggerMinPercentChange = 10
	cfg.Burst.SoftTriggerEnabled = true
	cfg.Burst.SoftTriggerMinPercentChange = 5
	cfg.Burst.HODCountRequired = 2
	cfg.Burst.HODWindowSeconds = 60
	cfg.Burst.WorkflowCooldownSeconds = 300
	cfg.Gappers.MinPercentChange = 10
	cfg.Gappers.MinPremarketVolume = 20000
	cfg.Gappers.RefreshSeconds = 5
	cfg.News.Provider = "rtpr"
	cfg.News.RestBaseURL = "https://api.rtpr.io"
	cfg.News.LookupLimit = 5
	cfg.News.FreshMinutes = 30
	cfg.News.RecentHours = 4
	cfg.News.TimeoutSeconds = 10
	cfg.LLM.Provider = "perplexity"
	cfg.LLM.Model = "sonar-pro"
	cfg.LLM.BaseURL = "https://api.perplexity.ai/chat/completions"
	cfg.LLM.SearchMode = "sec"
	cfg.LLM.ReasoningEffort = "low"
	cfg.LLM.PromptFile = "prompts/news_analysis.md"
	cfg.LLM.TimeoutSeconds = 90
	cfg.LLM.Enabled = true
	cfg.TTS.Provider = "openai"
	cfg.TTS.Model = "gpt-4o-mini-tts"
	cfg.TTS.Voice = "alloy"
	cfg.TTS.OutputFormat = "mp3"
	cfg.TTS.Enabled = true
	cfg.UI.ChartOpenerBaseURL = "http://localhost:8081"
	cfg.UI.MaxAlertCards = 200
	cfg.Broker.Provider = "dummy"
	cfg.Broker.DummyMode = true
	cfg.Risk.MaxSharesPerOrder = 1000
	cfg.Risk.MaxNotionalPerOrder = 2500
	cfg.Risk.MaxOrdersPerTickerPerDay = 3
	cfg.Risk.DuplicateOrderCooldownSeconds = 10
	cfg.Risk.AllowedOrderTypes = []string{"limit"}
	cfg.Risk.MaxLimitPriceDeviationPercentFromLast = 3
	cfg.Risk.RequireManualClick = true
	cfg.Risk.BlockIfNoMarketData = true
	cfg.Risk.BlockIfTickerNotInWatchlist = true
	cfg.Risk.BlockIfAlertStaleSeconds = 300
	cfg.TradeButtons = []TradeButtonConfig{
		{ID: "buy-small", Label: "Buy small", Side: "buy", Quantity: 100, OrderType: "limit", LimitPriceMode: "last_plus_percent", LimitPriceOffsetPercent: 0.25, TimeInForce: "DAY"},
		{ID: "buy-medium", Label: "Buy medium", Side: "buy", Quantity: 300, OrderType: "limit", LimitPriceMode: "last_plus_percent", LimitPriceOffsetPercent: 0.25, TimeInForce: "DAY"},
		{ID: "buy-large", Label: "Buy large", Side: "buy", Quantity: 500, OrderType: "limit", LimitPriceMode: "last_plus_percent", LimitPriceOffsetPercent: 0.25, TimeInForce: "DAY"},
	}
	return cfg
}

func (c *Config) applyDefaults() {
	d := Defaults()
	if c.Server.Host == "" {
		c.Server.Host = d.Server.Host
	}
	if c.Server.Port == 0 {
		c.Server.Port = d.Server.Port
	}
	if c.Session.Timezone == "" {
		c.Session.Timezone = d.Session.Timezone
	}
	if c.Session.PremarketStart == "" {
		c.Session.PremarketStart = d.Session.PremarketStart
	}
	if c.Session.RegularOpen == "" {
		c.Session.RegularOpen = d.Session.RegularOpen
	}
	if c.Session.RegularClose == "" {
		c.Session.RegularClose = d.Session.RegularClose
	}
	if len(c.Watchlists.Files) == 0 {
		c.Watchlists.Files = d.Watchlists.Files
	}
	if c.MarketData.Provider == "" {
		c.MarketData.Provider = d.MarketData.Provider
	}
	if c.MarketData.RestBaseURL == "" {
		c.MarketData.RestBaseURL = d.MarketData.RestBaseURL
	}
	if c.MarketData.WebSocketURL == "" {
		c.MarketData.WebSocketURL = d.MarketData.WebSocketURL
	}
	if c.MarketData.ReconnectSeconds == 0 {
		c.MarketData.ReconnectSeconds = d.MarketData.ReconnectSeconds
	}
	if c.MarketData.BackfillWorkers == 0 {
		c.MarketData.BackfillWorkers = d.MarketData.BackfillWorkers
	}
	if c.Burst.HODCountRequired == 0 {
		c.Burst.HODCountRequired = d.Burst.HODCountRequired
	}
	if c.Burst.HODWindowSeconds == 0 {
		c.Burst.HODWindowSeconds = d.Burst.HODWindowSeconds
	}
	if c.Burst.WorkflowCooldownSeconds == 0 {
		c.Burst.WorkflowCooldownSeconds = d.Burst.WorkflowCooldownSeconds
	}
	if c.Gappers.MinPercentChange == 0 {
		c.Gappers.MinPercentChange = d.Gappers.MinPercentChange
	}
	if c.Gappers.MinPremarketVolume == 0 {
		c.Gappers.MinPremarketVolume = d.Gappers.MinPremarketVolume
	}
	if c.Gappers.RefreshSeconds == 0 {
		c.Gappers.RefreshSeconds = d.Gappers.RefreshSeconds
	}
	if c.News.RestBaseURL == "" {
		c.News.RestBaseURL = d.News.RestBaseURL
	}
	if c.News.LookupLimit == 0 {
		c.News.LookupLimit = d.News.LookupLimit
	}
	if c.News.FreshMinutes == 0 {
		c.News.FreshMinutes = d.News.FreshMinutes
	}
	if c.News.RecentHours == 0 {
		c.News.RecentHours = d.News.RecentHours
	}
	if c.News.TimeoutSeconds == 0 {
		c.News.TimeoutSeconds = d.News.TimeoutSeconds
	}
	if c.LLM.Model == "" {
		c.LLM.Model = d.LLM.Model
	}
	if c.LLM.BaseURL == "" {
		c.LLM.BaseURL = d.LLM.BaseURL
	}
	if c.LLM.SearchMode == "" {
		c.LLM.SearchMode = d.LLM.SearchMode
	}
	if c.LLM.ReasoningEffort == "" {
		c.LLM.ReasoningEffort = d.LLM.ReasoningEffort
	}
	if c.LLM.PromptFile == "" {
		c.LLM.PromptFile = d.LLM.PromptFile
	}
	if c.LLM.TimeoutSeconds == 0 {
		c.LLM.TimeoutSeconds = d.LLM.TimeoutSeconds
	}
	if c.TTS.Model == "" {
		c.TTS.Model = d.TTS.Model
	}
	if c.TTS.Voice == "" {
		c.TTS.Voice = d.TTS.Voice
	}
	if c.TTS.OutputFormat == "" {
		c.TTS.OutputFormat = d.TTS.OutputFormat
	}
	if c.UI.MaxAlertCards == 0 {
		c.UI.MaxAlertCards = d.UI.MaxAlertCards
	}
	if c.Broker.Provider == "" {
		c.Broker.Provider = d.Broker.Provider
	}
	if c.Risk.MaxSharesPerOrder == 0 {
		c.Risk.MaxSharesPerOrder = d.Risk.MaxSharesPerOrder
	}
	if c.Risk.MaxNotionalPerOrder == 0 {
		c.Risk.MaxNotionalPerOrder = d.Risk.MaxNotionalPerOrder
	}
	if c.Risk.MaxOrdersPerTickerPerDay == 0 {
		c.Risk.MaxOrdersPerTickerPerDay = d.Risk.MaxOrdersPerTickerPerDay
	}
	if c.Risk.DuplicateOrderCooldownSeconds == 0 {
		c.Risk.DuplicateOrderCooldownSeconds = d.Risk.DuplicateOrderCooldownSeconds
	}
	if len(c.Risk.AllowedOrderTypes) == 0 {
		c.Risk.AllowedOrderTypes = d.Risk.AllowedOrderTypes
	}
	if c.Risk.BlockIfAlertStaleSeconds == 0 {
		c.Risk.BlockIfAlertStaleSeconds = d.Risk.BlockIfAlertStaleSeconds
	}
	if len(c.TradeButtons) == 0 {
		c.TradeButtons = d.TradeButtons
	}
}

func LoadWatchlists(paths []string) ([]string, map[string]bool, map[string]string, error) {
	seen := map[string]bool{}
	names := map[string]string{}
	var ordered []string
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		var wl Watchlist
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load watchlist %s: %w", path, err)
		}
		if err := yaml.Unmarshal(b, &wl); err != nil {
			return nil, nil, nil, fmt.Errorf("parse watchlist %s: %w", path, err)
		}
		add := func(t, name string) {
			s := strings.ToUpper(strings.TrimSpace(t))
			if s == "" {
				return
			}
			if strings.TrimSpace(name) != "" && names[s] == "" {
				names[s] = strings.TrimSpace(name)
			}
			if seen[s] {
				return
			}
			seen[s] = true
			ordered = append(ordered, s)
		}
		for _, t := range wl.Tickers {
			add(t, "")
		}
		for _, entry := range wl.Watchlist {
			add(entry.Symbol, entry.Name)
		}
	}
	if len(ordered) == 0 {
		return nil, nil, nil, fmt.Errorf("watchlist is empty")
	}
	return ordered, seen, names, nil
}

func SessionTimes(loc *time.Location, start, open string, now time.Time) (time.Time, time.Time, error) {
	parse := func(v string) (int, int, error) {
		t, err := time.Parse("15:04", v)
		if err != nil {
			return 0, 0, err
		}
		return t.Hour(), t.Minute(), nil
	}
	sh, sm, err := parse(start)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	oh, om, err := parse(open)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	n := now.In(loc)
	return time.Date(n.Year(), n.Month(), n.Day(), sh, sm, 0, 0, loc), time.Date(n.Year(), n.Month(), n.Day(), oh, om, 0, 0, loc), nil
}
