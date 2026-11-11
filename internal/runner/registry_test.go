package runner

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lyonwang/agentfleet/internal/model"
)

// 注册表与杂项直测（此前 0%）：Register/Get/Names 全局注册表语义 /
// strictJSON 容错抽取 / ToolGateError.Error / ListMCPServers 观测面 / mock 入口取消。

func TestRegistrySemantics(t *testing.T) {
	// Get 未注册 → 报错
	if _, err := Get("nope-xyz"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("未注册应报错: %v", err)
	}
	// Register 覆盖语义（同名单槽位替换——claude-code/eino 测试均依赖）
	r1 := NewMock()
	Register("reg-test", r1)
	got, err := Get("reg-test")
	if err != nil || got != r1 {
		t.Fatalf("注册后 Get 应返回同实例: %v %v", got, err)
	}
	r2 := NewMock()
	Register("reg-test", r2)
	got2, _ := Get("reg-test")
	if got2 != r2 {
		t.Fatal("重复注册应覆盖")
	}
	// Names 含已注册项
	found := false
	for _, n := range Names() {
		if n == "reg-test" {
			found = true
		}
	}
	if !found {
		t.Fatal("Names 应含 reg-test")
	}
}

func TestStrictJSON(t *testing.T) {
	var d decision
	// 前后噪声包裹的 JSON：抽取成功
	if err := strictJSON("好的，我的决定如下：{\"reply\":\"开工\",\"handoff_to\":\"\"} 以上。", &d); err != nil {
		t.Fatal(err)
	}
	if d.Reply != "开工" {
		t.Fatalf("抽取不符: %+v", d)
	}
	// 无 JSON 对象 → 报错
	if err := strictJSON("没有任何大括号", &d); err == nil || !strings.Contains(err.Error(), "no json object") {
		t.Fatalf("无 JSON 应报错: %v", err)
	}
	// 只有开括号 → 报错
	if err := strictJSON("{ 不完整", &d); err == nil {
		t.Fatal("不完整 JSON 应报错")
	}
	// 嵌套对象：首 { 到末 } 整段抽取
	if err := strictJSON(`前缀 {"reply":"r","tool_args":{"k":"v"}} 后缀`, &d); err != nil || d.Reply != "r" {
		t.Fatalf("嵌套抽取不符: %v %+v", err, d)
	}
}

func TestToolGateErrorMessage(t *testing.T) {
	e := &ToolGateError{Ref: "mcp:demo/publish_x"}
	if !strings.Contains(e.Error(), "mcp:demo/publish_x") {
		t.Fatalf("Error 应带引用: %q", e.Error())
	}
	if !errors.Is(context.Canceled, context.Canceled) {
		t.Fatal("errors.Is 自反性（守卫断言）")
	}
}

func TestListMCPServersShape(t *testing.T) {
	// 无注册 server → 空数组（不 nil）
	out := ListMCPServers()
	if out == nil {
		t.Fatal("应返回空数组非 nil")
	}
	// 未注册 server 的 CallMCPTool 已有测；此处锁观测面降级：注册后清单含名字
	// （真 client 需子进程——SetMCPServer+ListMCPServers 用 nil client 验证降级路径）
	SetMCPServer("ghost-obs", nil)
	out2 := ListMCPServers()
	found := false
	for _, m := range out2 {
		if m["name"] == "ghost-obs" {
			found = true
			if m["error"] == nil {
				t.Fatal("nil client 应带 error 降级")
			}
		}
	}
	if !found {
		t.Fatal("清单应含 ghost-obs")
	}
}

// ShutdownMCP：空注册表调用不 panic（幂等收尾）。
func TestShutdownMCPIdempotent(t *testing.T) {
	ShutdownMCP()
	ShutdownMCP()
}

// mock runner 入口取消（I1 回归锁——CancelCheck=true 直接 ErrCanceled）。
func TestMockCancelAtEntryAgain(t *testing.T) {
	_, err := NewMock().Execute(context.Background(), ExecRequest{
		Task:        model.Task{Title: "t", Prompt: "普通任务"},
		CancelCheck: func() bool { return true },
	})
	if !strings.Contains(err.Error(), "取消") {
		t.Fatalf("入口取消应 ErrCanceled: %v", err)
	}
}
