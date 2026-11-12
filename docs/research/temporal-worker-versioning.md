# 调研：Temporal Worker Versioning（版本化执行体的升级语义）

> 2026-11-13 · 深度组件冲刺 B 切片 · 对标对象：Temporal Worker Versioning（2025 Worker
> Deployment Versions 模型）。动机：I11 跨版本续跑拒绝是「硬 Pinned」语义——Temporal 是
> 版本化执行体升级的业界标准（Pinned/AutoUpgrade/灰度），对读确认语义与演进路径。

## 一、Deployment Versions 模型

- **标识**：Deployment Version = deployment name（聚合跨版本 Workers）+ Build ID（一次发布）。
  Worker 启动轮询时上报自己的版本。
- **三版本定义**：
  - **Current**：新 Workflow 默认路由处（除非被 pinned 在别的版本）。
  - **Ramping**：按百分比灰度（[0,100]），分桶按 **Workflow ID 确定性**——同一 Workflow 判定一致。
  - **Target**：Auto-Upgrade Workflow 的升级去向（Current 或 Ramping）。
- **两种 Versioning Behavior**：
  - **Pinned**：保证在单一版本上完成（迁移须显式 `workflow update-options`）。
  - **AutoUpgrade**：Current 变更自动迁移，但**须手动保持 replay-safe（patching）**。
- 状态机：Inactive → Active → Draining（仍有 open pinned）→ Drained。

## 二、对 I11 的对读（核心）

| Temporal | agentfleet I11 现状 | 同构判定 |
|----------|--------------------|---------|
| Pinned（单版本完成） | 跨版本续跑拒绝（run 锁定其版本） | **完全同构** |
| AutoUpgrade（Current 变更自动迁移+patching） | replay 端点（清零重试=完全重跑新版本代码） | **同构**（我们的「完全重跑」天然 replay-safe——无 history 重放，比 patching 简单） |
| Versioning Override 优先于继承 | 版本拒绝优先于重放通道 | 同构 |
| Cron 永不继承版本 | automation 建的任务每次独立 | 同构 |

**结论**：I11 的「硬拒绝 + 完全重跑」= Temporal Pinned + AutoUpgrade 的组合简化版——
我们的 Workflow 无 history 重放需求（步骤观察显式落库），**不需要 patching 机制**，
语义更简且正确。业界标准验证了现设计。

## 三、Ramping 灰度（唯一有分量的借鉴点）

- 确定性分桶（Workflow ID → 95%/5%）保证同一 Workflow 判定一致——不依赖协调时序。
- 对 agentfleet：发布新 agent 版本时**按 task ID 百分比灰度**路由新任务（Pinned run 不受影响）——
  平滑升级能力。I38 入池（可选演进，非缺陷：当前手动 publish+立即全量切换足够个人级）。

## 四、明确不抄

- Deployment Versions 服务端状态机（Draining/Drained 生命周期）——单机无长驻 worker 集群。
- AutoUpgrade 的 patching 机制——无 history 重放。
- Worker 轮询上报——我们的版本随 run 落库（agent_version 列）。

## 五、来源

- [Worker Versioning（官方 2025 模型）](https://docs.temporal.io/worker-versioning)
- [Worker Versioning（legacy Build ID 模型）](https://docs.temporal.io/encyclopedia/worker-versioning-legacy)
- [Keith Tenzer: Temporal Worker Versioning](https://keithtenzer.com/temporal/Temporal_Worker_Versioning)
- [Migration Guide（GitHub #4433）](https://github.com/temporalio/documentation/issues/4433)
