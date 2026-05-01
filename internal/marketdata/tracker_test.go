package marketdata

import (
	"testing"
	"time"
)

func TestTrackerUsesPreviousCloseReference(t *testing.T) {
	loc := time.UTC
	tracker := NewTracker(SessionClock{Location: loc, PremarketStart: "04:00", RegularOpen: "09:30"})
	ts := time.Date(2026, 5, 1, 8, 0, 0, 0, loc)
	update := tracker.Update(Tick{Ticker: "ABCD", Price: 11, Size: 100, At: ts, ReferencePrice: 10})
	if update.Snapshot.ReferencePrice != 10 {
		t.Fatalf("expected previous close reference 10, got %.2f", update.Snapshot.ReferencePrice)
	}
	if update.Snapshot.PercentChange != 10 {
		t.Fatalf("expected 10%% change, got %.2f", update.Snapshot.PercentChange)
	}
}

func TestTrackerCanReceiveReferenceBeforePrice(t *testing.T) {
	loc := time.UTC
	tracker := NewTracker(SessionClock{Location: loc, PremarketStart: "04:00", RegularOpen: "09:30"})
	ts := time.Date(2026, 5, 1, 8, 0, 0, 0, loc)
	tracker.Update(Tick{Ticker: "ABCD", ReferencePrice: 10, At: ts})
	update := tracker.Update(Tick{Ticker: "ABCD", Price: 12, Size: 100, At: ts.Add(time.Minute)})
	if update.Snapshot.ReferencePrice != 10 {
		t.Fatalf("expected previous close reference 10, got %.2f", update.Snapshot.ReferencePrice)
	}
	if update.Snapshot.PercentChange != 20 {
		t.Fatalf("expected 20%% change, got %.2f", update.Snapshot.PercentChange)
	}
}

func TestTrackerKeepsUpdatingAfterRegularOpenAndClose(t *testing.T) {
	loc := time.UTC
	tracker := NewTracker(SessionClock{Location: loc, PremarketStart: "04:00", RegularOpen: "09:30", RegularClose: "16:00"})
	pre := tracker.Update(Tick{Ticker: "ABCD", Price: 10.50, Size: 100, ReferencePrice: 10, At: time.Date(2026, 5, 1, 8, 0, 0, 0, loc)})
	if pre.Snapshot.PremarketCumulativeVolume != 100 {
		t.Fatalf("expected premarket volume 100, got %d", pre.Snapshot.PremarketCumulativeVolume)
	}
	regular := tracker.Update(Tick{Ticker: "ABCD", Price: 12, Size: 1000, At: time.Date(2026, 5, 1, 10, 0, 0, 0, loc)})
	if regular.Snapshot.Session != SessionRegular {
		t.Fatalf("expected regular session, got %s", regular.Snapshot.Session)
	}
	if regular.Snapshot.LastPrice != 12 || regular.Snapshot.PercentChange != 20 {
		t.Fatalf("regular update did not refresh price/change: %+v", regular.Snapshot)
	}
	if regular.Snapshot.PremarketCumulativeVolume != 1100 {
		t.Fatalf("regular update should keep adding volume since 04:00, got %d", regular.Snapshot.PremarketCumulativeVolume)
	}
	after := tracker.Update(Tick{Ticker: "ABCD", Price: 13, Size: 2000, At: time.Date(2026, 5, 1, 16, 30, 0, 0, loc)})
	if after.Snapshot.Session != SessionAfterHours {
		t.Fatalf("expected afterhours session, got %s", after.Snapshot.Session)
	}
	if after.Snapshot.LastPrice != 13 || after.Snapshot.PercentChange != 30 {
		t.Fatalf("afterhours update did not refresh price/change: %+v", after.Snapshot)
	}
	if after.Snapshot.PremarketCumulativeVolume != 3100 {
		t.Fatalf("afterhours update should keep adding volume since 04:00, got %d", after.Snapshot.PremarketCumulativeVolume)
	}
}
