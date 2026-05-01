package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type MassiveProvider struct {
	APIKey           string
	RestBaseURL      string
	WebSocketURL     string
	ReconnectSeconds int
	BackfillWorkers  int
}

func (p MassiveProvider) BackfillPremarket(ctx context.Context, symbols []string, loc *time.Location, premarketStart string, out chan<- Tick) {
	if strings.TrimSpace(p.APIKey) == "" {
		return
	}
	if loc == nil {
		loc = time.Local
	}
	workers := p.BackfillWorkers
	if workers <= 0 {
		workers = 6
	}
	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range jobs {
				if err := p.backfillSymbol(ctx, sym, loc, premarketStart, out); err != nil {
					log.Printf("[marketdata] backfill %s: %v", sym, err)
				}
			}
		}()
	}
	for _, sym := range symbols {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return
		case jobs <- strings.ToUpper(strings.TrimSpace(sym)):
		}
	}
	close(jobs)
	wg.Wait()
	log.Printf("[marketdata] premarket aggregate backfill complete for %d symbols", len(symbols))
}

func (p MassiveProvider) backfillSymbol(ctx context.Context, symbol string, loc *time.Location, premarketStart string, out chan<- Tick) error {
	if symbol == "" {
		return nil
	}
	ref, err := p.previousClose(ctx, symbol)
	if err != nil {
		return err
	}
	now := time.Now().In(loc)
	startHM := parseClock(premarketStart, 4, 0)
	start := time.Date(now.Year(), now.Month(), now.Day(), startHM.hour, startHM.min, 0, 0, loc)
	if now.Before(start) {
		select {
		case out <- Tick{Ticker: symbol, ReferencePrice: ref, At: now}:
		case <-ctx.Done():
		}
		return nil
	}
	bars, err := p.minuteBars(ctx, symbol, start, now)
	if err != nil {
		return err
	}
	if len(bars) == 0 {
		select {
		case out <- Tick{Ticker: symbol, ReferencePrice: ref, At: now}:
		case <-ctx.Done():
		}
		return nil
	}
	for _, bar := range bars {
		select {
		case out <- Tick{Ticker: symbol, Price: bar.Close, Size: bar.Volume, At: bar.Start, ReferencePrice: ref}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type minuteBar struct {
	Close  float64
	Volume int64
	Start  time.Time
}

func (p MassiveProvider) previousClose(ctx context.Context, symbol string) (float64, error) {
	var resp struct {
		Results []struct {
			C float64 `json:"c"`
		} `json:"results"`
	}
	if err := p.getJSON(ctx, path.Join("v2", "aggs", "ticker", symbol, "prev"), url.Values{"adjusted": {"true"}}, &resp); err != nil {
		return 0, err
	}
	if len(resp.Results) == 0 || resp.Results[0].C <= 0 {
		return 0, fmt.Errorf("previous close unavailable")
	}
	return resp.Results[0].C, nil
}

func (p MassiveProvider) minuteBars(ctx context.Context, symbol string, start, end time.Time) ([]minuteBar, error) {
	var resp struct {
		Results []struct {
			C float64 `json:"c"`
			V float64 `json:"v"`
			T int64   `json:"t"`
		} `json:"results"`
	}
	err := p.getJSON(ctx, path.Join("v2", "aggs", "ticker", symbol, "range", "1", "minute", start.Format("2006-01-02"), end.Format("2006-01-02")), url.Values{
		"adjusted": {"true"},
		"sort":     {"asc"},
		"limit":    {"50000"},
	}, &resp)
	if err != nil {
		return nil, err
	}
	out := make([]minuteBar, 0, len(resp.Results))
	for _, row := range resp.Results {
		if row.C <= 0 || row.V <= 0 || row.T <= 0 {
			continue
		}
		ts := time.UnixMilli(row.T)
		if ts.Before(start) || ts.After(end) {
			continue
		}
		out = append(out, minuteBar{Close: row.C, Volume: int64(row.V), Start: ts})
	}
	return out, nil
}

func (p MassiveProvider) getJSON(ctx context.Context, endpoint string, query url.Values, out any) error {
	base := strings.TrimRight(p.RestBaseURL, "/")
	if base == "" {
		base = "https://api.massive.com"
	}
	u, err := url.Parse(base + "/" + strings.TrimLeft(endpoint, "/"))
	if err != nil {
		return err
	}
	q := u.Query()
	for key, vals := range query {
		for _, val := range vals {
			q.Add(key, val)
		}
	}
	q.Set("apiKey", p.APIKey)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func (p MassiveProvider) Run(ctx Context, symbols []string, out chan<- Tick) error {
	if strings.TrimSpace(p.APIKey) == "" {
		log.Printf("[marketdata] MASSIVE_API_KEY is empty; live market data disabled")
		<-ctx.Done()
		return ctx.Err()
	}
	wsURL := p.WebSocketURL
	if wsURL == "" {
		wsURL = "wss://socket.massive.com/stocks"
	}
	reconnect := time.Duration(p.ReconnectSeconds) * time.Second
	if reconnect <= 0 {
		reconnect = 5 * time.Second
	}
	for {
		err := p.runOnce(ctx, wsURL, symbols, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("[marketdata] massive websocket disconnected: %v", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(reconnect):
		}
	}
}

func (p MassiveProvider) runOnce(ctx Context, wsURL string, symbols []string, out chan<- Tick) error {
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]string{"action": "auth", "params": p.APIKey}); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	params := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		s := strings.ToUpper(strings.TrimSpace(sym))
		if s != "" {
			params = append(params, "T."+s)
			params = append(params, "Q."+s)
		}
	}
	if len(params) > 0 {
		if err := conn.WriteJSON(map[string]string{"action": "subscribe", "params": strings.Join(params, ",")}); err != nil {
			return fmt.Errorf("subscribe: %w", err)
		}
	}

	errCh := make(chan error, 1)
	go func() {
		for {
			var raws []json.RawMessage
			if err := conn.ReadJSON(&raws); err != nil {
				errCh <- err
				return
			}
			for _, raw := range raws {
				var ev struct {
					Ev  string  `json:"ev"`
					Sym string  `json:"sym"`
					P   float64 `json:"p"`
					S   int64   `json:"s"`
					BP  float64 `json:"bp"`
					AP  float64 `json:"ap"`
					T   int64   `json:"t"`
				}
				if err := json.Unmarshal(raw, &ev); err != nil || ev.Sym == "" {
					continue
				}
				at := time.Now()
				if ev.T > 0 {
					at = time.UnixMilli(ev.T)
				}
				tick := Tick{Ticker: ev.Sym, At: at}
				switch ev.Ev {
				case "T":
					if ev.P <= 0 {
						continue
					}
					tick.Price = ev.P
					tick.Size = ev.S
				case "Q":
					if ev.BP <= 0 && ev.AP <= 0 {
						continue
					}
					tick.Bid = ev.BP
					tick.Ask = ev.AP
				default:
					continue
				}
				select {
				case out <- tick:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			return ctx.Err()
		case err := <-errCh:
			return err
		}
	}
}
