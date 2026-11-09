// Package seed 演示工作区：3 个角色 agent + 一条含依赖/handoff/审批门的任务链 + 评测集。
// 幂等：按 agent 名查重，已存在则跳过。
package seed

import (
	"context"
	"fmt"
	"log"

	"github.com/lyonwang/agentfleet/internal/model"
	"github.com/lyonwang/agentfleet/internal/store"
)

func Run(ctx context.Context, st store.Store, runner string, model_ string) error {
	existing, err := st.ListAgents(ctx, store.AgentFilter{})
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		log.Printf("[seed] 已有 %d 个 agent，跳过种子", len(existing))
		return nil
	}

	prompter := &model.Agent{Runner: runner, Model: model_, MaxConcurrency: 2, Enabled: true}
	mk := func(name, role, sys string, mc int) (int64, error) {
		a := *prompter
		a.Name, a.Role, a.SystemPrompt, a.MaxConcurrency = name, role, sys, mc
		return st.CreateAgent(ctx, &a)
	}

	// 角色设定参考企业真实分工：值班响应 → 修复执行 → 评审把关
	intake, err := mk("intake", "值班响应工程师",
		"你是 oncall 值班响应工程师，负责接收告警与工单，做初步定位。能自己处理的直接给出结论；需要修复执行的交接给 fixer；需要评审把关的交接给 reviewer。", 2)
	if err != nil {
		return err
	}
	fixer, err := mk("fixer", "修复执行工程师",
		"你是修复执行工程师，负责给出具体修复方案与执行步骤。涉及对外发布或删除操作时必须 needs_approval=true。", 1)
	if err != nil {
		return err
	}
	reviewer, err := mk("reviewer", "评审与质检",
		"你是评审与质检工程师，负责复核其他 agent 的产出：核对方案完整性、风险与验证步骤，给出最终结论。", 1)
	if err != nil {
		return err
	}
	analyst, err := mk("analyst", "数据分析工程师",
		"你是数据分析工程师，负责从监控、告警与任务记录中提炼数据结论：可用性、故障次数、趋势。产出以数据明细为主，不负责对外文案。", 1)
	if err != nil {
		return err
	}
	summarizer, err := mk("summarizer", "内容汇总与合规",
		"你是内容汇总工程师，负责把数据材料汇总为结构化对外文档；涉及对外发布的产出必须 needs_approval=true 等待人工确认。", 1)
	if err != nil {
		return err
	}

	// 执行环境示例（对标 worktree）：告警修复任务绑定 release-hotfix 环境
	wsID, err := st.CreateWorkspace(ctx, &model.Workspace{
		Name: "release-hotfix", RootPath: "~/workspaces/release-hotfix",
		Branch: "hotfix/p99-latency", Note: "P99 延迟修复的隔离执行环境"})
	if err != nil {
		return err
	}

	// 经验沉淀示例（对标 compound skills）：值班处置 SOP，任务可引用注入
	skillID, err := st.SaveSkill(ctx, &model.Skill{
		Name: "线上告警处置 SOP",
		Tags: []string{"oncall", "运维"},
		Content: "1) 先确认影响面与是否有近期变更；2) 止血优先于定位（回滚/扩容/降级）；" +
			"3) 按 Runbook 逐项检查并记录；4) 恢复后必须留复盘记录；涉及变更需走审批。",
	})
	if err != nil {
		return err
	}

	// 示例小队（I30，对标 Multica squads）：intake 为 leader，fixer/reviewer 为成员
	squadID, err := st.CreateSquad(ctx, &model.Squad{
		Name: "告警响应小队", Description: "P2 告警的接收-修复-复核闭环",
		LeaderID: intake, Instructions: "定位类工作自己做；修复交给 fixer；复核交给 reviewer；发布必须走审批门。"})
	if err != nil {
		return err
	}
	for _, mid := range []int64{fixer, reviewer} {
		_ = st.AddSquadMember(ctx, &model.SquadMember{SquadID: squadID, MemberType: "agent", MemberID: mid})
	}

	// 任务链 1：告警处置全链路——绑定工作区 + 引用 SOP 经验
	t1, err := st.CreateTask(ctx, &model.Task{
		Title: "P2 告警：订单服务 P99 延迟上涨", AgentID: intake, Priority: 8,
		WorkspaceID: wsID, SkillIDs: []int64{skillID},
		Prompt: "orderservice P99 从 120ms 涨到 800ms，监控显示 db 连接池打满。先搜索 runbook 获取处置预案，定位初步原因后 handoff:fixer 执行修复；修复方案涉及变更操作，需人工审批。"})
	if err != nil {
		return err
	}
	// 任务链 2：周报双 agent 链——analyst（数据明细）→ handoff → summarizer（汇总成稿）→ 审批门
	if _, err = st.CreateTask(ctx, &model.Task{
		Title: "生成订单服务本周数据周报", AgentID: analyst, Priority: 5,
		Prompt: "从监控与告警记录生成订单服务本周数据周报。先搜索近期告警与故障记录，产出数据明细（可用性、告警次数、趋势）。完成后 handoff:summarizer 汇总成对外稿件。"}); err != nil {
		return err
	}
	if _, err = st.CreateTask(ctx, &model.Task{
		Title: "周报对外发布", AgentID: summarizer, Priority: 5,
		DependsOn: []int64{t1, t1 + 1},
		Prompt:    "将已确认的数据周报汇总为对外版本并准备发布。此操作涉及对外发布，需要审批。"}); err != nil {
		return err
	}
	// 任务 3：演示 handoff（reviewer 复核）
	if _, err = st.CreateTask(ctx, &model.Task{
		Title: "修复方案复核", AgentID: intake, Priority: 6,
		Prompt: "复核订单服务延迟问题的修复方案，handoff:reviewer 进行质检结论输出。"}); err != nil {
		return err
	}

	// 自动化示例：工作日 09:30 晨会摘要（cron 定时建任务的种子演示）
	if _, err := st.CreateAutomation(ctx, &model.Automation{
		AgentID: intake, Name: "晨会摘要", CronExpr: "30 9 * * 1-5",
		Title: "生成今日晨会摘要", Priority: 6, Enabled: true,
		Prompt: "汇总昨日任务执行情况与今日待办，生成晨会摘要草稿（搜索近期任务时用搜索能力）。"}); err != nil {
		return err
	}

	// 评测集：运维诊断 8 case（要点式评分）
	did, err := st.CreateDataset(ctx, &model.EvalDataset{
		Name: "运维诊断基准 v1", Description: "值班响应质量：定位思路、风险判断、升级时机"})
	if err != nil {
		return err
	}
	cases := []model.EvalCase{
		{Input: "CPU 100%，如何判断是业务流量还是死循环？", Expected: map[string]any{"points": []string{"先看流量是否上涨", "火焰图或 pprof 定位热点", "区分正常扩容与异常循环"}}},
		{Input: "数据库连接池打满的排查顺序？", Expected: map[string]any{"points": []string{"慢查询", "连接泄漏", "流量突增", "限流或扩容连接池"}}},
		{Input: "服务 OOM 后第一时间做什么？", Expected: map[string]any{"points": []string{"保留现场（heap dump）", "先回滚或重启止血", "再分析内存泄漏"}}},
		{Input: "缓存击穿与穿透的区别和对策？", Expected: map[string]any{"points": []string{"击穿是热点 key 过期", "穿透是不存在的 key", "互斥重建", "布隆过滤器或空值缓存"}}},
		{Input: "MQ 消息积压如何应急？", Expected: map[string]any{"points": []string{"扩容消费者", "批量拉取", "评估上游限流", "必要时扩队列分区"}}},
		{Input: "发布后接口错误率升高，回滚还是热修？", Expected: map[string]any{"points": []string{"先快速回滚止血", "保留现场分析根因", "热修需再走发布流程"}}},
		{Input: "如何判断要不要半夜叫醒老板（升级）？", Expected: map[string]any{"points": []string{"影响面评估", "资金或资损相关必须升级", "有预案可不升级"}}},
		{Input: "什么是变更窗口，为什么重要？", Expected: map[string]any{"points": []string{"低峰期变更", "避免与业务高峰叠加", "出问题影响面可控"}}},
	}
	if err := st.SaveCases(ctx, did, cases); err != nil {
		return err
	}

	// 评测集 2：代码评审基准 v1（R2-a）——评审视角的 completeness/风险识别/可维护性
	did2, err := st.CreateDataset(ctx, &model.EvalDataset{
		Name: "代码评审基准 v1", Description: "Code Review 能力：风险识别、性能意识、兼容性与合规"})
	if err != nil {
		return err
	}
	cases2 := []model.EvalCase{
		{Input: "评审：for 循环里逐条 INSERT 数据库，共 1 万条。有什么问题？", Expected: map[string]any{"points": []string{"网络往返放大", "批量写入或 COPY", "事务包裹保证原子性", "失败中断与幂等"}}},
		{Input: "评审：接口响应字段从 int 改成了 string。你关注什么？", Expected: map[string]any{"points": []string{"破坏性变更", "调用方兼容", "灰度与版本策略", "序列化层影响"}}},
		{Input: "评审：goroutine 闭包里直接引用 for 循环变量。", Expected: map[string]any{"points": []string{"变量捕获陷阱", "按值传参", "数据竞争风险", "goroutine 生命周期"}}},
		{Input: "评审：一段下游 HTTP 调用没有设置超时。", Expected: map[string]any{"points": []string{"必须设置超时或 context", "重试与熔断", "连接池占用泄漏", "对整体 SLO 的影响"}}},
		{Input: "评审：同事用 panic 处理业务校验失败。", Expected: map[string]any{"points": []string{"panic 只用于不可恢复错误", "业务错误用 error 传递", "recover 只在边界", "错误码与日志规范"}}},
		{Input: "评审：SELECT * 且无 LIMIT 查千万级大表。", Expected: map[string]any{"points": []string{"列裁剪", "分页或 LIMIT", "覆盖索引", "大表全扫与慢查询风险"}}},
		{Input: "评审：缓存更新逻辑是先删缓存再更新数据库。", Expected: map[string]any{"points": []string{"双写不一致窗口", "先更新 DB 再删缓存", "延迟双删或 binlog 订阅", "并发读旧值风险"}}},
		{Input: "评审：日志语句打印了用户手机号明文。", Expected: map[string]any{"points": []string{"PII 脱敏合规", "日志最小化", "分级与采样", "存储加密与访问审计"}}},
	}
	if err := st.SaveCases(ctx, did2, cases2); err != nil {
		return err
	}

	// 评测集 3：文档问答基准 v1（R2-b）——企业文档要点问答的完整性（FDE 知识库叙事）
	did3, err := st.CreateDataset(ctx, &model.EvalDataset{
		Name: "文档问答基准 v1", Description: "企业文档要点问答：定位准确性、完整性、合规意识"})
	if err != nil {
		return err
	}
	cases3 := []model.EvalCase{
		{Input: "《发布流程 v2.3》：发布前必须完成哪些检查？", Expected: map[string]any{"points": []string{"回归测试通过", "审批单", "监控与告警预案", "回滚方案"}}},
		{Input: "《API 网关接入指南》：限流默认值是多少，如何申请提升？", Expected: map[string]any{"points": []string{"默认 QPS 阈值", "工单申请流程", "容量评估", "生效时间"}}},
		{Input: "《故障定级标准》：什么情况定 P1？", Expected: map[string]any{"points": []string{"资损或大面积不可用", "响应时限", "升级路径", "通报要求"}}},
		{Input: "《数据分级规范》：用户手机号属于哪一级，能否出域？", Expected: map[string]any{"points": []string{"敏感级别判定", "脱敏要求", "出域审批", "加密传输"}}},
		{Input: "《值班手册》：半夜被告警叫醒的第一步是什么？", Expected: map[string]any{"points": []string{"先确认影响面", "止血优先于定位", "按 runbook 检查", "必要时升级"}}},
		{Input: "《SLA 承诺》：核心接口的可用性承诺与赔偿条款是什么？", Expected: map[string]any{"points": []string{"可用性指标", "赔偿触发条件", "免责条款", "统计口径"}}},
		{Input: "《新人文档》：如何申请开发环境的数据库权限？", Expected: map[string]any{"points": []string{"工单流程", "审批人", "权限最小化", "有效期与回收"}}},
		{Input: "《供应商接入规范》：第三方回调接口有哪些安全要求？", Expected: map[string]any{"points": []string{"签名验签", "IP 白名单", "重放防护", "密钥轮换"}}},
	}
	if err := st.SaveCases(ctx, did3, cases3); err != nil {
		return err
	}
	log.Printf("[seed] 完成：%d agents, 4 tasks, 3 datasets(%d+%d+%d cases)", 5, len(cases), len(cases2), len(cases3))
	return nil
}

func Demo() string { return fmt.Sprintf("agentfleet seed") }
