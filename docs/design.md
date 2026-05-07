# CloudOps Copilot — 云原生智能运维平台技术方案

> 文档版本：v3.1 | 更新时间：2026-05-07
> 文档定位：以 AI ChatOps 引擎为核心的技术方案，面向秋招 AI 方向项目展示。
> 落地边界：`server-monitor` 是已实现的监控告警底座；AI ChatOps、诊断、Runbook、审批和审计是二改目标能力，部分能力参考 `chatops` 原型迁移。

---

## 1. 项目概述

### 1.1 一句话定义

CloudOps Copilot 是一个基于 LLM 的云原生智能运维平台，通过自然语言交互、意图识别、多源证据融合和智能决策引擎，实现告警自动诊断、运维知识检索和人工确认执行的 AIOps 闭环。

### 1.2 项目核心价值

传统运维平台依赖人工经验判断告警根因、手动查阅 Runbook、逐个系统排查指标。CloudOps Copilot 在现有监控告警闭环之上，构建了一套 AI 驱动的智能运维引擎：

| 传统运维 | CloudOps Copilot |
|---|---|
| 人工逐条查看告警，凭经验判断 | AI 自动采集多源证据，生成结构化诊断报告 |
| 手动翻阅 Runbook 文档 | 基于告警上下文自动检索匹配的 Runbook 片段 |
| 运维知识散落在各系统 | LLM 融合指标、日志、历史、知识库，给出综合建议 |
| 执行操作缺乏审批和追溯 | Human-in-the-loop 安全框架，写操作必须审批和审计 |
| 查询需要熟悉 PromQL / kubectl | 自然语言交互，LLM 自动编排工具链 |

### 1.3 AI 核心能力矩阵

| AI 能力 | 技术实现 | 业务价值 |
|---|---|---|
| 自然语言理解 | LLM Intent Parsing + Entity Extraction | 用自然语言查询监控状态和执行运维操作 |
| 智能意图路由 | 结构化工具调用协议 + Rule-based Fallback | 精准识别用户意图，路由到对应工具链 |
| 多源证据融合 | Evidence Collector + Rule Analyzer | 聚合 Prometheus 指标、Redis 告警、MySQL 历史、Runbook |
| 知识增强生成 | RAG (Markdown + Keyword → 向量化) | 基于真实运维知识生成诊断，降低幻觉 |
| 智能决策推荐 | Confidence Scoring + Risk Grading | 区分低/中/高风险动作，推荐可执行方案 |
| 人机协同执行 | Human-in-the-loop + Audit Trail | 写操作必须人工审批，全链路可追溯 |

### 1.4 技术栈全景

```text
┌─────────────────────────────────────────────────────────────────┐
│                        前端层 (Vue 3 + TS)                       │
│  监控大屏 │ 告警中心 │ Copilot 对话 │ 诊断报告 │ 审批管理 │ 审计  │
└──────────────────────────────┬──────────────────────────────────┘
                               │ HTTP + WebSocket
┌──────────────────────────────┴──────────────────────────────────┐
│                     API Gateway (Gin + Middleware)               │
│  CORS │ OTel │ Logging │ Recovery │ Metrics │ RateLimit │ Auth  │
└──────┬──────────┬──────────┬──────────┬──────────┬──────────────┘
       │          │          │          │          │
┌──────┴───┐ ┌────┴────┐ ┌──┴───┐ ┌────┴────┐ ┌───┴──────────┐
│ AI 引擎  │ │ 监控服务 │ │ 告警  │ │ 主机服务 │ │ 认证/权限    │
│ Copilot  │ │ Prom查询 │ │ Webhook│ │ Host    │ │ JWT + RBAC   │
│ Tool Reg │ │ VM查询   │ │ Kafka │ │ Cache   │ │ TokenVersion│
│ Diagnosis│ │          │ │ Redis │ │         │ │              │
│ RAG      │ │          │ │ MySQL │ │         │ │              │
└──────┬───┘ └────┬────┘ └──┬───┘ └────┬────┘ └───┬──────────┘
       │          │         │          │          │
┌──────┴──────────┴─────────┴──────────┴──────────┴──────────────┐
│                       基础设施层                                  │
│  Redis │ MySQL │ Kafka │ Prometheus │ VM │ ES │ Jaeger │ K8s   │
└─────────────────────────────────────────────────────────────────┘
```

---

## 2. 系统总体架构

### 2.1 系统组成

CloudOps Copilot 由两个协同子系统构成。当前已经落地的是 `server-monitor` 监控告警底座，AI ChatOps 引擎属于二改规划，`chatops` 目录提供可迁移的原型代码。

| 子系统 | 职责 | 当前状态 |
|---|---|---|
| **AI ChatOps 引擎** | 自然语言交互、意图识别、工具编排、告警诊断、知识检索、安全执行 | 二改设计方案，基于 `chatops` 原型升级 |
| **server-monitor** | 指标采集、Prometheus 查询、告警管理、实时推送、可观测性 | 监控底座已实现 |

### 2.2 整体架构图

```text
                          ┌─────────────────────┐
                          │    用户 / 运维人员    │
                          └──────┬──────────────┘
                                 │
                    ┌────────────┴────────────┐
                    │  自然语言 / 告警事件触发  │
                    └────────────┬────────────┘
                                 │
              ┌──────────────────┴──────────────────┐
              │          server-web (Gin)            │
              │                                      │
              │  ┌──────────────────────────────┐   │
              │  │      AI ChatOps 引擎          │   │
              │  │                              │   │
              │  │  ┌────────┐  ┌────────────┐  │   │
              │  │  │ NLU    │  │ Tool       │  │   │
              │  │  │ Pipeline│→│ Registry   │  │   │
              │  │  └────────┘  └─────┬──────┘  │   │
              │  │                     │         │   │
              │  │  ┌────────────┐  ┌──┴───────┐ │   │
              │  │  │ Diagnosis  │  │ Evidence  │ │   │
              │  │  │ Engine     │←│ Collector │ │   │
              │  │  └─────┬──────┘  └──────────┘ │   │
              │  │        │                       │   │
              │  │  ┌─────┴──────┐  ┌──────────┐ │   │
              │  │  │ Action     │  │ RAG      │ │   │
              │  │  │ Advisor    │  │ Retriever│ │   │
              │  │  └────────────┘  └──────────┘ │   │
              │  └──────────────────────────────┘   │
              │                                      │
              │  ┌──────────────────────────────┐   │
              │  │      监控告警服务              │   │
              │  │  Host │ Alert │ Auth │ WS     │   │
              │  └──────────────────────────────┘   │
              └──────────────────┬──────────────────┘
                                 │
         ┌───────────┬───────────┼───────────┬────────────┐
         │           │           │           │            │
    ┌────┴────┐ ┌────┴────┐ ┌───┴───┐ ┌─────┴─────┐ ┌───┴────┐
    │ Redis   │ │ MySQL   │ │ Kafka │ │Prometheus │ │ K8s    │
    │缓存/状态│ │持久存储 │ │事件总线│ │ /VM 指标  │ │ 资源   │
    └─────────┘ └─────────┘ └───────┘ └───────────┘ └────────┘
```

### 2.3 server-monitor 当前能力概览

server-monitor 是已落地的监控告警底座，为 AI 引擎提供数据基础：

| 模块 | 路径 | 核心能力 |
|---|---|---|
| server-probe | `server-monitor/server-probe/` | 采集 CPU/内存/磁盘/网络/负载/进程，暴露 `/metrics` |
| server-web | `server-monitor/server-web/` | Gin API、JWT/RBAC、Prometheus 查询、告警 Webhook、WebSocket、Kafka |
| alert-service | `server-monitor/alert-service/` | 消费 Kafka `alert-events`，Redis 去重和告警状态聚合 |
| frontend | `server-monitor/frontend/` | Vue 3 + TS + Pinia + ECharts 监控控制台 |
| pkg | `server-monitor/pkg/` | logger、httpmiddleware、configutil、shutdown、tracer |

### 2.4 chatops 原型当前能力

chatops 是独立运行的 AI ChatOps 原型，为 AI 引擎提供基础实现参考：

| 组件 | 路径 | 当前能力 |
|---|---|---|
| 后端服务 | `chatops/server/` | Go + Gin，`POST /api/chat`，Redis 会话/缓存/限流 |
| LLM 服务 | `chatops/server/service/llm.go` | DeepSeek API 调用，system prompt 定义 5 个动作 |
| K8s 查询 | `chatops/server/service/k8s.go` | client-go，Pod/Deployment/Service/Node 列表 |
| Prometheus | `chatops/server/service/prometheus.go` | CPU/内存快捷查询 + 原始 PromQL range query |
| 工具执行 | `chatops/server/handler/chat.go` | LLM 返回 JSON 指令 → `switch-case` 分发执行 |

原型不足（AI 引擎升级方向）：

1. 无 Tool Registry：`switch-case` 硬编码，无法动态注册、校验、限流
2. 无意图分层：所有请求走同一条 LLM 路径，无确定性规则前置
3. 无证据融合：单工具调用，不组合多源数据
4. 无诊断管线：缺乏 Evidence → Rule → LLM 的结构化推理链
5. 无知识检索：没有 Runbook / RAG
6. 无安全框架：无审批、审计、风险分级
7. 无超时控制：LLM 调用使用 `http.DefaultClient`
8. 未复用 server-monitor：独立 Redis、无 JWT/RBAC、无日志/Trace

---

## 3. AI 核心引擎设计

AI 核心引擎是 CloudOps Copilot 的灵魂，负责将自然语言转化为可执行的运维动作，并将执行结果转化为人类可理解的建议。

### 3.1 引擎总体架构

```text
用户输入 / 告警事件
       │
       ▼
┌──────────────┐
│  NLU Pipeline │ ← 自然语言理解
│  ┌──────────┐ │
│  │ Intent   │ │  意图分类
│  │ Classifier│ │
│  └────┬─────┘ │
│  ┌────┴─────┐ │
│  │ Entity   │ │  实体抽取
│  │ Extractor │ │
│  └────┬─────┘ │
│  ┌────┴─────┐ │
│  │ Context  │ │  上下文管理
│  │ Manager  │ │
│  └────┬─────┘ │
└──────┼───────┘
       │
       ▼
┌──────────────┐
│  Decision     │ ← 智能决策
│  Engine       │
│  ┌──────────┐ │
│  │ Tool     │ │  工具选择与编排
│  │ Orchestr. │ │
│  └────┬─────┘ │
│  ┌────┴─────┐ │
│  │ Evidence │ │  证据采集与融合
│  │ Fusion   │ │
│  └────┬─────┘ │
│  ┌────┴─────┐ │
│  │ Reasoning│ │  推理与建议生成
│  │ Module   │ │
│  └────┬─────┘ │
└──────┼───────┘
       │
       ▼
┌──────────────┐
│  Safety       │ ← 安全执行
│  Framework    │
│  ┌──────────┐ │
│  │ Risk     │ │  风险评估
│  │ Assessor │ │
│  └────┬─────┘ │
│  ┌────┴─────┐ │
│  │ Approval │ │  人工审批
│  │ Gate     │ │
│  └────┬─────┘ │
│  ┌────┴─────┐ │
│  │ Audit    │ │  审计记录
│  │ Logger   │ │
│  └──────────┘ │
└──────────────┘
```

### 3.2 NLU Pipeline（自然语言理解管线）

#### 3.2.1 意图分类

采用 **规则前置 + LLM 兜底** 的两级路由策略：

```text
用户输入
  │
  ├─ 规则匹配（确定性、低延迟、零 Token 消耗）
  │   ├─ 关键词匹配：包含"告警/firing/warning" → alert_query
  │   ├─ 关键词匹配：包含"CPU/内存/磁盘/负载" → metric_query
  │   ├─ 关键词匹配：包含"Pod/Deployment/Node" → k8s_query
  │   ├─ 关键词匹配：包含"诊断/分析/根因" → diagnosis_request
  │   └─ 精确命令：以 "/" 开头 → command_mode
  │
  └─ LLM 意图解析（模糊/复杂/多意图输入）
      ├─ System Prompt 定义意图空间和输出格式
      ├─ LLM 返回结构化 JSON：{intent, entities, confidence}
      └─ confidence < 阈值时请求用户澄清
```

意图空间定义：

| 意图 | 说明 | 典型输入 |
|---|---|---|
| `alert_query` | 查询告警状态 | "当前有哪些 firing 告警？" |
| `metric_query` | 查询监控指标 | "node-1 的 CPU 趋势" |
| `k8s_query` | 查询 K8s 资源 | "default 命名空间有哪些 Pod？" |
| `diagnosis_request` | 触发告警诊断 | "帮我分析这条 HighCPU 告警" |
| `action_request` | 请求执行操作 | "重启 order-service" |
| `knowledge_query` | 查询运维知识 | "HighCPU 怎么排查？" |
| `general_chat` | 通用对话 | "你好"、"你能做什么？" |

#### 3.2.2 实体抽取

LLM 在意图分类的同时抽取关键实体：

```json
{
  "intent": "metric_query",
  "entities": {
    "hostname": "node-1",
    "metric_type": "cpu",
    "time_range": "15m"
  },
  "confidence": 0.92
}
```

实体类型：

| 实体 | 来源 | 示例 |
|---|---|---|
| `hostname` | 用户输入 / 告警上下文 | `node-1`、`10.0.0.5:9090` |
| `namespace` | 用户输入 / 默认值 | `default`、`production` |
| `metric_type` | 关键词映射 | `cpu` → `server_monitor_cpu_usage_percent` |
| `time_range` | 用户输入 / 默认值 | `15m`、`1h`、`6h`、`24h` |
| `alert_name` | 用户输入 / 告警事件 | `HighCPU`、`HostDown` |
| `severity` | 用户输入 / 告警上下文 | `warning`、`critical` |
| `resource_type` | 用户输入 | `pod`、`deployment`、`service`、`node` |
| `resource_name` | 用户输入 | `order-service` |

#### 3.2.3 上下文管理

多轮对话的上下文状态机：

```text
Session State
  ├── session_id: string
  ├── user_id: string (from JWT)
  ├── turn_count: int
  ├── current_intent: string
  ├── pending_entities: map[string]string  (待补全实体)
  ├── active_alert_fingerprint: string     (当前关联告警)
  ├── active_diagnosis_id: uint            (当前关联诊断)
  ├── tool_call_history: []ToolCallResult  (本轮已调用工具)
  └── last_evidence: json.RawMessage       (最近一次证据快照)
```

实体补全策略：

1. 用户说"帮我诊断这条告警"但未指定具体告警 → 查询 Redis `alert:active`，若只有 1 条则自动关联，多条则追问
2. 用户说"CPU 怎么样"但未指定主机 → 查询主机列表，若只有 1 台则自动关联，多台则追问
3. 上下文继承：上一轮诊断的告警，下一轮"还有其他建议吗"自动关联

### 3.3 Decision Engine（智能决策引擎）

#### 3.3.1 工具编排策略

根据意图和实体，决策引擎选择并编排工具调用。Phase 1 默认只开放只读工具；`action_request` 只创建建议或待审批动作，不直接执行写操作。

```text
意图: alert_query
  → alert.list_active (如果指定了 severity 则过滤)
  → 结果格式化返回

意图: diagnosis_request
  → alert.list_active (获取告警详情)
  → alert.history (获取历史模式)
  → host.metrics (获取关联指标，可并行)
  → prom.query_range (获取补充指标，可并行)
  → runbook.search (获取知识片段，可并行)
  → Rule Analyzer (确定性分析)
  → LLM Summarizer (归纳与建议)
  → 返回结构化诊断报告

意图: action_request
  → Risk Assessor (评估风险等级)
  → 低风险: 只读类动作直接执行；写操作仍需进入审批
  → 中风险: 创建 PendingAction，等待审批
  → 高风险: 拒绝执行，仅生成建议
```

工具编排核心算法——并行证据采集：

```text
func (e *DecisionEngine) orchestrate(ctx context.Context, intent Intent, entities Entities) (*Decision, error) {
    // Phase 1: 确定需要的工具列表
    tools := e.selectTools(intent, entities)

    // Phase 2: 按依赖关系分组。这里是设计示意，实际实现要结合
    // server-web 现有 service 接口逐步落地。
    groups := e.dependencyGroups(tools)
    //   Group 0 (无依赖，可并行): alert.list_active, host.metrics, runbook.search
    //   Group 1 (依赖 Group 0):   prom.query_range (需要 instance 参数)
    //   Group 2 (依赖全部证据):   LLM Summarizer

    // Phase 3: 逐组执行
    var evidence EvidenceBundle
    for _, group := range groups {
        results := e.executeParallel(ctx, group, entities, evidence)
        evidence.Merge(results)
    }

    // Phase 4: 推理与建议
    decision := e.reason(ctx, evidence, intent)
    return decision, nil
}
```

#### 3.3.2 证据融合模型

多源证据融合为统一的 Evidence Bundle：

```json
{
  "alert_context": {
    "fingerprint": "abc123",
    "alert_name": "HighCPU",
    "severity": "warning",
    "instance": "node-1:9090",
    "starts_at": "2026-05-07T10:00:00Z",
    "labels": {"job": "server-probe", "instance": "node-1:9090"}
  },
  "active_alerts": [
    {"name": "HighCPU", "instance": "node-1:9090", "severity": "warning"}
  ],
  "metrics": [
    {
      "name": "server_monitor_cpu_usage_percent",
      "instance": "node-1:9090",
      "window": "15m",
      "max": 93.2,
      "avg": 86.7,
      "last": 91.1,
      "trend": "rising"
    },
    {
      "name": "server_monitor_load1",
      "instance": "node-1:9090",
      "window": "15m",
      "max": 8.5,
      "avg": 6.2,
      "last": 7.8
    }
  ],
  "history": [
    {
      "fingerprint": "abc123",
      "fired_at": "2026-05-06T14:00:00Z",
      "resolved_at": "2026-05-06T14:30:00Z",
      "recurrence_count": 3
    }
  ],
  "runbooks": [
    {
      "title": "HighCPU 排查手册",
      "matched_keywords": ["HighCPU", "cpu"],
      "snippet": "1. 查看 CPU 15m 趋势 2. 对比 load 和进程数..."
    }
  ]
}
```

#### 3.3.3 置信度评分

诊断建议的置信度基于证据充分度计算：

```text
confidence = w1 * alert_evidence_score
           + w2 * metric_evidence_score
           + w3 * history_evidence_score
           + w4 * runbook_evidence_score

其中:
  alert_evidence_score  = 有活跃告警详情 ? 1.0 : 0.3
  metric_evidence_score = 有15m+趋势数据 ? 1.0 : 有即时值 ? 0.6 : 0.2
  history_evidence_score = 有历史记录 ? 0.8 : 0.3
  runbook_evidence_score = 命中Runbook ? 1.0 : 0.4

  w1=0.3, w2=0.3, w3=0.2, w4=0.2 (可配置)
```

置信度分级：

| 置信度 | 等级 | 行为 |
|---|---|---|
| ≥ 0.8 | High | 直接展示诊断结论，标注"高置信度" |
| 0.5 ~ 0.8 | Medium | 展示诊断结论，附加"建议补充更多证据" |
| < 0.5 | Low | 展示已有证据，明确告知"证据不足，建议人工排查" |

### 3.4 Tool Registry（工具注册中心）

#### 3.4.1 核心接口

```go
type ToolSchema struct {
    Name        string         `json:"name"`
    Description string         `json:"description"`
    Parameters  []ParamSchema  `json:"parameters"`
    RiskLevel   string         `json:"risk_level"`
    ReadOnly    bool           `json:"read_only"`
    Timeout     time.Duration  `json:"timeout"`
}

type ParamSchema struct {
    Name        string  `json:"name"`
    Type        string  `json:"type"`
    Required    bool    `json:"required"`
    Description string  `json:"description"`
    Enum        []string `json:"enum,omitempty"`
    Default     interface{} `json:"default,omitempty"`
}

type ToolResult struct {
    Success bool            `json:"success"`
    Data    json.RawMessage `json:"data"`
    Error   string          `json:"error,omitempty"`
    Duration time.Duration  `json:"duration"`
}

type Tool interface {
    Name() string
    Description() string
    Schema() ToolSchema
    Run(ctx context.Context, args json.RawMessage) (ToolResult, error)
}
```

#### 3.4.2 Registry 职责

```text
ToolRegistry
  ├── 注册: Register(tool Tool) error
  ├── 查询: Get(name string) (Tool, error)
  ├── 列表: List() []ToolSchema
  ├── 校验: Validate(name string, args json.RawMessage) error
  ├── 执行: Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
  │     ├── 参数校验 (JSON Schema validation)
  │     ├── 超时控制 (per-tool timeout, 默认 30s)
  │     ├── 权限检查 (RBAC: viewer 只读, admin 可写)
  │     ├── Trace 注入 (从 ctx 提取 trace_id)
  │     ├── 敏感过滤 (屏蔽 Secret/Token/Password 字段)
  │     └── 调用日志 (记录 tool_name, args_hash, duration, success)
  └── 健康检查: HealthCheck(ctx context.Context) map[string]bool
```

#### 3.4.3 第一版工具清单

**只读工具（实施计划 Phase 1 落地）：**

| 工具名 | 说明 | 参数 | 复用来源 |
|---|---|---|---|
| `host.list` | 主机列表 | `status, search, sort, risk, group_id` | `host.Service` |
| `host.metrics` | 主机趋势指标 | `instance, window(15m/1h/6h/24h)` | `host.Service` + `prometheus.Client` |
| `alert.list_active` | 活跃告警 | `severity(可选)` | `alert.Service` / Redis `alert:active` |
| `alert.events` | 告警事件流 | `count(默认20)` | `alert.Service` / Redis Stream |
| `alert.history` | 告警历史 | `status, severity, alert_name, instance, page, page_size` | MySQL `AlertHistory` |
| `alert.rule_list` | 告警规则 | 无 | MySQL `AlertRule` |
| `prom.query_range` | 受控范围查询 | `query, start, end, step, max_points(1000)` | `prometheus.Client` |

**Runbook 检索工具（实施计划 Phase 4 落地）：**

| 工具名 | 说明 | 参数 | 复用来源 |
|---|---|---|---|
| `runbook.search` | Runbook 检索 | `keywords[], alert_name(可选)` | 新增 |

**K8s 只读工具（实施计划 Phase 7 落地，迁移 `chatops` 原型能力）：**

| 工具名 | 说明 | 参数 |
|---|---|---|
| `k8s.get_pods` | Pod 列表 | `namespace` |
| `k8s.get_deployments` | Deployment 列表 | `namespace` |
| `k8s.get_services` | Service 列表 | `namespace` |
| `k8s.get_nodes` | Node 状态 | 无 |
| `k8s.get_events` | K8s 事件 | `namespace, limit` |
| `k8s.get_logs` | Pod 日志 | `namespace, pod_name, tail_lines(100)` |

**写操作工具（实施计划 Phase 6-7 落地，必须经过审批和审计）：**

| 工具名 | 说明 | 风险 | 限制 |
|---|---|---|---|
| `k8s.restart_deployment` | 重启 Deployment | 中 | 只能指定 namespace/name，需 admin 审批 |
| `k8s.scale_deployment` | 扩缩容 Deployment | 中 | replicas 必须在 [1, max_replicas] 范围，需 admin 审批 |

#### 3.4.4 PromQL 安全约束

`prom.query_range` 工具的安全限制：

```text
1. 时间范围: end - start ≤ 7d (防止大范围查询拖垮 Prometheus)
2. 步长: step ≥ 15s
3. 返回点数: (end-start)/step ≤ 1000
4. 超时: 30s
5. 禁止的 PromQL 模式:
   - 不允许包含 offset > 7d
   - 不允许子查询 (subquery)
   - 不允许 __internal_ 前缀的指标
6. 结果大小: 响应 body ≤ 1MB
```

### 3.5 LLM 集成设计

#### 3.5.1 LLM 调用架构

```text
┌─────────────────────────────────────────────┐
│              LLM Client                      │
│                                              │
│  ┌─────────────┐  ┌──────────────────────┐  │
│  │ Prompt       │  │ Response             │  │
│  │ Builder      │  │ Parser               │  │
│  │              │  │                      │  │
│  │ - System     │  │ - JSON extraction    │  │
│  │   Prompt     │  │ - Schema validation  │  │
│  │ - Evidence   │  │ - Error recovery     │  │
│  │   Injection  │  │ - Retry logic        │  │
│  │ - History    │  │                      │  │
│  │   Truncation │  │                      │  │
│  └──────┬──────┘  └──────────┬───────────┘  │
│         │                     │              │
│  ┌──────┴─────────────────────┴───────────┐  │
│  │          HTTP Client                    │  │
│  │  - Timeout: 60s                         │  │
│  │  - Retry: 2次, 指数退避                  │  │
│  │  - Circuit Breaker: 5次失败后熔断30s     │  │
│  │  - Token 限制: 输入 ≤ 4096, 输出 ≤ 2048 │  │
│  └────────────────────────────────────────┘  │
└─────────────────────────────────────────────┘
```

#### 3.5.2 Prompt 工程

**Chat 模式 System Prompt 结构：**

```text
你是 CloudOps Copilot，一个云原生智能运维助手。

## 你的能力
你可以通过工具查询监控指标、告警状态、K8s 资源和运维知识库。

## 工具列表
{tool_schemas}

## 输出规则
1. 当用户问题匹配工具能力时，返回 JSON 指令：{"tool": "tool_name", "args": {...}}
2. 当需要多个工具时，返回 JSON 数组
3. 当用户问题不匹配任何工具时，用自然语言回答
4. 必须基于工具返回的真实数据回答，不得编造集群状态
5. 涉及写操作时，必须提示用户需要审批

## 安全约束
- 不得泄露 Secret、Token、密码
- 不得建议删除 Namespace、PVC 等高风险操作
- 所有建议必须标注风险等级
```

说明：当前 `chatops` 原型已经采用“LLM 输出 JSON 指令、后端解析执行”的方式；二改会保留这种可控协议，并用 Tool Registry 做工具名、参数、权限和风险校验，而不是直接信任模型输出。

**Diagnosis 模式 Prompt 结构：**

```text
你是 CloudOps Copilot 的告警诊断引擎。

## 当前告警
{alert_context}

## 采集到的证据
{evidence_json}

## 命中的 Runbook
{runbook_snippets}

## 确定性分析结果
{rule_analysis}

## 输出要求
请基于以上证据生成结构化诊断报告，包含：
1. summary: 一句话总结
2. severity_assessment: 严重程度评估
3. root_cause_hypotheses: 根因假设列表，每个包含 cause, confidence(high/medium/low), evidence
4. recommended_actions: 建议动作列表，每个包含 type, risk(low/medium/high), requires_approval
5. next_steps: 后续排查步骤

注意：只基于证据推理，不做无依据的推测。
```

#### 3.5.3 上下文窗口管理

```text
Token 预算分配 (以 8K context 为例):
  System Prompt:    ~500 tokens
  Tool Schemas:     ~300 tokens
  Evidence Bundle:  ~2000 tokens (按优先级截断)
  Runbook Snippets: ~1000 tokens (Top 2 片段)
  Chat History:     ~2000 tokens (最近 6 轮)
  Current Message:  ~200 tokens
  Output Reserve:   ~2000 tokens

Evidence 截断优先级:
  1. 告警详情 (最高，不可截断)
  2. 当前指标趋势
  3. 历史告警模式
  4. Runbook 片段
  5. 关联告警 (最低，可截断)
```

---

## 4. ChatOps 模块详细设计

### 4.1 Chat API

```text
POST   /api/v1/copilot/chat          发送消息
GET    /api/v1/copilot/sessions       会话列表
GET    /api/v1/copilot/sessions/:id   会话详情
DELETE /api/v1/copilot/sessions/:id   删除会话
GET    /api/v1/copilot/sessions/:id/messages  会话消息
```

**POST /api/v1/copilot/chat**

请求：

```json
{
  "message": "当前有哪些 firing 告警？",
  "session_id": "sess_abc123"
}
```

响应：

```json
{
  "session_id": "sess_abc123",
  "reply": "当前有 2 条 firing 告警：\n1. HighCPU (warning) - node-1:9090\n2. HighMemory (critical) - node-2:9090",
  "intent": "alert_query",
  "confidence": 0.95,
  "tool_calls": [
    {
      "name": "alert.list_active",
      "status": "success",
      "duration_ms": 23
    }
  ],
  "suggestions": ["查看 HighCPU 诊断报告", "查看 HighMemory 详情"]
}
```

### 4.2 会话管理

```text
存储策略:
  - 短期会话: Redis List (chat:session:<id>, TTL 2h, 最多 50 条消息)
  - 持久化:   MySQL chat_sessions + chat_messages (用户主动保存或诊断关联)

Redis Key 设计:
  chat:session:<session_id>     List    消息列表
  chat:session:<session_id>:meta  Hash   元数据 (user_id, title, created_at)

会话生命周期:
  1. 首次请求无 session_id → 创建新会话
  2. 每次消息追加到 Redis List
  3. 超过 TTL 自动过期
  4. 诊断报告关联时自动持久化到 MySQL
```

### 4.3 请求处理流程

```text
POST /api/v1/copilot/chat
  │
  ├─ 1. JWT 鉴权 + RBAC 检查
  ├─ 2. 限流检查 (Redis 滑动窗口, 复用 server-web RateLimit)
  ├─ 3. 请求校验 (message 非空, 长度 ≤ 2000)
  ├─ 4. 会话加载/创建
  │
  ├─ 5. NLU Pipeline
  │     ├─ 规则匹配 (关键词 → intent)
  │     └─ LLM 兜底 (复杂输入 → structured intent)
  │
  ├─ 6. Decision Engine
  │     ├─ 工具选择 (intent → tool list)
  │     ├─ 实体补全 (缺失实体 → 追问 / 默认值)
  │     ├─ 工具执行 (并行 / 串行, 带超时和 trace)
  │     └─ 证据融合 (多源结果 → Evidence Bundle)
  │
  ├─ 7. Response Generation
  │     ├─ 只读结果: 直接格式化返回
  │     ├─ 诊断结果: LLM 归纳 + 结构化报告
  │     └─ 写操作: 创建 PendingAction, 返回审批链接
  │
  ├─ 8. 会话持久化 (Redis)
  └─ 9. 返回响应
```

---

## 5. 告警诊断引擎

### 5.1 触发方式

| 触发源 | 方式 | 说明 |
|---|---|---|
| 用户手动 | 告警详情页点击"生成诊断" | 同步触发，返回诊断 ID |
| 用户对话 | "帮我分析这条 HighCPU 告警" | Chat 流程中触发 |
| 告警事件 | Diagnosis Worker 消费 `alert-events` | 异步触发，对 firing 告警自动诊断 |

**禁止的触发方式：**
- 不在 Alertmanager Webhook 同步请求中调用 LLM
- 不对每条 `resolved` 事件自动诊断

### 5.2 Diagnosis Pipeline

```text
触发源
  │
  ▼
┌────────────────────┐
│ AlertContextParser  │  解析告警上下文，提取 fingerprint/alert_name/instance/labels
└────────┬───────────┘
         │
         ▼
┌────────────────────┐
│ EvidenceCollector   │  并行采集多源证据
│  ├─ alert.list_active   (Redis)
│  ├─ alert.history       (MySQL)
│  ├─ host.metrics        (Prometheus)
│  ├─ prom.query_range    (Prometheus)
│  └─ runbook.search      (内存/文件)
└────────┬───────────┘
         │
         ▼
┌────────────────────┐
│ RuleAnalyzer       │  确定性规则分析（LLM 前置）
└────────┬───────────┘
         │
         ▼
┌────────────────────┐
│ LLMSummarizer      │  基于证据 + 规则分析 + Runbook 生成诊断
└────────┬───────────┘
         │
         ▼
┌────────────────────┐
│ ActionAdvisor      │  生成建议动作，评估风险等级
└────────┬───────────┘
         │
         ▼
┌────────────────────┐
│ DiagnosisReport     │  持久化到 MySQL，WebSocket 推送前端
│ Store + Notify      │
└────────────────────┘
```

### 5.3 RuleAnalyzer（确定性规则引擎）

LLM 调用前先做确定性分析，降低幻觉风险和 Token 消耗：

| 告警类型 | 确定性判断逻辑 |
|---|---|
| `HighCPU` / `CriticalCPU` | CPU 15m 最大值、平均值、持续时间、load 同步升高、进程数变化 |
| `HighMemory` | 内存使用率趋势、可用内存绝对值、是否持续上升 |
| `HighDisk` | mountpoint、剩余空间绝对值、增长速率 |
| `HostDown` | `up{job="server-probe"}` 是否为 0、是否多节点同时异常、网络分区判断 |
| `HighErrorRate` | 5xx 比例、请求量是否足够（> 100 QPS）、时间分布 |
| `HighLatency` | P95/P99 延迟、错误率是否同时升高、是否与部署时间相关 |

规则输出示例：

```json
{
  "alert_name": "HighCPU",
  "rule_results": [
    {
      "rule": "cpu_sustained_high",
      "passed": true,
      "detail": "CPU 15m avg=86.7%, max=93.2%, 持续超过 80% 阈值"
    },
    {
      "rule": "load_correlated",
      "passed": true,
      "detail": "load1=7.8, 与 CPU 趋势正相关"
    },
    {
      "rule": "process_anomaly",
      "passed": false,
      "detail": "process_count=156, 与 7 天均值 148 偏差 5.4%, 无显著异常"
    }
  ],
  "summary": "CPU 持续高位，load 同步升高，进程数无明显异常"
}
```

### 5.4 LLM 诊断输出

```json
{
  "summary": "主机 node-1 CPU 使用率持续过高，load 同步升高，可能由业务负载增加导致",
  "severity_assessment": "warning",
  "confidence": 0.82,
  "root_cause_hypotheses": [
    {
      "cause": "业务流量增长导致 CPU 消耗升高",
      "confidence": "medium",
      "evidence": [
        "CPU 15m avg > 85%",
        "load1 同步升高至 7.8",
        "进程数无显著变化，排除进程泄漏"
      ]
    },
    {
      "cause": "定时任务或批处理作业占用",
      "confidence": "low",
      "evidence": [
        "历史告警显示该主机每日 10:00 左右触发",
        "当前时间 10:15"
      ]
    }
  ],
  "recommended_actions": [
    {
      "type": "inspect_process",
      "description": "查看 CPU 占用 Top 进程",
      "risk": "low",
      "requires_approval": false
    },
    {
      "type": "scale_deployment",
      "description": "扩容关联 Deployment 副本",
      "risk": "medium",
      "requires_approval": true,
      "target": {"namespace": "production", "name": "order-service", "replicas": 3}
    }
  ],
  "next_steps": [
    "查看 CPU Top 进程排名",
    "确认近期是否有发布或流量变化",
    "检查历史告警模式，判断是否为周期性"
  ]
}
```

---

## 6. RAG 知识库

### 6.1 演进路线

```text
Phase 1 (二改第一版): Markdown + 关键词检索
  └── 零额外依赖，启动时加载到内存，按 alertname/关键词匹配

Phase 2 (优化): Markdown + TF-IDF + BM25
  └── 改进排序质量，支持模糊匹配，仍无需向量数据库

Phase 3 (进阶): Embedding + 向量数据库
  └── 引入 text-embedding 模型 + Milvus/Qdrant，支持语义检索
```

### 6.2 Phase 1 实现

**Runbook 目录结构：**

```text
server-monitor/runbooks/
  high-cpu.md
  critical-cpu.md
  high-memory.md
  high-disk.md
  host-down.md
  high-error-rate.md
  high-latency.md
  k8s-pod-crashloopbackoff.md
  k8s-deployment-unavailable.md
```

**Runbook 模板：**

```markdown
# HighCPU

## 适用告警
- HighCPU
- CriticalCPU

## 典型现象
CPU 使用率持续高于阈值。

## 关键指标
- server_monitor_cpu_usage_percent
- server_monitor_load1
- server_monitor_process_count

## 排查步骤
1. 查看 CPU 15m 趋势。
2. 对比 load 和进程数。
3. 查看同实例历史告警。
4. 确认近期发布、流量或批处理任务。

## 建议动作
- 低风险：继续观察、通知负责人、收集进程信息。
- 中风险：扩容副本、重启异常工作负载。
- 高风险：删除资源、修改 Secret、批量变更配置。

## 风险说明
任何写操作必须人工确认。
```

**检索算法：**

```text
输入: alert_name, keywords[]
输出: Top N Runbook 片段 (N=2)

算法:
  1. 精确匹配: alert_name == Runbook 标题或"适用告警"列表 → score += 10
  2. 关键词匹配: keywords 与 Runbook 正文关键词交集 → score += 交集数 * 2
  3. 指标名匹配: 证据中的指标名出现在"关键指标" → score += 5
  4. 按 score 降序，取 Top N
  5. 截取每个 Runbook 的前 500 字符作为 snippet
```

---

## 7. 人机协同安全框架

### 7.1 核心原则

1. **AI 只建议，不默认执行**：写操作必须经过人工审批
2. **最小权限**：K8s 写操作使用独立 ServiceAccount，最小 RBAC
3. **全链路审计**：成功、失败、拒绝、超时都记录 AuditLog
4. **风险分级**：不同风险等级对应不同审批策略
5. **可回滚**：所有写操作记录前置状态，支持回滚

### 7.2 动作风险分级

| 风险 | 示例 | 策略 |
|---|---|---|
| 低 | 查询指标、查询告警、查看 Runbook | 允许直接执行 |
| 中 | restart deployment、scale deployment | 必须审批，记录审计 |
| 高 | delete namespace、delete pvc、修改 secret | 禁止执行，仅生成建议 |

### 7.3 审批流程

```text
AI 推荐: restart deployment/order-service
  │
  ▼
创建 PendingAction (status=pending)
  │
  ├── WebSocket 推送审批通知给 admin
  │
  ▼
admin 查看风险、证据、诊断报告
  │
  ├── 审批 (approve)
  │     │
  │     ▼
  │   ActionExecutor 校验白名单 + RBAC
  │     │
  │     ├── 执行成功 → status=executed, AuditLog 记录
  │     └── 执行失败 → status=failed, AuditLog 记录错误
  │
  └── 拒绝 (reject)
        │
        ▼
      status=rejected, AuditLog 记录
```

### 7.4 Action API

```text
POST   /api/v1/actions/pending          创建待审批动作
GET    /api/v1/actions/pending          待审批列表 (admin)
GET    /api/v1/actions/:id              动作详情
POST   /api/v1/actions/:id/approve      审批通过 (admin)
POST   /api/v1/actions/:id/reject       拒绝 (admin)
POST   /api/v1/actions/:id/execute      执行 (admin, approve 后)
```

### 7.5 K8s 写操作白名单

| 动作 | 资源 | 限制 |
|---|---|---|
| `restart_deployment` | Deployment | 只能指定 namespace/name |
| `scale_deployment` | Deployment | replicas ∈ [1, max_replicas]（可配置） |
| `annotate_resource` | Pod/Deployment | 只允许固定 annotation 前缀 |

---

## 8. ChatOps 与 server-monitor 协作机制

### 8.1 协作架构总览

ChatOps 引擎不是独立服务，而是嵌入 `server-web` 的模块化组件，通过共享基础设施与 server-monitor 协同工作：

```text
┌──────────────────────────────────────────────────────────────┐
│                      server-web (Gin)                         │
│                                                              │
│  ┌─────────────────────────┐  ┌──────────────────────────┐  │
│  │   ChatOps AI 引擎       │  │   监控告警服务            │  │
│  │                         │  │                          │  │
│  │  CopilotHandler ────────┼──┼→ alert.Service           │  │
│  │  ToolRegistry ──────────┼──┼→ host.Service            │  │
│  │  DiagnosisEngine ───────┼──┼→ prometheus.Client        │  │
│  │  RAGRetriever           │  │  auth.Service            │  │
│  │  ActionAdvisor          │  │  webhook.Handler         │  │
│  └──────────┬──────────────┘  └──────────┬───────────────┘  │
│             │                             │                  │
│             └──────────┬──────────────────┘                  │
│                        │                                     │
│             ┌──────────┴──────────┐                          │
│             │   共享基础设施       │                          │
│             │  Redis │ MySQL │    │                          │
│             │  Kafka │ WebSocket │                           │
│             │  JWT │ RBAC │ Log  │                           │
│             │  Trace │ Metrics   │                           │
│             └────────────────────┘                           │
└──────────────────────────────────────────────────────────────┘
```

### 8.2 交互流程详解

#### 8.2.1 ChatOps 查询告警

```text
用户: "当前有哪些 firing 告警？"
  │
  ├─ [1] POST /api/v1/copilot/chat
  │     Auth: JWT → user_id, role
  │     RateLimit: Redis 滑动窗口
  │
  ├─ [2] NLU: 规则匹配 → intent=alert_query
  │
  ├─ [3] Decision: 选择 alert.list_active
  │
  ├─ [4] ToolRegistry.Execute("alert.list_active", {})
  │     └─ 调用 alert.Service.GetActiveAlerts()
  │         └─ Redis HGETALL alert:active
  │
  ├─ [5] 格式化结果返回
  │
  └─ [6] Redis 保存会话消息
```

#### 8.2.2 告警事件触发自动诊断

```text
Prometheus Rule 触发 HighCPU
  │
  ├─ [1] Alertmanager → POST /api/v1/webhook/alertmanager
  │     server-web alert.Service.HandleWebhook()
  │     └─ Redis HSET alert:active
  │     └─ Redis Stream XADD alert:events + dedupe
  │     └─ MySQL INSERT alert_histories
  │     └─ Kafka PRODUCE alert-events
  │     └─ Redis Pub/Sub → WebSocket → 前端
  │
  ├─ [2] Diagnosis Worker 消费 alert-events
  │     └─ 过滤: 只处理 firing 事件
  │     └─ 去重: 检查是否已有进行中/已完成的诊断
  │
  ├─ [3] EvidenceCollector 并行采集
  │     ├─ Redis HGET alert:active → 告警详情
  │     ├─ MySQL SELECT alert_histories → 历史模式
  │     ├─ Prometheus range_query → CPU/load/process 指标
  │     └─ Runbook.search("HighCPU") → 排查手册
  │
  ├─ [4] RuleAnalyzer 确定性分析
  │
  ├─ [5] LLM Summarizer 生成诊断报告
  │
  ├─ [6] MySQL INSERT diagnosis_reports
  │
  ├─ [7] WebSocket 推送诊断状态
  │     └─ Phase 1: 复用 /ws/alerts 的 WebSocket Hub
  │     └─ Phase 2: 可选 Redis Pub/Sub diagnosis:channel 支持多副本广播
  │
  └─ [8] 如有建议动作 → 创建 PendingAction → 通知 admin
```

#### 8.2.3 用户对话触发诊断

```text
用户: "帮我分析 node-1 的 HighCPU 告警"
  │
  ├─ [1] NLU: intent=diagnosis_request, entities={hostname:node-1, alert_name:HighCPU}
  │
  ├─ [2] 实体补全: 查询 Redis alert:active，获取 fingerprint
  │
  ├─ [3] Decision: 选择诊断工具链
  │     ├─ alert.list_active (获取告警详情)
  │     ├─ alert.history (获取历史)
  │     ├─ host.metrics (获取指标)
  │     ├─ prom.query_range (补充指标)
  │     └─ runbook.search (获取知识)
  │
  ├─ [4] 并行执行工具链 → Evidence Bundle
  │
  ├─ [5] RuleAnalyzer + LLM Summarizer → 诊断报告
  │
  ├─ [6] MySQL 持久化 + WebSocket 推送
  │
  └─ [7] 返回结构化诊断结果
```

### 8.3 数据共享方式

#### 8.3.1 Redis 共享

| Key | 类型 | 生产者 | 消费者 | 说明 |
|---|---|---|---|---|
| `alert:active` | Hash | server-web, alert-service | ChatOps 诊断引擎 | 活跃告警状态 |
| `alert:events` | Stream | server-web | ChatOps 诊断引擎 | 告警事件流 |
| `alert:stats` | Hash | alert-service | ChatOps 诊断引擎 | 告警统计 |
| `hosts:list` | String | server-web host.Service | ChatOps Tool | 主机列表缓存 |
| `dashboard:overview` | String | server-web | ChatOps Tool | 总览缓存 |
| `chat:session:<id>` | List | ChatOps Copilot | ChatOps Copilot | 会话历史 |
| `diagnosis:task:<id>` | Hash | ChatOps 诊断引擎 | ChatOps 诊断引擎 | 诊断任务状态 |

**一致性说明：** `server-web` 和 `alert-service` 都写 `alert:active`，但写入载荷和去重策略不同。ChatOps 读取时应优先使用 `server-web` 的 `alert:events` / `alert_histories` 获取完整 Alertmanager payload；`alert-service` 写入的 `alert:active` 和 `alert:stats` 更适合作为活跃状态和统计补充。二改时如果继续共用 `alert:active`，需要在 payload 中增加 `source` 或统一序列化结构，避免不同服务覆盖字段导致诊断证据不一致。

#### 8.3.2 MySQL 共享

| 表 | 生产者 | 消费者 | 说明 |
|---|---|---|---|
| `users` | server-web auth | ChatOps (鉴权) | 共享 JWT 校验 |
| `alert_histories` | server-web alert | ChatOps 诊断引擎 | 告警历史 |
| `alert_rules` | server-web | ChatOps Tool | 告警规则 |
| `notification_channels` | server-web | — | 通知渠道 |
| `host_groups` | server-web | ChatOps Tool | 主机分组 |
| `diagnosis_reports` | ChatOps 诊断引擎 | 前端 | 诊断报告 (新增) |
| `pending_actions` | ChatOps ActionAdvisor | 前端 | 待审批动作 (新增) |
| `audit_logs` | ChatOps 安全框架 | 前端 | 审计日志 (新增) |
| `chat_sessions` | ChatOps Copilot | 前端 | 会话 (新增) |
| `chat_messages` | ChatOps Copilot | 前端 | 消息 (新增) |

#### 8.3.3 Kafka 共享

| Topic | 生产者 | 消费者 | 说明 |
|---|---|---|---|
| `alert-events` | server-web | alert-service, Diagnosis Worker | 告警事件 |
| `operation-events` | 当前已初始化；后续由 ChatOps ActionExecutor 生产 | 审计模块 | 审批和执行事件 |
| `diagnosis-events` | 后续可选新增 | WebSocket 推送 / 审计模块 | 诊断状态事件；Phase 1 可先不新增 Topic |

### 8.4 事件响应机制

#### 8.4.1 事件订阅模型

```text
Diagnosis Worker 订阅:
  Topic:        alert-events
  ConsumerGroup: diagnosis-worker
  Offset:       参考现有 Sarama 配置使用 OffsetOldest，依赖 Redis 幂等去重避免重复诊断
  过滤:         只处理 status=firing 的事件

处理逻辑:
  1. 接收 alert-event
  2. 检查 Redis diagnosis:task:<fingerprint> 是否已有进行中诊断
  3. 如有 → 跳过 (避免重复诊断)
  4. 如无 → 创建诊断任务 → 执行 Diagnosis Pipeline
  5. 诊断完成 → 更新 MySQL diagnosis_reports → 推送 WebSocket
```

#### 8.4.2 WebSocket 推送扩展

当前 WebSocket 推送告警事件和主机列表。二改扩展推送类型：

```json
{
  "type": "diagnosis_update",
  "data": {
    "diagnosis_id": 42,
    "fingerprint": "abc123",
    "alert_name": "HighCPU",
    "status": "completed",
    "summary": "CPU 持续过高，可能由业务负载增加导致",
    "severity": "warning",
    "confidence": 0.82
  }
}
```

```json
{
  "type": "action_pending",
  "data": {
    "action_id": 7,
    "action_type": "restart_deployment",
    "target": "production/order-service",
    "risk_level": "medium",
    "requested_by": "ai-copilot",
    "diagnosis_id": 42
  }
}
```

### 8.5 接口定义

#### 8.5.1 ChatOps 内部调用 server-monitor 服务接口

ChatOps 引擎通过 Go 接口调用 server-monitor 现有服务，而非 HTTP 内部调用：

```go
type AlertReader interface {
    GetActiveAlerts(ctx context.Context) ([]AlertRecord, error)
    GetAlertEvents(ctx context.Context, count int64) ([]AlertEvent, error)
}

type HostReader interface {
    ListHosts(ctx context.Context, opts HostListOptions) (*HostListResult, error)
    GetHostMetrics(ctx context.Context, instance string, window string) (*HostMetrics, error)
}

type MetricsQuerier interface {
    QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (*PromResult, error)
}

type HistoryReader interface {
    ListAlertHistories(ctx context.Context, opts HistoryListOptions) (*HistoryListResult, error)
}
```

**设计理由：**
- 进程内调用，零网络开销
- 复用 server-web 已有的 Redis/Prometheus/MySQL 连接池
- 接口化便于单元测试时 mock
- 不引入 gRPC 或 HTTP 内部调用的额外复杂度

#### 8.5.2 Diagnosis Worker 与 Kafka 交互

```go
type DiagnosisConsumer interface {
    Consume(ctx context.Context, handler func(event AlertEvent) error) error
}

type DiagnosisProducer interface {
    PublishDiagnosisEvent(ctx context.Context, event DiagnosisEvent) error
}
```

#### 8.5.3 ActionExecutor 与 K8s 交互

```go
type K8sExecutor interface {
    RestartDeployment(ctx context.Context, namespace, name string) error
    ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error
}
```

### 8.6 通信协议

#### 8.6.1 ChatOps → 前端 (HTTP + WebSocket)

```text
HTTP:
  - 请求: POST /api/v1/copilot/chat
  - 响应: Content-Type: application/json
  - 编码: UTF-8
  - 超时: 客户端 120s (LLM 调用可能较慢)

WebSocket:
  - 路径: /ws/alerts (复用现有)
  - 消息格式: JSON { type, data }
  - 新增 type: diagnosis_update, action_pending, action_status
  - 心跳: 复用现有 ping/pong
```

#### 8.6.2 Diagnosis Worker → Kafka

```text
Consumer:
  - Topic: alert-events
  - Group ID: diagnosis-worker
  - Offset 策略: 参考现有 Sarama Consumer，默认 OffsetOldest，依赖 Redis 幂等去重避免重复诊断
  - 并发: 通过 worker 数量和消费组实例数控制
  - 超时: 跟随 Sarama ConsumerGroup 配置，按实际压测调整

Producer:
  - Topic: operation-events；diagnosis-events 可选
  - Acks: WaitForLocal
  - Retry: 3
  - MaxMessageBytes: 1MB
```

### 8.7 异常处理策略

| 异常场景 | 处理策略 | 恢复方式 |
|---|---|---|
| LLM API 超时 | 返回"AI 服务暂时不可用"，降级为规则分析结果 | 重试 2 次，指数退避 |
| LLM API 返回错误 | 记录错误日志，返回已有证据 + 规则分析 | 自动降级 |
| LLM 输出格式错误 | JSON 解析失败时尝试修复（提取 JSON 块），修复失败则降级 | 自动降级 |
| Prometheus 查询超时 | 跳过该指标证据，标记 evidence_score 降低 | 诊断继续，标注"指标数据不完整" |
| Redis 连接失败 | ChatOps 降级为无会话模式，诊断降级为无告警状态 | 自动重连 |
| MySQL 写入失败 | 诊断报告暂存 Redis，后台重试写入 | 异步重试，告警 |
| Kafka 消费延迟 | 诊断 Worker 记录 lag 指标，lag > 1000 时告警 | 自动恢复 |
| K8s API 不可用 | K8s 工具返回错误，不影响其他工具 | 工具级别降级 |
| 工具执行超时 | 单工具超时 30s，超时后跳过，标记 evidence 不完整 | 诊断继续 |
| 重复诊断请求 | Redis 去重检查，已有进行中诊断则跳过 | 幂等处理 |

**降级策略总结：**

```text
完整模式:  LLM + Rule + Evidence + Runbook → 结构化诊断报告
降级模式1: Rule + Evidence (无 LLM) → 确定性分析 + 原始证据
降级模式2: Evidence Only (无 LLM 无 Rule) → 原始证据展示
降级模式3: Chat Only (无工具) → 纯对话模式
```

### 8.8 性能优化方案

| 优化点 | 方案 | 预期效果 |
|---|---|---|
| LLM 调用延迟 | 异步诊断 + WebSocket 推送结果 | 用户不阻塞等待 |
| 工具并行执行 | EvidenceCollector 并行调用独立工具 | 证据采集时间从 O(n) 降到 O(1) |
| 会话缓存 | Redis 缓存查询结果 (TTL 30s) | 相同问题不重复调用工具 |
| 诊断去重 | Redis 检查进行中诊断 | 避免重复 LLM 调用 |
| Prometheus 查询 | 限制时间范围和步长 | 防止大查询拖垮 Prometheus |
| LLM Token 优化 | 证据按优先级截断，控制输入 Token | 降低成本和延迟 |
| 规则前置 | 确定性规则在 LLM 前执行 | 减少不必要的 LLM 调用 |
| 工具结果缓存 | 相同参数的工具结果缓存 30s | 减少重复查询 |

---

## 9. 数据模型

### 9.1 现有模型（server-monitor）

| 模型 | 表名 | 核心字段 |
|---|---|---|
| `User` | `users` | id, username, password(bcrypt hash), role(admin/viewer), token_version |
| `HostGroup` | `host_groups` | id, name, description |
| `HostGroupMember` | `host_group_members` | id, group_id, instance |
| `AlertRule` | `alert_rules` | id, name, expr, duration, severity, summary, description, enabled |
| `NotificationChannel` | `notification_channels` | id, name, type, url, enabled |
| `AlertHistory` | `alert_histories` | id, fingerprint, alert_name, instance, severity, status, summary, labels_json, fired_at, resolved_at |

### 9.2 新增模型（ChatOps）

#### DiagnosisReport

```text
id                    uint64     primaryKey, autoIncrement
alert_history_id      uint64     索引, 关联告警历史
fingerprint           varchar(64) 告警指纹
alert_name            varchar(128) 告警名称
target_kind           varchar(32)  目标类型 (host/k8s_pod/k8s_deployment)
target_name           varchar(256) 目标名称
namespace             varchar(128) K8s 命名空间 (可选)
severity              varchar(32)  严重程度
status                varchar(32)  诊断状态: pending/running/completed/failed
summary               text         一句话总结
root_cause            text         根因分析 JSON
evidence_json         text         证据快照 JSON
runbooks_json         text         命中 Runbook JSON
recommended_actions_json text      建议动作 JSON
rule_analysis_json    text         规则分析结果 JSON
confidence            float64      置信度 0-1
llm_prompt_hash       varchar(64)  Prompt 哈希 (用于缓存和审计)
llm_model             varchar(64)  使用的 LLM 模型
trigger_type          varchar(32)  触发方式: manual/chat/auto
created_by            uint         创建者 user_id
created_at            time.Time
updated_at            time.Time
```

#### PendingAction

```text
id                    uint64     primaryKey, autoIncrement
diagnosis_report_id   uint64     索引, 关联诊断报告
action_type           varchar(64) 动作类型
target_kind           varchar(32) 目标类型
target_name           varchar(256) 目标名称
namespace             varchar(128) K8s 命名空间
params_json           text        动作参数 JSON
risk_level            varchar(16) 风险等级: low/medium/high
status                varchar(32) 状态: pending/approved/rejected/executing/executed/failed/cancelled
requested_by          varchar(32) 请求来源: ai-copilot / user
approved_by           uint        审批人 user_id
executed_by           uint        执行人 user_id
result_json           text        执行结果 JSON
error_message         text        错误信息
created_at            time.Time
approved_at           *time.Time
executed_at           *time.Time
updated_at            time.Time
```

#### AuditLog

```text
id                    uint64     primaryKey, autoIncrement
actor                 varchar(128) 操作者 (username / ai-copilot)
actor_role            varchar(32)  角色
action                varchar(64)  动作类型
resource_type         varchar(64)  资源类型
resource_id           varchar(128) 资源标识
request_json          text         请求内容 (脱敏后)
result                varchar(32)  结果: success/failure/denied/timeout
error_message         text         错误信息
trace_id              varchar(64)  OpenTelemetry Trace ID
created_at            time.Time
```

#### ChatSession / ChatMessage

```text
chat_sessions:
  id          uint64     primaryKey
  user_id     uint       索引
  title       varchar(256)
  created_at  time.Time
  updated_at  time.Time

chat_messages:
  id              uint64     primaryKey
  session_id      uint64     索引
  role            varchar(16) user/assistant/system
  content         text
  intent          varchar(64) 识别的意图
  tool_calls_json text        工具调用记录 JSON
  created_at      time.Time
```

### 9.3 存储职责划分

| 存储 | 当前职责 | 新增职责 |
|---|---|---|
| **Prometheus** | 短期指标、规则计算、告警触发 | — |
| **VictoriaMetrics** | 长期指标 | — |
| **MySQL** | 用户、分组、规则、渠道、告警历史 | 诊断报告、待审批动作、审计日志、会话持久化 |
| **Redis** | 缓存、限流、活跃告警、事件流、Pub/Sub | 会话短期存储、诊断任务状态、工具调用缓存 |
| **Kafka** | `alert-events` 告警事件；`operation-events` 已预留 | `operation-events` 操作事件；`diagnosis-events` 可选新增 |
| **Elasticsearch** | 日志存储 | — |

---

## 10. API 接口规范

### 10.1 现有 API（server-monitor）

```text
公开接口:
  GET    /metrics
  GET    /healthz
  GET    /readyz
  GET    /readyz/full
  GET    /swagger/*
  POST   /api/v1/auth/login
  POST   /api/v1/webhook/alertmanager

登录接口:
  GET    /api/v1/auth/me
  GET    /api/v1/hosts
  GET    /api/v1/hosts/:instance/metrics
  GET    /api/v1/dashboard/overview
  GET    /api/v1/alerts/active
  GET    /api/v1/alerts/events
  GET    /api/v1/alert-histories
  GET    /api/v1/host-groups
  GET    /api/v1/host-groups/:id
  GET    /api/v1/alert-rules
  GET    /api/v1/alert-rules/:id
  GET    /api/v1/channels
  GET    /api/v1/channels/:id
  GET    /ws/alerts

admin 接口:
  POST   /api/v1/auth/register
  GET    /api/v1/users
  DELETE /api/v1/users/:id
  POST   /api/v1/host-groups
  PUT    /api/v1/host-groups/:id
  DELETE /api/v1/host-groups/:id
  POST   /api/v1/host-groups/:id/members
  DELETE /api/v1/host-groups/:id/members
  POST   /api/v1/alert-rules
  POST   /api/v1/alert-rules/sync
  PUT    /api/v1/alert-rules/:id
  DELETE /api/v1/alert-rules/:id
  POST   /api/v1/channels
  PUT    /api/v1/channels/:id
  DELETE /api/v1/channels/:id
  POST   /api/v1/channels/:id/test
```

### 10.2 新增 API（ChatOps）

**Copilot 对话：**

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/api/v1/copilot/chat` | 登录 | 发送消息，获取 AI 回复 |
| `GET` | `/api/v1/copilot/sessions` | 登录 | 会话列表 |
| `GET` | `/api/v1/copilot/sessions/:id` | 登录 | 会话详情 |
| `GET` | `/api/v1/copilot/sessions/:id/messages` | 登录 | 会话消息列表 |
| `DELETE` | `/api/v1/copilot/sessions/:id` | 登录 | 删除会话 |

**诊断报告：**

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/api/v1/diagnosis` | 登录 | 手动触发诊断 |
| `GET` | `/api/v1/diagnosis` | 登录 | 诊断报告列表 |
| `GET` | `/api/v1/diagnosis/:id` | 登录 | 诊断报告详情 |

**动作审批：**

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/api/v1/actions/pending` | admin | 待审批动作列表 |
| `GET` | `/api/v1/actions/:id` | admin | 动作详情 |
| `POST` | `/api/v1/actions/:id/approve` | admin | 审批通过 |
| `POST` | `/api/v1/actions/:id/reject` | admin | 拒绝 |
| `POST` | `/api/v1/actions/:id/execute` | admin | 执行 |

**审计日志：**

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `/api/v1/audit-logs` | admin | 审计日志列表 |

### 10.3 权限矩阵

| 能力 | viewer | admin |
|---|---|---|
| ChatOps 对话 | ✅ | ✅ |
| 触发只读诊断 | ✅ | ✅ |
| 查看诊断报告 | ✅ | ✅ |
| 创建待审批动作 | ❌ | ✅ |
| 审批动作 | ❌ | ✅ |
| 执行动作 | ❌ | ✅ |
| 查看审计日志 | ❌ | ✅ |

---

## 11. 安全设计

### 11.1 认证与授权

复用 server-monitor 现有 JWT + RBAC 体系：

- JWT Token (HS256)，Token Version 支持强制下线
- `admin` / `viewer` 角色
- ChatOps API 全部要求登录
- 写操作全部要求 admin

### 11.2 LLM 安全边界

| 安全要求 | 实现方式 |
|---|---|
| LLM 输出不可信 | Tool Registry 校验工具名和参数，不直接执行 LLM 输出 |
| 防止 Prompt 注入 | 用户输入经过转义后注入 Prompt，限制最大长度 2000 字符 |
| 敏感信息不泄露 | 禁止将 Secret/Token/Password 传给 LLM，工具结果脱敏 |
| PromQL 注入防护 | 限制查询范围、步长、返回大小，禁止子查询 |
| K8s 写操作白名单 | 只允许 restart_deployment / scale_deployment，参数校验 |
| 幻觉抑制 | Prompt 明确要求基于证据回答，Rule Analyzer 提供确定性锚点 |

### 11.3 敏感配置

| 配置 | 存储方式 | 说明 |
|---|---|---|
| `JWT_SECRET` | 环境变量 / K8s Secret | 至少 32 字节 |
| `LLM_API_KEY` | 环境变量 / K8s Secret | 二改新增，LLM API Key |
| `LLM_API_URL` | 环境变量 / ConfigMap | 二改新增，LLM API 地址 |
| `ADMIN_PASSWORD` | 环境变量 / K8s Secret | 初始管理员密码 |
| `REDIS_PASSWORD` | 环境变量 / K8s Secret | Redis 密码 |
| `MYSQL_PASSWORD` | 环境变量 / K8s Secret | MySQL 密码 |

---

## 12. 技术选型与创新点

### 12.1 核心技术选型

| 技术 | 选型 | 选型依据 |
|---|---|---|
| 后端语言 | Go 1.26 | 高并发、低延迟、云原生生态，与 server-monitor 一致 |
| HTTP 框架 | Gin v1.12 | 项目已在用，中间件生态成熟 |
| LLM | DeepSeek API (OpenAI 兼容) | `chatops` 原型已接入，性价比高；二改通过自定义 JSON 工具协议实现可控 Tool Calling |
| ORM | GORM v1.25 | 项目已在用，与 MySQL 集成成熟 |
| Redis | go-redis/v9 | 项目已在用，支持 Stream/Pub/Sub/Lua |
| Kafka | IBM/sarama v1.48 | 项目已在用，KRaft 模式无需 ZooKeeper |
| K8s 客户端 | client-go v0.36 | chatops 原型已有，Go 生态标准 |
| Prometheus | client_golang v1.23 | 项目已在用 |
| 链路追踪 | OpenTelemetry | 项目已在用，统一 Trace 上下文 |
| 前端 | Vue 3 + TS + Vite | 项目已在用 |

### 12.2 不引入的技术（及原因）

| 技术 | 不引入原因 |
|---|---|
| LangChain / LlamaIndex | Go 生态无成熟实现；自研 Tool Registry 更轻量可控 |
| 向量数据库 (Milvus/Qdrant) | Phase 1 关键词检索足够；避免增加部署复杂度 |
| gRPC | 进程内调用无需 gRPC；避免引入 proto 管理成本 |
| 独立 ChatOps 微服务 | 复用 server-web 基础设施更高效；避免服务间通信开销 |

### 12.3 AI 技术创新点

| 创新点 | 说明 | 秋招亮点 |
|---|---|---|
| **规则 + LLM 混合推理** | 确定性规则前置分析，LLM 负责归纳和建议，降低幻觉 | 体现对 AI 局限性的理解，工程化落地 |
| **多源证据融合诊断** | 聚合 Prometheus 指标、Redis 告警、MySQL 历史、Runbook 知识 | 不是简单 Chat，而是结构化推理链 |
| **Tool Registry 架构** | 替代 switch-case，支持注册/校验/限流/审计的工具执行框架 | 体现软件工程能力，可扩展可测试 |
| **Human-in-the-loop 安全** | AI 只建议不执行，写操作必须审批，全链路审计 | 体现 AI 安全意识，生产级设计 |
| **渐进式 RAG** | 从关键词检索到 BM25 到向量检索的演进路径 | 体现技术深度和务实迭代思维 |
| **降级容错** | LLM 不可用时降级为规则分析，工具超时时跳过 | 体现系统可靠性设计 |

---

## 13. 实现难点与解决方案

### 13.1 LLM 输出不确定性

**难点：** LLM 可能返回非 JSON 格式、错误的工具名、不安全的参数。

**解决方案：**
1. Prompt 工程约束输出格式
2. Tool Registry 强校验：工具名必须在注册列表中，参数必须符合 Schema
3. JSON 解析失败时尝试修复（提取 ```json``` 代码块）
4. 修复失败则降级为纯对话模式
5. 不信任 LLM 输出的任何执行指令，必须经过 Registry 校验

### 13.2 诊断实时性

**难点：** LLM 调用延迟 5-30s，用户不能阻塞等待。

**解决方案：**
1. 自动诊断走异步 Worker：Webhook 快速返回，Diagnosis Worker 后台执行
2. 手动 Chat 诊断可返回 diagnosis_id 和"正在诊断..."，诊断完成后通过 WebSocket 推送结果
3. 并行证据采集：独立工具并行调用，总时间取决于最慢工具
4. 规则前置：确定性规则快速给出初步结论，LLM 补充深度分析

### 13.3 多源数据一致性

**难点：** Redis 活跃告警由 server-web 和 alert-service 同时写入，去重策略不同。

**解决方案：**
1. ChatOps 读取 `alert:active` 时以 server-web 写入的数据为主
2. 诊断报告中标注数据来源和采集时间
3. 关键决策（如是否自动诊断）使用 fingerprint 去重，避免重复处理
4. 证据快照写入 `evidence_json`，保证诊断可复现

### 13.4 Prompt 注入防护

**难点：** 用户可能通过精心构造的输入操纵 LLM 行为。

**解决方案：**
1. 用户输入长度限制 2000 字符
2. 用户输入注入 Prompt 时做转义处理
3. 工具名和参数由 Registry 校验，不直接执行 LLM 输出
4. System Prompt 明确安全约束
5. 写操作不依赖 LLM 输出，而是由 ActionAdvisor 基于规则生成

### 13.5 Token 成本控制

**难点：** 每次诊断可能消耗大量 Token（证据 + 历史 + Runbook）。

**解决方案：**
1. 证据按优先级截断，控制输入 Token ≤ 4096
2. 规则前置减少不必要的 LLM 调用
3. 相同 fingerprint + evidence hash 的诊断结果缓存（基于 llm_prompt_hash）
4. 查询缓存（相同问题 30s 内不重复调用）
5. Phase 1 复用 `chatops` 的 DeepSeek 接入方式，后续将 LLM Provider 抽象成接口以便切换模型

---

## 14. 部署架构

### 14.1 Docker Compose 本地部署

在现有 `server-monitor/docker-compose.yml` 基础上新增。以下配置当前尚未进入 Compose 文件，属于二改实施项：

| 新增/变更 | 说明 |
|---|---|
| 环境变量 `LLM_API_KEY` | DeepSeek API Key |
| 环境变量 `LLM_API_URL` | LLM API 地址 |
| 环境变量 `LLM_MODEL` | 模型名称 (默认 deepseek-chat) |
| 环境变量 `LLM_TIMEOUT` | LLM 超时 (默认 60s) |
| 环境变量 `DIAGNOSIS_ENABLED` | 是否启用自动诊断 (默认 false) |
| 环境变量 `DIAGNOSIS_WORKER_COUNT` | 诊断 Worker 数量 (默认 1) |
| Runbook 挂载 | `./runbooks:/app/runbooks` |

无需新增服务容器，AI 引擎嵌入 `server-web`。

### 14.2 Kubernetes / Helm 部署

在现有 Helm Chart 基础上新增：

| 新增 | 说明 |
|---|---|
| Secret `llm-config` 或扩展现有 Secret | LLM_API_KEY, LLM_API_URL |
| ConfigMap `runbooks` | Runbook Markdown 文件 |
| Deployment 环境变量注入 | LLM/Diagnosis 相关配置 |
| K8s ServiceAccount | ChatOps 写操作专用，最小 RBAC |

### 14.3 当前 Docker Compose 服务清单

| 服务 | 作用 | 端口 |
|---|---|---|
| `redis` | 缓存、限流、Pub/Sub、告警状态 | 6379 |
| `mysql` | 用户、分组、规则、渠道、告警历史 | 3306 |
| `jaeger` | Trace 查询与 OTLP 接收 | 16686, 4317, 4318 |
| `server-probe` | 监控探针 | 9090 |
| `prometheus` | 指标抓取、短期存储、规则计算 | 9091 |
| `victoriametrics` | 长期指标存储 | 8428 |
| `kafka` | 事件总线 (KRaft) | 19092 |
| `alert-service` | 告警事件消费 | 8081 |
| `alertmanager` | 告警路由和 Webhook | 9093 |
| `grafana` | 指标大盘 | 3000 |
| `elasticsearch` | 日志存储 | 9200 |
| `kibana` | 日志查询 | 5601 |
| `fluent-bit` | Docker 日志采集 | — |
| `server-web` | API、WebSocket、前端；AI 引擎为二改新增模块 | 8080 |

---

## 15. 测试方案

### 15.1 单元测试

| 模块 | 测试重点 | Mock 方式 |
|---|---|---|
| NLU Pipeline | 意图分类准确性、实体抽取完整性、规则匹配覆盖 | Mock LLM Client |
| Tool Registry | 未注册工具拒绝、参数校验、超时、权限检查 | Mock Tool 实现 |
| Prometheus Tool | 查询模板、时间范围限制、空结果、超时 | httptest.Server |
| Alert Tool | 活跃告警、事件流、历史告警 | Mock Redis/MySQL |
| Runbook Retriever | 关键词命中、Top N 排序、空结果、多 Runbook 匹配 | 内存 Runbook |
| Diagnosis Pipeline | Evidence 组合、LLM 输入脱敏、报告落库 | Mock LLM + Mock Tools |
| Rule Analyzer | 各告警类型规则逻辑、边界值 | 纯函数测试 |
| Action Policy | 白名单、风险等级、RBAC、参数范围 | Mock K8s Client |
| Audit | 成功/失败/拒绝/超时都记录 | Mock DB |
| Evidence Fusion | 置信度计算、证据截断、降级 | 构造 Evidence Bundle |

### 15.2 集成测试

| 场景 | 测试方式 |
|---|---|
| Chat → Tool → 数据源 | httptest + 真实 Redis (本地) + Mock Prometheus |
| Alert Event → Diagnosis | Kafka Consumer + Mock LLM + 真实 Redis |
| Action 审批流程 | HTTP API + 真实 MySQL + Mock K8s |

### 15.3 端到端测试

| 场景 | 验证点 |
|---|---|
| 用户对话查询告警 | 自然语言 → 正确告警数据 |
| 告警触发自动诊断 | firing 事件 → 诊断报告 → WebSocket 推送 |
| 审批执行闭环 | AI 建议 → admin 审批 → 执行 → 审计记录 |

### 15.4 验收标准

1. 当前监控大盘能力不回退
2. 告警 Webhook 链路不回退
3. `server-web` Webhook 不被 LLM 阻塞
4. AI 诊断必须引用真实证据
5. AI 推荐动作默认只进入待审批状态
6. 所有写操作可追溯
7. LLM 不可用时降级为规则分析
8. 测试失败或未执行必须明确说明

---

## 16. 分阶段实施计划

### Phase 1: ChatOps 合并入口

**目标：** 在 server-web 增加 Copilot Chat API，接入只读工具

**改动：**
1. server-web 新增 `copilot/` 包：handler, service, nlu, tool
2. 新增 `POST /api/v1/copilot/chat`
3. 第一版工具：`host.list`, `host.metrics`, `alert.list_active`, `alert.events`, `alert.history`, `prom.query_range`
4. 复用 JWT、RBAC、Redis、限流、日志、Trace
5. 前端增加 Copilot 页面

**不做：** Kubernetes 工具、RAG、诊断、审批

### Phase 2: Tool Registry

**目标：** 替换 switch-case，支持注册/校验/超时/日志

**改动：**
1. 实现 `Tool` 接口和 `ToolRegistry`
2. 将 Phase 1 工具迁移到 Registry
3. 增加参数校验、超时控制、调用日志

**验收：** LLM 无法调用未注册工具，参数错误返回清晰错误

### Phase 3: 告警诊断报告

**目标：** 支持对单条告警生成结构化诊断

**改动：**
1. 新增 `DiagnosisReport` 模型
2. 实现 EvidenceCollector + RuleAnalyzer + LLMSummarizer
3. 新增 `POST /api/v1/diagnosis` 手动触发
4. 前端增加诊断报告页面

**验收：** 报告可持久化、前端可查看、展示证据来源

### Phase 4: Runbook 检索

**目标：** Markdown Runbook + 关键词检索

**改动：**
1. 增加 Runbook Markdown 文件
2. 实现 RAGRetriever
3. 将 Runbook 片段加入诊断上下文

**验收：** HighCPU/CriticalCPU/HighMemory/HighDisk/HostDown 至少有 Runbook，报告中展示命中片段

### Phase 5: 异步诊断 Worker

**目标：** 消费 alert-events，对 firing 告警自动诊断

**改动：**
1. Diagnosis Worker 消费 `alert-events`
2. 去重检查（Redis）
3. WebSocket 推送诊断状态

**验收：** Alertmanager 告警后可异步生成诊断，Webhook 不被 LLM 阻塞

### Phase 6: 动作审批与审计

**目标：** PendingAction + AuditLog + 审批流程

**改动：**
1. 新增 `PendingAction` 和 `AuditLog` 模型
2. ActionAdvisor 生成建议动作
3. 审批 API 和前端页面
4. ActionExecutor 执行白名单动作
5. `operation-events` Kafka Topic

**验收：** 未审批动作不能执行，非 admin 不能审批，成功/失败/拒绝都记录审计

### Phase 7: Kubernetes 深度接入

**目标：** K8s 只读工具 + 谨慎开放写操作

**改动：**
1. 合并 chatops K8s 只读能力到 Tool Registry
2. 新增 `k8s.get_events`, `k8s.get_logs`
3. 谨慎开放 `restart_deployment`, `scale_deployment`
4. 独立 ServiceAccount + 最小 RBAC

**验收：** K8s 只读工具可用于 ChatOps 和诊断，写操作受白名单/审批/审计控制

---

## 17. 简历与面试表达

### 17.1 项目描述（AI 方向）

> CloudOps Copilot：基于 LLM 的云原生智能运维平台。在 Prometheus + Alertmanager + Kafka 监控闭环之上，构建了 AI 驱动的告警诊断引擎：通过自然语言理解识别运维意图，多源证据融合（指标 + 告警 + 历史 + Runbook）生成结构化诊断报告，规则 + LLM 混合推理降低幻觉，Human-in-the-loop 安全框架确保写操作可审批可审计。

### 17.2 AI 技术亮点

1. **NLU 管线设计**：规则前置 + LLM 兜底的两级意图路由，确定性规则零 Token 消耗，LLM 处理模糊输入
2. **多源证据融合**：并行采集 Prometheus 指标、Redis 告警状态、MySQL 历史模式、Runbook 知识，融合为结构化 Evidence Bundle
3. **规则 + LLM 混合推理**：Rule Analyzer 确定性分析前置，提供推理锚点；LLM 负责归纳和建议，降低幻觉风险
4. **Tool Registry 架构**：注册式工具框架，支持 Schema 校验、超时控制、权限检查、调用审计，替代 switch-case
5. **Human-in-the-loop**：AI 只建议不执行，写操作必须审批，全链路审计日志，风险分级策略
6. **降级容错**：LLM 不可用时降级为规则分析，工具超时时跳过，保证系统可用性

### 17.3 面试讲解主线

```text
1. 问题：传统运维依赖人工经验，告警排查效率低
   │
2. 方案：在监控闭环上构建 AI 诊断引擎
   │
3. 核心设计：
   │  ├── NLU: 规则+LLM 两级路由 → 意图识别
   │  ├── 证据融合: 并行采集多源数据 → Evidence Bundle
   │  ├── 混合推理: Rule Analyzer + LLM → 诊断报告
   │  ├── 安全执行: Tool Registry + HITL → 可控操作
   │  └── 降级容错: LLM 失败 → 规则分析 → 原始证据
   │
4. 技术细节：
   │  ├── Prompt 工程: System Prompt + Evidence Injection + 输出约束
   │  ├── Token 管理: 证据优先级截断 + 查询缓存 + 规则前置
   │  └── 性能优化: 异步诊断 + 并行工具调用 + WebSocket 推送
   │
5. 工程落地：
      ├── Go + Gin + Redis + MySQL + Kafka + Prometheus
      ├── Docker Compose + Kubernetes + Helm
      └── 单元测试 + 集成测试 + 端到端测试
```

### 17.4 可量化的成果表达

以下数字建议在实现和压测后替换成真实数据，简历中不要直接保留占位符：

- 实现 **8+ 个** 只读工具和 **2 个** 审批型写操作工具的 Tool Registry。
- 告警诊断从人工排查 **30 分钟级** 缩短到 AI 辅助 **分钟级** 生成初步诊断。
- 通过规则前置、工具缓存和证据截断减少不必要的 LLM 调用。
- 证据融合覆盖指标、告警、历史、Runbook、K8s、日志等多类数据源。
- Human-in-the-loop 确保写操作全部经过审批和审计。

---

## 18. 不做清单

1. 不重写 server-monitor 架构
2. 不把所有服务强行合成单体
3. 不删除现有 Prometheus/VictoriaMetrics/Alertmanager/Kafka/Redis/MySQL 链路
4. 不引入 LangChain、复杂 Agent 框架或向量数据库（Phase 1）
5. 不让 LLM 直接执行写操作
6. 不开放删除 Namespace/PVC/Secret 等高风险动作
7. 不把高频指标写入 MySQL
8. 不在 Webhook 同步请求中阻塞等待 LLM
9. 不绕过现有认证/RBAC/限流/日志/Trace
10. 不将 ChatOps 作为独立微服务部署（嵌入 server-web）

---

## 19. 附录：当前代码实况速查

### A.1 server-probe 指标

| Collector | 指标名 |
|---|---|
| CPU | `server_monitor_cpu_usage_percent` |
| Memory | `server_monitor_memory_usage_percent`, `server_monitor_memory_total_bytes`, `server_monitor_memory_available_bytes` |
| Disk | `server_monitor_disk_usage_percent`, `server_monitor_disk_total_bytes`, `server_monitor_disk_free_bytes`, `server_monitor_disk_read_bytes_total`, `server_monitor_disk_write_bytes_total` |
| Network | `server_monitor_network_recv_bytes_total`, `server_monitor_network_sent_bytes_total` |
| Load | `server_monitor_load1`, `server_monitor_load5`, `server_monitor_load15` |
| Process | `server_monitor_process_count`, `server_monitor_uptime_seconds` |

### A.2 server-web 自身指标

```text
http_requests_total                    Counter, HTTP 请求计数
http_request_duration_seconds          Histogram, HTTP 请求延迟
websocket_connections_active           Gauge, 当前 WebSocket 连接数
server_web_kafka_alert_events_total    Counter, Kafka 告警事件发送计数
```

### A.3 alert-service 指标

```text
alert_service_kafka_messages_total
alert_service_alert_events_total
alert_service_kafka_ready
```

### A.4 Redis Key 汇总

| Key | 类型 | 使用者 |
|---|---|---|
| `hosts:list` | String | server-web |
| `dashboard:overview` | String | server-web |
| `alert:active` | Hash | server-web, alert-service, ChatOps |
| `alert:events` | Stream | server-web, ChatOps |
| `alert:event:dedupe:*` | String | server-web |
| `alert:channel` | Pub/Sub | server-web → WebSocket |
| `alert:stats` | Hash | alert-service, ChatOps |
| `alert:dedup:*` | String | alert-service |
| `ratelimit:<ip>:<path>` | Sorted Set | server-web |
| `chat:session:<id>` | List | ChatOps (新增) |
| `diagnosis:task:<id>` | Hash | ChatOps (新增) |

### A.5 Kafka Topic 汇总

| Topic | 生产者 | 消费者 |
|---|---|---|
| `alert-events` | server-web | alert-service, Diagnosis Worker (新增) |
| `operation-events` | 当前已初始化；后续 ChatOps ActionExecutor 生产 | 审计模块 (新增) |
| `diagnosis-events` | 可选新增 | WebSocket 推送 / 审计模块 |
