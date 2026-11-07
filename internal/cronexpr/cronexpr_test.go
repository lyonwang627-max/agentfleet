package cronexpr

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, spec string) *Expr {
	t.Helper()
	e, err := Parse(spec)
	if err != nil {
		t.Fatalf("Parse(%q): %v", spec, err)
	}
	return e
}

func TestParseEveryMinute(t *testing.T) {
	e := mustParse(t, "* * * * *")
	after := time.Date(2026, 11, 6, 1, 40, 30, 0, time.Local)
	next := e.Next(after)
	want := time.Date(2026, 11, 6, 1, 41, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("got %v want %v", next, want)
	}
}

func TestParseStepAndList(t *testing.T) {
	e := mustParse(t, "*/15 9-17 * * 1-5")                   // 工作日 9-17 点每 15 分钟
	after := time.Date(2026, 11, 6, 10, 7, 0, 0, time.Local) // 周五
	next := e.Next(after)
	if next.Hour() != 10 || next.Minute() != 15 {
		t.Fatalf("want 10:15, got %v", next)
	}
	// 17:45 之后下一次应是下周一 9:00（2026-11-06 是周五）
	after2 := time.Date(2026, 11, 6, 17, 46, 0, 0, time.Local)
	next2 := e.Next(after2)
	if next2.Weekday() != time.Monday || next2.Day() != 9 || next2.Hour() != 9 {
		t.Fatalf("want Mon Nov 9 09:00, got %v", next2)
	}
}

func TestDomDowOrRule(t *testing.T) {
	// 标准 cron：dom 与 dow 同时受限取或 —— 每月 1 号或每周一 0 点
	e := mustParse(t, "0 0 1 * 1")
	// 2026-11-02 是周一 → 应命中
	after := time.Date(2026, 11, 1, 12, 0, 0, 0, time.Local)
	next := e.Next(after)
	if next.Day() != 2 || next.Weekday() != time.Monday {
		t.Fatalf("want Nov 2 (Mon), got %v", next)
	}
}

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"* * * *", "61 * * * *", "* 25 * * *", "a * * * *", "* * 0 * *"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}
