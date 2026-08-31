package calendar

import (
	"context"
	"fmt"
	"sort"
	"time"

	"google.golang.org/api/calendar/v3"
)

// TimeInterval represents a time span.
type TimeInterval struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// QueryFreeBusy fetches busy intervals for all participant emails within [timeMin, timeMax].
func (s *Service) QueryFreeBusy(
	ctx context.Context,
	emails []string,
	timeMin, timeMax time.Time,
	timeZone string,
) ([]TimeInterval, error) {
	if timeZone == "" {
		timeZone = "UTC"
	}

	var items []*calendar.FreeBusyRequestItem
	for _, email := range emails {
		if email != "" {
			items = append(items, &calendar.FreeBusyRequestItem{Id: email})
		}
	}

	req := &calendar.FreeBusyRequest{
		TimeMin:  timeMin.Format(time.RFC3339),
		TimeMax:  timeMax.Format(time.RFC3339),
		TimeZone: timeZone,
		Items:    items,
	}

	res, err := s.srv.Freebusy.Query(req).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("freebusy query failed: %w", err)
	}

	var busyIntervals []TimeInterval
	for _, calInfo := range res.Calendars {
		for _, period := range calInfo.Busy {
			start, err1 := time.Parse(time.RFC3339, period.Start)
			end, err2 := time.Parse(time.RFC3339, period.End)
			if err1 == nil && err2 == nil {
				busyIntervals = append(busyIntervals, TimeInterval{Start: start, End: end})
			}
		}
	}

	return busyIntervals, nil
}

// MergeIntervals sorts and merges overlapping or contiguous time intervals.
func MergeIntervals(intervals []TimeInterval) []TimeInterval {
	if len(intervals) <= 1 {
		return intervals
	}

	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Start.Before(intervals[j].Start)
	})

	merged := []TimeInterval{intervals[0]}
	for _, current := range intervals[1:] {
		lastIdx := len(merged) - 1
		last := &merged[lastIdx]

		if !current.Start.After(last.End) {
			if current.End.After(last.End) {
				last.End = current.End
			}
		} else {
			merged = append(merged, current)
		}
	}

	return merged
}

// FindAvailableSlots calculates open meeting slots of `slotDuration` within `[windowStart, windowEnd]`.
func FindAvailableSlots(
	busyIntervals []TimeInterval,
	windowStart, windowEnd time.Time,
	slotDuration time.Duration,
) []TimeInterval {
	mergedBusy := MergeIntervals(busyIntervals)
	var freeIntervals []TimeInterval

	cursor := windowStart

	for _, busy := range mergedBusy {
		bStart := busy.Start
		bEnd := busy.End

		if bEnd.Before(windowStart) || bStart.After(windowEnd) {
			continue
		}
		if bStart.Before(windowStart) {
			bStart = windowStart
		}
		if bEnd.After(windowEnd) {
			bEnd = windowEnd
		}

		if cursor.Before(bStart) {
			freeIntervals = append(freeIntervals, TimeInterval{Start: cursor, End: bStart})
		}
		if bEnd.After(cursor) {
			cursor = bEnd
		}
	}

	if cursor.Before(windowEnd) {
		freeIntervals = append(freeIntervals, TimeInterval{Start: cursor, End: windowEnd})
	}

	var candidateSlots []TimeInterval
	for _, free := range freeIntervals {
		slotStart := free.Start
		for {
			slotEnd := slotStart.Add(slotDuration)
			if slotEnd.After(free.End) {
				break
			}
			candidateSlots = append(candidateSlots, TimeInterval{
				Start: slotStart,
				End:   slotEnd,
			})
			slotStart = slotEnd
		}
	}

	return candidateSlots
}
