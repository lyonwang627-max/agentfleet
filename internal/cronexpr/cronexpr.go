// Package cronexpr 极简 5 域 cron 表达式求值器（分 时 日 月 周，支持 * / , -）。
// 为什么自带而不引 robfig/cron：平台只需要 Next() 一个能力，120 行可控实现
// 换零依赖与零间接层——控制面软件的依赖洁癖（见 ARCHITECTURE 依赖原则）。
package cronexpr

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Expr struct {
	minute, hour, dom, month, dow map[int]bool
	domStar, dowStar              bool
}

// Parse 解析 5 域 cron。dow: 0-6（0=周日）。dom 与 dow 同时受限时按标准 cron 语义取或。
func Parse(spec string) (*Expr, error) {
	fields := strings.Fields(strings.TrimSpace(spec))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron 需要 5 个域（分 时 日 月 周），得到 %d 个: %q", len(fields), spec)
	}
	e := &Expr{}
	var err error
	if e.minute, err = parseField(fields[0], 0, 59); err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	if e.hour, err = parseField(fields[1], 0, 23); err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	if e.dom, err = parseField(fields[2], 1, 31); err != nil {
		return nil, fmt.Errorf("dom: %w", err)
	}
	if e.month, err = parseField(fields[3], 1, 12); err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	if e.dow, err = parseField(fields[4], 0, 6); err != nil {
		return nil, fmt.Errorf("dow: %w", err)
	}
	e.domStar = fields[2] == "*"
	e.dowStar = fields[4] == "*"
	return e, nil
}

// parseField 解析单域：支持 *、n、*/step、a-b、a-b/step、逗号组合。
func parseField(field string, lo, hi int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("空子项")
		}
		rangePart, stepPart := part, ""
		if i := strings.Index(part, "/"); i >= 0 {
			rangePart, stepPart = part[:i], part[i+1:]
		}
		step := 1
		if stepPart != "" {
			v, err := strconv.Atoi(stepPart)
			if err != nil || v <= 0 {
				return nil, fmt.Errorf("非法步长 %q", stepPart)
			}
			step = v
		}
		start, end := lo, hi
		switch {
		case rangePart == "*":
			// 全域
		case strings.Contains(rangePart, "-"):
			bounds := strings.SplitN(rangePart, "-", 2)
			a, err1 := strconv.Atoi(bounds[0])
			b, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil || a > b || a < lo || b > hi {
				return nil, fmt.Errorf("非法范围 %q", rangePart)
			}
			start, end = a, b
		default:
			v, err := strconv.Atoi(rangePart)
			if err != nil || v < lo || v > hi {
				return nil, fmt.Errorf("非法值 %q", rangePart)
			}
			start, end = v, v
		}
		for v := start; v <= end; v += step {
			out[v] = true
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("域为空")
	}
	return out, nil
}

// Next 返回 after 之后（严格大于）的下一个触发时刻，精度为分钟；找不到返回零值（理论上 4 年内必命中）。
func (e *Expr) Next(after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := after.AddDate(4, 0, 0)
	for t.Before(limit) {
		if !e.month[int(t.Month())] {
			// 跳到下月 1 号 0 点
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
			continue
		}
		if !e.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, 1)
			continue
		}
		if !e.hour[t.Hour()] {
			t = t.Truncate(time.Hour).Add(time.Hour)
			continue
		}
		if e.minute[t.Minute()] {
			return t
		}
		t = t.Truncate(time.Minute).Add(time.Minute)
	}
	return time.Time{}
}

// dayMatches 标准 cron 规则：dom/dow 两者都受限时取「或」，任一为 * 时取「且」（即仅检查受限者）。
func (e *Expr) dayMatches(t time.Time) bool {
	domOK := e.dom[t.Day()]
	// Go: Sunday=0，与 cron 0=周日一致
	dowOK := e.dow[int(t.Weekday())]
	switch {
	case e.domStar && e.dowStar:
		return true
	case e.domStar:
		return dowOK
	case e.dowStar:
		return domOK
	default:
		return domOK || dowOK
	}
}
