# CloudOps Copilot Phase 1 实施方案

> 方案版本：v1.0
> 制定日期：2026-05-07
> 依据文档：`docs/design.md` v3.1
> 阶段定位：ChatOps 合并入口，完成 server-web 内嵌 Copilot Chat API 与只读查询闭环。

---

## 1. 阶段目标

Phase 1 的目标是在不重写 `server-monitor` 现有监控告警底座的前提下，将 `chatops` 原型能力以可控方式合并到 `server-web`，形成第一版可用的 Copilot 对话入口。

本阶段完成后，用户可以在现有登录态下通过自然语言查询主机、指标和告警数据；后端能够识别基础意图、调用只读工具、生成结构化回复，并复用现有认证、权限、Redis、限流、日志和 Trace 能力。

### 1.1 核心交付物

| 交付物 | 内容 | 验收标准 |
|---|---|---|
| Copilot 后端模块 | `server-web` 新增 `copilot` 相关 handler/service/nlu/tool 代码 | `POST /api/v1/copilot/chat` 可登录访问并返回结构化响应 |
| 只读工具闭环 | 支持主机、指标、活跃告警、告警事件、告警历史、受控 PromQL 查询 | 工具结果来自真实 `server-monitor` 数据源或可测试 mock |
| 会话短期存储 | Redis 保存最近会话消息与元数据 | 会话可创建、追加、读取，TTL 生效 |
| 前端 Copilot 页面 | 在现有 Vue 控制台增加基础对话页面 | 登录用户可发送消息并看到回复、意图和工具调用状态 |
| 配置与部署说明 | 补充本地、Docker Compose、Helm 所需配置项 | 不泄露密钥，敏感配置走环境变量或 Secret |
| 测试与验收记录 | 单元测试、接口测试、回归检查 | 不影响现有监控、告警、认证和 WebSocket 链路 |

### 1.2 本阶段不做

1. 不实现 Kubernetes 工具。
2. 不实现 RAG / Runbook 检索。
3. 不生成持久化诊断报告。
4. 不实现异步 Diagnosis Worker。
5. 不实现 PendingAction、审批、审计和写操作执行。
6. 不引入 LangChain、向量数据库或独立 ChatOps 微服务。
7. 不让 LLM 直接执行任何写操作。

---

## 2. 范围边界

### 2.1 需要复用的现有能力

| 能力 | 复用方式 |
|---|---|
| Gin 路由与中间件 | Copilot API 挂载到现有 `server-web` API 路由组 |
| JWT / RBAC | 所有 Copilot API 要求登录，Phase 1 不区分 admin/viewer 的只读能力 |
| Redis | 复用现有 Redis 客户端保存会话、工具短缓存和限流状态 |
| RateLimit | 复用现有 Redis 滑动窗口限流，避免 LLM 与查询接口被滥用 |
| Logger / Recovery | 复用现有日志、异常恢复和请求上下文 |
| OpenTelemetry | 工具调用、LLM 调用和外部查询写入当前 Trace |
| Prometheus Client | 复用现有指标查询客户端，不重复封装 Prometheus 连接 |
| Alert / Host Service | 通过现有 service 读取主机、告警和历史数据 |

### 2.2 新增能力边界

| 模块 | Phase 1 实现程度 |
|---|---|
| NLU Pipeline | 规则优先，LLM 兜底；先覆盖常见中文/英文关键词 |
| 工具执行 | 实现轻量只读工具适配层，为 Phase 2 Tool Registry 预留接口形态 |
| LLM Client | 复用 `chatops` 原型的 OpenAI 兼容 DeepSeek 调用方式，增加 timeout 和错误处理 |
| Response Generation | 只读查询直接格式化；复杂问题由 LLM 基于工具结果归纳 |
| 会话管理 | Redis 短期存储为主，MySQL 持久化仅保留接口设计，不在 Phase 1 强制落地 |
| 前端 | 基础对话页，不做诊断报告、审批台和审计页 |

---

## 3. 总体实施路径

Phase 1 拆为 6 个小模块推进，每个模块可以单独验证、单独提交。

```text
模块 1：后端骨架与路由
  ↓
模块 2：会话管理与请求校验
  ↓
模块 3：NLU 规则识别与 LLM 兜底
  ↓
模块 4：只读工具适配与响应生成
  ↓
模块 5：前端 Copilot 页面
  ↓
模块 6：部署配置、测试与回归验收
```

---

## 4. 详细实施步骤

### 4.1 模块 1：后端骨架与路由

**目标：** 在 `server-web` 中建立 Copilot 入口，但不接入复杂业务逻辑。

**实施步骤：**

1. 新增 `server-web/copilot/` 包结构：
   - `handler`：HTTP 请求解析和响应。
   - `service`：对话流程编排。
   - `nlu`：意图识别和实体抽取。
   - `tool`：只读工具适配层。
   - `llm`：LLM 客户端。
   - `session`：会话读写。
2. 在现有 API 路由中挂载：
   - `POST /api/v1/copilot/chat`
   - `GET /api/v1/copilot/sessions`
   - `GET /api/v1/copilot/sessions/:id/messages`
   - `DELETE /api/v1/copilot/sessions/:id`
3. 接入现有认证中间件，所有接口必须登录。
4. 建立统一响应结构，与 `docs/design.md` 的 Chat API 对齐。
5. 增加基础 handler 单元测试，覆盖未登录、空消息、超长消息。

**预计修改文件：**

| 文件/目录 | 说明 |
|---|---|
| `server-monitor/server-web/copilot/...` | 新增 Copilot 后端代码 |
| `server-monitor/server-web/api/...` | 挂载路由 |
| `server-monitor/server-web/main.go` 或路由初始化文件 | 注入 Copilot service |

**验收标准：**

1. 未登录访问返回 401。
2. 空消息返回 400。
3. 合法请求返回 `session_id`、`reply`、`intent`、`tool_calls` 字段。
4. 不影响现有 `/api/v1/hosts`、`/api/v1/alerts/active`、`/ws/alerts`。

### 4.2 模块 2：会话管理与请求校验

**目标：** 建立 Redis 短期会话，确保请求可控。

**实施步骤：**

1. 定义请求结构：
   - `message` 必填，长度 `1~2000`。
   - `session_id` 可选，缺失时自动创建。
2. 定义响应结构：
   - `session_id`
   - `reply`
   - `intent`
   - `confidence`
   - `tool_calls`
   - `suggestions`
3. Redis Key 设计：
   - `chat:session:<session_id>`：List，保存最近 50 条消息。
   - `chat:session:<session_id>:meta`：Hash，保存 user_id、title、created_at、updated_at。
4. TTL 策略：
   - 会话默认 TTL：2 小时。
   - 每次追加消息刷新 TTL。
5. 增加会话归属校验，用户只能读取自己的会话。
6. 对消息内容做基础清洗：
   - 去除首尾空白。
   - 拒绝超长输入。
   - 不将 token、password、secret 等敏感字段写入日志。

**技术要求：**

1. Redis 操作必须带 `context.Context`。
2. Redis 失败时返回清晰错误，不 panic。
3. 会话 ID 使用不可预测随机值，不能使用自增 ID 暴露规模。
4. 日志记录只保留 `message_length` 和 `session_id`，不默认打印完整用户输入。

**验收标准：**

1. 首次请求可创建 session。
2. 同一 session 可追加消息。
3. 超过 50 条自动裁剪旧消息。
4. 其他用户不能读取当前用户 session。

### 4.3 模块 3：NLU 规则识别与 LLM 兜底

**目标：** 实现第一版自然语言意图识别。

**意图清单：**

| 意图 | 触发示例 | Phase 1 行为 |
|---|---|---|
| `alert_query` | 当前有哪些告警、firing alerts | 调用 `alert.list_active` |
| `alert_event_query` | 最近告警事件、最新 resolved | 调用 `alert.events` |
| `alert_history_query` | 最近一周 CPU 告警历史 | 调用 `alert.history` |
| `host_query` | 当前主机列表、有哪些机器离线 | 调用 `host.list` |
| `metric_query` | node-1 CPU 怎么样、内存趋势 | 调用 `host.metrics` 或 `prom.query_range` |
| `general_chat` | 平台能做什么、帮我解释结果 | 不调用工具或用 LLM 归纳 |
| `unknown` | 无法识别 | 返回澄清问题 |

**规则识别策略：**

1. 告警关键词：
   - `告警`、`alert`、`firing`、`resolved`、`severity`。
2. 主机关键词：
   - `主机`、`host`、`instance`、`机器`、`节点`。
3. 指标关键词：
   - `CPU`、`内存`、`磁盘`、`负载`、`网络`、`metric`。
4. 历史关键词：
   - `历史`、`history`、`过去`、`最近一周`。
5. 实体抽取：
   - `instance`：优先识别 `host:port`、IP、主机名。
   - `severity`：识别 `critical`、`warning`、`info`。
   - `window`：识别 `15m`、`1h`、`6h`、`24h`，默认 `15m`。

**LLM 兜底策略：**

1. 规则无法识别或置信度低于 `0.6` 时调用 LLM。
2. LLM 只返回结构化意图 JSON，不直接返回可执行命令。
3. LLM 输出必须经过 JSON 解析和字段白名单校验。
4. JSON 解析失败时降级为 `unknown`，返回澄清问题。
5. LLM 请求设置超时，默认 60 秒，可通过配置调整。

**验收标准：**

1. 常见告警/主机/指标查询无需 LLM 即可命中。
2. LLM 返回未定义意图时被拒绝。
3. LLM 不可用时基础规则查询仍可用。
4. 单元测试覆盖中英文关键词、空输入、超长输入、模糊输入。

### 4.4 模块 4：只读工具适配与响应生成

**目标：** 接入 Phase 1 只读工具，形成真实数据查询闭环。

**工具清单：**

| 工具名 | 参数 | 数据来源 | 安全限制 |
|---|---|---|---|
| `host.list` | `status, search, sort, risk, group_id` | `host.Service` | 只读，分页或限制返回数量 |
| `host.metrics` | `instance, window` | `host.Service` + Prometheus | window 限定为 `15m/1h/6h/24h` |
| `alert.list_active` | `severity` | Redis `alert:active` / alert service | severity 白名单 |
| `alert.events` | `count` | Redis Stream `alert:events` | count 默认 20，最大 100 |
| `alert.history` | `status, severity, alert_name, instance, page, page_size` | MySQL `AlertHistory` | page_size 最大 100 |
| `prom.query_range` | `query, start, end, step, max_points` | Prometheus Client | 时间范围、步长、点数、响应大小限制 |

**实施步骤：**

1. 定义轻量工具接口，保持与 Phase 2 Tool Registry 的接口方向一致：

```go
type ReadOnlyTool interface {
    Name() string
    Run(ctx context.Context, args json.RawMessage) (ToolResult, error)
}
```

2. 为每个工具封装参数结构和校验函数。
3. 工具调用统一设置 timeout。
4. 工具结果统一做脱敏处理：
   - 过滤 `password`
   - 过滤 `token`
   - 过滤 `secret`
   - 过滤 `authorization`
5. `prom.query_range` 增加安全校验：
   - `end - start <= 7d`
   - `step >= 15s`
   - `max_points <= 1000`
   - 响应 body 不超过 1MB
   - 禁止明显危险或内部查询模式。
6. 响应生成：
   - 简单列表类查询由后端模板格式化。
   - 多工具结果可交给 LLM 归纳，但 Prompt 必须只注入脱敏后的证据。
   - LLM 失败时返回原始结构化摘要，不让整个接口不可用。

**验收标准：**

1. `alert_query` 能查询活跃告警。
2. `host_query` 能查询主机列表。
3. `metric_query` 能查询单主机关键指标趋势。
4. `prom.query_range` 的越界时间范围被拒绝。
5. 工具超时不会拖垮整个请求。

### 4.5 模块 5：前端 Copilot 页面

**目标：** 在现有 Vue 3 控制台中提供可用的 Copilot 对话入口。

**实施步骤：**

1. 增加路由：
   - `/copilot`
2. 增加 API 封装：
   - `sendCopilotMessage`
   - `listCopilotSessions`
   - `listCopilotMessages`
   - `deleteCopilotSession`
3. 页面布局：
   - 左侧会话列表。
   - 中间消息流。
   - 底部输入框。
   - 右侧或消息内展示工具调用状态。
4. 交互状态：
   - 发送中。
   - 工具调用中。
   - LLM 超时/失败。
   - 空会话。
   - 未识别意图。
5. 安全与体验：
   - 输入长度限制 2000。
   - 防重复提交。
   - 错误信息不暴露敏感后端细节。
   - 使用现有登录态和请求拦截器。

**验收标准：**

1. 登录用户可进入 Copilot 页面。
2. 可发送“当前有哪些告警？”并展示结果。
3. 请求失败时页面有明确错误状态。
4. 页面不影响现有 Overview、Hosts、Alerts、Settings 等路由。

### 4.6 模块 6：配置、部署与回归验收

**目标：** 让 Phase 1 在本地和容器环境可配置、可关闭、可验证。

**新增配置：**

| 配置名 | 默认值 | 用途 | 是否敏感 | 配置方式 |
|---|---|---|---|---|
| `COPILOT_ENABLED` | `true` | 是否启用 Copilot API | 否 | env / values |
| `LLM_API_KEY` | 空 | LLM API Key | 是 | env / Secret |
| `LLM_API_URL` | DeepSeek OpenAI 兼容地址 | LLM API 地址 | 否 | env / ConfigMap |
| `LLM_MODEL` | `deepseek-chat` | 模型名称 | 否 | env / ConfigMap |
| `LLM_TIMEOUT` | `60s` | LLM 请求超时 | 否 | env / ConfigMap |
| `COPILOT_SESSION_TTL` | `2h` | Redis 会话 TTL | 否 | env / ConfigMap |
| `COPILOT_MAX_MESSAGE_LENGTH` | `2000` | 单条消息最大长度 | 否 | env / ConfigMap |
| `COPILOT_MAX_SESSION_MESSAGES` | `50` | 单会话保留消息数 | 否 | env / ConfigMap |

**部署要求：**

1. Docker Compose：
   - 在 `server-web` 增加 Copilot/LLM 环境变量。
   - 不新增独立服务。
   - 不把 `LLM_API_KEY` 写死到 Compose 文件。
2. Kubernetes / Helm：
   - `LLM_API_KEY` 走 Secret。
   - 非敏感配置走 values/env。
   - 默认允许关闭 Copilot，便于灰度。
3. 本地开发：
   - 未配置 `LLM_API_KEY` 时，规则查询仍可用。
   - LLM 相关功能返回明确降级提示。

**回归验收：**

1. `docker compose config` 通过。
2. `server-web` 启动成功。
3. `/readyz` 和 `/readyz/full` 正常。
4. Prometheus 查询、活跃告警、告警 WebSocket 不回退。
5. Copilot API 在未配置 LLM 时仍可完成规则类只读查询。

---

## 5. 资源分配

### 5.1 人员角色

| 角色 | 人数 | 职责 |
|---|---:|---|
| 后端开发 | 1 | Copilot API、NLU、会话、只读工具、配置、后端测试 |
| 前端开发 | 1 | Copilot 页面、API 封装、交互状态、前端联调 |
| 测试/验证 | 1 | 接口测试、回归测试、异常场景、验收记录 |
| DevOps 支持 | 0.5 | Docker Compose、Helm values、Secret/ConfigMap 配置建议 |
| 项目负责人 | 0.5 | 范围控制、里程碑确认、风险处理、验收签核 |

如果只有单人执行，建议按“后端优先、前端其次、部署验证最后”的顺序推进，避免先做 UI 后发现 API 边界变化。

### 5.2 基础设施资源

| 资源 | 用途 | 要求 |
|---|---|---|
| Redis | 会话、限流、短缓存 | 复用现有实例 |
| MySQL | 告警历史读取 | Phase 1 不新增强制表结构 |
| Prometheus / VictoriaMetrics | 指标查询 | 复用现有查询链路 |
| Kafka | 无新增强依赖 | Phase 1 不消费新 Topic |
| LLM Provider | 意图兜底和结果归纳 | 支持 OpenAI 兼容 Chat Completions |
| 本地 Docker Compose | 联调与回归 | 使用现有 `server-monitor` 栈 |

---

## 6. 时间节点

以 2026-05-08 作为 T0 启动日估算，Phase 1 建议用 10 个工作日完成。若实际启动日调整，可按 T+N 平移。

| 时间 | 工作日 | 里程碑 | 交付结果 |
|---|---:|---|---|
| 2026-05-08 | T+1 | 启动与接口骨架 | 路由、请求/响应结构、基础鉴权完成 |
| 2026-05-11 | T+2 | 会话管理 | Redis 会话创建、追加、读取、TTL 完成 |
| 2026-05-12 | T+3 | NLU 规则识别 | 常见告警/主机/指标意图可识别 |
| 2026-05-13 | T+4 | LLM 兜底 | LLM Client、timeout、JSON 校验、降级完成 |
| 2026-05-14 | T+5 | 只读工具第一批 | `host.list`、`alert.list_active`、`alert.events` 完成 |
| 2026-05-15 | T+6 | 只读工具第二批 | `host.metrics`、`alert.history`、`prom.query_range` 完成 |
| 2026-05-18 | T+7 | 后端联调 | Chat API 串通 NLU、工具、响应生成 |
| 2026-05-19 | T+8 | 前端页面 | Copilot 页面、消息流、工具状态完成 |
| 2026-05-20 | T+9 | 部署配置 | Compose/Helm 配置说明、灰度开关完成 |
| 2026-05-21 | T+10 | 全量验收 | 测试、回归、问题修复、验收记录完成 |

### 6.1 关键评审点

| 评审点 | 时间 | 通过条件 |
|---|---|---|
| 范围评审 | T+1 结束 | API 和不做清单确认，未引入后续阶段能力 |
| 后端中期评审 | T+6 结束 | 只读工具均可通过单元测试或接口测试 |
| 前后端联调评审 | T+8 结束 | 页面能完成至少 3 类自然语言查询 |
| 发布前评审 | T+10 结束 | 回归通过，风险清单关闭或有明确降级方案 |

---

## 7. 技术要求

### 7.1 后端技术要求

1. Copilot 代码必须嵌入 `server-web`，不新增独立微服务。
2. 所有 API 必须复用现有 JWT 鉴权。
3. Phase 1 只开放只读能力，不执行任何写操作。
4. 所有外部调用必须设置 timeout。
5. 所有 Redis、MySQL、Prometheus 调用必须传递 `context.Context`。
6. 工具参数必须先校验再执行。
7. 工具结果进入 LLM 前必须脱敏和截断。
8. LLM 输出不可信，必须经过 JSON 解析和白名单校验。
9. LLM 不可用时必须降级，不能导致基础查询不可用。
10. 不新增不必要依赖，优先使用现有 Gin、GORM、go-redis、Prometheus Client。

### 7.2 PromQL 安全要求

1. 查询范围不得超过 7 天。
2. `step` 不小于 15 秒。
3. 返回点数不超过 1000。
4. 单次响应大小不超过 1MB。
5. 禁止访问内部或敏感指标命名空间。
6. 查询失败返回可理解错误，不泄露 Prometheus 内部细节。

### 7.3 前端技术要求

1. 使用现有 Vue 3 + TypeScript + Vite 技术栈。
2. 复用现有布局、路由守卫和请求拦截器。
3. 不引入新的大型 UI 框架。
4. 消息流需要支持 loading、error、empty、timeout 状态。
5. 工具调用状态要可见，但不暴露敏感参数。
6. 页面在桌面和常见移动宽度下不出现文本溢出或内容重叠。

### 7.4 可观测性要求

1. 每次 Chat 请求记录 request id / trace id。
2. 每次工具调用记录：
   - tool_name
   - duration_ms
   - success
   - error_type
3. LLM 调用记录：
   - model
   - duration_ms
   - success
   - prompt_hash
4. 不记录完整敏感输入和密钥。
5. 后续可扩展 Prometheus 指标：
   - `copilot_requests_total`
   - `copilot_request_duration_seconds`
   - `copilot_tool_calls_total`
   - `copilot_llm_requests_total`

---

## 8. 测试方案

### 8.1 单元测试

| 模块 | 测试内容 | Mock 方式 |
|---|---|---|
| Handler | 鉴权、空消息、超长消息、响应结构 | `httptest` |
| Session | Redis Key、TTL、归属校验、消息裁剪 | Redis mock 或内存实现 |
| NLU | 中英文关键词、实体抽取、未知意图 | 表驱动测试 |
| LLM Client | timeout、非 JSON、错误状态码、降级 | `httptest.Server` |
| Tool Args | 参数校验、默认值、非法枚举 | 纯函数测试 |
| Prom Query Guard | 时间范围、step、max_points、危险模式 | 表驱动测试 |
| Response Generation | 空结果、多结果、工具失败部分降级 | 构造工具结果 |

### 8.2 集成测试

| 场景 | 验证点 |
|---|---|
| Chat 查询活跃告警 | 自然语言命中 `alert_query`，返回真实或 mock 告警 |
| Chat 查询主机列表 | 命中 `host_query`，返回主机摘要 |
| Chat 查询指标 | 命中 `metric_query`，Prometheus 查询参数合法 |
| LLM 不可用 | 规则类查询仍可用，复杂查询返回降级提示 |
| Redis 不可用 | 返回明确错误，服务不 panic |

### 8.3 回归测试

| 现有能力 | 验证方式 |
|---|---|
| 登录认证 | 登录、获取 `/api/v1/auth/me` |
| 主机列表 | 请求 `/api/v1/hosts` |
| Dashboard | 请求 `/api/v1/dashboard/overview` |
| 活跃告警 | 请求 `/api/v1/alerts/active` |
| 告警事件 | 请求 `/api/v1/alerts/events` |
| WebSocket | `/ws/alerts` 握手成功 |
| Readyz | `/readyz` 和 `/readyz/full` 正常 |

### 8.4 必须执行的验证命令

后端改动完成后：

```bash
goimports -w <本次修改的 Go 文件>
go test ./...
go vet ./...
```

前端改动完成后：

```bash
npm run lint
npm run build
```

部署配置改动完成后：

```bash
docker compose config
helm lint <chart-path>
kubectl apply --dry-run=client -f <manifest>
```

如果某个命令因本地缺少工具、服务未启动或环境受限无法执行，验收记录必须明确写明“未执行原因”，不能写成默认通过。

---

## 9. 风险评估与应对措施

| 风险 | 概率 | 影响 | 应对措施 | 责任角色 |
|---|---|---|---|---|
| LLM 输出不稳定或非 JSON | 中 | 中 | 规则优先；LLM 输出白名单校验；失败降级为澄清问题 | 后端 |
| LLM 延迟过高 | 高 | 中 | 设置 timeout；简单查询不走 LLM；前端展示 loading 和超时提示 | 后端/前端 |
| PromQL 查询过大拖慢 Prometheus | 中 | 高 | 限制范围、step、点数和响应大小；默认使用模板查询 | 后端 |
| 会话 Redis Key 污染或泄露 | 低 | 中 | Key 前缀隔离；session_id 随机；用户归属校验；TTL 自动清理 | 后端 |
| Copilot 影响现有 API 性能 | 中 | 高 | 限流；工具 timeout；不在现有告警 Webhook 同步链路中调用 LLM | 后端 |
| 敏感信息进入 Prompt 或日志 | 中 | 高 | 脱敏字段黑名单；日志只记录摘要；Secret 只走环境变量 | 后端/DevOps |
| Phase 1 范围膨胀 | 中 | 高 | 坚持不做清单；K8s/RAG/审批/诊断报告全部后移 | 项目负责人 |
| 前端体验不足导致不可用 | 中 | 中 | 先实现最小可用消息流；必须覆盖错误和空状态 | 前端 |
| 配置缺失导致启动失败 | 中 | 中 | `COPILOT_ENABLED` 开关；LLM Key 缺失时降级，不阻塞启动 | 后端/DevOps |
| 现有工作区有未提交改动 | 高 | 中 | 每个模块前检查 `git status`；只改目标文件；不覆盖用户改动 | 全员 |

---

## 10. 发布与灰度策略

### 10.1 灰度开关

Phase 1 必须提供 `COPILOT_ENABLED` 开关：

1. `false`：不注册 Copilot 路由或返回 404/disabled。
2. `true`：启用 Copilot API。
3. 缺省建议本地为 `true`，生产或演示环境根据 LLM Key 是否配置决定。

### 10.2 发布步骤

1. 合并后端 Copilot 基础代码。
2. 合并前端 Copilot 页面。
3. 配置 `LLM_API_KEY` 和非敏感 LLM 参数。
4. 启动 `server-web`，检查 readyz。
5. 执行只读查询冒烟测试。
6. 执行现有监控和告警回归。
7. 开启前端入口。

### 10.3 回滚方案

1. 配置级回滚：
   - 将 `COPILOT_ENABLED=false`。
   - 保留代码但关闭入口。
2. 路由级回滚：
   - 移除前端菜单入口。
   - 后端返回 disabled。
3. 版本级回滚：
   - 回退到上一版镜像。
   - Redis 中 `chat:*` 会话可直接过期，不需要迁移。

---

## 11. 验收标准

Phase 1 完成必须同时满足以下条件：

1. `POST /api/v1/copilot/chat` 已接入登录态。
2. 规则 NLU 能识别告警、主机、指标、告警历史等基础意图。
3. 至少 6 个只读工具可用：
   - `host.list`
   - `host.metrics`
   - `alert.list_active`
   - `alert.events`
   - `alert.history`
   - `prom.query_range`
4. 未配置 LLM 时，规则类只读查询仍可用。
5. LLM 输出非法 JSON 时不会执行任何工具外动作。
6. PromQL 越界查询会被拒绝。
7. Redis 会话 TTL 和消息裁剪生效。
8. 前端 Copilot 页面可发送消息并展示结果。
9. 现有监控、告警、WebSocket、登录能力不回退。
10. 测试结果明确记录：
    - 已执行并通过。
    - 已执行但失败。
    - 未执行及原因。

---

## 12. 建议提交拆分

Phase 1 不建议一次提交全部内容，建议按以下粒度提交：

```bash
git add server-monitor/server-web/copilot server-monitor/server-web/api
git commit -m "feat: add copilot chat api skeleton"

git add server-monitor/server-web/copilot/session server-monitor/server-web/copilot/nlu
git commit -m "feat: add copilot session and intent parsing"

git add server-monitor/server-web/copilot/tool
git commit -m "feat: add copilot readonly tools"

git add server-monitor/frontend/src
git commit -m "feat: add copilot chat page"

git add server-monitor/docker-compose.yml server-monitor/charts/server-monitor
git commit -m "chore: add copilot runtime configuration"
```

实际提交文件以最终改动为准，提交前必须重新检查 `git status`，避免把无关改动带入提交。

---

## 13. Phase 1 完成后的下一步

Phase 1 验收通过后，进入 Phase 2：Tool Registry。

Phase 2 的重点是把 Phase 1 的轻量工具适配层升级为正式注册中心，补齐 Schema 校验、统一 timeout、权限检查、调用日志、健康检查和未注册工具拒绝能力。Phase 1 编码时应避免写死 switch-case 到不可迁移的程度，为 Phase 2 留出平滑演进空间。
