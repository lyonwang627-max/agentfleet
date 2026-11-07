# 调研笔记：eino Callbacks（组件级可观测性钩子）

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[Callback User Manual](https://www.cloudwego.io/docs/eino/core_modules/devops/observability/)、[callbacks 包](https://pkg.go.dev/github.com/cloudwego/eino/callbacks)、[Chapter 6 教程](https://www.cloudwego.io/docs/eino/quick_start/chapter_06_callback_and_trace)、[eino-ext/czeloop 集成](https://github.com/cloudwego/eino-ext)

## 机制是什么

eino 的横切关注点方案：`callbacks.Handler` 接口在**每个组件执行的五个生命周期时点**触发（OnStart / OnStartStream / OnEnd / OnEndStream / OnError），携带类型化的回调输入（model 输入输出、tool 调用信息、graph 节点信息）。日志/追踪/指标全部通过 Handler 实现，**不修改组件逻辑**。

注册两级：全局（`callbacks.AppendGlobalHandlers`）或按组件/图运行挂载。社区已有 CozeLoop、Braintrust 等 tracer handler，导出 trace 时保留 graph/agent→model→tool 的**父子关系**。

## 怎么实现

Handler 模式（观察者）：框架在组件执行前后/出错时调用 handler，输入是强类型结构；HandlerBuilder / TimingChecker 是官方便捷封装。本质与我们 runner 的 `Recorder` 回调同构，但颗粒度到组件生命周期且由框架契约保证触发。

## 对 agentfleet 的可借鉴点

对照 F7 适配器：我们的 EinoChatModel 实现 Generate 时**没有接 eino 回调**——网关侧的用量/trace 靠 gateway 层自采。可借鉴：
1. **回调化 trace 注入**：给 EinoChatModel 挂一个把 model 输入输出转成 fleetd run_steps 的 Handler——eino 编排层的步骤就自动进我们的 trace，不再要求编排代码手动 emit（对应我们 runner Recorder 的 eino 侧等价物）。
2. **五时点 vs 我们两时点**：我们的 Recorder 只有「步完成」一个时点；eino 的 OnStart/OnError 分离让「开始时间」与「错误路径」有独立锚点——启发 F9/F14 的错误路径 trace 细化（当前失败步只有 error 字符串）。
3. **父子关系保留**：CozeLoop handler 保留 graph→model→tool 层级——我们的 trace 是平铺 steps，父子层级（handoff 链/多轮循环嵌套）可借鉴其 span 嵌套思想。

## 明确不抄的部分与理由

- **全局 Handler 注册**：fleetd 是单控制面多 runner，全局注册会跨 runner 泄漏上下文；按组件挂载（构建时注入）更贴合我们的显式依赖风格。
- **CozeLoop/Braintrust 外部 tracer**：本地零依赖是硬约束；借其「父子关系保留」思想，不引外部 SaaS。

## 落地建议

进 IDEAS 池为 **I15 eino 回调桥接**（S 量级：`-tags eino` 下实现 callbacks.Handler，把 eino 组件生命周期事件转成 fleetd run_steps；需要 eino 构建环境验证——依赖 `-tags eino` 测试链路已通）。
