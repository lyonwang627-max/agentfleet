package cronexpr

import (
	"strings"
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

// Next 跳转分支直测：跳月/跳日/小时推进/无匹配零值/二月 30 号边界。
func TestNextMonthSkip(t *testing.T) {
	// 只在 1 月执行：11 月的下一命中应跨月跳到明年 1 月 1 日 0 点
	e := mustParse(t, "0 0 1 1 *")
	next := e.Next(time.Date(2026, 11, 10, 15, 0, 0, 0, time.Local))
	want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("got %v want %v", next, want)
	}
}

func TestNextDaySkip(t *testing.T) {
	// 只在 1 号执行：从 11 月 10 日跳到 12 月 1 日
	e := mustParse(t, "0 12 1 * *")
	next := e.Next(time.Date(2026, 11, 10, 12, 30, 0, 0, time.Local))
	want := time.Date(2026, 12, 1, 12, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("got %v want %v", next, want)
	}
}

func TestNextHourAdvance(t *testing.T) {
	// 每天只 9 点：8:30 的下一命中 9:00（同日小时推进）
	e := mustParse(t, "0 9 * * *")
	next := e.Next(time.Date(2026, 11, 10, 8, 30, 0, 0, time.Local))
	want := time.Date(2026, 11, 10, 9, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("got %v want %v", next, want)
	}
}

func TestNextNoMatchZero(t *testing.T) {
	// 2 月 30 号：永不命中 → 4 年搜索耗尽返回零值
	e := mustParse(t, "0 0 30 2 *")
	next := e.Next(time.Date(2026, 11, 10, 0, 0, 0, 0, time.Local))
	if !next.IsZero() {
		t.Fatalf("2月30号应无匹配零值: %v", next)
	}
}

func TestNextFeb29(t *testing.T) {
	// 2 月 29 号（闰年语义）：从 2026 出发应命中 2028-02-29
	e := mustParse(t, "0 0 29 2 *")
	next := e.Next(time.Date(2026, 11, 10, 0, 0, 0, 0, time.Local))
	want := time.Date(2028, 2, 29, 0, 0, 0, 0, time.Local)
	if !next.Equal(want) {
		t.Fatalf("got %v want %v", next, want)
	}
}

// parseField/Parse 错误路径与组合形态（91.7%→~98%）。
func TestParseFieldErrorsAndShapes(t *testing.T) {
	// 非法形态 → 全部报错
	bad := []string{
		"*/0 0 * * *",    // 步长非正
		"*/-1 0 * * *",   // 步长负数
		"1-70 * * * *",   // 分钟范围越界（>59）
		"0 24 * * *",     // 小时越界
		"0 0 32 * *",     // 日越界
		"0 0 0 13 *",     // 月越界
		"0 0 0 0 7",      // 周越界（0-6）
		"abc * * * *",    // 非数字
		"1-abc * * * *",  // 范围非数字
		"40-1 * * * *",   // 范围倒置
		",,, * * * *",    // 空子项
		"1,,2 * * * *",   // 中间空子项
	}
	for _, spec := range bad {
		if _, err := Parse(spec); err == nil {
			t.Fatalf("非法 spec 应报错: %q", spec)
		}
	}
	// 组合形态：逗号 + 范围 + 步长混合
	e := mustParse(t, "0,15-30/5,45 9-11/2 * * 1-5")
	next := e.Next(time.Date(2026, 11, 13, 9, 0, 0, 0, time.Local)) // 周五 9:00
	if next.Format("15:04") != "09:15" {
		t.Fatalf("混合形态首次命中不符: %v", next)
	}
	// 9:46 之后：9 点分钟域已尽 → 跳到 11 点（hour 9-11/2=9,11）的 11:45
	next2 := e.Next(time.Date(2026, 11, 13, 9, 46, 0, 0, time.Local))
	if next2.Format("15:04") != "11:00" {
		t.Fatalf("跨小时命中不符: %v", next2)
	}
}

func TestParseFieldErrorMessages(t *testing.T) {
	// 错误信息带域前缀（排查友好）
	_, err := Parse("70 * * * *")
	if err == nil || !strings.Contains(err.Error(), "minute") {
		t.Fatalf("错误应带域前缀: %v", err)
	}
	_, err = Parse("* * * * 9")
	if err == nil || !strings.Contains(err.Error(), "dow") {
		t.Fatalf("错误应带域前缀: %v", err)
	}
}
