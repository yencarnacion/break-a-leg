package ui

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"break-a-leg/internal/broker"
	"break-a-leg/internal/config"
	"break-a-leg/internal/events"
)

type GapperRow struct {
	Ticker          string    `json:"ticker"`
	LastPrice       float64   `json:"last_price"`
	PercentChange   float64   `json:"percent_change"`
	PremarketVolume int64     `json:"premarket_volume"`
	CompanyName     string    `json:"company_name,omitempty"`
	HasNews         bool      `json:"has_news"`
	NewsStatus      string    `json:"news_status,omitempty"`
	NewsHeadline    string    `json:"news_headline,omitempty"`
	NewsURL         string    `json:"news_url,omitempty"`
	LastUpdate      time.Time `json:"last_update"`
}

type TradeRequest struct {
	ButtonID       string  `json:"button_id"`
	Quantity       int64   `json:"quantity,omitempty"`
	LimitPrice     float64 `json:"limit_price,omitempty"`
	OrderType      string  `json:"order_type,omitempty"`
	TimeInForce    string  `json:"time_in_force,omitempty"`
	IdempotencyKey string  `json:"idempotency_key,omitempty"`
}

type TradeResponse struct {
	Intent broker.OrderIntent       `json:"intent"`
	Record events.TradeResultRecord `json:"record"`
}

type SimOrderRequest struct {
	AlertID  string `json:"alert_id,omitempty"`
	Ticker   string `json:"ticker,omitempty"`
	Side     string `json:"side,omitempty"`
	Quantity int64  `json:"quantity"`
}

type SimOrderRow struct {
	ID           string     `json:"id"`
	Ticker       string     `json:"ticker"`
	Side         string     `json:"side"`
	Quantity     int64      `json:"quantity"`
	OpenPrice    float64    `json:"open_price"`
	OpenAt       time.Time  `json:"open_at"`
	ClosePrice   float64    `json:"close_price,omitempty"`
	CloseAt      *time.Time `json:"close_at,omitempty"`
	Status       string     `json:"status"`
	LastPrice    float64    `json:"last_price"`
	ExitEstimate float64    `json:"exit_estimate"`
	UnrealizedPL float64    `json:"unrealized_pl"`
	RealizedPL   float64    `json:"realized_pl"`
	TotalPL      float64    `json:"total_pl"`
}

type SimOrdersResponse struct {
	Orders       []SimOrderRow `json:"orders"`
	RealizedPL   float64       `json:"realized_pl"`
	UnrealizedPL float64       `json:"unrealized_pl"`
	TotalPL      float64       `json:"total_pl"`
}

type Server struct {
	cfg          config.Config
	mu           sync.RWMutex
	alerts       map[string]*events.Alert
	subscribers  map[chan events.Alert]struct{}
	tradeHandler func(alertID string, req TradeRequest) (TradeResponse, error)
	simBuy       func(req SimOrderRequest) (SimOrderRow, error)
	simClose     func(id string) (SimOrderRow, error)
	simOrders    func() SimOrdersResponse
	gappers      func() []GapperRow
	health       func() events.Health
}

func NewServer(cfg config.Config, tradeHandler func(string, TradeRequest) (TradeResponse, error), simBuy func(SimOrderRequest) (SimOrderRow, error), simClose func(string) (SimOrderRow, error), simOrders func() SimOrdersResponse, gappers func() []GapperRow, health func() events.Health) *Server {
	return &Server{cfg: cfg, alerts: map[string]*events.Alert{}, subscribers: map[chan events.Alert]struct{}{}, tradeHandler: tradeHandler, simBuy: simBuy, simClose: simClose, simOrders: simOrders, gappers: gappers, health: health}
}

func (s *Server) UpsertAlert(alert events.Alert) {
	s.mu.Lock()
	cp := alert
	s.alerts[alert.ID] = &cp
	for ch := range s.subscribers {
		select {
		case ch <- cp:
		default:
		}
	}
	s.mu.Unlock()
}

func (s *Server) GetAlert(id string) (events.Alert, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.alerts[id]
	if !ok {
		return events.Alert{}, false
	}
	return *a, true
}

func (s *Server) RecentAlerts() []events.Alert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]events.Alert, 0, len(s.alerts))
	for _, a := range s.alerts {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if max := s.cfg.UI.MaxAlertCards; max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/alerts/", s.handleAlertPage)
	mux.HandleFunc("/web/", http.StripPrefix("/web/", http.FileServer(http.Dir("web"))).ServeHTTP)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/gappers", s.handleGappers)
	mux.HandleFunc("/api/sim/orders/", s.handleSimOrderPath)
	mux.HandleFunc("/api/sim/orders", s.handleSimOrders)
	mux.HandleFunc("/api/alerts", s.handleAlerts)
	mux.HandleFunc("/api/alerts/", s.handleAlertPath)
	return mux
}

func (s *Server) handleAlertPage(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/alerts/"), "/")
	if id == "" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	alert, ok := s.GetAlert(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, alertPageHTML(alert))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, "web/index.html")
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.health())
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, s.RecentAlerts())
}

func (s *Server) handleGappers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.gappers == nil {
		writeJSON(w, []GapperRow{})
		return
	}
	writeJSON(w, s.gappers())
}

func (s *Server) handleSimOrders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if s.simOrders == nil {
			writeJSON(w, SimOrdersResponse{})
			return
		}
		writeJSON(w, s.simOrders())
	case http.MethodPost:
		if s.simBuy == nil {
			http.Error(w, "simulator unavailable", http.StatusServiceUnavailable)
			return
		}
		var req SimOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		row, err := s.simBuy(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, row)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSimOrderPath(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/sim/orders/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "close" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if s.simClose == nil {
		http.Error(w, "simulator unavailable", http.StatusServiceUnavailable)
		return
	}
	row, err := s.simClose(parts[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, row)
}

func (s *Server) handleAlertPath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/alerts/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 1 && r.Method == http.MethodGet {
		a, ok := s.GetAlert(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, a)
		return
	}
	if len(parts) == 2 && parts[1] == "audio" && r.Method == http.MethodGet {
		a, ok := s.GetAlert(id)
		if !ok || a.AudioPath == "" {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(a.AudioPath); err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, a.AudioPath)
		return
	}
	if len(parts) == 2 && parts[1] == "trade-intent" && r.Method == http.MethodPost {
		if s.tradeHandler == nil {
			http.Error(w, "trade handler unavailable", http.StatusServiceUnavailable)
			return
		}
		var req TradeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, err := s.tradeHandler(id, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, resp)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan events.Alert, 32)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subscribers, ch)
		s.mu.Unlock()
		close(ch)
	}()

	for _, a := range s.RecentAlerts() {
		writeSSE(w, a)
	}
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case a := <-ch:
			writeSSE(w, a)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, a events.Alert) {
	b, _ := json.Marshal(a)
	fmt.Fprintf(w, "event: alert\ndata: %s\n\n", b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func alertPageHTML(a events.Alert) string {
	articleTitle := ""
	articleBody := "No article body available."
	articleSource := ""
	articleURL := ""
	articleCreated := ""
	if a.Article != nil {
		articleTitle = a.Article.Title
		articleBody = a.Article.Body
		articleSource = a.Article.Source
		articleURL = a.Article.URL
		if !a.Article.CreatedAt.IsZero() {
			articleCreated = a.Article.CreatedAt.Format(time.RFC1123)
		}
	}
	newsLink := ""
	if articleURL != "" {
		newsLink = fmt.Sprintf(`<a href="%s" target="_blank" rel="noreferrer">Open press release</a>`, html.EscapeString(articleURL))
	}
	bidText := "-"
	if a.Snapshot.Bid > 0 {
		bidText = fmt.Sprintf("$%.4f", a.Snapshot.Bid)
	} else if a.Snapshot.LastPrice > 0 {
		bidText = fmt.Sprintf("$%.4f", a.Snapshot.LastPrice)
	}
	askText := "-"
	if a.Snapshot.Ask > 0 {
		askText = fmt.Sprintf("$%.4f", a.Snapshot.Ask)
	} else if a.Snapshot.LastPrice > 0 {
		askText = fmt.Sprintf("$%.4f", a.Snapshot.LastPrice)
	}
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>%s analysis</title>
  <link rel="stylesheet" href="/web/styles.css">
</head>
<body class="detail-page">
  <main class="detail-shell" data-alert-id="%s">
    <header class="detail-hero">
      <div>
        <p class="detail-kicker"><span id="detail-burst">%s</span> · <span id="detail-updated">%s</span></p>
        <h1 id="detail-ticker">%s</h1>
        <p id="detail-news-status">%s</p>
      </div>
      <div class="detail-stats">
        <div><span>Best bid</span><strong id="detail-bid">%s</strong></div>
        <div><span>Best offer</span><strong id="detail-ask">%s</strong></div>
        <div><span>Last</span><strong id="detail-last">$%.4f</strong></div>
        <div><span>Change</span><strong id="detail-change">%.2f%%</strong></div>
        <div><span>Volume since 04:00</span><strong id="detail-volume">%d</strong></div>
        <div><span>HOD count</span><strong id="detail-hods">%d</strong></div>
      </div>
    </header>
    <section class="detail-section">
      <h2>Press Release</h2>
      <p id="detail-source" class="detail-source">%s %s</p>
      <h3 id="detail-headline">%s</h3>
      <p id="detail-news-link">%s</p>
      <div id="detail-article" class="article-full">%s</div>
    </section>
    <section class="detail-section analysis-section">
      <h2>LLM Analysis</h2>
      <div id="detail-analysis" class="markdown detail-markdown">%s</div>
    </section>
    <section class="detail-section detail-trade">
      <h2>Paper Trade</h2>
      <div class="detail-actions">
        <button class="detail-sim-order detail-sim-buy" type="button" data-side="buy" data-qty="100">Buy 100</button>
        <button class="detail-sim-order detail-sim-buy" type="button" data-side="buy" data-qty="1000">Buy 1000</button>
        <button class="detail-sim-order detail-sim-sell" type="button" data-side="sell" data-qty="100">Sell 100</button>
        <button class="detail-sim-order detail-sim-sell" type="button" data-side="sell" data-qty="1000">Sell 1000</button>
        <label class="detail-custom-buy">
          <span>Custom shares</span>
          <input id="detail-custom-qty" type="number" min="1" step="1" value="100">
        </label>
        <button id="detail-buy-custom" class="detail-sim-order detail-sim-buy" type="button" data-side="buy">Buy custom</button>
        <button id="detail-sell-custom" class="detail-sim-order detail-sim-sell" type="button" data-side="sell">Sell custom</button>
      </div>
      <div id="detail-trade-log" class="detail-trade-log"></div>
    </section>
  </main>
  <script src="/web/detail.js"></script>
</body>
</html>`,
		html.EscapeString(a.Ticker),
		html.EscapeString(a.ID),
		html.EscapeString(string(a.BurstStatus)), html.EscapeString(a.UpdatedAt.Format(time.RFC1123)),
		html.EscapeString(a.Ticker),
		html.EscapeString(a.NewsStatus),
		bidText,
		askText,
		a.Snapshot.LastPrice,
		a.Snapshot.PercentChange,
		a.Snapshot.PremarketCumulativeVolume,
		a.HODCount,
		html.EscapeString(articleSource), html.EscapeString(articleCreated),
		html.EscapeString(articleTitle),
		newsLink,
		linkifyPlainText(articleBody),
		markdownToHTML(a.LLMMarkdown),
	)
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"']+`)

func linkifyPlainText(text string) string {
	escaped := html.EscapeString(text)
	return urlPattern.ReplaceAllStringFunc(escaped, func(raw string) string {
		href := strings.TrimRight(raw, ".,);]")
		trailing := strings.TrimPrefix(raw, href)
		return fmt.Sprintf(`<a href="%s" target="_blank" rel="noreferrer">%s</a>%s`, href, href, trailing)
	})
}

func markdownToHTML(md string) string {
	if strings.TrimSpace(md) == "" {
		return `<p>Analysis pending or unavailable.</p>`
	}
	lines := strings.Split(html.EscapeString(md), "\n")
	var out strings.Builder
	inList := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		switch {
		case trim == "":
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
		case strings.HasPrefix(trim, "### "):
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			out.WriteString("<h3>" + inlineMarkdown(strings.TrimPrefix(trim, "### ")) + "</h3>")
		case strings.HasPrefix(trim, "* "):
			if !inList {
				out.WriteString("<ul>")
				inList = true
			}
			out.WriteString("<li>" + inlineMarkdown(strings.TrimPrefix(trim, "* ")) + "</li>")
		default:
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			out.WriteString("<p>" + inlineMarkdown(trim) + "</p>")
		}
	}
	if inList {
		out.WriteString("</ul>")
	}
	return out.String()
}

func inlineMarkdown(s string) string {
	for {
		start := strings.Index(s, "**")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start+2:], "**")
		if end < 0 {
			return s
		}
		end += start + 2
		s = s[:start] + "<strong>" + s[start+2:end] + "</strong>" + s[end+2:]
	}
}
