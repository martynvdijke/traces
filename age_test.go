package main

import "testing"

func TestParseLenientDate(t *testing.T) {
	cases := []struct {
		in     string
		wantOK bool
		year   int
		month  int
		day    int
	}{
		{"2022-03-15", true, 2022, 3, 15},
		{"2022-03", true, 2022, 3, 1},
		{"2022", true, 2022, 1, 1},
		{" 2022-03-15 ", true, 2022, 3, 15},
		{"", false, 0, 0, 0},
		{"not-a-date", false, 0, 0, 0},
		{"2022-13-01", false, 0, 0, 0},
		{"15-03-2022", false, 0, 0, 0},
	}
	for _, c := range cases {
		got, ok := parseLenientDate(c.in)
		if ok != c.wantOK {
			t.Errorf("parseLenientDate(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got.Year() != c.year || int(got.Month()) != c.month || got.Day() != c.day {
			t.Errorf("parseLenientDate(%q) = %v, want %d-%d-%d", c.in, got, c.year, c.month, c.day)
		}
	}
}

func TestAgeAt(t *testing.T) {
	cases := []struct {
		name      string
		birth     string
		event     string
		wantYears int
		wantMonth int
		wantOK    bool
	}{
		{"spec example", "2022-03-15", "2025-05-20", 3, 2, true},
		{"exact birthday", "2022-03-15", "2025-03-15", 3, 0, true},
		{"day before birthday", "2022-03-15", "2025-03-14", 2, 11, true},
		{"same day", "2022-03-15", "2022-03-15", 0, 0, true},
		{"one month later same day", "2022-03-15", "2022-04-15", 0, 1, true},
		{"one month minus a day", "2022-03-15", "2022-04-14", 0, 0, true},
		{"year boundary decrement", "2022-03-15", "2023-02-20", 0, 11, true},
		{"event before birth", "2022-03-15", "2021-01-01", 0, 0, false},
		{"partial birth year only", "2022", "2024-06-01", 2, 5, true},
		{"partial event month only", "2022-03-15", "2024-06", 2, 2, true},
		{"unparseable birth", "whenever", "2024-06-01", 0, 0, false},
		{"unparseable event", "2022-03-15", "soon", 0, 0, false},
		{"empty birth", "", "2024-06-01", 0, 0, false},
		{"empty event", "2022-03-15", "", 0, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			y, m, ok := AgeAt(c.birth, c.event)
			if ok != c.wantOK {
				t.Fatalf("AgeAt(%q,%q) ok = %v, want %v", c.birth, c.event, ok, c.wantOK)
			}
			if !ok {
				return
			}
			if y != c.wantYears || m != c.wantMonth {
				t.Errorf("AgeAt(%q,%q) = %dy %dm, want %dy %dm", c.birth, c.event, y, m, c.wantYears, c.wantMonth)
			}
		})
	}
}

func TestLifeGroup(t *testing.T) {
	cases := []struct {
		name  string
		birth string
		event string
		want  string
	}{
		{"first day of life", "2022-03-15", "2022-03-15", "Year 1"},
		{"day before first birthday", "2022-03-15", "2023-03-14", "Year 1"},
		{"first birthday", "2022-03-15", "2023-03-15", "Year 2"},
		{"across calendar years in year 1", "2022-03-15", "2023-01-10", "Year 1"},
		{"event before birth", "2022-03-15", "2021-12-01", "Before"},
		{"no birth date uses calendar year", "", "2015-07-04", "2015"},
		{"unparseable birth uses calendar year", "??", "2015-07-04", "2015"},
		{"undated event", "2022-03-15", "someday", "Undated"},
		{"empty event date", "2022-03-15", "", "Undated"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := LifeGroup(c.birth, c.event); got != c.want {
				t.Errorf("LifeGroup(%q,%q) = %q, want %q", c.birth, c.event, got, c.want)
			}
		})
	}
}
