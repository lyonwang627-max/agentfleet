# 调研：LangSmith 评测与反馈闭环（eval 引擎对标）

> 2026-11-13 · 深度组件冲刺 B 切片 · 对标对象：LangSmith（LLM 评测与观测的业界标杆）。
> 动机：agentfleet eval 引擎（要点命中+LLM-as-judge+F17 处方闭环）vs LangSmith 全套
> 评测面——验证设计同构性与抓真缺口。

## 一、五类 Evaluator

| 类型 | 语义 | agentfleet 对应 |
|------|------|----------------|
| Code | 确定性函数（非空/可编译/精确匹配） | expectedPoints 命中 + fuzzyHit ✓ |
| LLM-as-judge | reference-free（毒性/语气）+ reference-based（事实准确） | judge 评语 + score ✓ |
| Decision model | 类型化问题（真值概率/固定选项/有序等级）→ 独立 feedback | 未实现 |
| **Pairwise** | 两版本输出对比判定优劣（摘要任务「直接打分难、两两易」） | F18-c compareEvals（score delta 近似——非逐例对比） |
| Human | 人工审查 + rubric | I29 审批门（人审带意见）✓ |

Evaluator 输出统一为 feedback 字典（key + score/value + comment）——**与我们的
EvalCaseResult{Score, Pass, JudgeComment} 形态一致**。

## 二、数据集版本化——agentfleet 真缺口

LangSmith：**example 增删改自动产生新版本**，版本可打标签/在 CI 中定位——保证
「dataset 更新不破坏工作流」与可复现评测。

agentfleet：`SaveCases` 直接覆盖 dataset 的 cases——**数据集变更多次评测间不可比**：
F18-c compare 按 case_id 对齐，dataset 改动后旧 eval 的 case_id 对不上新 cases。
→ **I40 入池：数据集版本化**（cases 变更产生新版本快照，评测 run 记录引用的版本）。

## 三、实验对比与标注闭环

- **Experiment**：同一 dataset × 特定应用版本的评测结果（含每 case 输出/分数/trace）；
  多实验并排对比（不同 prompt/模型）——F18-c compareEvals 已有雏形（verdict/翻转统计）。
- **Annotation Queue**：结构化人工反馈（single-run rubric 审查 / pairwise A/B）；
  **线上发现的问题可直接导出为 dataset 供未来评测**——线下验证修复、线上再确认。
- 与 F17 RSI 处方闭环同构：线上问题 → 诊断 → 处方 → 版本化修复 → 再评测确认 ✓。

## 四、在线 vs 离线

- 离线（agentfleet 现状）：dataset 全量批量，可对照参考答案。
- 在线：production run 实时 reference-free 评测（质量启发式/安全检查）——agentfleet
  缺位；演进方向为 production run 的实时抽样 judge（F16 预审的观测面延伸）。

## 五、明确不抄

- SaaS 托管平台/按 trace 计价（本地零依赖硬约束）。
- Decision model evaluator（Jev 决策模型——个人级三类型够用）。
- ES 全文检索（同 Temporal 调研结论）。

## 六、来源

- [LangSmith Evaluation Concepts（官方）](https://docs.langchain.com/langsmith/evaluation-concepts)
- [Manage Datasets（版本化）](https://docs.langchain.com/langsmith/manage-datasets)
- [Evaluation Types](https://docs.langchain.com/langsmith/evaluation-types)
- [LangSmith Evaluation Platform](https://www.langchain.com/langsmith/evaluation)
