//go:build eino

package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/lyonwang/agentfleet/internal/gateway"
	"github.com/lyonwang/agentfleet/internal/model"
)

// eino 适配器（F7）单测：消息角色转换 + mock 网关下的确定性 Generate + Execute JSON 协议。
func TestEinoChatModelGenerate(t *testing.T) {
	gw := gateway.New("mock")
	cm := NewEinoChatModel(gw)
	out, err := cm.Generate(context.Background(), []*schema.Message{
		schema.SystemMessage("你是测试助手"),
		schema.UserMessage("hello eino"),
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if out == nil || out.Role != schema.Assistant || !strings.Contains(out.Content, "hello eino") {
		t.Fatalf("输出异常: %+v", out)
	}
}

func TestEinoRunnerExecute(t *testing.T) {
	gw := gateway.New("mock")
	r := NewEinoRunner(gw)
	recorder := func(_ context.Context, s *model.RunStep) error { return nil }
	res, err := r.Execute(context.Background(), ExecRequest{
		Agent:    model.Agent{Name: "tester", Model: "mock"},
		Task:     model.Task{Title: "eino 链路验证", Prompt: "普通任务，直接完成。"},
		Recorder: recorder,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Reply == "" {
		t.Fatal("reply 为空")
	}
}
