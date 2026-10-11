package week

import (
	"errors"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestOf(t *testing.T) {
	for in, want := range map[string]string{
		"2026-10-11T15:00:00Z":      "2026-10-10", // spec example
		"2026-10-11T03:00:00Z":      "2026-10-10", // first instant of the week
		"2026-10-11T02:59:59Z":      "2026-10-03", // last second of the previous week
		"2026-10-11T02:59:59.999Z":  "2026-10-03",
		"2026-10-10T23:00:00Z":      "2026-10-03", // the label Saturday itself belongs to the week before
		"2026-10-18T02:59:59Z":      "2026-10-10", // last second of the week
		"2026-10-18T03:00:00Z":      "2026-10-17",
		"2026-10-14T12:00:00Z":      "2026-10-10", // midweek
		"2026-10-10T22:00:00-05:00": "2026-10-10", // Sat 22:00 in Chicago = Sun 03:00 UTC
		"2026-10-10T21:59:59-05:00": "2026-10-03",
		"2000-01-02T03:00:00Z":      "2000-01-01",
		"2026-12-27T03:00:00Z":      "2026-12-26", // across a year end
		"2027-01-02T12:00:00Z":      "2026-12-26",
		"2027-01-03T03:00:00Z":      "2027-01-02",
	} {
		if got := Of(at(in)).String(); got != want {
			t.Errorf("Of(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestWindows(t *testing.T) {
	k := Of(at("2026-10-12T00:00:00Z"))
	if k.String() != "2026-10-10" {
		t.Fatal(k)
	}
	if !k.Start().Equal(at("2026-10-11T03:00:00Z")) || !k.End().Equal(at("2026-10-18T03:00:00Z")) {
		t.Fatalf("start %v end %v", k.Start(), k.End())
	}
	labels := k.DayLabels()
	want := []string{"2026-10-10", "2026-10-11", "2026-10-12", "2026-10-13", "2026-10-14", "2026-10-15", "2026-10-16"}
	for i := range want {
		if labels[i] != want[i] {
			t.Fatalf("labels %v", labels)
		}
		from, to := k.Day(i)
		if !from.Equal(k.Start().Add(time.Duration(i)*24*time.Hour)) || to.Sub(from) != 24*time.Hour {
			t.Fatalf("day %d = %v..%v", i, from, to)
		}
	}
	for in, want := range map[string]int{
		"2026-10-11T03:00:00Z": 0, "2026-10-12T02:59:59Z": 0, "2026-10-12T03:00:00Z": 1,
		"2026-10-18T02:59:59Z": 6, "2026-10-18T03:00:00Z": -1, "2026-10-11T02:59:59Z": -1,
	} {
		if got := k.DayIndex(at(in)); got != want {
			t.Errorf("DayIndex(%s) = %d, want %d", in, got, want)
		}
	}
	if k.Add(-1).String() != "2026-10-03" || k.Add(12).String() != "2027-01-02" {
		t.Fatal("Add")
	}
	r := Range(k.Add(-2), k)
	if len(r) != 3 || r[0].String() != "2026-09-26" || r[2] != k || len(Range(k, k.Add(-1))) != 0 {
		t.Fatalf("Range %v", r)
	}
}

func TestParse(t *testing.T) {
	now := at("2026-10-11T15:00:00Z")
	for _, ok := range []string{"2026-10-10", "2026-10-03", "2000-01-01", "2013-06-15"} {
		k, err := Parse(ok, now)
		if err != nil || k.String() != ok {
			t.Errorf("Parse(%s) = %v, %v", ok, k, err)
		}
	}
	for in, want := range map[string]error{
		"2026-10-17":  ErrRange,    // next week
		"2099-01-03":  ErrRange,    // far future Saturday
		"1999-12-25":  ErrRange,    // before 2000
		"2026-10-11":  ErrSaturday, // a Sunday
		"2026-10-09":  ErrSaturday,
		"2026-10-1":   ErrFormat,
		"2026-13-01":  ErrFormat,
		"2026-02-30":  ErrFormat,
		"20261010":    ErrFormat,
		"":            ErrFormat,
		"2026-10-10x": ErrFormat,
	} {
		if _, err := Parse(in, now); !errors.Is(err, want) {
			t.Errorf("Parse(%q) err = %v, want %v", in, err, want)
		}
	}
	// Just before the boundary, the coming week is still the future.
	if _, err := Parse("2026-10-10", at("2026-10-11T02:59:59Z")); !errors.Is(err, ErrRange) {
		t.Errorf("week before it starts: %v", err)
	}
	if _, err := Parse("2026-10-10", at("2026-10-11T03:00:00Z")); err != nil {
		t.Errorf("week at its start: %v", err)
	}
}
