# 调研：Dify plugin daemon（插件化工具运行时）

> 2026-11-13 · 深度组件冲刺 B 切片 · 对标对象：Dify plugin daemon（dify-plugin-daemon，
> 2025 plugin 体系重构）。动机：I8 统一工具池之后，工具生态的下一步是「插件化」——
> Dify 是该方向最完整的生产实现（独立 daemon 进程/四类插件/多 runtime）。

## 一、拆分动机（为什么 daemon 独立于 API server）

- 原设计「models/tools 随 Dify 一起安装」——代码与主仓库紧耦合，**依赖冲突与版本管理**
  是核心痛点；多 workspace 下不能靠挂载源码目录解决。
- 拆分后：插件可独立安装/卸载/运行；daemon 是 API server 与插件间的中间人。

## 二、四类插件语义

| 类型 | 语义 | agentfleet 对应 |
|------|------|----------------|
| Tool | 工具（原随主体安装，现独立选用） | MCP server（stdio 子进程） |
| Model | 模型，可「作为工具」用（OCR/ASR/TTS） | gateway.ListModels 动态发现 |
| Endpoint | **外部平台 webhook 接入**——生成随机 URL 收 Discord 等请求转发给插件，插件无需长驻 HTTP server | **缺位**——I7 只有出站，入站是空位 |
| Agent-strategy | 自定义 agent 策略，经**反向调用**（Reverse Call：参数接收/操作执行/结果交付）插拔 | runner 注册表（mock/llm/claude-code/eino） |

## 三、runtime 三形态

- **Local**：插件为父进程管理的 subprocess（stdin/stdout 管道，含依赖安装），一键 compose 部署——
  **与 agentfleet MCP stdio 客户端完全同构**（JSON-RPC 2.0 换行分帧 vs Dify 管道协议）。
- **Serverless**：SaaS 版 AWS Lambda 弹性扩展（十万级用户×自定义插件数）——规模驱动选型。
- **Debug**：本地插件与 Dify 建 TCP 长连接即视为已安装（调试态）；无状态服务承接有状态长连接
  的方案借鉴 etcd——Redis HashMap 管插件连接状态保证请求转发正确 pod + IP 池投票测可达 +
  master 定期健康检查清退。**多实例部署时的有状态连接治理参考**。

## 四、安全面

- 安装基于**非对称签名**（审核过的插件私钥标记 certified；未签名默认拒装）。
- 插件必须声明权限与数据存储策略，未声明直接拒。

## 五、对 agentfleet 的借鉴

1. **I37 入池候选：入站 webhook endpoint**（Dify Endpoint 类型对读——与 I7 出站对称）：
   平台生成一次性 URL → 外部系统回调 → 校验/映射 → 自动建任务。FDE 集成叙事补全
   （双向集成：告警系统进=入站、结果通知出=出站）。S-M 量级。
2. **反向调用模式确认**：I34 CC runner 的「外部进程驱动」若进一步做双向（外部 agent
   回调平台 API 查任务上下文/发事件），即为 Dify Reverse Call 同构——已有基础（CC 可调
   fleetd API），无需新基建。
3. **MCP 已覆盖工具插件面**：Dify local runtime 与 agentfleet MCP stdio 同构——不引入
   plugin daemon 服务，MCP + ToolProvider（I8）组合已是轻量等价实现。

## 六、明确不抄

- 独立 plugin daemon 服务（MCP stdio 子进程已是轻量等价；daemon 的依赖安装/市场/多
  workspace 复杂度不匹配单用户平台）。
- Serverless runtime（规模不符）。
- 插件市场/签名审核链（单用户自部署，无第三方分发）。

## 七、来源

- [dify-plugin-daemon（GitHub）](https://github.com/langgenius/dify-plugin-daemon)
- [Dify Plugin System: Design and Implementation（官方博客）](https://dify.ai/blog/dify-plugin-system-design-and-implementation)
- [Dify 架构拆解（Dwarves Memo）](https://memo.d.foundation/breakdown/dify)
- [AWS 案例：可扩展安全插件平台](https://aws.amazon.com/solutions/case-studies/dify-lambda-case-study)
