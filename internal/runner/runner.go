// Package runner 定义可插拔的 Agent 执行运行时。
// v1 内置 mock（确定性重放，演示/测试用）与 llm（本地模型 JSON 协议）；
// Eino、外部 CLI runner 等通过 Register 接入（见 ROADMAP F7）。
package runner

import (
	"context"
	"fmt"
	"sync"

	"github.com/lyonwang/agentfleet/internal/model"
)

// Recorder 由调度器注入，runner 边执行边落 step（trace 实时可看）。
type Recorder func(ctx context.Context, s *model.RunStep) error

// ArtifactDraft runner 产出的结构化产物（调度器负责落库成 Artifact）。
type ArtifactDraft struct {
	Kind    string `json:"kind"` // report | patch | diff | data | log
	Name    string `json:"name"`
	Content string `json:"content"`
}

type ExecRequest struct {
	Agent     model.Agent
	Task      model.Task
	Recorder  Recorder
	Workspace *model.Workspace // 任务绑定的执行环境（nil=未绑定）
	Skills    []model.Skill    // 任务引用的经验（已由调度器解析，注入参考信息）
	// ModelOverride 任务级路由的模型覆盖（F18-b；空=用 agent.Model）
	ModelOverride string
}

type Result struct {
	Reply         string // 任务产出（写入 task.result）
	HandoffTo     string // 非空 = 交接给该名字的 agent
	Note          string // handoff / 审批说明
	NeedsApproval bool   // true = 任务进入人工审批门（task.status=review）
	Artifacts     []ArtifactDraft // 结构化产物（报告/补丁等，落 artifacts 表）
}

type Runner interface {
	Execute(ctx context.Context, req ExecRequest) (Result, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Runner{}
)

func Register(name string, r Runner) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = r
}

func Get(name string) (Runner, error) {
	mu.RLock()
	defer mu.RUnlock()
	r, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("runner %q not registered", name)
	}
	return r, nil
}

func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
