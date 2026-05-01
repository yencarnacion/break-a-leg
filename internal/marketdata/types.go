package marketdata

import "time"

type SessionState string

const (
	SessionPremarket  SessionState = "premarket"
	SessionRegular    SessionState = "regular"
	SessionAfterHours SessionState = "afterhours"
	SessionClosed     SessionState = "closed"
)

type Tick struct {
	Ticker         string
	Price          float64
	Size           int64
	At             time.Time
	ReferencePrice float64
	Bid            float64
	Ask            float64
}

type Snapshot struct {
	Ticker                    string       `json:"ticker"`
	LastPrice                 float64      `json:"last_price"`
	ReferencePrice            float64      `json:"reference_price"`
	PercentChange             float64      `json:"percent_change"`
	PremarketCumulativeVolume int64        `json:"premarket_cumulative_volume"`
	HighOfDay                 float64      `json:"high_of_day"`
	LowOfDay                  float64      `json:"low_of_day"`
	HODEventTimes             []time.Time  `json:"hod_event_times"`
	LODEventTimes             []time.Time  `json:"lod_event_times"`
	Session                   SessionState `json:"session"`
	LastUpdate                time.Time    `json:"last_update"`
	Bid                       float64      `json:"bid,omitempty"`
	Ask                       float64      `json:"ask,omitempty"`
}

type Update struct {
	Snapshot Snapshot
	NewHOD   bool
	NewLOD   bool
}

type Provider interface {
	Run(ctx Context, symbols []string, out chan<- Tick) error
}

type Context interface {
	Done() <-chan struct{}
	Err() error
}
