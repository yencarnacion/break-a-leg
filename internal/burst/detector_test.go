package burst

import (
	"testing"
	"time"

	"break-a-leg/internal/config"
	"break-a-leg/internal/marketdata"
)

func testConfig() config.BurstConfig {
	return config.BurstConfig{
		Enabled:                     true,
		MinPremarketVolume:          20000,
		FullTriggerMinPercentChange: 10,
		SoftTriggerEnabled:          true,
		SoftTriggerMinPercentChange: 5,
		HODCountRequired:            2,
		HODWindowSeconds:            60,
		WorkflowCooldownSeconds:     300,
	}
}

func update(volume int64, percent float64, hods ...time.Time) marketdata.Update {
	return marketdata.Update{
		NewHOD: true,
		Snapshot: marketdata.Snapshot{
			Ticker:                    "ABCD",
			Session:                   marketdata.SessionPremarket,
			PremarketCumulativeVolume: volume,
			PercentChange:             percent,
			HODEventTimes:             hods,
			LastUpdate:                hods[len(hods)-1],
		},
	}
}

func TestHODBurstWithinWindowTriggers(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	d := NewDetector(testConfig())
	ev := d.Evaluate(update(25000, 12, now.Add(-30*time.Second), now))
	if ev.Kind != TriggerFull {
		t.Fatalf("expected full trigger, got %q", ev.Kind)
	}
}

func TestHODBurstOutsideWindowDoesNotTrigger(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	d := NewDetector(testConfig())
	ev := d.Evaluate(update(25000, 12, now.Add(-90*time.Second), now))
	if ev.Kind != TriggerNone {
		t.Fatalf("expected no trigger, got %q", ev.Kind)
	}
}

func TestHODBurstBelowVolumeDoesNotTrigger(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	d := NewDetector(testConfig())
	ev := d.Evaluate(update(19999, 12, now.Add(-30*time.Second), now))
	if ev.Kind != TriggerNone {
		t.Fatalf("expected no trigger, got %q", ev.Kind)
	}
}

func TestBelowTenPercentIsSoftOnly(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	d := NewDetector(testConfig())
	ev := d.Evaluate(update(25000, 6, now.Add(-30*time.Second), now))
	if ev.Kind != TriggerSoft {
		t.Fatalf("expected soft trigger, got %q", ev.Kind)
	}
}

func TestCooldownPreventsDuplicateWorkflow(t *testing.T) {
	now := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	d := NewDetector(testConfig())
	d.SetNow(func() time.Time { return now })
	first := d.Evaluate(update(25000, 12, now.Add(-30*time.Second), now))
	if first.Kind != TriggerFull || first.Cooldown {
		t.Fatalf("expected first full trigger, got %+v", first)
	}
	second := d.Evaluate(update(30000, 14, now.Add(-10*time.Second), now))
	if second.Kind != TriggerFull || !second.Cooldown {
		t.Fatalf("expected cooldown full event, got %+v", second)
	}
}
