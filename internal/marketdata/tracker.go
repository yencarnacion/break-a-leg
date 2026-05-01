package marketdata

import (
	"strings"
	"sync"
	"time"
)

type SessionClock struct {
	Location       *time.Location
	PremarketStart string
	RegularOpen    string
	RegularClose   string
}

type Tracker struct {
	clock SessionClock
	mu    sync.Mutex
	bySym map[string]*Snapshot
}

func NewTracker(clock SessionClock) *Tracker {
	if clock.Location == nil {
		clock.Location = time.Local
	}
	return &Tracker{clock: clock, bySym: make(map[string]*Snapshot)}
}

func (t *Tracker) Update(tick Tick) Update {
	t.mu.Lock()
	defer t.mu.Unlock()

	sym := strings.ToUpper(strings.TrimSpace(tick.Ticker))
	now := tick.At
	if now.IsZero() {
		now = time.Now()
	}
	s, ok := t.bySym[sym]
	if !ok {
		ref := tick.ReferencePrice
		if ref <= 0 && tick.Price > 0 {
			ref = tick.Price
		}
		s = &Snapshot{Ticker: sym, ReferencePrice: ref, HighOfDay: tick.Price, LowOfDay: tick.Price}
		t.bySym[sym] = s
	}

	newHOD := false
	newLOD := false
	if tick.ReferencePrice > 0 {
		s.ReferencePrice = tick.ReferencePrice
	}
	if s.ReferencePrice <= 0 && tick.Price > 0 {
		s.ReferencePrice = tick.Price
	}
	if tick.Price > 0 {
		s.LastPrice = tick.Price
		if s.HighOfDay <= 0 || tick.Price > s.HighOfDay {
			s.HighOfDay = tick.Price
			s.HODEventTimes = append(s.HODEventTimes, now)
			newHOD = true
		}
		if s.LowOfDay <= 0 || tick.Price < s.LowOfDay {
			s.LowOfDay = tick.Price
			s.LODEventTimes = append(s.LODEventTimes, now)
			newLOD = true
		}
		if s.ReferencePrice > 0 {
			s.PercentChange = ((tick.Price - s.ReferencePrice) / s.ReferencePrice) * 100
		}
	}
	if tick.Bid > 0 {
		s.Bid = tick.Bid
	}
	if tick.Ask > 0 {
		s.Ask = tick.Ask
	}
	s.Session = t.session(now)
	if s.Session != SessionClosed && tick.Size > 0 {
		s.PremarketCumulativeVolume += tick.Size
	}
	s.LastUpdate = now

	cp := copySnapshot(*s)
	return Update{Snapshot: cp, NewHOD: newHOD, NewLOD: newLOD}
}

func (t *Tracker) Snapshot(symbol string) (Snapshot, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.bySym[strings.ToUpper(strings.TrimSpace(symbol))]
	if !ok {
		return Snapshot{}, false
	}
	return copySnapshot(*s), true
}

func (t *Tracker) Snapshots() []Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Snapshot, 0, len(t.bySym))
	for _, s := range t.bySym {
		out = append(out, copySnapshot(*s))
	}
	return out
}

func (t *Tracker) session(ts time.Time) SessionState {
	loc := t.clock.Location
	if loc == nil {
		loc = time.Local
	}
	start := parseClock(t.clock.PremarketStart, 4, 0)
	open := parseClock(t.clock.RegularOpen, 9, 30)
	closeTime := parseClock(t.clock.RegularClose, 16, 0)
	x := ts.In(loc)
	pm := time.Date(x.Year(), x.Month(), x.Day(), start.hour, start.min, 0, 0, loc)
	ro := time.Date(x.Year(), x.Month(), x.Day(), open.hour, open.min, 0, 0, loc)
	rc := time.Date(x.Year(), x.Month(), x.Day(), closeTime.hour, closeTime.min, 0, 0, loc)
	if !x.Before(pm) && x.Before(ro) {
		return SessionPremarket
	}
	if !x.Before(ro) && x.Before(rc) {
		return SessionRegular
	}
	if !x.Before(rc) {
		return SessionAfterHours
	}
	return SessionClosed
}

type clockHM struct{ hour, min int }

func parseClock(v string, dh, dm int) clockHM {
	t, err := time.Parse("15:04", v)
	if err != nil {
		return clockHM{dh, dm}
	}
	return clockHM{t.Hour(), t.Minute()}
}

func copySnapshot(s Snapshot) Snapshot {
	s.HODEventTimes = append([]time.Time(nil), s.HODEventTimes...)
	s.LODEventTimes = append([]time.Time(nil), s.LODEventTimes...)
	return s
}
