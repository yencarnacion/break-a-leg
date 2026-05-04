package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"break-a-leg/internal/broker"
	"break-a-leg/internal/burst"
	"break-a-leg/internal/config"
	"break-a-leg/internal/events"
	"break-a-leg/internal/llm"
	"break-a-leg/internal/marketdata"
	"break-a-leg/internal/news"
	"break-a-leg/internal/risk"
	"break-a-leg/internal/storage"
	"break-a-leg/internal/tts"
	"break-a-leg/internal/ui"
)

type app struct {
	cfg           config.Config
	store         storage.Store
	ui            *ui.Server
	tracker       *marketdata.Tracker
	detector      *burst.Detector
	newsProvider  news.Provider
	llmAnalyzer   llm.Analyzer
	ttsProvider   tts.Provider
	broker        broker.Broker
	riskEngine    *risk.Engine
	watchlist     map[string]bool
	companyNames  map[string]string
	symbols       []string
	mu            sync.Mutex
	alertByTicker map[string]string
	newsPolls     map[string]bool
	simOrders     map[string]*simOrder
}

type simOrder struct {
	ID         string
	Ticker     string
	Side       string
	Quantity   int64
	OpenPrice  float64
	OpenAt     time.Time
	ClosePrice float64
	CloseAt    *time.Time
}

func main() {
	configPath := flag.String("config", "config.yaml", "config file")
	watchlistOverride := flag.String("watchlist", "", "comma-separated watchlist file override")
	flag.Parse()

	_ = godotenv.Load(".env")
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if strings.TrimSpace(*watchlistOverride) != "" {
		cfg.Watchlists.Files = splitCSV(*watchlistOverride)
	}
	symbols, watchset, companyNames, err := config.LoadWatchlists(cfg.Watchlists.Files)
	if err != nil {
		log.Fatalf("load watchlists: %v", err)
	}
	store := storage.New("data")
	if err := store.Ensure(); err != nil {
		log.Fatalf("ensure data dirs: %v", err)
	}
	setupLogging(store)

	loc, err := time.LoadLocation(cfg.Session.Timezone)
	if err != nil {
		log.Printf("[config] load timezone %q failed: %v; using America/New_York", cfg.Session.Timezone, err)
		loc, _ = time.LoadLocation("America/New_York")
	}

	a := &app{
		cfg:           cfg,
		store:         store,
		tracker:       marketdata.NewTracker(marketdata.SessionClock{Location: loc, PremarketStart: cfg.Session.PremarketStart, RegularOpen: cfg.Session.RegularOpen, RegularClose: cfg.Session.RegularClose}),
		detector:      burst.NewDetector(cfg.Burst),
		newsProvider:  news.RTPRProvider{APIKey: os.Getenv("RTPR_API_KEY"), BaseURL: cfg.News.RestBaseURL, Freshness: news.FreshnessConfig{FreshMinutes: cfg.News.FreshMinutes, RecentHours: cfg.News.RecentHours}, Client: &http.Client{Timeout: time.Duration(cfg.News.TimeoutSeconds) * time.Second}},
		llmAnalyzer:   llm.PerplexityAnalyzer{APIKey: os.Getenv("PPLX_API_KEY"), BaseURL: cfg.LLM.BaseURL, SearchMode: cfg.LLM.SearchMode, Client: &http.Client{Timeout: time.Duration(cfg.LLM.TimeoutSeconds) * time.Second}},
		ttsProvider:   tts.OpenAIProvider{APIKey: os.Getenv("OPENAI_API_KEY"), Model: cfg.TTS.Model, Voice: cfg.TTS.Voice, OutputFormat: cfg.TTS.OutputFormat, Client: &http.Client{Timeout: 30 * time.Second}},
		broker:        broker.DummyBroker{TradingEnabled: cfg.Broker.TradingEnabled, DummyMode: true},
		riskEngine:    risk.NewEngine(cfg.Risk, cfg.Broker.TradingEnabled),
		watchlist:     watchset,
		companyNames:  companyNames,
		symbols:       symbols,
		alertByTicker: map[string]string{},
		newsPolls:     map[string]bool{},
		simOrders:     map[string]*simOrder{},
	}
	a.ui = ui.NewServer(cfg, a.handleTradeIntent, a.simBuy, a.simClose, a.simOrdersResponse, a.gappers, a.health)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticks := make(chan marketdata.Tick, 2048)
	provider := marketdata.MassiveProvider{APIKey: os.Getenv("MASSIVE_API_KEY"), RestBaseURL: cfg.MarketData.RestBaseURL, WebSocketURL: cfg.MarketData.WebSocketURL, ReconnectSeconds: cfg.MarketData.ReconnectSeconds, BackfillWorkers: cfg.MarketData.BackfillWorkers}
	go func() {
		provider.BackfillPremarket(ctx, symbols, loc, cfg.Session.PremarketStart, ticks)
		if err := provider.Run(ctx, symbols, ticks); err != nil && ctx.Err() == nil {
			log.Printf("[marketdata] stopped: %v", err)
		}
	}()
	go a.consumeTicks(ctx, ticks)
	go a.pollNoNewsAlerts(ctx)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{Addr: addr, Handler: a.ui.Routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("[server] listening at http://%s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

func (a *app) consumeTicks(ctx context.Context, ticks <-chan marketdata.Tick) {
	for {
		select {
		case <-ctx.Done():
			return
		case tick := <-ticks:
			if !a.watchlist[strings.ToUpper(tick.Ticker)] {
				continue
			}
			update := a.tracker.Update(tick)
			a.updateExistingAlert(update.Snapshot)
			if tick.Price <= 0 {
				continue
			}
			ev := a.detector.Evaluate(update)
			if ev.Kind != burst.TriggerNone {
				a.handleBurst(ev)
			}
		}
	}
}

func (a *app) handleBurst(ev burst.Event) {
	id := alertID(ev.Ticker, ev.LatestHODTime)
	alert := events.Alert{
		ID:             id,
		Ticker:         ev.Ticker,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		Snapshot:       ev.Snapshot,
		BurstStatus:    events.AlertStatus(ev.Kind),
		HODCount:       ev.HODCount,
		FirstHODTime:   ev.FirstHODTime,
		LatestHODTime:  ev.LatestHODTime,
		CooldownActive: ev.Cooldown,
		NewsStatus:     "pending",
		LLMStatus:      "pending",
		TTSStatus:      "pending",
		RiskStatus:     "not checked",
		BrokerMode:     brokerMode(a.cfg),
	}
	if ev.Kind == burst.TriggerSoft {
		alert.NewsStatus = "visual-only soft trigger"
		alert.LLMStatus = "skipped"
		alert.TTSStatus = "skipped"
	}
	if ev.Cooldown {
		alert.NewsStatus = "cooldown active"
		alert.LLMStatus = "skipped"
		alert.TTSStatus = "skipped"
	}
	a.mu.Lock()
	a.alertByTicker[ev.Ticker] = id
	a.mu.Unlock()
	a.publish(alert, "alerts", id)
	if ev.Kind == burst.TriggerFull && !ev.Cooldown {
		go a.runWorkflow(alert)
	}
}

func (a *app) runWorkflow(alert events.Alert) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(a.cfg.News.TimeoutSeconds)*time.Second)
	articles, err := a.newsProvider.Lookup(ctx, alert.Ticker, a.cfg.News.LookupLimit)
	cancel()
	if err != nil {
		alert.NewsStatus = "news lookup error"
		alert.NewsError = err.Error()
		alert.LLMStatus = "skipped"
		alert.TTSStatus = "skipped: no press release"
		a.publish(alert, "news", alert.ID)
		return
	}
	_, _ = a.store.WriteJSON("news", alert.ID, articles)
	article := news.PickNewestPreferred(articles)
	if article == nil {
		alert.NewsStatus = string(news.FreshnessNone)
		alert.LLMStatus = "skipped"
		if a.cfg.News.RunLLMIfNoNews && a.cfg.LLM.Enabled {
			alert.LLMStatus = "pending"
		}
		a.publish(alert, "alerts", alert.ID)
		if a.cfg.News.RunLLMIfNoNews && a.cfg.LLM.Enabled {
			a.runLLM(&alert, news.Article{
				Ticker:    alert.Ticker,
				Title:     "No fresh RTPR press release found",
				Source:    "RTPR",
				CreatedAt: time.Now(),
				Body:      "No RTPR article was returned for this HOD burst. Analyze only the market context and explicitly state that outside article context is unavailable.",
				Freshness: news.FreshnessNone,
			})
		}
		alert.TTSStatus = "skipped: no press release"
		a.publish(alert, "alerts", alert.ID)
		return
	}
	alert.Article = article
	alert.NewsStatus = string(article.Freshness)
	a.publish(alert, "alerts", alert.ID)
	if a.cfg.LLM.Enabled {
		a.runLLM(&alert, *article)
	} else {
		alert.LLMStatus = "disabled"
		a.publish(alert, "alerts", alert.ID)
	}
	a.runTTS(&alert, fmt.Sprintf("%s HOD burst, up %.0f percent. News: %s", alert.Ticker, alert.Snapshot.PercentChange, article.Title))
}

func (a *app) pollNoNewsAlerts(ctx context.Context) {
	for {
		timer := time.NewTimer(durationUntilSecond(time.Now(), 30))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			a.checkNoNewsAlerts(ctx)
		}
	}
}

func (a *app) checkNoNewsAlerts(ctx context.Context) {
	for _, alert := range a.ui.RecentAlerts() {
		if alert.BurstStatus != events.AlertStatusFull || alert.CooldownActive || alert.Article != nil || alert.NewsStatus != string(news.FreshnessNone) {
			continue
		}
		if a.markNewsPoll(alert.ID) {
			go a.checkNoNewsAlert(ctx, alert.ID)
		}
	}
}

func (a *app) checkNoNewsAlert(parent context.Context, alertID string) {
	defer a.clearNewsPoll(alertID)

	alert, ok := a.ui.GetAlert(alertID)
	if !ok || alert.Article != nil || alert.NewsStatus != string(news.FreshnessNone) {
		return
	}
	lookupCtx, cancel := context.WithTimeout(parent, time.Duration(a.cfg.News.TimeoutSeconds)*time.Second)
	articles, err := a.newsProvider.Lookup(lookupCtx, alert.Ticker, a.cfg.News.LookupLimit)
	cancel()
	if err != nil {
		alert.NewsError = "post-alert news poll: " + err.Error()
		alert.ProviderErrors = append(alert.ProviderErrors, alert.NewsError)
		a.publish(alert, "alerts", alert.ID)
		return
	}
	_, _ = a.store.WriteJSON("news", alert.ID+"-post-alert", articles)
	article := pickNewestAfter(articles, alert.CreatedAt)
	if article == nil {
		return
	}
	alert.Article = article
	alert.NewsStatus = string(article.Freshness)
	alert.NewsError = ""
	alert.LLMStatus = "pending"
	alert.LLMError = ""
	alert.LLMMarkdown = ""
	alert.TTSStatus = "pending"
	alert.TTSError = ""
	a.publish(alert, "alerts", alert.ID)
	log.Printf("[news] post-alert article found ticker=%s alert=%s headline=%q", alert.Ticker, alert.ID, article.Title)
	if a.cfg.LLM.Enabled {
		a.runLLM(&alert, *article)
	} else {
		alert.LLMStatus = "disabled"
		a.publish(alert, "alerts", alert.ID)
	}
	a.runTTS(&alert, fmt.Sprintf("%s HOD burst update. News: %s", alert.Ticker, article.Title))
}

func (a *app) runLLM(alert *events.Alert, article news.Article) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(a.cfg.LLM.TimeoutSeconds)*time.Second)
	defer cancel()
	md, err := a.llmAnalyzer.Analyze(ctx, llm.Request{PromptPath: a.cfg.LLM.PromptFile, Model: a.cfg.LLM.Model, ReasoningEffort: a.cfg.LLM.ReasoningEffort, Snapshot: alert.Snapshot, Article: article})
	if err != nil {
		alert.LLMStatus = "error"
		alert.LLMError = err.Error()
		a.publish(*alert, "alerts", alert.ID)
		return
	}
	alert.LLMStatus = "complete"
	alert.LLMMarkdown = md
	_, _ = a.store.WriteText("llm", alert.ID+".md", md)
	a.publish(*alert, "alerts", alert.ID)
}

func (a *app) runTTS(alert *events.Alert, line string) {
	if !a.cfg.TTS.Enabled {
		alert.TTSStatus = "disabled"
		a.publish(*alert, "alerts", alert.ID)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	audio, err := a.ttsProvider.Speak(ctx, truncate(line, 4096))
	if err != nil {
		alert.TTSStatus = "error"
		alert.TTSError = err.Error()
		a.publish(*alert, "alerts", alert.ID)
		return
	}
	path, err := a.store.WriteBytes("audio", fmt.Sprintf("%s-%d.%s", alert.ID, time.Now().UnixNano(), a.ttsProvider.Extension()), audio)
	if err != nil {
		alert.TTSStatus = "error"
		alert.TTSError = err.Error()
		a.publish(*alert, "alerts", alert.ID)
		return
	}
	alert.TTSStatus = "complete"
	alert.AudioPath = path
	a.publish(*alert, "alerts", alert.ID)
}

func (a *app) updateExistingAlert(snapshot marketdata.Snapshot) {
	a.mu.Lock()
	id := a.alertByTicker[snapshot.Ticker]
	a.mu.Unlock()
	if id == "" {
		return
	}
	alert, ok := a.ui.GetAlert(id)
	if !ok {
		return
	}
	alert.Snapshot = snapshot
	alert.UpdatedAt = time.Now()
	a.publish(alert, "alerts", id)
}

func (a *app) handleTradeIntent(alertID string, req ui.TradeRequest) (ui.TradeResponse, error) {
	alert, ok := a.ui.GetAlert(alertID)
	if !ok {
		return ui.TradeResponse{}, fmt.Errorf("alert not found")
	}
	button, ok := a.tradeButton(req.ButtonID)
	if !ok {
		return ui.TradeResponse{}, fmt.Errorf("unknown trade button %q", req.ButtonID)
	}
	quantity := button.Quantity
	if req.Quantity > 0 {
		quantity = req.Quantity
	}
	orderType := button.OrderType
	if req.OrderType != "" {
		orderType = req.OrderType
	}
	tif := button.TimeInForce
	if req.TimeInForce != "" {
		tif = req.TimeInForce
	}
	limit := req.LimitPrice
	if limit <= 0 {
		limit = derivedLimit(alert.Snapshot.LastPrice, button)
	}
	key := req.IdempotencyKey
	if key == "" {
		key = alert.ID + ":" + button.ID
	}
	intent := broker.OrderIntent{
		ID:                         "intent-" + shortHash(key+time.Now().String()),
		IdempotencyKey:             key,
		Ticker:                     alert.Ticker,
		Side:                       button.Side,
		Quantity:                   quantity,
		OrderType:                  orderType,
		LimitPrice:                 limit,
		TimeInForce:                tif,
		SourceAlertID:              alert.ID,
		CreatedAt:                  time.Now(),
		UserSelectedButton:         button.ID,
		CurrentMarketPriceSnapshot: alert.Snapshot.LastPrice,
		Bid:                        alert.Snapshot.Bid,
		Ask:                        alert.Snapshot.Ask,
	}
	decision := a.riskEngine.Evaluate(intent, risk.AlertContext{ID: alert.ID, Ticker: alert.Ticker, CreatedAt: alert.CreatedAt, Watchlisted: a.watchlist[alert.Ticker], Snapshot: alert.Snapshot})
	intent.RiskCheckResult = broker.RiskCheckResult{Allowed: decision.Allowed, Reasons: decision.Reasons}
	_, _ = a.store.WriteJSON("risk", storage.Name(alert.Ticker, intent.ID), decision)
	result := broker.OrderResult{Accepted: false, Broker: a.cfg.Broker.Provider, DummyMode: a.cfg.Broker.DummyMode, Reason: "blocked by risk guardrails", ReceivedAt: time.Now(), IdempotencyKey: intent.IdempotencyKey}
	var callErr error
	if decision.Allowed {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		result, callErr = a.broker.PlaceOrderIntent(ctx, intent)
		cancel()
	}
	record := events.TradeResultRecord{At: time.Now(), Intent: intent, Risk: decision, Broker: result}
	if callErr != nil {
		record.Error = callErr.Error()
	}
	alert.TradeResults = append(alert.TradeResults, record)
	alert.RiskStatus = statusFromDecision(decision)
	alert.UpdatedAt = time.Now()
	a.publish(alert, "trades", storage.Name(alert.Ticker, intent.ID))
	return ui.TradeResponse{Intent: intent, Record: record}, callErr
}

func (a *app) simBuy(req ui.SimOrderRequest) (ui.SimOrderRow, error) {
	qty := req.Quantity
	if qty <= 0 {
		return ui.SimOrderRow{}, fmt.Errorf("quantity must be positive")
	}
	side := strings.ToLower(strings.TrimSpace(req.Side))
	if side == "" {
		side = "buy"
	}
	if side != "buy" && side != "sell" {
		return ui.SimOrderRow{}, fmt.Errorf("side must be buy or sell")
	}
	ticker := strings.ToUpper(strings.TrimSpace(req.Ticker))
	if ticker == "" && strings.TrimSpace(req.AlertID) != "" {
		alert, ok := a.ui.GetAlert(req.AlertID)
		if !ok {
			return ui.SimOrderRow{}, fmt.Errorf("alert not found")
		}
		ticker = alert.Ticker
	}
	if ticker == "" {
		return ui.SimOrderRow{}, fmt.Errorf("ticker is required")
	}
	snapshot, ok := a.tracker.Snapshot(ticker)
	if !ok || snapshot.LastPrice <= 0 {
		return ui.SimOrderRow{}, fmt.Errorf("no market data for %s", ticker)
	}
	openPrice := snapshot.Ask
	if side == "sell" {
		openPrice = snapshot.Bid
	}
	if openPrice <= 0 {
		openPrice = snapshot.LastPrice
	}
	if side == "sell" {
		openPrice -= 0.10
	} else {
		openPrice += 0.10
	}
	now := time.Now()
	order := &simOrder{
		ID:        "sim-" + shortHash(fmt.Sprintf("%s:%s:%d:%d", ticker, side, qty, now.UnixNano())),
		Ticker:    ticker,
		Side:      side,
		Quantity:  qty,
		OpenPrice: openPrice,
		OpenAt:    now,
	}
	a.mu.Lock()
	a.simOrders[order.ID] = order
	a.mu.Unlock()
	_, _ = a.store.WriteJSON("trades", storage.Name(ticker, order.ID), order)
	return a.simOrderRow(order), nil
}

func (a *app) simClose(id string) (ui.SimOrderRow, error) {
	a.mu.Lock()
	order := a.simOrders[id]
	a.mu.Unlock()
	if order == nil {
		return ui.SimOrderRow{}, fmt.Errorf("sim order not found")
	}
	if order.CloseAt != nil {
		return a.simOrderRow(order), nil
	}
	snapshot, ok := a.tracker.Snapshot(order.Ticker)
	if !ok || snapshot.LastPrice <= 0 {
		return ui.SimOrderRow{}, fmt.Errorf("no market data for %s", order.Ticker)
	}
	closePrice := snapshot.Bid
	if simOrderSide(order) == "sell" {
		closePrice = snapshot.Ask
	}
	if closePrice <= 0 {
		closePrice = snapshot.LastPrice
	}
	if simOrderSide(order) == "sell" {
		closePrice += 0.10
	} else {
		closePrice -= 0.10
	}
	now := time.Now()
	a.mu.Lock()
	order.ClosePrice = closePrice
	order.CloseAt = &now
	a.mu.Unlock()
	_, _ = a.store.WriteJSON("trades", storage.Name(order.Ticker, order.ID+"-close"), order)
	return a.simOrderRow(order), nil
}

func (a *app) simOrdersResponse() ui.SimOrdersResponse {
	a.mu.Lock()
	orders := make([]*simOrder, 0, len(a.simOrders))
	for _, order := range a.simOrders {
		orders = append(orders, order)
	}
	a.mu.Unlock()
	sort.Slice(orders, func(i, j int) bool { return orders[i].OpenAt.After(orders[j].OpenAt) })
	var resp ui.SimOrdersResponse
	for _, order := range orders {
		row := a.simOrderRow(order)
		resp.Orders = append(resp.Orders, row)
		resp.RealizedPL += row.RealizedPL
		resp.UnrealizedPL += row.UnrealizedPL
	}
	resp.TotalPL = resp.RealizedPL + resp.UnrealizedPL
	return resp
}

func (a *app) simOrderRow(order *simOrder) ui.SimOrderRow {
	side := simOrderSide(order)
	row := ui.SimOrderRow{
		ID:        order.ID,
		Ticker:    order.Ticker,
		Side:      side,
		Quantity:  order.Quantity,
		OpenPrice: order.OpenPrice,
		OpenAt:    order.OpenAt,
		Status:    "open",
	}
	snapshot, ok := a.tracker.Snapshot(order.Ticker)
	if ok {
		row.LastPrice = snapshot.LastPrice
		exitEstimate := snapshot.Bid
		if side == "sell" {
			exitEstimate = snapshot.Ask
		}
		if exitEstimate <= 0 {
			exitEstimate = snapshot.LastPrice
		}
		if exitEstimate > 0 {
			if side == "sell" {
				row.ExitEstimate = exitEstimate + 0.10
			} else {
				row.ExitEstimate = exitEstimate - 0.10
			}
		}
	}
	if order.CloseAt != nil {
		row.Status = "closed"
		row.ClosePrice = order.ClosePrice
		row.CloseAt = order.CloseAt
		row.ExitEstimate = order.ClosePrice
		row.RealizedPL = simOrderPL(side, order.OpenPrice, order.ClosePrice, order.Quantity)
		row.TotalPL = row.RealizedPL
		return row
	}
	if row.ExitEstimate > 0 {
		row.UnrealizedPL = simOrderPL(side, order.OpenPrice, row.ExitEstimate, order.Quantity)
		row.TotalPL = row.UnrealizedPL
	}
	return row
}

func simOrderSide(order *simOrder) string {
	if strings.ToLower(strings.TrimSpace(order.Side)) == "sell" {
		return "sell"
	}
	return "buy"
}

func simOrderPL(side string, openPrice, closePrice float64, quantity int64) float64 {
	if side == "sell" {
		return (openPrice - closePrice) * float64(quantity)
	}
	return (closePrice - openPrice) * float64(quantity)
}

func (a *app) gappers() []ui.GapperRow {
	minPct := a.cfg.Gappers.MinPercentChange
	if minPct == 0 {
		minPct = 10
	}
	minVol := a.cfg.Gappers.MinPremarketVolume
	if minVol == 0 {
		minVol = 20000
	}
	newsByTicker := map[string]events.Alert{}
	for _, alert := range a.ui.RecentAlerts() {
		if alert.Article == nil || alert.Article.Title == "" {
			continue
		}
		if existing, ok := newsByTicker[alert.Ticker]; !ok || alert.UpdatedAt.After(existing.UpdatedAt) {
			newsByTicker[alert.Ticker] = alert
		}
	}
	rows := make([]ui.GapperRow, 0)
	for _, snapshot := range a.tracker.Snapshots() {
		if !a.watchlist[snapshot.Ticker] {
			continue
		}
		if snapshot.PercentChange < minPct || snapshot.PremarketCumulativeVolume < minVol {
			continue
		}
		newsAlert, hasNews := newsByTicker[snapshot.Ticker]
		headline := ""
		newsURL := ""
		newsStatus := ""
		if hasNews {
			headline = newsAlert.Article.Title
			newsURL = newsAlert.Article.URL
			newsStatus = newsAlert.NewsStatus
		}
		rows = append(rows, ui.GapperRow{
			Ticker:          snapshot.Ticker,
			LastPrice:       snapshot.LastPrice,
			PercentChange:   snapshot.PercentChange,
			PremarketVolume: snapshot.PremarketCumulativeVolume,
			CompanyName:     a.companyNames[snapshot.Ticker],
			HasNews:         hasNews,
			NewsStatus:      newsStatus,
			NewsHeadline:    headline,
			NewsURL:         newsURL,
			LastUpdate:      snapshot.LastUpdate,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].PercentChange == rows[j].PercentChange {
			return rows[i].Ticker < rows[j].Ticker
		}
		return rows[i].PercentChange > rows[j].PercentChange
	})
	return rows
}

func (a *app) tradeButton(id string) (config.TradeButtonConfig, bool) {
	for _, button := range a.cfg.TradeButtons {
		if button.ID == id {
			return button, true
		}
	}
	return config.TradeButtonConfig{}, false
}

func (a *app) publish(alert events.Alert, kind, name string) {
	alert.UpdatedAt = time.Now()
	a.ui.UpsertAlert(alert)
	if _, err := a.store.WriteJSON(kind, name, alert); err != nil {
		log.Printf("[storage] write %s/%s: %v", kind, name, err)
	}
}

func (a *app) health() events.Health {
	return events.Health{
		OK:                    true,
		Now:                   time.Now(),
		MarketProvider:        a.cfg.MarketData.Provider,
		NewsProvider:          a.cfg.News.Provider,
		LLMProvider:           a.cfg.LLM.Provider,
		TTSProvider:           a.cfg.TTS.Provider,
		BrokerProvider:        a.cfg.Broker.Provider,
		DummyMode:             a.cfg.Broker.DummyMode,
		TradingEnabled:        a.cfg.Broker.TradingEnabled,
		WatchlistCount:        len(a.symbols),
		ChartOpenerBaseURL:    a.cfg.UI.ChartOpenerBaseURL,
		GappersRefreshSeconds: a.cfg.Gappers.RefreshSeconds,
	}
}

func setupLogging(_ storage.Store) {
	f, err := os.OpenFile("data/logs/app.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("open app log failed: %v", err)
		return
	}
	log.SetOutput(io.MultiWriter(os.Stdout, f))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func alertID(ticker string, ts time.Time) string {
	if ts.IsZero() {
		ts = time.Now()
	}
	return strings.ToUpper(ticker) + "-" + ts.Format("20060102-150405.000")
}

func shortHash(v string) string {
	sum := sha1.Sum([]byte(v))
	return hex.EncodeToString(sum[:])[:12]
}

func brokerMode(cfg config.Config) string {
	if cfg.Broker.DummyMode {
		if !cfg.Broker.TradingEnabled {
			return "DUMMY MODE / trading disabled"
		}
		return "DUMMY MODE"
	}
	return "LIVE MODE"
}

func derivedLimit(last float64, button config.TradeButtonConfig) float64 {
	if last <= 0 {
		return 0
	}
	switch button.LimitPriceMode {
	case "last_plus_percent":
		return last * (1 + button.LimitPriceOffsetPercent/100)
	case "last_minus_percent":
		return last * (1 - button.LimitPriceOffsetPercent/100)
	default:
		return last
	}
}

func statusFromDecision(d risk.Decision) string {
	if d.Allowed {
		return "allowed"
	}
	if len(d.Reasons) == 0 {
		return "blocked"
	}
	return "blocked: " + strings.Join(d.Reasons, "; ")
}

func truncate(v string, max int) string {
	if len(v) <= max {
		return v
	}
	return v[:max]
}

func durationUntilSecond(now time.Time, second int) time.Duration {
	if second < 0 {
		second = 0
	}
	if second > 59 {
		second = 59
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute(), second, 0, now.Location())
	if !next.After(now) {
		next = next.Add(time.Minute)
	}
	return next.Sub(now)
}

func pickNewestAfter(articles []news.Article, after time.Time) *news.Article {
	var picked *news.Article
	for i := range articles {
		a := articles[i]
		if a.CreatedAt.IsZero() || !a.CreatedAt.After(after) {
			continue
		}
		if picked == nil || a.CreatedAt.After(picked.CreatedAt) {
			cp := a
			picked = &cp
		}
	}
	return picked
}

func (a *app) markNewsPoll(alertID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.newsPolls[alertID] {
		return false
	}
	a.newsPolls[alertID] = true
	return true
}

func (a *app) clearNewsPoll(alertID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.newsPolls, alertID)
}
