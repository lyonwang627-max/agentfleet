// Package runtime 执行环境抽象（I16，对标 OpenHands Runtime）：
// 「在 workspace 中执行」先具备可探测的健康语义——workspace 不再只是元数据绑定。
// 三实现路线：Mock（恒就绪，测试/演示）/ Local（本机目录+git 分支校验）/ Docker（第 2 轮）。
package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lyonwang/agentfleet/internal/model"
)

// Status 运行时健康三态（与 workspace 归档语义 status=active/archived 正交）。
type Status string

const (
	StatusProvisioning Status = "provisioning" // 探测中/尚未探测
	StatusReady        Status = "ready"        // 可执行
	StatusDegraded     Status = "degraded"     // 不可执行（原因见 Note）
)

// Runtime 执行环境探测器：判定 workspace 当前能否承载执行。
type Runtime interface {
	Name() string
	Probe(ctx context.Context, ws *model.Workspace) (Status, string)
}

// Mock 恒就绪（测试/无本机环境演示）。
type Mock struct{}

func (Mock) Name() string { return "mock" }
func (Mock) Probe(_ context.Context, _ *model.Workspace) (Status, string) {
	return StatusReady, "mock 恒就绪"
}

// Local 本机目录运行时：目录存在 + git 分支匹配（若 .git 存在且配置了 Branch）。
type Local struct{}

func (Local) Name() string { return "local" }

func (Local) Probe(ctx context.Context, ws *model.Workspace) (Status, string) {
	if strings.TrimSpace(ws.RootPath) == "" {
		return StatusDegraded, "未配置 root_path"
	}
	if info, err := os.Stat(ws.RootPath); err != nil || !info.IsDir() {
		return StatusDegraded, fmt.Sprintf("目录不存在或非目录: %s", ws.RootPath)
	}
	if _, err := os.Stat(ws.RootPath + "/.git"); err == nil && strings.TrimSpace(ws.Branch) != "" {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, "git", "-C", ws.RootPath, "rev-parse", "--abbrev-ref", "HEAD")
		out, err := cmd.Output()
		if err != nil {
			return StatusDegraded, "git 分支探测失败: " + err.Error()
		}
		branch := strings.TrimSpace(string(out))
		if branch != ws.Branch {
			return StatusDegraded, fmt.Sprintf("分支不匹配: 当前 %s，配置 %s", branch, ws.Branch)
		}
	}
	return StatusReady, "目录与分支校验通过"
}
