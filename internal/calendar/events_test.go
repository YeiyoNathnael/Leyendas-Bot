package calendar

import (
	"testing"
	"time"
)

func TestMergeIntervals(t *testing.T) {
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

	intervals := []TimeInterval{
		{Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour)},
		{Start: now.Add(30 * time.Minute), End: now.Add(90 * time.Minute)},
		{Start: now.Add(4 * time.Hour), End: now.Add(5 * time.Hour)},
	}

	merged := MergeIntervals(intervals)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged intervals, got %d", len(merged))
	}

	if !merged[0].Start.Equal(now.Add(30*time.Minute)) || !merged[0].End.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("unexpected first merged interval: %v to %v", merged[0].Start, merged[0].End)
	}
}

func TestFindAvailableSlots(t *testing.T) {
	now := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	windowStart := now
	windowEnd := now.Add(4 * time.Hour) // 9:00 to 13:00

	busy := []TimeInterval{
		{Start: now.Add(1 * time.Hour), End: now.Add(2 * time.Hour)}, // 10:00 to 11:00 busy
	}

	slots := FindAvailableSlots(busy, windowStart, windowEnd, 1*time.Hour)
	if len(slots) != 3 {
		// 9:00-10:00 (1 slot), 11:00-12:00 (1 slot), 12:00-13:00 (1 slot) -> total 3 slots
		t.Fatalf("expected 3 available 1-hour slots, got %d: %+v", len(slots), slots)
	}

	if !slots[0].Start.Equal(now) || !slots[0].End.Equal(now.Add(1*time.Hour)) {
		t.Fatalf("unexpected first slot: %+v", slots[0])
	}
	if !slots[1].Start.Equal(now.Add(2*time.Hour)) || !slots[1].End.Equal(now.Add(3*time.Hour)) {
		t.Fatalf("unexpected second slot: %+v", slots[1])
	}
}
