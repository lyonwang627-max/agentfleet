# 调研笔记：Celery 任务撤销语义（Revoke/Terminate 与取消传播）

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[Celery Workers Guide](https://docs.celeryq.dev/en/stable/userguide/workers.html)、[Tasks 用户指南](https://docs.celeryq.dev/en/stable/userguide/tasks.html)、[GitGuardian 任务韧性](https://blog.gitguardian.com/celery-tasks-retries-errors)

## 机制是什么

Celery 的撤销分两档：**revoke**（默认）把任务 ID 加入 worker 的撤销集合——未开始的任务被跳过，**已运行的不受影响**；**terminate=True** 向执行子进程发 SIGTERM/SIGKILL——能杀在跑的任务，但是「重锤」（可能连带 worker 池、且有时序竞态：撤销广播到达时任务可能已开始/结束，状态还不更新）。撤销状态默认驻内存，worker 重启即丢（需 persist）。

社区共识的取消三原则：**协作式取消**（任务内部检查取消标志，如 AbortableTask）、**幂等设计**（worker 丢失时消息重投）、**优雅关闭**（SIGTERM → 先完成在途任务）。

## 怎么实现

revoke 集合广播 → worker 认领时查集合跳过；terminate 走信号。竞态根源：撤销请求与任务执行的**时序不可控**（广播延迟、任务恰好已开始）。

## 对 agentfleet 的可借鉴点

对照现状：cancel 端点只改任务状态（running 任务的 runner 会继续跑完然后被状态守卫拒绝写入——「取消后仍变 done」的错乱已观察过）。借鉴 Celery 教训，I1 取消传播的正确设计：
1. **两档语义**（Celery 对齐）：cancel 默认=协作式（任务本体状态改 canceled，runner 拿到的 execCtx 加 cancel 标记——runner 在工具步间检查，类似 AbortableTask）；**不做 terminate**（杀进程属重锤，个人平台无此需求）。
2. **runner 侧检查点**：多轮循环（F11）每轮开始时检查任务状态是否仍 running——已取消则立即返回。这与 F14 看门狗共用「调度器可中断 runner」的通道。
3. **幂等保护**：canceled 是终态，状态守卫已保证 cancel 后 runner 结果被拒——需要补的是「cancel 后再 dispatch」的竞态（claim 后 cancel 到达），解法=runner 开始时二次确认任务状态（从 DB 重取，Celery 教训 #5 的同源）。

## 明确不抄的部分与理由

- **terminate/SIGKILL**：runner 是 fleetd 进程内函数调用而非独立子进程，无进程可杀；ctx 取消即协作式中断，语义更干净。
- **广播撤销集合**：我们的 cancel 直接写 DB 状态（持久化天然具备，Celery 的 persist 问题不存在），无需额外广播机制。

## 落地建议

**I1 实现方案据此定稿**（进实现时照此执行）：
1. `ExecRequest` 加 `CancelCheck func() bool`（调度器注入：查任务状态是否仍 running）。
2. llm/mock runner 在每个工具步开始前调 CancelCheck——已取消则返回 `ErrCanceled`。
3. cancel 端点保持现状（状态改 canceled）；runner 返回 ErrCanceled 时调度器不重试不告警，任务保持 canceled。
4. 集成测试：cancel 后 runner 在下一个工具步边界退出，任务终态 canceled。
量级 M（runner 三处插桩 + 调度器 ErrCanceled 分支 + 测试）。
