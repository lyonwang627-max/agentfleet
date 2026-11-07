# 调研笔记：Dify 应用版本发布机制（Draft/Publish 与环境隔离）

> 2026-11-07 · 自驱迭代 B 类切片 · 参考：[Dify Version Control 官方文档](https://docs.dify.ai/en/cloud/use-dify/build/version-control)、[Dify 1.18.0 Release Notes](https://github.com/langgenius/dify/releases)

## 机制是什么

Dify 的版本控制生命周期：①所有编辑都在 **draft** 上进行，不影响运行中的应用；②**publish** 把 draft 快照成一个不可变发布版本；③每次 publish 产生新版本；④可把旧版本 **restore 回 draft** 继续编辑；⑤改完再发布。核心是 **draft 与 published 状态分离**：终端用户只看到 published 快照，编辑不破坏生产。

1.18.0 增强：**draft hash** 追踪草稿是否相对上次发布发生分叉（驱动「可恢复/可发布」判定与版本还原）；环境变量机制；便携应用包。1.19.0 加了嵌套工作流 tracing。替代路线：工作流导出 YAML 进 Git 做多环境（dev/staging/prod）管理。

## 怎么实现

draft 与 published 分表/分状态存储，发布即快照拷贝；draft hash = 草稿配置指纹，与最近发布版本指纹比对得出「已分叉」状态。导入导出默认导 draft，可选导指定历史版本——进一步印证两态分离。

## 对 agentfleet 的可借鉴点

对照 F2 的 Agent 版本管理：我们已有 publish/rollback（链只前进），**缺的是 draft 概念**——现在 PATCH /agents/{id} 直接改运行时配置（编辑即上线），Dify 的 draft/published 分离正是为了解决这个问题：
1. **agents 表加 draft_config 字段**：PATCH 只改 draft；POST /publish 才把 draft 快照为版本并生效到运行时配置。正在跑的任务不受编辑影响。
2. **draft hash**：草稿指纹 vs 最新发布指纹，前端展示「有未发布变更」徽章（我们实弹修过的「配置热改影响在途 run」问题与此同源——调度器在执行前 GetAgent 取的就是运行时配置）。
3. **restore to draft ≠ 直接发布**：回滚可以先落 draft 供二次编辑——比我们现在「回滚即发布」多一档保守度。

## 明确不抄的部分与理由

- **YAML 导出 + Git 多环境管理**：对我们是过度方案——单控制面 + 版本链已可回溯，引入 Git 依赖只为多环境，收益不匹配当前规模。
- **环境变量机制**：agentfleet 的模型/定价已走 env，Dify 的应用内环境变量面向多租户 SaaS 场景，暂无此需求。

## 落地建议

进 IDEAS 池为 **I14 draft/published 分离**（M 量级：agents.draft_config 列 + PATCH 语义迁移 + draft hash + 前端「未发布变更」徽章；F2 的 publish/rollback 语义保留，回滚改落 draft）。这是把 F2 从「版本记录」升级为「真正的发布流程」。
