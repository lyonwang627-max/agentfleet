package runner

import (
	"errors"
	"strings"
	"testing"
)

// 多轮工具循环（F11-a）单测：fake 驱动三种轨迹——直达最终/工具回合/步数耗尽强制收尾。
func TestRunToolLoopDirect(t *testing.T) {
	calls := 0
	d, obs, steps, err := runToolLoop("sys", "user",
		func(system, user string) (decision, error) {
			calls++
			return decision{Reply: "直接完成"}, nil
		},
		func(ref string, args map[string]any) (string, error) { return "", errors.New("不应调用") },
		nil)
	if err != nil || d.Reply != "直接完成" || steps != 1 || len(obs) != 0 || calls != 1 {
		t.Fatalf("直达轨迹异常: d=%+v steps=%d obs=%v calls=%d err=%v", d, steps, obs, calls, err)
	}
}

func TestRunToolLoopWithObservation(t *testing.T) {
	turn := 0
	var lastUser string
	d, obs, steps, err := runToolLoop("sys", "base-prompt",
		func(system, user string) (decision, error) {
			turn++
			lastUser = user
			if turn == 1 {
				return decision{Tool: "mcp:demo/echo", ToolArgs: map[string]any{"msg": "hi"}}, nil
			}
			return decision{Reply: "基于观察的最终结论"}, nil
		},
		func(ref string, args map[string]any) (string, error) { return "echo: hi", nil },
		nil)
	if err != nil || steps != 2 || len(obs) != 1 || obs[0] != "echo: hi" {
		t.Fatalf("工具轨迹异常: d=%+v steps=%d obs=%v err=%v", d, steps, obs, err)
	}
	// 观察结果必须回灌进下一轮 prompt
	if !strings.Contains(lastUser, "已执行工具 mcp:demo/echo") || !strings.Contains(lastUser, "echo: hi") {
		t.Fatalf("观察未回灌: %q", lastUser)
	}
	if !strings.Contains(lastUser, "base-prompt") {
		t.Fatal("基础 prompt 丢失")
	}
}

func TestRunToolLoopStepLimit(t *testing.T) {
	turns := 0
	d, obs, steps, err := runToolLoop("sys", "user",
		func(system, user string) (decision, error) {
			turns++
			return decision{Tool: "mcp:demo/echo"}, nil // 永远要工具
		},
		func(ref string, args map[string]any) (string, error) { return "obs", nil },
		nil)
	if err != nil {
		t.Fatal(err)
	}
	if steps != maxToolSteps || len(obs) != maxToolSteps || turns != maxToolSteps {
		t.Fatalf("步数上限未生效: steps=%d obs=%v turns=%d", steps, obs, turns)
	}
	// 步数耗尽强制收尾：带说明的 reply（不失败——已有观察仍可用）
	if !strings.Contains(d.Reply, "已达工具调用步数上限") {
		t.Fatalf("强制收尾缺失: %+v", d)
	}
}

func TestRunToolLoopToolErrorBecomesObservation(t *testing.T) {
	turn := 0
	d, obs, _, err := runToolLoop("sys", "user",
		func(system, user string) (decision, error) {
			turn++
			if turn == 1 {
				return decision{Tool: "mcp:demo/echo"}, nil
			}
			return decision{Reply: "工具失败后改用已有信息完成"}, nil
		},
		func(ref string, args map[string]any) (string, error) { return "", errors.New("server down") },
		nil)
	if err != nil {
		t.Fatal(err)
	}
	// 工具失败转成观察（不中断循环），下一轮能看到错误信息
	if len(obs) != 1 || !strings.Contains(obs[0], "server down") {
		t.Fatalf("工具错误应成为观察: %v", obs)
	}
	if !strings.Contains(d.Reply, "完成") {
		t.Fatalf("失败后应继续决策: %+v", d)
	}
}
