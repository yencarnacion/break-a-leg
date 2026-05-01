package news

import (
	"testing"
	"time"
)

func TestFreshnessClassification(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	cfg := FreshnessConfig{FreshMinutes: 30, RecentHours: 4}
	if got := Classify(now.Add(-20*time.Minute), now, cfg); got != FreshnessFresh {
		t.Fatalf("expected fresh, got %s", got)
	}
	if got := Classify(now.Add(-2*time.Hour), now, cfg); got != FreshnessRecent {
		t.Fatalf("expected recent, got %s", got)
	}
	if got := Classify(now.Add(-5*time.Hour), now, cfg); got != FreshnessOld {
		t.Fatalf("expected old, got %s", got)
	}
}
