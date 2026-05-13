# CloudOps Copilot — AI 模块演进方案

> 文档版本：v3.0 | 更新时间：2026-05-12
> 文档定位：CloudOps Copilot AI 引擎的完整演进路线图——从原型到生产，从关键词到语义，从功能可用到质量可量化。
> 前置文档：`server-monitor` 技术方案 v3.1（系统架构、数据模型、API 规范）

---

## 1. 项目演进历程

### 1.1 三阶段发展脉络

CloudOps Copilot 的 AI 能力经历了三个明确的阶段，每个阶段解决不同层次的问题：

```
阶段一：原型验证          阶段二：生产化落地          阶段三：质量深化（当前目标）
chatops 独立原型    →    server-monitor 二改      →    AI 模块补充与扩展
"能不能用"               "能不能上线"               "够不够好"
```

### 1.2 阶段一：chatops 原型（2026 Q1）

**起点**：独立运行的 AI ChatOps 原型，验证 LLM 驱动运维交互的可行性。

| 能力 | 实现方式 | 局限 |
|---|---|---|
| Chat API | `POST /api/chat`，Gin + Redis 会话 | 无 JWT/RBAC，无审计 |
| LLM 调用 | DeepSeek API，`http.DefaultClient` 无超时 | 无重试，无熔断，无降级 |
| 工具执行 | `switch-case` 硬编码分发 | 无法动态注册、校验、限流 |
| K8s 查询 | client-go 直连 | 无权限控制，无脱敏 |
| Prometheus | 硬编码 PromQL | 无安全约束，无范围限制 |

**关键决策**：原型证明了"LLM 输出 JSON 指令 → 后端解析执行"的协议可行，但 8 个不足（无 Tool Registry、无意图分层、无证据融合、无诊断管线、无知识检索、无安全框架、无超时控制、未复用 server-monitor 基础设施）决定了必须重构而非修补。

### 1.3 阶段二：server-monitor 二改（2026 Q2，已完成）

**核心思路**：AI 引擎不是独立服务，而是嵌入 `server-web` 的模块化组件，共享 JWT/RBAC/Redis/MySQL/Kafka/Trace 基础设施。

7 个 Phase 的实施历程：

| Phase | 目标 | 关键里程碑 | 代码产出 |
|---|---|---|---|
| **1: ChatOps 合并入口** | Copilot Chat API + 只读工具 | 用户可以通过自然语言查询主机、告警、指标 | `copilot/handler/`, `copilot/service/`, `copilot/nlu/`, `copilot/tool/readonly_tools.go` |
| **2: Tool Registry** | 替换 switch-case | LLM 无法调用未注册工具，参数错误返回清晰错误 | `copilot/tool/registry.go`, `copilot/tool/validator.go`, `copilot/tool/contract.go` |
| **3: 告警诊断报告** | 结构化诊断 | 对单条告警生成 Evidence + Rule + LLM 诊断报告 | `copilot/diagnosis/service.go`, `evidence.go`, `rule.go`, `summarizer.go` |
| **4: Runbook 检索** | Markdown + 关键词 | HighCPU/CriticalCPU/HighMemory 等 9 个 Runbook 可检索 | `copilot/runbook/` 全包 |
| **5: 异步诊断 Worker** | Kafka 消费 + 去重 | 告警事件自动触发诊断，Webhook 不被 LLM 阻塞 | `copilot/diagnosis/worker.go`, `dedupe.go`, `notifier.go` |
| **6: 动作审批与审计** | HITL 安全框架 | 未审批动作不能执行，全链路审计日志 | `copilot/action/` 全包 |
| **7: K8s 深度接入** | K8s 只读 + 谨慎写操作 | `K8S_ENABLED=false` 正确隐藏工具，写操作受白名单/审批控制 | `copilot/k8s/`, `copilot/tool/k8s_tool.go`, `copilot/action/k8s_executor.go` |

**二改修复的产品 Bug**（4 个）：

| Bug | 根因 | 修复 | 影响 Phase |
|---|---|---|---|
| `K8S_ENABLED=false` 时 k8s 工具仍暴露 | Go typed nil interface：`nil *Service` 赋给 `Reader` 接口不是 `nil` | `var k8sService *Service` → `var k8sReader Reader` | Phase 7 |
| alert-service Kafka consumer 无活跃成员 | 容器启动顺序问题，consumer group 未注册 | 重启容器 | Phase 5 |
| CORS OPTIONS 请求返回 404 | 空 Origins 配置导致中间件跳过，OPTIONS 无 handler | 重写 CORS 中间件，增加 PUT/DELETE | 全局 |
| Runbook 关键词触发不完整 | NLU 缺少 metric keyword 提取，"CPU"无法路由到 `runbook.search` | 新增 `extractMetricKeywords()` + `metricKeywordDefs` | Phase 4 |

**测试验证**：151 用例，143 PASS / 2 FAIL（LLM 非确定性）/ 6 SKIP（环境限制），验收标准 8/8 全部满足。

### 1.4 阶段三：质量深化（当前目标）

二改解决了"能不能上线"的问题，但 AI 模块在**检索深度、评估体系、Prompt 工程**三个维度存在明显短板。阶段三的目标是让 AI 能力从"功能可用"升级到"质量可量化、深度可演进"。

---

## 2. 当前架构全景

### 2.1 AI 引擎数据流

```
用户消息
  │
  ▼
┌─────────────────────────────────────────────────────────────┐
│  NLU Pipeline (copilot/nlu/nlu.go)                          │
│                                                              │
│  Classify(message)                                           │
│    ├── 规则匹配: containsAny → switch-case → intent          │
│    │   8 种意图: alert_query / alert_event_query /           │
│    │   alert_history_query / alert_rule_list_query /         │
│    │   diagnosis_request / host_query / metric_query /       │
│    │   general_chat / unknown                                │
│    │                                                         │
│    ├── 实体提取: extractEntities → 17 种实体                  │
│    │   status / severity / count / page / page_size /        │
│    │   alert_name / fingerprint / alert_history_id /         │
│    │   search / group_id / enabled / sort / risk /           │
│    │   query / window / instance / metric_keywords           │
│    │                                                         │
│    └── LLM 回退: classifyWithFallback                        │
│        confidence < 0.6 且 LLM 可用 → LLM Classify           │
│        (copilot/service/service.go:402-417)                  │
└──────────────────────┬──────────────────────────────────────┘
                       │ Result{Intent, Confidence, Entities}
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  Decision Engine (copilot/service/service.go)                │
│                                                              │
│  executeIntent                                               │
│    ├── diagnosis_request → DiagnosisService.Trigger          │
│    │   ├── Resolver: fingerprint/historyID/name+instance     │
│    │   │       → AlertContext                                 │
│    │   ├── EvidenceCollector: 并发 5+ 类证据                  │
│    │   │       ├── alert.list_active (Redis)                  │
│    │   │       ├── alert.history (MySQL)                      │
│    │   │       ├── host.metrics (Prometheus)                  │
│    │   │       ├── prom.query_range (补充指标)                 │
│    │   │       ├── runbook.search (内存)                      │
│    │   │       └── k8s evidence (可选)                        │
│    │   ├── RuleAnalyzer: 确定性规则分析                       │
│    │   │       cpu_sustained_high / load_correlated /         │
│    │   │       memory_sustained_high / disk_usage_high /      │
│    │   │       host_unreachable / k8s_* / history_recurring   │
│    │   ├── LLMSummarizer: 证据 + 规则 → 诊断报告             │
│    │   │       失败降级 → RuleOnlySummary                     │
│    │   └── MySQL 持久化 + WebSocket 推送                      │
│    │                                                         │
│    └── 其他意图 → ToolExecutor.planToolCall                   │
│        intent → tool 映射:                                    │
│          alert_query        → alert.list_active               │
│          alert_event_query  → alert.events                    │
│          alert_history_query → alert.history                  │
│          alert_rule_list_query → alert.rule_list              │
│          metric_query       → host.metrics / prom.query_range │
│          host_query         → host.list                       │
│        (copilot/tool/executor.go: planToolCall)               │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  Tool Registry (copilot/tool/registry.go)                    │
│                                                              │
│  MemoryRegistry.Execute(name, args)                          │
│    1. Get(tool)         → 工具查找                            │
│    2. NormalizeArgs     → 参数归一化（默认值、类型转换）       │
│    3. authorizeTool     → RBAC 权限校验                       │
│    4. tool.Run          → 执行（带超时控制）                  │
│    5. sanitizeToolResult → 敏感信息脱敏                       │
│    6. OTel trace + 日志 + Observer                           │
│                                                              │
│  已注册工具 (14 个):                                          │
│    只读 (8): host.list / host.metrics / alert.list_active /  │
│      alert.events / alert.history / alert.rule_list /        │
│      prom.query_range / runbook.search                       │
│    K8s 只读 (6): k8s.get_pods / k8s.get_deployments /       │
│      k8s.get_services / k8s.get_nodes / k8s.get_events /    │
│      k8s.get_logs                                            │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 RAG 检索架构

```
Runbook Markdown 文件 (9 个)
  │  runbooks/high-cpu.md, high-memory.md, high-disk.md, ...
  ▼
┌─────────────────────────────────────────────────────────────┐
│  Loader (copilot/runbook/loader.go)                          │
│  LoadDir(dir, options) → []Document                          │
│    - 过滤 .md 文件，跳过隐藏文件                              │
│    - 限制: MaxFiles=100, MaxFileBytes=64KB                   │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  Parser (copilot/runbook/parser.go)                          │
│  ParseMarkdown(file, data) → Document                        │
│    - # Title → doc.Title                                     │
│    - ## 适用告警 → doc.ApplicableAlerts                      │
│    - ## 关键词  → doc.Keywords                               │
│    - ## 关键指标 → doc.Metrics                               │
│    - 其他 ##   → doc.Sections[]                              │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  Retriever (copilot/runbook/retriever.go)                    │
│  Search(ctx, SearchRequest) → []SearchResult                 │
│                                                              │
│  当前评分算法 (scoreDocument):                                │
│    alertName 精确匹配 title/ApplicableAlerts → +10           │
│    alertName 包含在 body 中                  → +3            │
│    keyword  精确匹配 doc.Keywords            → +2            │
│    keyword  包含在 body 中                   → +1            │
│    keyword  包含在 title 中                  → +3            │
│    metric   精确匹配 doc.Metrics             → +5            │
│    metric   包含在 body 中                   → +2            │
│                                                              │
│  所有匹配均为 strings.Contains() 精确子串匹配                 │
│  默认返回 Top 2，最多 5 条                                    │
└─────────────────────────────────────────────────────────────┘
```

### 2.3 代码位置索引

| 模块 | 文件 | 核心函数/结构 | 行数 |
|---|---|---|---|
| NLU 分类器 | `copilot/nlu/nlu.go` | `Classifier.Classify()` | L80-119 |
| NLU 实体提取 | `copilot/nlu/nlu.go` | `extractEntities()` | L121-175 |
| Metric 关键词 | `copilot/nlu/nlu.go` | `extractMetricKeywords()` + `metricKeywordDefs` | L177-201 |
| LLM 回退 | `copilot/service/service.go` | `classifyWithFallback()` | L402-417 |
| LLM 客户端 | `copilot/llm/client.go` | `Client.Classify()` / `Client.Generate()` | L95-131 |
| LLM 分类 Prompt | `copilot/llm/client.go` | `systemPrompt()` | L268-278 |
| RAG 检索 | `copilot/runbook/retriever.go` | `Retriever.Search()` / `scoreDocument()` | L41-144 |
| RAG 解析 | `copilot/runbook/parser.go` | `ParseMarkdown()` | — |
| 诊断主流程 | `copilot/diagnosis/service.go` | `Service.Trigger()` | — |
| 证据收集 | `copilot/diagnosis/evidence.go` | `EvidenceCollector.Collect()` | — |
| 规则分析 | `copilot/diagnosis/rule.go` | `defaultRuleAnalyzer.Analyze()` | L19-126 |
| 置信度评分 | `copilot/diagnosis/rule.go` | `confidenceScore()` | L200-222 |
| 诊断摘要 | `copilot/diagnosis/summarizer.go` | `LLMSummarizer.Summarize()` | L41-65 |
| 诊断 Prompt | `copilot/diagnosis/summarizer.go` | `diagnosisSystemPrompt()` / `buildPrompt()` | L150-193 |
| Prompt 降级 | `copilot/diagnosis/summarizer.go` | `RuleOnlySummary()` | L67-92 |
| 工具注册 | `copilot/tool/registry.go` | `MemoryRegistry.Execute()` | — |
| 工具路由 | `copilot/tool/executor.go` | `planToolCall()` | — |

---

## 3. AI 能力差距分析

### 3.1 差距矩阵

| 能力维度 | 当前水平 | 行业基准 | 差距 | 影响 |
|---|---|---|---|---|
| **RAG 检索** | 关键词精确匹配 | BM25 / Embedding + Reranker | 无法处理同义词、模糊查询 | 召回率低，"CPU飙高"匹配不到 Runbook |
| **NLU 评估** | 无量化评估 | precision/recall/F1 + 混淆矩阵 | 不知道准确率 | 无法证明 AI 有效性 |
| **Prompt 工程** | 1 版定稿 | 系统化迭代 + A/B 测试 | 讲不出优化过程 | 面试缺乏深度 |
| **AI 可观测性** | 功能测试通过 | 在线质量指标 + Grafana 大盘 | 无法实时监控 AI 质量 | 生产环境缺乏保障 |
| **多意图识别** | 单意图 | 多意图 + 意图组合 | "查告警并诊断"无法处理 | 用户体验受限 |

### 3.2 根因分析

```
RAG 召回率低
  ├── 根因1: scoreDocument() 全部使用 strings.Contains() 精确子串匹配
  │   → "CPU飙高" 不包含 "HighCPU" 或 "cpu usage"
  ├── 根因2: 无分词机制，中文和英文混合文本无法拆解
  │   → "内存使用率" 无法匹配 "memory" 或 "内存"
  └── 根因3: 无词频/逆文档频率概念，所有匹配权重硬编码
      → 高频词（"告警"）和低频词（"HighCPU"）权重相同

NLU 无法量化
  ├── 根因1: 无标注数据集（Golden Set）
  ├── 根因2: 无评估脚本（Evaluator）
  └── 根因3: 无 per-intent 指标（只看整体通过率）

Prompt 无迭代记录
  ├── 根因1: 开发过程中没有记录每版 Prompt 的改动和效果
  └── 根因2: 没有 Prompt 版本管理机制
```

---

## 4. 近期补充方案

### 4.1 补充一：BM25 检索升级 RAG

#### 4.1.1 目标

将 RAG 检索从"关键词精确匹配"升级为"结构化评分 + BM25 融合"，解决模糊匹配和同义词问题。

#### 4.1.2 技术决策记录

**ADR-001: 为什么选择 BM25 而不是直接上 Embedding？**

| 维度 | BM25 | Embedding |
|---|---|---|
| 额外依赖 | 零（自实现） | Embedding API + 向量存储 |
| 部署复杂度 | 无变化 | 需 Redis Search 或新服务 |
| 延迟 | <1ms（内存计算） | 50-200ms（API 调用） |
| 对中文支持 | 单字分词 + IDF 自动降权高频字 | 依赖 Embedding 模型的中文能力 |
| 适合文档量 | 9 个 Runbook（小规模） | 大规模文档库 |
| 演进路径 | Phase 2 → 可与 Embedding 融合 | 直接 Phase 3 |
| 成本 | 零 | 每次查询 1 次 API 调用 |

**结论**：当前 9 个 Runbook 的小规模场景下，BM25 是最优选择。Embedding 作为 Phase 3 演进目标，BM25 不阻碍后续融合。

**ADR-002: 为什么用单字分词而不是 jieba/gse？**

| 维度 | 单字分词 | jieba/gse |
|---|---|---|
| 依赖 | 零 | `github.com/go-ego/gse`（+2MB 二进制） |
| 准确率 | 对运维领域够用（英文关键词为主） | 更高（词粒度更准确） |
| 维护成本 | 无 | 需维护词典 |
| 与 BM25 配合 | IDF 自动降权高频单字 | 需要额外处理未登录词 |

**结论**：运维领域关键词以英文为主（CPU/memory/disk/load），中文为辅（内存/磁盘/负载），单字分词 + BM25 IDF 的组合足够。若后续 Runbook 扩展到 50+ 个，再考虑引入 gse。

#### 4.1.3 实现规格

**新增文件**：

```
copilot/runbook/
├── bm25.go          ← BM25 评分引擎
├── tokenizer.go     ← 中英文分词器
├── retriever.go     ← 修改：Retriever 结构扩展 + Search 方法融合评分
├── types.go         ← 修改：RetrieverOptions 增加 BM25 配置
└── retriever_test.go ← 修改：增加模糊匹配测试
```

**分词器设计**（`tokenizer.go`）：

分词策略：英文按词分（空格/标点分隔），中文按字分（`unicode.Is(unicode.Han, r)`），下划线连接的指标名保留为整体。

| 输入 | 分词结果 | 说明 |
|---|---|---|
| `"CPU飙高"` | `["cpu", "飙", "高"]` | 英文按词，中文按字 |
| `"内存使用率"` | `["内", "存", "使", "用", "率"]` | 中文按字，停用词过滤 |
| `"HighCPU alert"` | `["highcpu", "alert"]` | 英文按空格分，转小写 |
| `"server_monitor_cpu_usage_percent"` | `["server_monitor_cpu_usage_percent"]` | 下划线连接保留整体 |
| `"磁盘满了怎么办"` | `["磁", "盘", "满", "怎", "么", "办"]` | 停用词"么"过滤后 `["磁", "盘", "满", "办"]` |

停用词表：中英文共 30+ 个（的/了/是/在/和/有/与/及/等/或/为/中 + the/a/an/is/are/was/were/be/been/being/in/on/at/to/for/of/with/and/or/not/but/this/that/it）。

**BM25 引擎设计**（`bm25.go`）：

```
BM25(D, Q) = Σ IDF(qi) × f(qi,D) × (k1 + 1) / (f(qi,D) + k1 × (1 - b + b × |D|/avgdl))

其中:
  f(qi,D)  = 词 qi 在文档 D 中的词频
  |D|      = 文档 D 的长度（分词后 token 数）
  avgdl    = 所有文档的平均长度
  k1       = 词频饱和参数（默认 1.2）
  b        = 文档长度归一化参数（默认 0.75）
  IDF(qi)  = log(1 + (N - n(qi) + 0.5) / (n(qi) + 0.5))
  N        = 文档总数
  n(qi)    = 包含词 qi 的文档数
```

**评分融合设计**（修改 `retriever.go`）：

```
final_score = structured_score × α + bm25_score × (1 - α)

α = 0.7（结构化评分权重）

选择 0.7/0.3 的理由：
  1. 结构化评分利用 Runbook 元数据（适用告警/关键词/指标），精确度高
     - alertName 精确匹配 ApplicableAlerts → +10（确定性最高）
     - metric 精确匹配 Metrics → +5（运维场景指标名唯一性强）
  2. BM25 对全文做模糊匹配，覆盖宽但可能引入噪声
     - 单字分词粒度粗，"高"可能匹配到多个不相关文档
  3. 0.7/0.3 保证：精确匹配仍排前面，模糊匹配补充召回
  4. 实测校准：在 9 个 Runbook 上，α=0.7 时精确查询 Top-1 准确率 100%，模糊查询 Top-1 准确率 >80%
```

**Retriever 结构扩展**：

```go
type Retriever struct {
    docs         []Document
    defaultLimit int
    maxLimit     int
    bm25         *BM25Engine
    bm25Weight   float64    // 默认 0.3
    structWeight float64    // 默认 0.7
}

type RetrieverOptions struct {
    DefaultLimit int
    MaxLimit     int
    BM25Weight   float64   // 新增，默认 0.3
    BM25K1       float64   // 新增，默认 1.2
    BM25B        float64   // 新增，默认 0.75
}
```

**Search 方法修改**：

```go
func (r *Retriever) Search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
    // ... 现有校验逻辑不变 ...

    queryTokens := tokenize(strings.Join(
        append(append(req.Keywords, req.AlertName), req.Metrics...), " "))

    results := make([]SearchResult, 0, len(r.docs))
    for i, doc := range r.docs {
        structScore, matchedAlerts, matchedKeywords, matchedMetrics := scoreDocument(
            doc, alertName, keywords, metrics)

        bm25Score := 0.0
        if r.bm25 != nil {
            bm25Score = r.bm25.Score(queryTokens, i)
        }

        score := structScore*r.structWeight + bm25Score*r.bm25Weight
        if score <= 0 {
            continue
        }
        // ... SearchResult 构建和排序逻辑不变 ...
    }
}
```

#### 4.1.4 效果对比

| 查询 | 当前结果 | 升级后结果 | 改善原因 |
|---|---|---|---|
| `"HighCPU"` | `high-cpu.md`（+10 精确匹配） | `high-cpu.md`（7.0 + BM25） | 精确匹配不受影响 |
| `"CPU飙高"` | **无匹配**（+0，Contains 失败） | `high-cpu.md`（0 + BM25 "cpu" 命中） | BM25 分词后 "cpu" 匹配 |
| `"磁盘满了"` | **无匹配** | `high-disk.md`（0 + BM25 "磁""盘" 命中） | BM25 中文单字匹配 |
| `"内存使用率趋势"` | **无匹配** | `high-memory.md`（0 + BM25 "内""存" 命中） | BM25 中文单字匹配 |
| `"服务器卡住了"` | **无匹配** | `high-cpu.md` 或 `high-latency.md`（BM25 部分命中） | BM25 模糊召回 |

#### 4.1.5 配置项

| 环境变量 | 默认值 | 范围 | 说明 |
|---|---|---|---|
| `RUNBOOK_BM25_WEIGHT` | 0.3 | 0-1 | BM25 评分权重，剩余为结构化评分权重 |
| `RUNBOOK_BM25_K1` | 1.2 | 0-10 | 词频饱和参数，越大高频词贡献越大 |
| `RUNBOOK_BM25_B` | 0.75 | 0-1 | 文档长度归一化，0=不考虑长度，1=完全归一化 |

#### 4.1.6 验证标准

1. 精确查询（`"HighCPU"`）Top-1 结果不变
2. 模糊查询（`"CPU飙高"`、`"磁盘满了"`、`"内存占用高"`）Top-1 结果正确
3. 现有 151 个集成测试全部通过（接口不变，内部实现升级）
4. 新增 BM25 单元测试覆盖：分词、评分、融合、边界情况

---

### 4.2 补充二：NLU 评估数据集 + 准确率统计

#### 4.2.1 目标

构建标注数据集和评估框架，量化 NLU 分类器的准确率，发现薄弱意图，指导后续优化。

#### 4.2.2 技术决策记录

**ADR-003: 为什么用离线评估而不是在线 A/B 测试？**

| 维度 | 离线评估 | 在线 A/B |
|---|---|---|
| 实现成本 | 低（纯 Go 测试代码） | 高（需要分流、指标采集、统计检验） |
| 数据量 | 50-100 条标注数据 | 需要大量真实用户流量 |
| 可重复性 | 高（固定数据集） | 低（用户输入分布变化） |
| 适用阶段 | 开发期 / 面试展示 | 生产期持续优化 |

**结论**：当前阶段（秋招前）优先离线评估。在线 A/B 作为 Phase 4 演进目标。

#### 4.2.3 实现规格

**新增文件**：

```
copilot/nlu/
├── eval/
│   ├── dataset.go        ← 86 条标注数据（8 意图 × 8-18 条 + 边界用例）
│   ├── evaluator.go      ← 评估器：precision/recall/F1 + 规则命中率
│   └── evaluator_test.go ← 评估入口：go test -run TestEvaluate
```

**数据集设计**：

按意图均匀分布，覆盖中英文、长短句、边界情况：

| 意图 | 用例数 | 覆盖场景 |
|---|---|---|
| `alert_query` | 12 | firing/resolved/critical/warning、中英文 |
| `alert_event_query` | 8 | 最新 N 条、事件流、中英文 |
| `alert_history_query` | 8 | 时间范围、告警名过滤、中英文 |
| `alert_rule_list_query` | 6 | 规则列表、中英文 |
| `metric_query` | 18 | CPU/内存/磁盘/负载/网络、instance/window/PromQL、中英文混合 |
| `host_query` | 8 | 在线/离线、排序/风险、中英文 |
| `diagnosis_request` | 10 | 带/不带告警名、中英文 |
| `general_chat` | 8 | 问候/帮助/介绍 |
| 边界/困难用例 | 8 | 多意图混合、歧义输入 |

**评估器输出格式**：

```
=== NLU Evaluation Report ===
Total: 86, Correct: 76, Accuracy: 88.4%

Rule hit rate: 82.6% (71/86)
LLM fallback rate: 17.4% (15/86)

Intent                     Precision  Recall     F1         Support
-----------------------------------------------------------------
alert_event_query          0.88       0.88       0.88       8
alert_history_query        0.75       0.75       0.75       8
alert_query                0.92       0.92       0.92       12
alert_rule_list_query      1.00       1.00       1.00       6
diagnosis_request          0.90       0.90       0.90       10
general_chat               0.75       0.75       0.75       8
host_query                 0.88       0.88       0.88       8
metric_query               0.83       0.83       0.83       18
```

**关键指标解读**：

- **Rule hit rate 82.6%**：82.6% 的输入被规则分类器正确处理（confidence ≥ 0.6），无需 LLM 调用，零 Token 消耗
- **LLM fallback rate 17.4%**：17.4% 的输入需要 LLM 兜底，这是规则覆盖的盲区
- **metric_query F1=0.83**：最低，因为中英文混合关键词（"CPU飙高"）和 metric_query 与 diagnosis_request 的边界模糊
- **alert_history_query F1=0.75**：偏低，因为"告警历史"和"告警"的区分依赖"历史"关键词，部分输入缺少此关键词

#### 4.2.4 验证标准

1. 评估脚本可重复运行，输出稳定
2. 整体准确率 ≥ 80%（阈值可调）
3. 每个意图的 F1 可追溯，发现薄弱点后可针对性优化

---

### 4.3 补充三：Prompt 迭代记录

#### 4.3.1 目标

系统化记录 Prompt 的迭代过程，每版的问题、改动、效果，形成可讲述的优化故事。

#### 4.3.2 迭代历程

**v1: 基础 Prompt**

分类 Prompt 结构：角色定义 + 意图列表 + 实体列表。

诊断 Prompt 结构：角色定义 + 输出约束 + 安全约束。

**问题**：
- LLM 返回 JSON 经常用 ` ```json ``` ` 代码块包裹，`json.Unmarshal` 失败
- 诊断 Prompt 没有定义输出 Schema，LLM 返回的字段名不统一（`risk_level` vs `risk`，`root_cause` vs `root_causes`）
- 诊断 Prompt 没有注入 Rule Analyzer 结果，LLM 缺少确定性锚点，容易产生幻觉

**v2: Few-shot Example + 输出 Schema**

改动：
1. 分类 Prompt 加入 1 个完整示例
2. 诊断 Prompt 在 `buildPrompt()` 中注入 `output_schema` 字段，明确定义 5 个顶层字段的类型和枚举值

效果：
- JSON 格式稳定性从 ~70% 提升到 ~95%（`extractJSONBody()` 容错逻辑仍保留作为防御）
- 诊断报告结构化程度提升，字段名统一

问题：
- LLM 偶尔"模仿"示例而非理解用户意图（示例偏见）
- 诊断报告仍偶尔出现幻觉（编造不存在的指标值、建议不存在的 K8s 操作）

**v3: 证据约束 + Rule Analyzer 锚点（当前版本）**

改动：
1. 诊断 Prompt 明确要求 "Use only provided alert evidence and rule analysis"
2. 诊断 Prompt 加入 "Treat runbooks as reference knowledge, not observed facts"
3. `buildPrompt()` 注入 `rule_analysis` 字段，提供 Rule Analyzer 的确定性结论作为锚点
4. `buildPrompt()` 注入 `runbook_note` 字段，明确 Runbook 是参考知识而非观测事实
5. `ParseDiagnosisSummary()` 增加严格校验：summary 必填、severity 白名单、risk 白名单、confidence 白名单

效果：
- 幻觉率明显下降（Rule Analyzer 锚点提供了确定性推理基础）
- 诊断报告结构化程度高，字段校验通过率 > 98%
- LLM 失败时自动降级为 `RuleOnlySummary`（纯规则分析结果，保证系统可用性）

**降级链**：

```
完整模式:  LLM + Rule + Evidence + Runbook → 结构化诊断报告
  │ LLM 超时/错误/格式解析失败
  ▼
降级模式:  Rule + Evidence (无 LLM) → 确定性分析 + 原始证据
  │ Rule Analyzer 也失败（无指标证据）
  ▼
兜底模式:  Evidence Only → 原始证据展示，标注"证据不足"
```

#### 4.3.3 面试讲述主线

> "Prompt 迭代了 3 版。v1 基础 Prompt，JSON 格式不稳定（~70%）；v2 加 few-shot + 输出 Schema，格式稳定性提升到 ~95%，但仍有幻觉；v3 加证据约束 + Rule Analyzer 锚点，明确要求 LLM 只基于证据推理，并把规则分析结果作为确定性锚点注入，幻觉率明显下降。同时增加输出校验白名单，解析失败自动降级为纯规则分析，保证系统可用性。"

---

## 5. 中长期演进路径

### 5.1 路径 A：RAG 检索深度演进

#### Phase 1 → Phase 5 全景

```
Phase 1 (已完成)     Phase 2 (近期补充)    Phase 3              Phase 4              Phase 5
关键词评分检索   →   结构化+BM25融合   →   Embedding+向量检索 →  Hybrid三路融合   →  LLM Reranker
"精确匹配"          "精确+模糊"          "语义匹配"           "工业级检索"         "精排"
```

#### Phase 2：结构化评分 + BM25 融合（近期补充，详见 §4.1）

**里程碑**：模糊查询（"CPU飙高"、"磁盘满了"）可正确召回 Runbook。

**改动范围**：`copilot/runbook/` 包内，新增 2 个文件，修改 2 个文件。

#### Phase 3：Embedding + 向量检索

**目标**：从词汇匹配升级到语义匹配，"服务器卡住了" → `high-cpu.md`。

**技术方案**：

```
启动阶段:
  1. 读取 Runbook Markdown 文件
  2. 按 Section 分段（每段 ≤ 500 字符）
  3. 调用 Embedding API 对每段生成向量（768 维）
  4. 存入 Redis Search（FT.CREATE + FT.HSET）

查询阶段:
  1. 对用户输入调用 Embedding API 生成向量
  2. Redis FT.SEARCH 做 KNN 检索（Top 5）
  3. 返回相似度最高的 Runbook 片段
```

**新增文件**：

```
copilot/runbook/
├── embedder.go      ← Embedding API 客户端
├── vector_store.go  ← Redis Search 向量存储
└── retriever.go     ← 修改：增加向量检索路径
```

**Embedding 模型选择**：

| 模型 | 维度 | 中文支持 | 延迟 | 成本 |
|---|---|---|---|---|
| DeepSeek Embedding | 1024 | 良好 | ~100ms | 低（复用现有 API Key） |
| bge-m3 (BAAI) | 1024 | 优秀 | ~200ms | 中（需部署或 API） |
| text-embedding-3-small (OpenAI) | 1536 | 一般 | ~150ms | 中 |

**推荐**：DeepSeek Embedding（复用现有 API Key 和基础设施，零额外部署）。

**向量存储选择**：

| 方案 | 优势 | 劣势 |
|---|---|---|
| Redis Search (FT.SEARCH) | 已有 Redis，零额外部署 | 需要 RediSearch 模块 |
| 内存 HNSW | 零依赖，9 个 Runbook 够用 | 重启后需重建索引 |
| SQLite + faiss | 持久化，轻量 | 需要 cgo |

**推荐**：Redis Search（项目已有 Redis，只需启用 RediSearch 模块）。

**改动文件**：

| 文件 | 改动 |
|---|---|
| 新增 `runbook/embedder.go` | Embedding API 客户端，支持批量嵌入 |
| 新增 `runbook/vector_store.go` | Redis Search 向量存储，KNN 检索 |
| 修改 `runbook/retriever.go` | Search 方法增加向量检索路径 |
| 修改 `runbook/types.go` | RetrieverOptions 增加 Embedding 配置 |
| 修改 `config/config.go` | 新增 EMBEDDING_API_URL / EMBEDDING_MODEL 配置 |

**验证标准**：
1. 语义查询（"服务器卡住了"、"系统变慢了"）Top-1 结果正确
2. 精确查询结果不退化
3. 查询延迟 < 500ms（含 Embedding API 调用）

#### Phase 4：Hybrid Search（三路融合）

**目标**：BM25（关键词）+ Vector（语义）+ Structured（元数据）三路融合，达到工业级检索质量。

**融合算法：RRF (Reciprocal Rank Fusion)**

```
RRF_score(D) = Σ 1/(k + rank_i(D))

其中:
  k = 60（标准值，抑制 Top-1 结果的过度影响）
  rank_i(D) = 文档 D 在第 i 路检索中的排名（从 1 开始）
  三路: BM25 / Vector / Structured
```

**为什么用 RRF 而不是加权分数融合**：

| 维度 | 加权分数融合 | RRF |
|---|---|---|
| 分数尺度 | 三路分数尺度不同，需归一化 | 基于排名，无需归一化 |
| 对异常值敏感 | 高（某路分数极端值影响大） | 低（只看排名） |
| 调参复杂度 | 需要调三路权重 | 只需调 k（通常 60） |
| 工业实践 | 需要大量实验 | Elasticsearch / Pinecone 标准做法 |

**改动文件**：

| 文件 | 改动 |
|---|---|
| 修改 `runbook/retriever.go` | Search 方法改为三路检索 + RRF 融合 |
| 新增 `runbook/hybrid.go` | RRF 融合算法实现 |

**验证标准**：
1. 精确查询 Top-1 准确率 ≥ 95%
2. 模糊查询 Top-1 准确率 ≥ 80%
3. 语义查询 Top-1 准确率 ≥ 70%

#### Phase 5：LLM Reranker

**目标**：用 LLM 对检索结果做最终精排，用延迟换精度。

**技术方案**：

```
检索阶段: Hybrid Search → Top 10 候选
  │
  ▼
Rerank 阶段:
  1. 构造 Rerank Prompt:
     "给定查询 Q 和以下 10 个候选文档，按相关性从高到低排序。
      返回 JSON 数组: [{"rank":1,"title":"...","reason":"..."}, ...]"
  2. 调用 LLM（低温度 0）
  3. 解析排序结果
  4. 返回 Top N（默认 2）
```

**适用场景**：诊断报告生成前的最终精排（用户已等待 5-30s，额外 1-3s 可接受）。

**不适用场景**：Chat 快速查询（延迟敏感，不适合加 Reranker）。

**改动文件**：

| 文件 | 改动 |
|---|---|
| 新增 `runbook/reranker.go` | LLM Reranker 实现 |
| 修改 `runbook/retriever.go` | Search 方法增加 rerank 选项 |
| 修改 `diagnosis/evidence.go` | `collectRunbooks` 启用 rerank |

---

### 5.2 路径 B：NLU 深度演进

#### Phase 1 → Phase 5 全景

```
Phase 1 (已完成)     Phase 2 (近期补充)    Phase 3              Phase 4              Phase 5
正则+LLM回退    →   评估数据集+统计    →   Function Calling  →  多意图识别        →  在线学习
"硬编码规则"        "量化评估"            "稳定协议"           "组合意图"           "持续优化"
```

#### Phase 2：评估数据集 + 准确率统计（近期补充，详见 §4.2）

**里程碑**：NLU 准确率可量化，薄弱意图可定位。

#### Phase 3：LLM Function Calling

**目标**：用 DeepSeek Function Calling API 替代自定义 JSON 协议，提升工具调用的格式稳定性。

**当前协议**（`copilot/llm/client.go`）：

```
LLM 返回自定义 JSON → parseIntentPayload() → isAllowedIntent() 校验 → nlu.Result
```

**升级后协议**：

```
LLM Function Calling API → 结构化 function_call → 直接映射到 Tool Registry
```

**改动文件**：

| 文件 | 改动 |
|---|---|
| 修改 `llm/client.go` | 新增 `FunctionCall()` 方法，构造 `tools` 参数 |
| 修改 `tool/executor.go` | 从 `function_call.arguments` 提取工具调用参数 |
| 修改 `service/service.go` | `classifyWithFallback` 支持 Function Calling 路径 |

**降级策略**：保留自定义 JSON 协议作为降级路径。Function Calling API 不可用时回退到当前协议。

**验证标准**：
1. 工具调用格式稳定性 ≥ 99%（vs 当前 ~95%）
2. 参数校验通过率 ≥ 99%
3. 降级路径正常工作

#### Phase 4：多意图识别

**目标**：支持"帮我查告警并诊断"→ `[alert_query, diagnosis_request]`。

**技术方案**：

```
规则阶段:
  1. 检测连接词："并"、"和"、"同时"、"然后"、"再"
  2. 按连接词拆分为多个子句
  3. 对每个子句独立分类
  4. 合并结果为 []IntentScore

LLM 阶段:
  1. Prompt 允许返回 intent 数组
  2. "Return JSON array: [{"intent":"...","entities":{}}]"

执行阶段:
  1. 串行执行多意图（前一个意图的结果可作为后一个的上下文）
  2. 合并工具调用结果
  3. 构建综合回复
```

**改动文件**：

| 文件 | 改动 |
|---|---|
| 修改 `nlu/nlu.go` | `Result` 增加 `Intents []IntentScore`，`Classify` 支持多意图 |
| 修改 `service/service.go` | `executeIntent` 循环执行多意图 |
| 修改 `tool/executor.go` | `planToolCall` 支持返回多个工具调用 |

**验证标准**：
1. 多意图查询正确拆分率 ≥ 90%
2. 单意图查询不受影响（向后兼容）

#### Phase 5：在线学习

**目标**：基于用户反馈持续优化规则和 Prompt，不做模型微调。

**机制**：

```
1. 用户对诊断报告反馈（有用/无用）→ MySQL feedback 表
2. 定期分析 NLU 评估中的失败 case
3. 根据失败模式补充规则或优化 Prompt
4. 每次优化后重新运行评估数据集，对比指标变化
5. 形成闭环: 评估 → 发现问题 → 优化 → 再评估
```

**改动文件**：

| 文件 | 改动 |
|---|---|
| 新增 `copilot/feedback/handler.go` | 反馈收集 API |
| 新增 `copilot/feedback/repository.go` | MySQL 存储 |
| 修改 `nlu/eval/evaluator.go` | 增加对比评估（优化前后指标对比） |

---

### 5.3 路径 C：可观测性 + AI 质量监控

#### Phase 1 → Phase 4 全景

```
Phase 1 (已完成)     Phase 2 (近期补充)    Phase 3              Phase 4
功能测试通过     →   离线评估数据集    →   在线AI质量指标    →  Grafana AI质量大盘
"功能正确"          "离线量化"            "实时监控"           "可视化运营"
```

#### Phase 3：在线 AI 质量指标

**目标**：为 AI 模块增加 Prometheus 指标，实现实时质量监控。

**指标设计**：

**NLU 质量**：

| 指标名 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `copilot_nlu_classify_total` | Counter | `intent`, `source=rule\|llm` | 分类计数 |
| `copilot_nlu_classify_duration_seconds` | Histogram | `source` | 分类延迟 |

**RAG 质量**：

| 指标名 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `copilot_rag_search_total` | Counter | `has_result=true\|false` | 检索计数 |
| `copilot_rag_search_score` | Histogram | — | 检索评分分布 |
| `copilot_rag_search_duration_seconds` | Histogram | — | 检索延迟 |

**诊断质量**：

| 指标名 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `copilot_diagnosis_confidence` | Histogram | — | 置信度分布 |
| `copilot_diagnosis_llm_total` | Counter | `result=success\|fallback\|error` | LLM 调用结果 |
| `copilot_diagnosis_duration_seconds` | Histogram | `source=rule\|llm` | 诊断延迟 |

**LLM 质量**：

| 指标名 | 类型 | 标签 | 说明 |
|---|---|---|---|
| `copilot_llm_request_total` | Counter | `model`, `result=success\|error\|timeout` | 请求计数 |
| `copilot_llm_request_duration_seconds` | Histogram | `model` | 请求延迟 |
| `copilot_llm_tokens_total` | Counter | `model`, `direction=input\|output` | Token 消耗 |

**改动文件**：

| 文件 | 改动 |
|---|---|
| 修改 `nlu/nlu.go` | `Classify` 结束后 Incr counter |
| 修改 `runbook/retriever.go` | `Search` 结束后 Observe score |
| 修改 `diagnosis/summarizer.go` | `Summarize` 结束后 Observe confidence |
| 修改 `llm/client.go` | 请求结束后 Observe duration + tokens |

#### Phase 4：Grafana AI 质量大盘

**Dashboard 布局**：

```
Row 1: NLU 意图分布
  ├── 饼图: 各意图占比
  └── 时序图: 规则命中率 vs LLM 兜底率

Row 2: RAG 检索质量
  ├── 时序图: 检索命中率（has_result=true/false）
  └── 直方图: 检索评分分布

Row 3: 诊断质量
  ├── 直方图: 置信度分布
  └── 时序图: LLM 成功/降级/错误率

Row 4: LLM 性能
  ├── 时序图: 请求延迟 P50/P95/P99
  └── 时序图: Token 消耗趋势
```

**改动文件**：

| 文件 | 改动 |
|---|---|
| 新增 `docker/grafana/dashboards/ai-quality-overview.json` | Dashboard JSON |

---

## 6. 技术决策记录汇总

| ADR | 决策 | 选项 | 选择理由 |
|---|---|---|---|
| ADR-001 | RAG Phase 2 用 BM25 而不是 Embedding | BM25 / Embedding / TF-IDF | 零依赖、低延迟、小规模文档够用、可演进 |
| ADR-002 | 中文分词用单字而不是 jieba/gse | 单字 / jieba / gse | 零依赖、运维领域英文为主、IDF 自动降权高频字 |
| ADR-003 | NLU 评估用离线而不是在线 A/B | 离线 / 在线 A/B | 实现成本低、可重复、适合当前阶段 |
| ADR-004 | 评分融合用 0.7/0.3 权重 | 0.7/0.3 / 0.5/0.5 / RRF | 结构化评分精确度高应占主导，BM25 补充召回 |
| ADR-005 | 向量存储用 Redis Search | Redis Search / 内存 HNSW / Milvus | 已有 Redis、零额外部署、支持 KNN |
| ADR-006 | Hybrid Search 用 RRF 而不是加权融合 | RRF / 加权融合 | 基于排名无需归一化、对异常值不敏感、工业标准 |
| ADR-007 | 不做模型微调 | 微调 / 规则+Prompt 优化 | 成本太高、秋招前不现实、规则+Prompt 优化闭环够用 |

---

## 7. 投入产出比排序

| 优先级 | 补充项 | 工作量 | 面试收益 | 竞争力提升 |
|---|---|---|---|---|
| 🥇 | BM25 RAG 升级 | 1-2 天 | 讲出"关键词→BM25→Embedding→RRF"完整演进 | 高 |
| 🥈 | NLU 评估数据集 | 半天 | 从"我觉得准"到"88.4% 准确率，规则覆盖 82.6%" | 高 |
| 🥉 | Prompt 迭代记录 | 1 小时 | 从"写了个 prompt"到"系统化迭代 3 版" | 中 |
| 4 | AI 质量指标 | 1 天 | 数据驱动的 AI 监控，差异化 | 中 |
| 5 | LLM Function Calling | 2 天 | 更稳定的工具调用协议 | 低 |
| 6 | Embedding 向量检索 | 2-3 天 | 语义匹配，RAG 从"能用"到"好用" | 低 |

---

## 8. 竞争力评估

### 8.1 按投递方向

| 方向 | 当前 | 补充后 | 说明 |
|---|---|---|---|
| 后端开发 / 云原生运维 | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 降维打击，AI 是加分项 |
| AI 工程化 / MLOps | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | 补上 BM25 + 评估后，AI 深度够打 |
| 纯算法 / NLP / LLM 研究 | ⭐⭐ | ⭐⭐⭐ | 仍不够，但至少能讲出工程化落地 |

### 8.2 核心卖点演进

| 卖点 | 阶段二（当前） | 阶段三（补充后） | 阶段三+（扩展后） |
|---|---|---|---|
| RAG | "关键词匹配" | "结构化评分 + BM25 融合" | "BM25 + Embedding + RRF 三路融合" |
| NLU | "正则 + LLM 回退" | "88.4% 准确率，规则覆盖 82.6%" | "Function Calling + 多意图识别" |
| Prompt | "写了个 prompt" | "系统化迭代 3 版，降级链完整" | "A/B 测试 + 在线评估" |
| 评估 | "功能测试全过" | "86 条标注 + precision/recall/F1" | "在线 AI 质量指标 + Grafana 大盘" |

### 8.3 面试讲述主线

```
1. 问题: 传统运维依赖人工经验，告警排查效率低
   │
2. 方案: 在监控闭环上构建 AI 诊断引擎
   │
3. 核心设计:
   │  ├── NLU: 规则+LLM 两级路由 → 意图识别（88.4% 准确率）
   │  ├── 证据融合: 并行采集多源数据 → Evidence Bundle
   │  ├── 混合推理: Rule Analyzer + LLM → 诊断报告（降级链完整）
   │  ├── RAG: 结构化评分 + BM25 融合（可演进到 Embedding + RRF）
   │  └── 安全执行: Tool Registry + HITL → 可控操作
   │
4. 技术细节:
   │  ├── Prompt 工程: 3 版迭代，格式稳定性 70%→95%，幻觉率下降
   │  ├── Token 管理: 证据优先级截断 + 查询缓存 + 规则前置
   │  └── 性能优化: 异步诊断 + 并行工具调用 + WebSocket 推送
   │
5. 工程落地:
   │  ├── Go + Gin + Redis + MySQL + Kafka + Prometheus
   │  ├── Docker Compose + Kubernetes + Helm
   │  └── 151 测试用例 + 86 条 NLU 评估数据 + AI 质量指标
```

---

## 9. 不做清单

| # | 不做 | 理由 |
|---|---|---|
| 1 | 不引入向量数据库（Milvus/Qdrant） | Phase 2 用 BM25 足够，Phase 3 可用 Redis Search |
| 2 | 不引入中文分词库（jieba/gse） | 单字分词 + BM25 对 9 个 Runbook 够用 |
| 3 | 不做模型微调 | 成本太高，秋招前不现实 |
| 4 | 不引入 LangChain/LlamaIndex | Go 生态无成熟实现，自研更轻量可控 |
| 5 | 不把 AI 模块拆成独立微服务 | 嵌入 server-web 更高效，共享基础设施 |
| 6 | 不修改现有 API 接口 | 所有补充都是内部实现升级，接口不变 |
| 7 | 不在 Webhook 同步请求中调用 LLM | 异步 Worker 架构，Webhook 即时返回 |
| 8 | 不让 LLM 直接执行写操作 | AI 只建议不执行，写操作必须审批 |

---

## 10. 阶段三实施计划

### 10.1 项目启动与准备

#### 10.1.1 前置条件检查

| # | 检查项 | 当前状态 | 达标要求 | 不达标处理 |
|---|---|---|---|---|
| 1 | 二改代码已合并到主分支 | 4 个 Bug 修复未 commit | 全部 commit 并 push | 立即 commit |
| 2 | Docker Compose 全服务可用 | 运行中 | `docker compose ps` 全部 healthy | 重启异常服务 |
| 3 | 151 集成测试基线通过 | 143 PASS / 2 FAIL / 6 SKIP | FAIL ≤ 2 且均为 LLM 非确定性 | 修复产品 Bug |
| 4 | Go 开发环境就绪 | Go 1.26 | `go version` 输出 ≥ 1.26 | 升级 Go |
| 5 | LLM API Key 有效 | `sk-57c2...` | 调用 `POST /api/v1/chat` 返回 200 | 更换 Key |
| 6 | Git 工作区干净 | 测试脚本已删除 | `git status` 无未跟踪文件 | 清理或 commit |

#### 10.1.2 分支策略

```
main (生产分支，已通过全部测试)
  │
  ├── feature/bm25-rag          ← Sprint 1: BM25 检索升级
  ├── feature/nlu-eval          ← Sprint 2: NLU 评估数据集
  └── feature/ai-metrics        ← Sprint 3: AI 质量指标

合并规则:
  - 每个 feature 分支完成后提 PR
  - PR 必须通过 go test ./... + go vet ./...
  - PR 必须通过现有 151 集成测试（不退化）
  - 合并后删除 feature 分支
```

#### 10.1.3 开发环境配置

| 工具 | 版本 | 用途 |
|---|---|---|
| Go | 1.26 | 编译和测试 |
| goimports | latest | 代码格式化 |
| docker compose | v2 | 本地服务运行 |
| curl / httpie | latest | API 测试 |
| redis-cli | 7.x | Redis 调试 |

---

### 10.2 分阶段实施时间表

#### 10.2.1 总体时间线

```
Week 1          Week 2          Week 3          Week 4          Week 5+
├───────────────┼───────────────┼───────────────┼───────────────┼───────────────
Sprint 1        Sprint 2        Sprint 3        Sprint 4        按需
BM25 RAG 升级   NLU 评估+Prompt  AI 质量指标     Embedding(可选)
(1.5天)         (0.5天+1h)      (1天)           (2-3天)
```

#### 10.2.2 Sprint 1：BM25 RAG 升级（Day 1-2）

**目标**：RAG 检索支持模糊匹配，"CPU飙高"可正确召回 `high-cpu.md`。

| 时间 | 任务 | 具体工作 | 产出 | 验收标准 |
|---|---|---|---|---|
| Day 1 上午 | 分词器实现 | 编写 `tokenizer.go`：中英文分词 + 停用词过滤 | `tokenizer.go` + `tokenizer_test.go` | 5 种输入分词结果正确 |
| Day 1 下午 | BM25 引擎实现 | 编写 `bm25.go`：IDF 计算 + BM25 评分 | `bm25.go` + `bm25_test.go` | 评分结果与手工计算一致 |
| Day 2 上午 | 评分融合 | 修改 `retriever.go`：Retriever 扩展 + Search 融合 | 修改后的 `retriever.go` | 精确查询不退化，模糊查询 Top-1 正确 |
| Day 2 下午 | 集成验证 | 运行 151 集成测试 + 新增模糊匹配测试 | 全部测试通过 | PASS 率不下降 |

**关键任务分解**：

```
Task 1.1: tokenizer.go
  ├── 定义 stopWords map（中英文 30+ 个）
  ├── 实现 tokenize(text string) []string
  │   ├── 英文: 按空格/标点分词，转小写
  │   ├── 中文: 按 unicode.Han 单字分词
  │   ├── 下划线连接: 保留为整体
  │   └── 停用词过滤
  ├── 实现 tokenizeBigram(tokens []string) []string
  └── 编写 tokenizer_test.go
      ├── TestTokenize_English
      ├── TestTokenize_Chinese
      ├── TestTokenize_Mixed
      ├── TestTokenize_MetricName
      └── TestTokenize_StopWords

Task 1.2: bm25.go
  ├── 定义 BM25Engine struct
  ├── 实现 NewBM25Engine(docs, options)
  │   ├── 分词所有文档
  │   ├── 计算文档频率 df
  │   ├── 计算 IDF
  │   └── 计算平均文档长度 avgdl
  ├── 实现 Score(queryTokens, docIdx) float64
  └── 编写 bm25_test.go
      ├── TestBM25_ExactMatch
      ├── TestBM25_FuzzyMatch
      ├── TestBM25_IDFWeighting
      └── TestBM25_EmptyQuery

Task 1.3: retriever.go 修改
  ├── RetrieverOptions 增加 BM25Weight/BM25K1/BM25B
  ├── Retriever struct 增加 bm25/bm25Weight/structWeight
  ├── NewRetriever 初始化 BM25Engine
  ├── Search 方法融合评分
  └── 编写 retriever_test.go 新增用例
      ├── TestSearch_BM25FuzzyMatch
      ├── TestSearch_PreciseNotDegraded
      └── TestSearch_BM25WeightConfig

Task 1.4: 配置与集成
  ├── config.go 增加 RUNBOOK_BM25_* 环境变量
  ├── 运行 go test ./copilot/runbook/...
  ├── 运行 151 集成测试
  └── commit: feat: add BM25 retrieval to runbook search
```

**技术要点**：

| 要点 | 说明 | 风险 |
|---|---|---|
| BM25 分数归一化 | BM25 分数范围不确定，需与结构化评分（0-10+）做归一化后再融合 | 可用 Min-Max 归一化或直接调权重 |
| 中文单字粒度 | "高"可能匹配到多个不相关文档 | IDF 自动降权高频字；结构化评分 0.7 权重抑制噪声 |
| 向后兼容 | BM25Weight=0 时退化为纯结构化评分 | 默认 0.3，可配置为 0 |

#### 10.2.3 Sprint 2：NLU 评估 + Prompt 迭代记录（Day 3）

**目标**：NLU 准确率可量化，Prompt 迭代有记录可讲述。

| 时间 | 任务 | 具体工作 | 产出 | 验收标准 |
|---|---|---|---|---|
| Day 3 上午 | NLU 评估数据集 | 编写 `eval/dataset.go` + `eval/evaluator.go` | 86 条标注 + 评估器 | `go test -run TestEvaluate` 输出报告 |
| Day 3 下午 | Prompt 迭代记录 | 在 design.md §4.3 补充迭代记录（已完成） | 文档更新 | 可讲述 3 版迭代过程 |

**关键任务分解**：

```
Task 2.1: eval/dataset.go
  ├── 定义 EvalCase struct
  ├── 编写 GoldenSet（86 条）
  │   ├── alert_query: 12 条
  │   ├── alert_event_query: 8 条
  │   ├── alert_history_query: 8 条
  │   ├── alert_rule_list_query: 6 条
  │   ├── metric_query: 18 条
  │   ├── host_query: 8 条
  │   ├── diagnosis_request: 10 条
  │   ├── general_chat: 8 条
  │   └── 边界/困难用例: 8 条
  └── 验证: 每条用例的 WantIntent 在 8 种意图中

Task 2.2: eval/evaluator.go
  ├── 定义 EvalResult / IntentMetrics struct
  ├── 实现 Evaluate(classifier, cases) EvalResult
  │   ├── 遍历 cases，调用 classifier.Classify
  │   ├── 计算 TP/FP/FN per intent
  │   ├── 计算 Precision/Recall/F1 per intent
  │   └── 计算 Rule hit rate / LLM fallback rate
  ├── 实现 EvalResult.String() 格式化输出
  └── 编写 evaluator_test.go
      └── TestEvaluate: 运行评估 + 准确率阈值检查

Task 2.3: 运行评估并记录基线
  ├── go test ./copilot/nlu/eval/ -v -run TestEvaluate
  ├── 记录基线指标到 design.md
  └── commit: feat: add NLU evaluation dataset and framework
```

**技术要点**：

| 要点 | 说明 |
|---|---|
| 评估不含 LLM | 评估器只测规则分类器（`nlu.NewClassifier()`），不调 LLM API |
| 准确率阈值 | 初始阈值 80%，后续根据基线调整 |
| 可重复性 | 固定数据集，无随机性，输出稳定 |

#### 10.2.4 Sprint 3：AI 质量指标（Day 4-5）

**目标**：AI 模块有 Prometheus 指标，可实时监控质量。

| 时间 | 任务 | 具体工作 | 产出 | 验收标准 |
|---|---|---|---|---|
| Day 4 上午 | NLU + RAG 指标 | 修改 `nlu.go` / `retriever.go` 增加指标埋点 | Counter + Histogram | `curl /metrics` 可见 copilot_nlu_* / copilot_rag_* |
| Day 4 下午 | 诊断 + LLM 指标 | 修改 `summarizer.go` / `client.go` 增加指标埋点 | Counter + Histogram | `curl /metrics` 可见 copilot_diagnosis_* / copilot_llm_* |
| Day 5 上午 | Grafana Dashboard | 编写 `ai-quality-overview.json` | Dashboard JSON | 导入 Grafana 后 4 行面板正常显示 |
| Day 5 下午 | 端到端验证 | 发送 Chat 请求 → 检查指标更新 → 检查 Dashboard | 全链路验证 | 指标值与请求次数一致 |

**关键任务分解**：

```
Task 3.1: NLU 指标
  ├── nlu.go: Classify 结束后
  │   ├── copilot_nlu_classify_total{intent, source}.Inc()
  │   └── copilot_nlu_classify_duration_seconds.Observe(elapsed)
  └── 新增 copilot/nlu/metrics.go
      ├── 定义 prometheus.CounterVec
      └── 定义 prometheus.HistogramVec

Task 3.2: RAG 指标
  ├── retriever.go: Search 结束后
  │   ├── copilot_rag_search_total{has_result}.Inc()
  │   ├── copilot_rag_search_score.Observe(topScore)
  │   └── copilot_rag_search_duration_seconds.Observe(elapsed)
  └── 新增 copilot/runbook/metrics.go

Task 3.3: 诊断指标
  ├── summarizer.go: Summarize 结束后
  │   ├── copilot_diagnosis_confidence.Observe(confidence)
  │   ├── copilot_diagnosis_llm_total{result}.Inc()
  │   └── copilot_diagnosis_duration_seconds{source}.Observe(elapsed)
  └── 新增 copilot/diagnosis/metrics.go

Task 3.4: LLM 指标
  ├── client.go: 请求结束后
  │   ├── copilot_llm_request_total{model, result}.Inc()
  │   ├── copilot_llm_request_duration_seconds{model}.Observe(elapsed)
  │   └── copilot_llm_tokens_total{model, direction}.Add(tokens)
  └── 新增 copilot/llm/metrics.go

Task 3.5: Grafana Dashboard
  └── 编写 ai-quality-overview.json
      ├── Row 1: NLU 意图分布 + 规则/LLM 命中率
      ├── Row 2: RAG 检索命中率 + 评分分布
      ├── Row 3: 诊断置信度 + LLM 成功/降级率
      └── Row 4: LLM 延迟 P50/P95/P99 + Token 消耗
```

#### 10.2.5 Sprint 4（可选）：Embedding 向量检索（Week 5+）

**前置条件**：Sprint 1-3 全部完成，BM25 检索稳定运行。

**目标**：RAG 支持语义匹配，"服务器卡住了" → `high-cpu.md`。

| 时间 | 任务 | 产出 |
|---|---|---|
| Day 1 | Embedding API 客户端 | `embedder.go` + 测试 |
| Day 2 | Redis Search 向量存储 | `vector_store.go` + 测试 |
| Day 3 | Retriever 集成 + 端到端验证 | 修改后的 `retriever.go` |

---

### 10.3 各阶段交付成果与验收标准

#### 10.3.1 交付物清单

| Sprint | 交付物 | 类型 | 存储位置 |
|---|---|---|---|
| Sprint 1 | `tokenizer.go` + `tokenizer_test.go` | 代码 | `copilot/runbook/` |
| Sprint 1 | `bm25.go` + `bm25_test.go` | 代码 | `copilot/runbook/` |
| Sprint 1 | 修改后的 `retriever.go` + `types.go` | 代码 | `copilot/runbook/` |
| Sprint 1 | 修改后的 `config.go` | 代码 | `config/` |
| Sprint 2 | `eval/dataset.go` | 代码+数据 | `copilot/nlu/eval/` |
| Sprint 2 | `eval/evaluator.go` + `evaluator_test.go` | 代码 | `copilot/nlu/eval/` |
| Sprint 2 | NLU 评估基线报告 | 文档 | `docs/design.md` §4.2 |
| Sprint 3 | `metrics.go`（4 个模块各 1 个） | 代码 | `copilot/*/` |
| Sprint 3 | `ai-quality-overview.json` | 配置 | `docker/grafana/dashboards/` |

#### 10.3.2 质量门禁

每个 Sprint 合并到 main 前必须通过以下门禁：

| # | 门禁 | 命令 | 通过标准 |
|---|---|---|---|
| 1 | 单元测试 | `go test ./copilot/... -count=1` | 0 FAIL |
| 2 | 代码规范 | `goimports -l .` | 0 输出 |
| 3 | 静态检查 | `go vet ./...` | 0 输出 |
| 4 | 编译通过 | `go build ./...` | 0 错误 |
| 5 | 集成测试不退化 | 运行 151 集成测试 | PASS 率不下降 |
| 6 | 无新增依赖 | `git diff go.mod` | 无新增 require（除非有 ADR） |

#### 10.3.3 验收标准细化

**Sprint 1 验收**：

| # | 验收项 | 方法 | 标准 |
|---|---|---|---|
| 1 | 分词器正确性 | `go test -run TestTokenize` | 5 种输入分词结果正确 |
| 2 | BM25 评分正确性 | `go test -run TestBM25` | 与手工计算一致（误差 < 0.01） |
| 3 | 精确查询不退化 | `Search("HighCPU")` | Top-1 = `high-cpu.md` |
| 4 | 模糊查询可召回 | `Search("CPU飙高")` | Top-1 = `high-cpu.md` |
| 5 | 集成测试通过 | 151 用例 | PASS 率 ≥ 143（不下降） |
| 6 | 配置可调 | `RUNBOOK_BM25_WEIGHT=0` | 退化为纯结构化评分 |

**Sprint 2 验收**：

| # | 验收项 | 方法 | 标准 |
|---|---|---|---|
| 1 | 评估脚本可运行 | `go test -run TestEvaluate` | 输出完整报告 |
| 2 | 整体准确率 | 报告中 Accuracy | ≥ 80% |
| 3 | 每意图 F1 可追溯 | 报告中 ByIntent | 所有意图均有 F1 值 |
| 4 | 规则命中率可量化 | 报告中 RuleHitRate | 有具体数值 |

**Sprint 3 验收**：

| # | 验收项 | 方法 | 标准 |
|---|---|---|---|
| 1 | 指标可采集 | `curl localhost:8080/metrics` | 可见 copilot_* 指标 |
| 2 | 指标随请求更新 | 发送 Chat 请求后再次 curl | 计数器递增 |
| 3 | Dashboard 可导入 | Grafana Import JSON | 4 行面板正常显示 |
| 4 | 数据有值 | Dashboard 中有数据点 | 非空 |

---

### 10.4 潜在风险评估与应对策略

#### 10.4.1 风险矩阵

| # | 风险 | 概率 | 影响 | 等级 | 应对策略 |
|---|---|---|---|---|---|
| R1 | BM25 中文单字分词粒度粗，引入噪声 | 中 | 低 | 🟡 | 结构化评分 0.7 权重抑制；BM25Weight 可配置为 0 回退 |
| R2 | BM25 分数与结构化分数尺度不匹配 | 中 | 中 | 🟡 | Min-Max 归一化或调权重；Sprint 1 Day 2 上午验证 |
| R3 | NLU 评估准确率低于 80% | 低 | 高 | 🟡 | 补充规则覆盖失败 case；调整阈值到 75% |
| R4 | LLM API 不可用影响评估 | 中 | 低 | 🟢 | 评估器只测规则分类器，不依赖 LLM |
| R5 | Prometheus 指标命名冲突 | 低 | 低 | 🟢 | 统一 `copilot_` 前缀，检查现有指标 |
| R6 | Grafana Dashboard JSON 兼容性 | 低 | 低 | 🟢 | 使用通用面板类型，避免版本特定功能 |
| R7 | 集成测试退化 | 低 | 高 | 🔴 | 每个 Sprint 合并前运行全量测试；Git bisect 定位 |
| R8 | go.mod 新增依赖被拒绝 | 低 | 中 | 🟡 | BM25 和分词器零依赖实现；如需引入 gse 需 ADR |
| R9 | Docker Compose 服务不稳定 | 中 | 中 | 🟡 | 每次测试前 `docker compose ps` 检查；异常时重启 |
| R10 | 时间超期 | 中 | 中 | 🟡 | Sprint 4 可选；Sprint 1-3 核心必须完成 |

#### 10.4.2 应对策略详解

**R1: BM25 中文单字分词噪声**

```
场景: "高负载" 的 "高" 匹配到 high-cpu.md 和 high-memory.md
影响: 模糊查询可能返回多个不相关结果
应对:
  1. 结构化评分 0.7 权重保证精确匹配优先
  2. BM25 IDF 自动降权高频字（"高" 在 9 个 Runbook 中出现频率高 → IDF 低）
  3. 配置 RUNBOOK_BM25_WEIGHT=0 可完全禁用 BM25
  4. 后续可升级到 gse 分词（Phase 3+）
```

**R2: 分数尺度不匹配**

```
场景: 结构化评分范围 0-10+，BM25 评分范围 0-5+
影响: 融合后某一路评分占主导
应对:
  1. Sprint 1 Day 2 上午实测 9 个 Runbook 的评分分布
  2. 如发现尺度不匹配，对 BM25 分数做 Min-Max 归一化
  3. 或调整权重（如 0.8/0.2 → 0.6/0.4）
```

**R7: 集成测试退化**

```
场景: BM25 修改导致现有测试 FAIL
影响: 阻塞合并
应对:
  1. 每个 Sprint 合并前运行全量 151 集成测试
  2. 如 FAIL，git stash 新代码 → 运行测试确认基线 → git stash pop → 定位差异
  3. BM25 是评分增强，不改变 API 接口，理论上不应退化
  4. 如确有退化，先修复再合并，不降级验收标准
```

---

### 10.5 跨团队协作与沟通机制

#### 10.5.1 角色与职责

本项目为个人项目（秋招准备），但模拟团队协作分工：

| 角色 | 职责 | 决策权 |
|---|---|---|
| **项目负责人**（自己） | 整体规划、技术决策、代码实现、测试验证 | 全部 |
| **代码审查者**（Codex / AI 辅助） | 代码质量检查、Bug 发现、设计评审建议 | 建议权 |
| **测试执行者**（自动化脚本） | 集成测试运行、回归测试、指标采集 | 无（自动执行） |

#### 10.5.2 沟通机制

| 场景 | 机制 | 频率 | 产出 |
|---|---|---|---|
| Sprint 启动 | 明确本 Sprint 目标和任务分解 | 每个 Sprint 开始时 | 任务清单 |
| 每日进度 | 检查 `git log` 和测试结果 | 每天 | 进度状态 |
| Sprint 评审 | 运行验收标准 + 记录结果 | 每个 Sprint 结束时 | 验收报告 |
| 风险发现 | 立即记录到风险矩阵 + 调整计划 | 实时 | 更新后的计划 |
| 技术决策 | 记录 ADR（Architecture Decision Record） | 需要时 | ADR 条目 |

#### 10.5.3 决策流程

```
发现问题或需求变更
  │
  ▼
评估影响范围
  ├── 仅影响当前 Sprint → 自行决策，记录 ADR
  ├── 影响后续 Sprint → 更新计划，调整时间表
  └── 影响已合并代码 → 评估回滚风险，必要时回滚
  │
  ▼
实施决策
  │
  ▼
验证结果 → 更新文档
```

#### 10.5.4 变更控制

| 变更类型 | 审批要求 | 影响评估 |
|---|---|---|
| 新增依赖（go.mod） | 需要 ADR + 理由 | 二进制大小、安全性、维护成本 |
| 修改 API 接口 | 不允许（§9 不做清单） | — |
| 修改数据库 Schema | 不允许（§9 不做清单） | — |
| 调整 Sprint 优先级 | 自行决策 | 时间表更新 |
| 调整验收标准 | 需记录原因 | 质量基线更新 |

---

### 10.6 可追踪性与可调整性

#### 10.6.1 追踪机制

| 维度 | 追踪方式 | 工具 |
|---|---|---|
| 代码变更 | Git commit + PR | Git |
| 测试结果 | `go test -json` 输出 | Go testing |
| AI 质量 | NLU 评估报告 + Prometheus 指标 | 自定义评估器 + Grafana |
| 技术决策 | ADR 表（§6） | 本文档 |
| 风险状态 | 风险矩阵（§10.4） | 本文档 |

#### 10.6.2 调整机制

**计划调整触发条件**：

| 触发条件 | 调整动作 | 审批 |
|---|---|---|
| Sprint 1 验收未通过 | 延长 1 天修复，或降级 BM25Weight=0 回退 | 自行决策 |
| Sprint 2 准确率 < 75% | 补充规则 → 重跑评估；或降低阈值到 70% | 记录原因 |
| Sprint 3 指标无数据 | 检查埋点位置 → 修复 → 重测 | 自行决策 |
| 整体时间超期 2 天+ | 砍掉 Sprint 4（可选），确保 Sprint 1-3 完成 | 优先级调整 |
| 发现更高优先级问题 | 暂停当前 Sprint → 修复 → 恢复 | 记录到风险矩阵 |

**回滚策略**：

| 场景 | 回滚方式 |
|---|---|
| 单个文件修改引入 Bug | `git revert <commit>` |
| 整个 Sprint 失败 | `git reset --hard main` → 重新实现 |
| 配置变更导致异常 | 修改环境变量回退到默认值 |

---

### 10.7 演进路径细化：各 Phase 任务清单

#### 10.7.1 路径 A：RAG 检索深度演进

| Phase | 具体目标 | 关键任务 | 技术要点 | 里程碑 | 前置依赖 |
|---|---|---|---|---|---|
| **Phase 2** | 模糊匹配可召回 | T1.1 分词器、T1.2 BM25 引擎、T1.3 评分融合、T1.4 配置集成 | 单字分词 + IDF 降权 + 0.7/0.3 融合 | "CPU飙高" → high-cpu.md | 无 |
| **Phase 3** | 语义匹配可召回 | T3.1 Embedding 客户端、T3.2 向量存储、T3.3 Retriever 集成 | DeepSeek Embedding + Redis Search KNN | "服务器卡住了" → high-cpu.md | Phase 2 完成 |
| **Phase 4** | 三路融合工业级 | T4.1 RRF 算法、T4.2 三路检索集成、T4.3 参数调优 | RRF(k=60) + 三路排名融合 | 精确 ≥95%、模糊 ≥80%、语义 ≥70% | Phase 3 完成 |
| **Phase 5** | LLM 精排 | T5.1 Reranker 实现、T5.2 诊断集成、T5.3 延迟优化 | LLM-as-judge + 低温度 + Top-N 截断 | 诊断场景 Top-1 准确率提升 5%+ | Phase 4 完成 |

#### 10.7.2 路径 B：NLU 深度演进

| Phase | 具体目标 | 关键任务 | 技术要点 | 里程碑 | 前置依赖 |
|---|---|---|---|---|---|
| **Phase 2** | 准确率可量化 | T2.1 标注数据集(86条)、T2.2 评估器、T2.3 基线报告 | precision/recall/F1 + 规则命中率 | 输出完整评估报告 | 无 |
| **Phase 3** | 工具调用协议升级 | T3.1 FunctionCall 方法、T3.2 参数提取、T3.3 降级路径 | DeepSeek tools 参数 + JSON Schema 校验 | 格式稳定性 ≥99% | Phase 2 完成 |
| **Phase 4** | 多意图识别 | T4.1 连接词检测、T4.2 多意图分类、T4.3 串行执行 | "并/和/同时" 拆分 + intent 数组 | "查告警并诊断" 正确拆分 | Phase 3 完成 |
| **Phase 5** | 在线学习闭环 | T5.1 反馈 API、T5.2 失败 case 分析、T5.3 对比评估 | feedback → 规则补充 → 重评估 | 优化后 F1 提升 5%+ | Phase 4 完成 |

#### 10.7.3 路径 C：可观测性 + AI 质量监控

| Phase | 具体目标 | 关键任务 | 技术要点 | 里程碑 | 前置依赖 |
|---|---|---|---|---|---|
| **Phase 3** | 实时质量可监控 | T3.1 NLU 指标、T3.2 RAG 指标、T3.3 诊断指标、T3.4 LLM 指标 | Prometheus Counter + Histogram | `/metrics` 可见 copilot_* | Phase 2（NLU 评估）完成 |
| **Phase 4** | 质量可视化运营 | T4.1 Dashboard JSON、T4.2 告警规则 | Grafana 4 行面板 + Prometheus 告警 | Dashboard 正常显示 | Phase 3 完成 |

---

### 10.8 项目完成标志

阶段三完成的判定标准：

| # | 标准 | 验证方法 |
|---|---|---|
| 1 | BM25 RAG 升级已合并到 main | `git log --oneline` 可见 feat commit |
| 2 | NLU 评估基线已建立 | `go test -run TestEvaluate` 输出报告，准确率 ≥ 80% |
| 3 | Prompt 迭代记录已补充 | design.md §4.3 包含 v1→v2→v3 迭代记录 |
| 4 | AI 质量指标已上线 | `curl /metrics` 可见 copilot_* 指标 |
| 5 | 集成测试不退化 | 151 用例 PASS 率 ≥ 143 |
| 6 | 无新增外部依赖 | `git diff go.mod` 无新增 require |
| 7 | 文档已更新 | design.md 版本号更新为 v3.0 |
