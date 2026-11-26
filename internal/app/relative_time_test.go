package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
)

func TestShortDateFollowsLocale(t *testing.T) {
	i18n.SetLocaleFunc(func() string { return "zh-Hans" })
	defer i18n.SetLocaleFunc(nil)

	now := time.Date(2026, time.November, 19, 15, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name string
		date time.Time
		want string
	}{
		{"today", time.Date(2026, time.November, 19, 9, 5, 0, 0, time.UTC), "09:05"},
		{"yesterday", now.AddDate(0, 0, -1), "昨天"},
		{"weekday", now.AddDate(0, 0, -3), "星期一"},
		{"this year", time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC), "1月2日"},
		{"previous year", time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC), "2025年1月2日"},
	} {
		if got := shortDate(tt.date, now); got != tt.want {
			t.Errorf("%s: shortDate = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRelativeTimeLocales(t *testing.T) {
	now := time.Date(2026, time.November, 19, 15, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name   string
		date   time.Time
		en, zh string
	}{
		{"now", now, "Just now", "刚刚"},
		{"future", now.Add(time.Hour), "Just now", "刚刚"},
		{"minutes", now.Add(-10 * time.Minute), "10 min ago", "10 分钟前"},
		{"hours", now.Add(-2 * time.Hour), "2 hr ago", "2 小时前"},
		{"yesterday", now.AddDate(0, 0, -1), "Yesterday", "昨天"},
		{"weekday", now.AddDate(0, 0, -3), "Monday", "星期一"},
		{"this year", time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC), "Jan 2", "1月2日"},
		{"previous year", time.Date(2025, time.January, 2, 0, 0, 0, 0, time.UTC), "Jan 2, 2025", "2025年1月2日"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for locale, want := range map[string]string{"en": tt.en, "zh-Hans": tt.zh} {
				if got := relativeTimeIn(tt.date, now, locale); got != want {
					t.Errorf("%s: got %q, want %q", locale, got, want)
				}
			}
		})
	}
	weekdays := []string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}
	for day, zh := range weekdays {
		date := time.Date(2026, time.November, 15+day, 15, 0, 0, 0, time.UTC)
		for locale, want := range map[string]string{"en": date.Weekday().String(), "zh-Hans": zh} {
			if got := relativeTimeIn(date, date.AddDate(0, 0, 3), locale); got != want {
				t.Errorf("weekday %d in %s: got %q, want %q", day, locale, got, want)
			}
		}
	}
}
