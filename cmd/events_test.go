package cmd

import (
	"math"
	"testing"
	"time"
)

func TestParseTimeArg_Duration_1h(t *testing.T) {
	ts, err := parseTimeArg("1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Now().Add(-1 * time.Hour).Unix()
	diff := math.Abs(float64(int64(ts) - expected))
	if diff > 2 {
		t.Errorf("1h: expected ~%d, got %d (diff %.0f)", expected, ts, diff)
	}
}

func TestParseTimeArg_Duration_24h(t *testing.T) {
	ts, err := parseTimeArg("24h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Now().Add(-24 * time.Hour).Unix()
	diff := math.Abs(float64(int64(ts) - expected))
	if diff > 2 {
		t.Errorf("24h: expected ~%d, got %d (diff %.0f)", expected, ts, diff)
	}
}

func TestParseTimeArg_Duration_7d(t *testing.T) {
	ts, err := parseTimeArg("7d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Now().Add(-7 * 24 * time.Hour).Unix()
	diff := math.Abs(float64(int64(ts) - expected))
	if diff > 2 {
		t.Errorf("7d: expected ~%d, got %d (diff %.0f)", expected, ts, diff)
	}
}

func TestParseTimeArg_Duration_30m(t *testing.T) {
	ts, err := parseTimeArg("30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Now().Add(-30 * time.Minute).Unix()
	diff := math.Abs(float64(int64(ts) - expected))
	if diff > 2 {
		t.Errorf("30m: expected ~%d, got %d (diff %.0f)", expected, ts, diff)
	}
}

func TestParseTimeArg_Duration_2w(t *testing.T) {
	ts, err := parseTimeArg("2w")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Now().Add(-2 * 7 * 24 * time.Hour).Unix()
	diff := math.Abs(float64(int64(ts) - expected))
	if diff > 2 {
		t.Errorf("2w: expected ~%d, got %d (diff %.0f)", expected, ts, diff)
	}
}

func TestParseTimeArg_UnixTimestamp(t *testing.T) {
	ts, err := parseTimeArg("1700000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if int64(ts) != 1700000000 {
		t.Errorf("expected 1700000000, got %d", ts)
	}
}

func TestParseTimeArg_ISODate(t *testing.T) {
	ts, err := parseTimeArg("2024-01-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected, _ := time.Parse("2006-01-02", "2024-01-01")
	if int64(ts) != expected.Unix() {
		t.Errorf("expected %d, got %d", expected.Unix(), ts)
	}
}

func TestParseTimeArg_ISODateTime(t *testing.T) {
	ts, err := parseTimeArg("2024-01-01T15:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected, _ := time.Parse(time.RFC3339, "2024-01-01T15:00:00Z")
	if int64(ts) != expected.Unix() {
		t.Errorf("expected %d, got %d", expected.Unix(), ts)
	}
}

func TestParseTimeArg_EmptyString(t *testing.T) {
	_, err := parseTimeArg("")
	if err == nil {
		t.Error("expected error for empty string, got nil")
	}
}

func TestParseTimeArg_InvalidInput(t *testing.T) {
	_, err := parseTimeArg("not-a-time")
	if err == nil {
		t.Error("expected error for invalid input, got nil")
	}
}

func TestParseTimeArg_WhitespaceHandling(t *testing.T) {
	ts, err := parseTimeArg("  1h  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := time.Now().Add(-1 * time.Hour).Unix()
	diff := math.Abs(float64(int64(ts) - expected))
	if diff > 2 {
		t.Errorf("trimmed 1h: expected ~%d, got %d", expected, ts)
	}
}
