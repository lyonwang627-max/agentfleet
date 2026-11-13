# 调研：OpenAI Structured Outputs（JSON 协议可靠性对标）

> 2026-11-14 · 深度组件冲刺 B 切片 · 对标对象：OpenAI Structured Outputs（json_object
> vs json_schema strict）。动机：agentfleet gateway 的 `format:"json"`（Ollama 原生）
> 属 json_object 级保证——与 strict 模式的差距是 JSON 协议可靠性的核心问题。

## 一、两级保证的本质差异

- **json_object**：只保证**语法合法**——模型仍可能省略必填字段、幻觉属性名、类型错。
  （gateway 现状：`format:"json"` 即此级——决策协议靠「解析失败降级/字段校验」补偿。）
- **json_schema strict**：**grammar-constrained decoding**（CFG 约束采样）——每步生成
  掩码非法 token，schema 违反**结构性不可能**。学术背书：ACL 2025 GCD 逻辑解析、
  ICML 2025 高效 GCD——格式合规可证明。

## 二、关键警告：失败「更稀有但更灾难性」（Simon Willison）

两种 JSON 模式下失败形态改变：模型可能陷入**退化循环**产出 schema 合法但无用的输出
（如空数组/全默认值），而无约束输出失败得更优雅（可检测的垃圾）。语义正确性 ≠ 格式
合规——**constrained decoding 修结构不修内容**（arXiv "When Correct Isn't Usable"）。

仍存在的失败模式（Reddit 288 次实验）：`max_tokens` 截断、拒答、语义非法但 schema
合法——**校验 + 重试逻辑仍是标准实践**。

## 三、与 agentfleet JSON 协议对读

| 层 | OpenAI strict | agentfleet 现状 | 缺口 |
|----|--------------|----------------|------|
| 语法合法 | json_object | format:"json" ✓（Ollama 原生） | 无 |
| schema 合法 | json_schema CFG | 无（手写 Unmarshal + 校验） | Ollama 支持 format=json_schema?（部分版本支持 structure 输出） |
| 语义校验 | 应用层 | decision 结构体校验 + I28 幻觉引导 + F16 预审 ✓ | 等价实现 |
| 失败处理 | 校验+重试 | 解析失败降级纯文本 / I28 引导回灌 ✓ | 等价实现 |

**结论**：我们的「format:json + 应用层校验 + 失败降级」组合在 Ollama 生态下是
**json_object 级的最优实践**——Ollama 的 structured outputs（json_schema）支持
随版本演进，若可用可直接升级到 strict 级（gateway 一处改动：payload.format 传 schema）。

## 四、可借鉴设计

1. **gateway JSON 通道升级预留**：`ChatJSON` 加可选 schema 参数——Ollama 新版
   `format` 接受 JSON Schema（grammar 约束），一次改动从 json_object 升 strict 级。
2. **决策协议 schema 固化**：decision 结构体（reply/handoff_to/tool/needs_approval）
   作为 schema 传入——与 I28 工具能力清单互补（清单管选择、schema 管格式）。
3. **Willison 警告的应用**：I28 幻觉引导回灌已覆盖「schema 合法但内容错」——
   引导重试是语义层的解法 ✓。

## 五、明确不抄

- Pydantic 重试循环全家桶（Go 侧手写校验已有）。
- OpenAI 专有 response_format 参数（Ollama 兼容层自行映射）。

## 六、来源

- [Structured model outputs（OpenAI 官方）](https://developers.openai.com/api/docs/guides/structured-outputs)
- [Simon Willison：Introducing Structured Outputs](https://simonwillison.net/2024/Aug/6/openai-structured-outputs)
- [Grammar-Constrained Decoding（ACL 2025）](https://arxiv.org/abs/2404.17780)
- [When Correct Isn't Usable（arXiv）](https://arxiv.org/abs/2411.08168)
