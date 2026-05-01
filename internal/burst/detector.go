package burst

import (
	"sync"
	"time"

	"break-a-leg/internal/config"
	"break-a-leg/internal/marketdata"
)

type TriggerKind string

const (
	TriggerNone TriggerKind = ""
	TriggerSoft TriggerKind = "soft"
	TriggerFull TriggerKind = "full"
)

type Event struct {
	Kind          TriggerKind
	Ticker        string
	Snapshot      marketdata.Snapshot
	HODCount      int
	FirstHODTime  time.Time
	LatestHODTime time.Time
	Cooldown      bool
}

type Detector struct {
	cfg      config.BurstConfig
	now      func() time.Time
	mu       sync.Mutex
	lastFull map[string]time.Time
	lastSoft map[string]time.Time
}

func NewDetector(cfg config.BurstConfig) *Detector {
	return &Detector{cfg: cfg, now: time.Now, lastFull: map[string]time.Time{}, lastSoft: map[string]time.Time{}}
}

func (d *Detector) SetNow(fn func() time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.now = fn
}

func (d *Detector) Evaluate(update marketdata.Update) Event {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.cfg.Enabled || !update.NewHOD {
		return Event{}
	}
	s := update.Snapshot
	if s.Session != marketdata.SessionPremarket {
		return Event{}
	}
	if s.PremarketCumulativeVolume < d.cfg.MinPremarketVolume {
		return Event{}
	}

	window := time.Duration(d.cfg.HODWindowSeconds) * time.Second
	if window <= 0 {
		window = time.Minute
	}
	required := d.cfg.HODCountRequired
	if required <= 0 {
		required = 2
	}
	cutoff := s.LastUpdate.Add(-window)
	var inWindow []time.Time
	for _, ts := range s.HODEventTimes {
		if !ts.Before(cutoff) && !ts.After(s.LastUpdate) {
			inWindow = append(inWindow, ts)
		}
	}
	if len(inWindow) < required {
		return Event{}
	}
	ev := Event{
		Ticker:        s.Ticker,
		Snapshot:      s,
		HODCount:      len(inWindow),
		FirstHODTime:  inWindow[0],
		LatestHODTime: inWindow[len(inWindow)-1],
	}

	now := d.now()
	cooldown := time.Duration(d.cfg.WorkflowCooldownSeconds) * time.Second
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	if s.PercentChange >= d.cfg.FullTriggerMinPercentChange {
		if last, ok := d.lastFull[s.Ticker]; ok && now.Sub(last) < cooldown {
			ev.Kind = TriggerFull
			ev.Cooldown = true
			return ev
		}
		d.lastFull[s.Ticker] = now
		ev.Kind = TriggerFull
		return ev
	}
	if d.cfg.SoftTriggerEnabled && s.PercentChange >= d.cfg.SoftTriggerMinPercentChange {
		if last, ok := d.lastSoft[s.Ticker]; ok && now.Sub(last) < cooldown {
			ev.Kind = TriggerSoft
			ev.Cooldown = true
			return ev
		}
		d.lastSoft[s.Ticker] = now
		ev.Kind = TriggerSoft
		return ev
	}
	return Event{}
}
