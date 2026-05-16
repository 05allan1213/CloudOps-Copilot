# CloudOps Copilot AI 智能化升级方案

> 版本：v1.4 | 日期：2026-05-16
> v1.4 变更：summary 示例补包名前缀、全文统一构建 cwd、.env.example 加入文件清单、上下文实体保存加 merge 保护

---

## 1. 现状问题

### 1.1 核心矛盾

系统已配置 DeepSeek API_KEY（`LLM_API_KEY`），但 LLM 仅用于两个边缘场景：

| 场景 | 触发条件 | 使用频率 |
|------|---------|---------|
| 意图分类兜底 | 规则置信度 < 0.6 | 低（规则匹配覆盖大部分场景） |
| 诊断摘要生成 | 用户主动发起诊断 | 极低（需要提供 fingerprint 等技术参数） |

**LLM 完全没有参与对话回复生成**，导致用户体验像 FAQ 机器人而非 AI 助手。

### 1.2 具体问题清单

| # | 问题 | 根因 | 用户感知 |
|---|------|------|---------|
| P1 | 回复是硬编码模板 | `buildReply()` 返回固定字符串 | "找到 N 条活跃告警。"——没有分析、没有建议 |
| P2 | 工具结果无自然语言总结 | `replyFromResult()` 只返回"找到 N 条" | 用户需要自己看原始 JSON 才能理解 |
| P3 | 没有多轮上下文 | 聊天历史存 Redis 但不传 LLM | "第二条帮我诊断"无法理解 |
| P4 | 建议是固定列表 | `buildSuggestions()` 按 intent 硬编码 | 每次都推荐一样的，不基于实际数据 |
| P5 | LLM 增强功能全关 | `TOOLS_CLASSIFY_ENABLED=false` 等 | API_KEY 白配了 |
| P6 | 诊断入口不友好 | 必须提供 fingerprint/alert_history_id | 普通用户不知道去哪找 |
| P7 | 没有主动引导 | AI 从不追问、不推荐下一步 | 感觉是查询工具不是 AI |

### 1.3 当前架构数据流

```
用户消息
  │
  ▼
nlu.Classifier.ClassifyMultiWithMax()    ← 纯关键词匹配
  │
  ▼
service.classifyWithFallback()           ← 置信度 < 0.6 才调 LLM
  │
  ▼
tool.Executor.Execute()                  ← 执行工具，返回原始 JSON
  │
  ▼
buildReply()                             ← 硬编码模板字符串
buildSuggestions()                       ← 硬编码建议列表
  │
  ▼
ChatResponse{reply, suggestions, tool_calls}
```

**关键缺陷：LLM 不在主路径上，工具结果不经过 LLM 处理。**

---

## 2. 目标体验

### 2.1 对话示例

**改造前：**

```
用户：当前有哪些活跃告警
AI：  找到 1 条活跃告警。
建议：[查看当前活跃告警, 查看严重级别告警]
```

**改造后：**

```
用户：当前有哪些活跃告警
AI：  当前有 1 条 critical 级别告警：node-1 触发了 HostDown，主机可能不可达。
      这个告警从 09:42 开始持续 firing 状态。需要我帮你诊断一下吗？
建议：[诊断 node-1 的 HostDown 告警, 查看 node-1 的 CPU 和内存趋势]
```

**多轮对话：**

```
用户：帮我诊断一下
AI：  好的，当前有 1 条 firing 告警（node-1 HostDown），我直接用这条发起诊断。
      ...（自动查证据、生成诊断报告）
      诊断完成：node-1 主机不可达，CPU 指标缺失，建议检查 exporter 进程和网络连通性。
      需要我查看 node-1 的 K8s Pod 状态吗？
建议：[查看 node-1 的 Pod 列表, 查看最近告警历史]
```

### 2.2 体验目标

| 维度 | 目标 |
|------|------|
| 自然度 | 回复像和运维专家聊天，不是查 API 文档 |
| 主动性 | 主动追问、推荐下一步，像豆包一样引导 |
| 上下文 | 记住上一轮说了什么，支持指代消解 |
| 智能性 | 模糊消息也能理解，自动推断用户意图 |
| 容错性 | LLM 挂了自动降级到模板，不影响可用性 |

---

## 3. 全局约束

以下约束贯穿所有步骤，任何改动不得违反：

### 3.1 路径基准

当前代码已在 `server-web/internal/copilot/` 下，所有路径以此为基准：

```
server-web/internal/copilot/
├── service/service.go
├── handler/handler.go
├── session/store.go
├── nlu/nlu.go
├── llm/client.go
├── tool/executor.go
├── diagnosis/summarizer.go
├── ...
```

import 路径格式：`server-web/internal/copilot/<sub>/`。

### 3.2 DI 约束

当前 `service.Config` 通过 `LLM`、`Tools`、`Diagnosis` 做依赖注入（见 `service.go:32-45`，`app.go:504-518`）。**所有新增能力必须遵循同一模式**：

- `app.go` 中构造具体实现，注入到 `service.Config`
- `service` 包只定义接口和编排逻辑，不 `new` 任何 LLM 客户端或摘要器
- 新增接口在 `service` 包中定义（使用方），实现在各自子包中

### 3.3 API 兼容约束

当前 `ChatResponse.Suggestions` 类型为 `[]string`（见 `service.go:112`）。**迭代 A 不改变此类型**，动态建议仍返回 `[]string`。结构化建议（含 `action`/`intent`/`params`）留到迭代 C。

### 3.4 未导出类型约束

`llm.chatUsage` 是未导出类型（见 `client.go:76`），其他 package 不能引用。**摘要接口不得暴露 `*llm.chatUsage`**，方案：
- `llm` 包导出 `ChatUsage` 类型（重命名 `chatUsage` → `ChatUsage`），或
- 摘要接口返回值不含 usage，内部记录即可

### 3.5 配置/Secret 边界

- **不修改 `.env` 文件**（可能携带真实 API key）
- 新增配置项通过 `docker-compose.yml` 的 `environment` 默认值或 `config.go` 的 `String()` 默认值提供
- 敏感值（`LLM_API_KEY` 等）通过 `.env.example` 文档化，实际值由运维注入
- Helm 部署时通过 Secret 管理

### 3.6 与 design.md 对齐

design.md §16.1 原则 6 规定 **Copilot 保持内聚**。AI 升级新增的子包（`summary/`、`context/`、`suggestion/`）必须在 `internal/copilot/` 下，不在 `internal/` 顶级新增包。

---

## 4. 迭代 A：LLM 工具结果摘要 + 动态文本建议

> 目标：工具执行后经过 LLM 生成自然语言摘要，失败时降级到模板。不改 API 格式，不改分类主路径，不做 SSE。

### A1. 启用已有 LLM 增强功能

**类型**：配置变更，零代码改动

| 配置项 | 当前默认值 | 改为 | 改动位置 |
|--------|-----------|------|---------|
| `COPILOT_TOOLS_CLASSIFY_ENABLED` | `false` | `true` | `docker-compose.yml` environment |
| `COPILOT_MULTI_INTENT_ENABLED` | `false` | `true` | `docker-compose.yml` environment |
| `RERANKER_ENABLED` | `false` | `true` | `docker-compose.yml` environment |
| `LLM_MAX_TOKENS` | `800` | `2048` | `docker-compose.yml` environment |

**不修改 `.env`**。上述配置通过 `docker-compose.yml` 的 `environment` 字段设默认值。

**注意**：`DIAGNOSIS_ENABLED` 不在迭代 A 中启用。启用 diagnosis worker 会启动 Kafka consumer 和 Redis 依赖路径（见 `app.go:698`），与迭代 A 的摘要目标无关。`DIAGNOSIS_ENABLED` 移至迭代 B 随智能诊断入口一起启用。

### A2. 结构调整：service.go 拆分

**目标**：`service.go` 当前 649 行，拆为 4 个文件，每文件职责单一。

**拆分方案**（纯代码移动，不改行为）：

| 目标文件 | 行数 | 从 service.go 迁出的函数 |
|---------|------|------------------------|
| `service/service.go` | ~300 | `Config`, `Service`, `NewService`, `Chat()`, 会话管理方法, 接口定义, 辅助函数 |
| `service/classify.go` | ~100 | `classifyWithFallback()`, `defaultClassifier()` |
| `service/execute.go` | ~120 | `executeTools()`, `executeIntent()`, `executeIntents()`, `executeDiagnosis()`, `extractContextEntities()`, `mergeEntities()`, `buildDiagnosisCandidatesReply()`, `buildDiagnosisErrorReply()`, `filterEmpty()` |
| `service/reply.go` | ~80 | `buildReply()`, `buildSuggestions()` |

**验证**：拆分后 `cd server-monitor/server-web && go build ./... && go test ./internal/copilot/...` 通过，行为不变。

### A3. llm 包增强

#### A3.1 导出 ChatUsage

**文件**：`internal/copilot/llm/client.go`

将 `chatUsage` 重命名为 `ChatUsage`（导出），同步更新所有内部引用。这是 A3.2 的前提——摘要接口不能返回未导出类型。

#### A3.2 新增 Chat 方法

**文件**：`internal/copilot/llm/client.go`

```go
type ChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

func (c *Client) Chat(ctx context.Context, messages []ChatMessage) (string, *ChatUsage, error)
```

与 `Generate` 的区别：
- `Generate`：固定 system + user 两条消息，temperature=0，用于分类
- `Chat`：传入完整消息列表，temperature=0.3，用于对话生成

实现：复用 `doRequest` 的 HTTP 逻辑，构建 `chatRequest{Model, Messages: messages, Temperature: 0.3, MaxTokens: c.maxTokens}`。

#### A3.3 拆出 prompt.go

**文件**：`internal/copilot/llm/prompt.go`（新增）

从 `client.go` 迁出：
- `systemPrompt()` → `prompt.go`
- `toolsSystemPrompt()` → `prompt.go`

### A4. 新增 summary 子包

**文件**：`internal/copilot/summary/summarizer.go`（新增）

```go
package summary

type LLMChatClient interface {
    Chat(ctx context.Context, messages []llm.ChatMessage) (string, *llm.ChatUsage, error)
}

type Summarizer struct {
    llm       LLMChatClient
    timeout   time.Duration
    maxPrompt int
}

type Options struct {
    LLM       LLMChatClient
    Timeout   time.Duration
    MaxPrompt int
}

func NewSummarizer(opts Options) *Summarizer { ... }
func (s *Summarizer) Summarize(ctx context.Context, input service.SummaryInput) (service.SummaryResult, error) { ... }
```

**关键设计**：

- **避免 import cycle**：`Summarizer.Summarize` 的入参和返回值类型定义在 `service` 包（`SummaryInput`/`SummaryResult`），不在 `summary` 包。`summary` 包 import `service` 来实现接口方法签名，`service` 不 import `summary`。依赖方向：`service` ← `summary`（单向）。
- `LLMChatClient` 接口定义在 `summary` 包（使用方），只含 `Chat` 一个方法
- `ErrFallback` 是明确的降级信号，`service` 层据此回退到 `buildReply()`

**service 包新增的类型定义**（`internal/copilot/service/types.go`）：

```go
package service

type SummaryInput struct {
    UserMessage string
    ToolCalls   []ToolCall
    Intent      string
}

type SummaryResult struct {
    Reply       string
    Suggestions []string
}

type Summarizer interface {
    Summarize(ctx context.Context, input SummaryInput) (SummaryResult, error)
}
```

**文件**：`internal/copilot/summary/prompt.go`（新增）

```go
package summary

func summarySystemPrompt() string { ... }
func chatFallbackPrompt() string { ... }
func unknownIntentPrompt() string { ... }
```

### A5. service 层接入摘要

**文件**：`internal/copilot/service/reply.go`

新增 `buildReplyWithSummary` 方法：

```go
func (s *Service) buildReplyWithSummary(
    ctx context.Context,
    message string,
    result nlu.Result,
    toolReply string,
    toolCalls []ToolCall,
) (string, []string) {
    if s.summarizer == nil || !s.summaryEnabled {
        return buildReply(result, toolReply, toolCalls), buildSuggestions(result)
    }

    hasSuccessfulTools := false
    for _, call := range toolCalls {
        if call.Status == "success" {
            hasSuccessfulTools = true
            break
        }
    }

    if !hasSuccessfulTools {
        if result.Intent == nlu.IntentGeneralChat || result.Intent == IntentUnknown {
            return s.chatWithLLM(ctx, message, result, toolCalls)
        }
        return buildReply(result, toolReply, toolCalls), buildSuggestions(result)
    }

    summaryResult, err := s.summarizer.Summarize(ctx, SummaryInput{
        UserMessage: message,
        ToolCalls:   toolCalls,
        Intent:      result.Intent,
    })
    if err != nil {
        return buildReply(result, toolReply, toolCalls), buildSuggestions(result)
    }

    reply := summaryResult.Reply
    suggestions := summaryResult.Suggestions
    if len(suggestions) == 0 {
        suggestions = buildSuggestions(result)
    }
    return reply, suggestions
}
```

**注意**：`message` 参数直接传入用户原始消息，不从 `result.Entities["message"]` 取（NLU 不保证有此实体）。

**文件**：`internal/copilot/service/service.go`

`Chat()` 中替换：

```go
// 当前：
reply := buildReply(parsed, toolReply, toolCalls)
suggestions := buildSuggestions(parsed)

// 改为：
reply, suggestions := s.buildReplyWithSummary(ctx, message, parsed, toolReply, toolCalls)
```

`Config` 新增字段：

```go
type Config struct {
    // 现有字段...
    Summarizer     Summarizer
    SummaryEnabled bool
}
```

`Summarizer` 接口和 `SummaryInput`/`SummaryResult` 类型定义在 A4 的 `service/types.go` 中。

### A6. app.go 组装

**文件**：`server-web/app.go` — `initCopilot` 函数

```go
// 在 initCopilot 中新增：
var copilotSummarizer *copilotsummary.Summarizer
if llmClient != nil {
    copilotSummarizer = copilotsummary.NewSummarizer(copilotsummary.Options{
        LLM:       llmClient,
        Timeout:   cfg.CopilotSummaryTimeout,
        MaxPrompt: cfg.CopilotSummaryMaxPromptBytes,
    })
}

copilotHandler := copilothandler.NewHandler(copilotservice.NewService(copilotservice.Config{
    // 现有
    Classifier:           copilotnlu.NewClassifier(...),
    LLM:                  llmClient,
    Tools:                toolExecutor,
    Diagnosis:            diagnosisService,
    Store:                copilotsession.NewRedisStore(...),
    // 新增
    Summarizer:           copilotSummarizer,
    SummaryEnabled:       cfg.CopilotSummaryEnabled,
}))
```

### A7. 配置项

**文件**：`server-web/internal/config/config.go`

新增字段：

```go
CopilotSummaryEnabled        bool
CopilotSummaryTimeout        time.Duration
CopilotSummaryMaxPromptBytes int
```

**文件**：`docker-compose.yml` — server-web environment

```yaml
COPILOT_SUMMARY_ENABLED: ${COPILOT_SUMMARY_ENABLED:-true}
COPILOT_SUMMARY_TIMEOUT_SECONDS: ${COPILOT_SUMMARY_TIMEOUT_SECONDS:-8}
COPILOT_SUMMARY_MAX_PROMPT_BYTES: ${COPILOT_SUMMARY_MAX_PROMPT_BYTES:-16384}
```

风格与现有 `docker-compose.yml` 的 map 形式一致（如 `COPILOT_ENABLED: ${COPILOT_ENABLED:-true}`）。

### A8. 迭代 A 验收标准

| 验收项 | 标准 |
|--------|------|
| 工具结果摘要 | 查询告警/主机/指标后，回复包含具体数据分析，非固定模板 |
| 动态建议 | 建议内容基于实际查询结果，仍为 `[]string` |
| 降级 | 断开 `LLM_API_KEY` 后系统仍正常使用（模板降级） |
| API 兼容 | `ChatResponse` 结构不变，`Suggestions` 仍为 `[]string` |
| DI | `Summarizer` 在 `app.go` 构造注入，`service` 不直接 new |
| 无 import cycle | `cd server-monitor/server-web && go vet ./internal/copilot/...` 无循环依赖 |
| service.go 行数 | 不超过 350 行 |
| 编译 | `cd server-monitor/server-web && go build ./... && go test ./internal/copilot/...` 通过 |

### A9. 迭代 A 文件改动清单

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `docker-compose.yml` | 修改 | 启用 LLM 增强配置 + 摘要配置默认值 |
| `internal/copilot/service/classify.go` | 新增 | 从 service.go 拆出 |
| `internal/copilot/service/execute.go` | 新增 | 从 service.go 拆出 |
| `internal/copilot/service/reply.go` | 新增 | 从 service.go 拆出 + 新增 buildReplyWithSummary |
| `internal/copilot/service/types.go` | 新增 | SummaryInput/SummaryResult/Summarizer 接口定义 |
| `internal/copilot/service/service.go` | 修改 | 瘦身 + Config 新增 Summarizer/SummaryEnabled 字段 |
| `internal/copilot/llm/client.go` | 修改 | 导出 ChatUsage + 新增 Chat 方法 |
| `internal/copilot/llm/prompt.go` | 新增 | 从 client.go 拆出 prompt 定义 |
| `internal/copilot/summary/summarizer.go` | 新增 | 摘要生成器（实现 service.Summarizer 接口） |
| `internal/copilot/summary/prompt.go` | 新增 | 摘要 prompt |
| `internal/config/config.go` | 修改 | 新增摘要配置项 |
| `app.go` | 修改 | initCopilot 中构造 Summarizer 并注入 |
| `server-monitor/.env.example` | 新增 | 文档化所有 AI 相关环境变量（含默认值说明），不含真实密钥 |

---

## 5. 迭代 B：多轮上下文 + 智能诊断入口

> 目标：LLM 调用时携带对话历史，支持指代消解；诊断入口自动推断目标。
> 前提：先设计 session 上下文存储接口。

### B1. session 上下文存储接口设计

**当前 session.Store 接口**（`session/store.go:36-42`）：

```go
type Store interface {
    GetMeta(ctx context.Context, sessionID string) (Meta, bool, error)
    AppendMessages(ctx context.Context, meta Meta, messages []Message, ttl time.Duration, maxMessages int) error
    ListSessions(ctx context.Context, userID uint64) ([]Summary, error)
    ListMessages(ctx context.Context, sessionID string) ([]Message, error)
    DeleteSession(ctx context.Context, userID uint64, sessionID string) error
}
```

**新增方法**（扩展 Store 接口）：

```go
type Store interface {
    // 现有方法...

    // 上下文实体存取
    GetContext(ctx context.Context, sessionID string) (SessionContext, error)
    SetContext(ctx context.Context, sessionID string, ctxData SessionContext, ttl time.Duration) error
}
```

**SessionContext 结构**：

```go
type SessionContext struct {
    LastIntent   string            `json:"last_intent"`
    LastEntities map[string]string `json:"last_entities"`
}
```

**Redis 存储**：

- Key：`chat:session:{sessionID}:ctx`（Redis Hash）
- 字段：`last_intent`、`last_entities`（JSON 序列化）
- TTL：与 session 相同（`refreshTTL` 时一并刷新）
- 兼容旧 session：`GetContext` 在 key 不存在时返回空 `SessionContext{}`，不报错

**RedisStore 实现**：

```go
func (s *RedisStore) GetContext(ctx context.Context, sessionID string) (SessionContext, error) {
    if !s.enabled() {
        return SessionContext{}, ErrUnavailable
    }
    fields, err := s.client.HGetAll(ctx, contextKey(sessionID))
    if err != nil {
        return SessionContext{}, fmt.Errorf("get copilot session context: %w", err)
    }
    if len(fields) == 0 {
        return SessionContext{}, nil
    }
    var result SessionContext
    result.LastIntent = fields["last_intent"]
    if raw, ok := fields["last_entities"]; ok {
        _ = json.Unmarshal([]byte(raw), &result.LastEntities)
    }
    return result, nil
}

func (s *RedisStore) SetContext(ctx context.Context, sessionID string, ctxData SessionContext, ttl time.Duration) error {
    if !s.enabled() {
        return ErrUnavailable
    }
    entitiesJSON, _ := json.Marshal(ctxData.LastEntities)
    if err := s.client.HSet(ctx, contextKey(sessionID), "last_intent", []byte(ctxData.LastIntent)); err != nil {
        return fmt.Errorf("set copilot session context: %w", err)
    }
    if err := s.client.HSet(ctx, contextKey(sessionID), "last_entities", entitiesJSON); err != nil {
        return fmt.Errorf("set copilot session context: %w", err)
    }
    return s.client.Expire(ctx, contextKey(sessionID), ttl)
}

func contextKey(sessionID string) string {
    return KeyPrefix + ":" + sessionID + ":ctx"
}
```

### B2. 新增 context 子包

**文件**：`internal/copilot/context/manager.go`（新增）

```go
package context

type Manager struct {
    store     sessionStore
    maxRounds int
}

type sessionStore interface {
    ListMessages(ctx context.Context, sessionID string) ([]session.Message, error)
    GetContext(ctx context.Context, sessionID string) (session.SessionContext, error)
    SetContext(ctx context.Context, sessionID string, ctxData session.SessionContext, ttl time.Duration) error
}

type Options struct {
    Store     sessionStore
    MaxRounds int
}

func NewManager(opts Options) *Manager { ... }
func (m *Manager) LoadHistory(ctx context.Context, sessionID string) ([]service.ChatHistoryItem, error) { ... }
func (m *Manager) LoadEntities(ctx context.Context, sessionID string) (map[string]string, error) { ... }
func (m *Manager) SaveEntities(ctx context.Context, sessionID string, entities map[string]string, ttl time.Duration) error { ... }
func (m *Manager) BuildMessages(systemPrompt string, history []service.ChatHistoryItem, userPrompt string) []llm.ChatMessage { ... }
```

**关键设计**：
- `sessionStore` 接口定义在 `context` 包（使用方），只含需要的 3 个方法
- `LoadHistory` 返回 `[]service.ChatHistoryItem`（类型定义在 `service` 包，避免 `summary` → `context` 依赖）
- `context` 包 import `service` 和 `session`，不被 `service` import（避免循环）
- `LoadHistory` 从 `ListMessages` 取最近 N 条，转为 `ChatHistoryItem`
- `BuildMessages` 组装完整消息列表供 LLM 调用

### B3. 摘要调用传入历史

**文件**：`internal/copilot/service/types.go`

`SummaryInput` 新增字段：

```go
type SummaryInput struct {
    UserMessage string
    ToolCalls   []ToolCall
    Intent      string
    History     []ChatHistoryItem  // 新增
}
```

`ChatHistoryItem` 也定义在 `service` 包（避免 `summary` → `context` → `service` 的间接循环）：

```go
type ChatHistoryItem struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}
```

**文件**：`internal/copilot/summary/summarizer.go`

`Summarize` 方法中，将 `input.History` 传入 `llm.ChatMessage` 列表。`summary` 包 import `service` 获取 `SummaryInput`（含 `ChatHistoryItem`），不直接 import `context` 包。

**文件**：`internal/copilot/context/manager.go`

`LoadHistory` 返回 `[]service.ChatHistoryItem`（而非 `context` 包自定义类型），保持类型一致性。`context` 包 import `service` 获取类型定义。

### B4. service 层接入上下文

**文件**：`internal/copilot/service/service.go`

`Config` 新增：

```go
type Config struct {
    // 现有...
    ContextManager ContextManager
}

type ContextManager interface {
    LoadHistory(ctx context.Context, sessionID string) ([]ChatHistoryItem, error)
    LoadEntities(ctx context.Context, sessionID string) (map[string]string, error)
    SaveEntities(ctx context.Context, sessionID string, entities map[string]string, ttl time.Duration) error
}
```

`ChatHistoryItem` 定义在 `service` 包（见 A4/B3），`ContextManager` 接口直接使用同包类型。

`Chat()` 中，在调用摘要前加载历史和上下文实体：

```go
var history []ChatHistoryItem
if s.contextManager != nil && sessionID != "" {
    history, _ = s.contextManager.LoadHistory(ctx, sessionID)
    ctxEntities, _ := s.contextManager.LoadEntities(ctx, sessionID)
    // 合并上下文实体到当前分类结果
    for k, v := range ctxEntities {
        if _, ok := parsed.Entities[k]; !ok && v != "" {
            parsed.Entities[k] = v
        }
    }
}

// ... 执行工具 ...

// 摘要时传入历史
reply, suggestions := s.buildReplyWithSummary(ctx, message, parsed, toolReply, toolCalls, history)

// 保存本轮上下文实体（仅提取到非空实体时保存，与旧实体 merge）
if s.contextManager != nil && sessionID != "" {
    newEntities := s.extractContextEntities(toolCalls, parsed.Intent)
    if len(newEntities) > 0 {
        oldEntities, _ := s.contextManager.LoadEntities(ctx, sessionID)
        merged := mergeEntities(oldEntities, newEntities)
        _ = s.contextManager.SaveEntities(ctx, sessionID, merged, s.sessionTTL)
    }
}
```

**注意**：`buildReplyWithSummary` 签名需同步更新，新增 `history []ChatHistoryItem` 参数。

**merge 策略**：`newEntities` 中的非空值覆盖 `oldEntities` 中的同 key 值；`newEntities` 中未涉及的 key 保留 `oldEntities` 的值。这样不会因为本轮没有提取到某个实体就丢失上一轮的有用上下文。

### B5. 智能诊断入口

**配置前提**：启用 `DIAGNOSIS_ENABLED=true`（`docker-compose.yml` environment），启动 diagnosis worker。

**文件**：`internal/copilot/service/execute.go`

修改 `executeDiagnosis`，当用户未提供诊断目标时自动推断：

```
1. 用户提供了 fingerprint/alert_history_id/alert_name+instance → 直接诊断（现有逻辑）
2. 用户没提供目标：
   a. 从上下文实体继承（LoadEntities 中的 last_entities）
   b. 如果上下文也没有 → 调用 alert.list_active 获取 firing 告警
      - 只有 1 条 → 自动用该告警诊断
      - 有多条 → 列出候选让用户选择
      - 没有 → 告知当前无活跃告警
```

### B6. app.go 组装

```go
copilotContextMgr := copilotcontext.NewManager(copilotcontext.Options{
    Store:     copilotsession.NewRedisStore(infra.redisClient),
    MaxRounds: cfg.CopilotChatHistoryMaxRounds,
})

copilotservice.NewService(copilotservice.Config{
    // 现有...
    ContextManager: copilotContextMgr,
})
```

### B7. 迭代 B 验收标准

| 验收项 | 标准 |
|--------|------|
| 多轮上下文 | "第二条帮我诊断"能正确关联上一轮结果 |
| 诊断入口 | "帮我诊断"无需提供 fingerprint，自动推断目标 |
| 上下文存储 | 旧 session 无 `:ctx` key 时正常降级，不报错 |
| 编译 | `cd server-monitor/server-web && go build ./... && go test ./internal/copilot/...` 通过 |

### B8. 迭代 B 文件改动清单

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `internal/copilot/session/store.go` | 修改 | 新增 `GetContext`/`SetContext` + `SessionContext` 类型 + `contextKey` |
| `internal/copilot/context/manager.go` | 新增 | 上下文管理器 |
| `internal/copilot/summary/summarizer.go` | 修改 | Input 新增 History 字段 |
| `internal/copilot/service/service.go` | 修改 | Config 新增 ContextManager，Chat() 加载历史 |
| `internal/copilot/service/execute.go` | 修改 | executeDiagnosis 自动推断目标 |
| `app.go` | 修改 | 构造 ContextManager 并注入 |

---

## 6. 迭代 C：LLM 分类主路径 + 结构化建议 + 流式响应

> 目标：LLM 提升为意图分类主路径，建议改为结构化对象，支持 SSE 流式响应。
> 此迭代涉及 API 格式变更和前端适配，需单独评估。

### C1. LLM 分类主路径

**文件**：`internal/copilot/service/classify.go`

修改 `classifyWithFallback` 阈值：

```
当前：规则置信度 >= 0.6 → 不调 LLM
改为：规则置信度 >= 0.9 → 不调 LLM（快速路径）
      规则置信度 < 0.9 → LLM 分类（主路径）
      LLM 失败 → 使用规则结果（fallback）
```

新增配置：`COPILOT_LLM_CLASSIFY_THRESHOLD`（默认 `0.9`）。

### C2. 结构化建议（API 变更）

**文件**：`internal/copilot/service/service.go`

`ChatResponse.Suggestions` 从 `[]string` 改为 `[]Suggestion`：

```go
type Suggestion struct {
    Text   string            `json:"text"`
    Action string            `json:"action,omitempty"`
    Intent string            `json:"intent,omitempty"`
    Params map[string]string `json:"params,omitempty"`
}
```

**前端适配**：前端需要更新建议渲染逻辑，点击时发送 `action` 消息。

**兼容策略**：通过 API 版本或配置开关控制，过渡期两种格式共存。

### C3. 流式响应

**文件**：`internal/copilot/llm/client.go`

新增 `ChatStream` 方法，使用 SSE 解析。

**文件**：`internal/copilot/handler/handler.go`

新增 SSE 端点或复用现有端点通过 `Accept: text/event-stream` 区分。

### C4. 迭代 C 文件改动清单

| 文件 | 改动类型 | 说明 |
|------|---------|------|
| `internal/copilot/service/classify.go` | 修改 | 分类阈值调整 |
| `internal/copilot/service/service.go` | 修改 | Suggestions 类型变更 |
| `internal/copilot/llm/client.go` | 修改 | 新增 ChatStream |
| `internal/copilot/handler/handler.go` | 修改 | SSE 端点 |
| `internal/config/config.go` | 修改 | 新增分类阈值配置 |

---

## 7. 降级策略

所有 LLM 调用都有规则 fallback，确保 LLM 不可用时系统仍可正常使用：

| 场景 | LLM 可用 | LLM 不可用 |
|------|---------|-----------|
| 意图分类 | 规则快速路径 + LLM 主路径 | 纯规则分类（当前行为） |
| 工具结果回复 | LLM 自然语言摘要 | 硬编码模板（`buildReply()`） |
| 建议列表 | LLM 动态生成 `[]string` | 硬编码建议列表（`buildSuggestions()`） |
| 闲聊回复 | LLM 对话 | 固定模板 |
| 诊断摘要 | LLM 生成 | 规则摘要（当前行为） |

---

## 8. 风险评估

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| LLM 延迟增加 | 高 | 用户体验变慢 | 摘要超时 8 秒自动降级；迭代 C 流式响应消除体感延迟 |
| LLM 输出格式不稳定 | 中 | JSON 解析失败 | 解析失败时降级到模板；prompt 明确要求 JSON |
| Token 消耗增加 | 高 | API 费用 | DeepSeek 极便宜；截断工具结果控制 prompt 大小 |
| LLM 幻觉 | 中 | 回复不准确 | prompt 限制"仅使用工具返回的数据"；原始数据仍通过 `tool_calls` 返回 |
| 上下文过长干扰 | 低 | 分类/摘要质量下降 | 历史限制 10 条；分类不传历史 |

---

## 9. 实施顺序

```
迭代 A（智能感质变）
  ├── A1. 启用 LLM 增强配置
  ├── A2. service.go 拆分（先拆后加）
  ├── A3. llm 包增强（导出 ChatUsage + Chat 方法 + prompt.go）
  ├── A4. 新增 summary 子包
  ├── A5. service 层接入摘要
  ├── A6. app.go 组装
  └── A7. 配置项
  验收：cd server-monitor/server-web && go build ./... && go test ./internal/copilot/... + 端到端测试

迭代 B（上下文 + 智能诊断）
  ├── B1. session 上下文存储接口
  ├── B2. 新增 context 子包
  ├── B3. 摘要调用传入历史
  ├── B4. service 层接入上下文
  ├── B5. 智能诊断入口
  └── B6. app.go 组装
  验收：cd server-monitor/server-web && go build ./... && go test ./internal/copilot/... + 多轮对话测试

迭代 C（API 变更 + 流式）
  ├── C1. LLM 分类主路径
  ├── C2. 结构化建议（API 变更）
  └── C3. 流式响应
  验收：cd server-monitor/server-web && go build ./... && go test ./internal/copilot/... + 前端联调
```

---

## 10. 验收标准

| 验收项 | 标准 | 迭代 |
|--------|------|------|
| 工具结果摘要 | 回复包含具体数据分析，非固定模板 | A |
| 动态建议 | 建议基于实际数据，仍为 `[]string` | A |
| 降级可用 | 断开 `LLM_API_KEY` 后系统正常 | A |
| API 兼容 | `ChatResponse` 结构不变 | A |
| DI | Summarizer 在 app.go 构造注入 | A |
| service.go 行数 | 不超过 350 行 | A |
| 多轮上下文 | "第二条帮我诊断"能关联上一轮 | B |
| 诊断入口 | "帮我诊断"无需 fingerprint | B |
| 上下文兼容 | 旧 session 无 `:ctx` key 时正常 | B |
| 编译通过 | `cd server-monitor/server-web && go build ./... && go test ./internal/copilot/...` 通过 | A/B/C |

---

## 11. 与 design.md 对齐清单

AI 升级完成后，需更新 design.md 以下章节：

| design.md 章节 | 需补充内容 |
|---------------|-----------|
| §16.2 目标结构 | `internal/copilot/` 下新增 `summary/`、`context/`、`suggestion/` 子包 |
| §17.2 文件移动映射 | 补充 10 个新文件的迁移映射 |
| §19.3 文件移动统计 | copilot 文件数从 ~65 更新为 ~75 |
| §20.2 CI 影响 | 无额外影响（新子包在 `copilot/` 下，`go test ./...` 自动包含） |
