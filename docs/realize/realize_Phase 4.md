# CloudOps Copilot Phase 4 实施方案

> 方案版本：v1.0
> 制定日期：2026-05-09
> 依据文档：`docs/design.md` v3.1
> 阶段定位：Runbook 检索，在 Phase 3 告警诊断报告基础上，将 Markdown 运维知识库接入 EvidenceCollector、Tool Registry、LLM Prompt 和前端诊断详情页。

---

## 1. 阶段目标

Phase 4 的目标是补齐 CloudOps Copilot 的第一版知识增强能力：为常见主机告警建立 Markdown Runbook，启动时加载为内存索引，通过关键词和告警名匹配返回 Top N 片段，并将命中片段写入 `DiagnosisReport.runbooks_json`、`EvidenceBundle.runbooks` 和 LLM 诊断上下文。

本阶段坚持轻量实现，不引入向量数据库、Embedding 服务、LangChain 或独立知识库服务。优先完成“真实 Runbook 内容可检索、可追溯、可展示、可降级”的闭环，为后续 BM25、Embedding 和更多 K8s Runbook 预留接口边界。

### 1.1 核心交付物

| 交付物 | 内容 | 验收标准 |
|---|---|---|
| Runbook Markdown 目录 | `server-monitor/runbooks/*.md`，覆盖 HighCPU、CriticalCPU、HighMemory、HighDisk、HostDown | 5 类核心告警均有可读 Runbook，内容包含适用告警、关键指标、排查步骤、建议动作、风险说明 |
| Runbook 数据结构 | `RunbookDocument`、`RunbookSection`、`RunbookSnippet`、`SearchRequest`、`SearchResult` | 可表达标题、适用告警、关键词、指标、摘要、命中原因和分数 |
| Markdown Loader / Parser | 启动时从本地目录加载 `.md` 文件并解析标题、章节、列表内容 | 空目录、非法文件、重复标题、超长文件均有明确错误或跳过策略 |
| RAGRetriever | 基于告警名、关键词、指标名和正文关键词进行排序检索 | HighCPU 等核心告警可稳定命中对应 Runbook Top 1 |
| `runbook.search` 工具 | 注册到 Copilot Tool Registry，提供只读低风险检索工具 | `/api/v1/copilot/tools` 可看到工具 schema；参数错误被 Registry 拒绝 |
| Diagnosis 接入 | EvidenceCollector 调用 `runbook.search`，LLMSummarizer 将片段注入 Prompt | 新诊断报告的 `runbooks_json` 不再固定为空，详情页可展示命中片段 |
| 前端展示 | 诊断详情页展示 Runbook 标题、分数、命中关键词、片段和来源文件 | 命中为空时展示“未命中 Runbook”，不影响报告展示 |
| 验证与回归 | 单元测试、接口测试、前端构建和配置校验 | `server-web` Go 测试通过；前端 build 通过；现有 Copilot/Diagnosis 链路不回退 |

### 1.2 本阶段不做

1. 不引入 Milvus、Qdrant、Elasticsearch 向量检索或 Embedding API。
2. 不实现 BM25 / TF-IDF 复杂排序；Phase 4 只做可解释的关键词加权检索。
3. 不做 Runbook 在线编辑、版本审批、文件上传或管理后台。
4. 不做异步 Diagnosis Worker；Phase 5 再消费 Kafka `alert-events` 自动诊断。
5. 不创建 PendingAction，不接入审批审计，不执行写操作。
6. 不新增 Kubernetes 只读或写操作工具；K8s Runbook 文件可以预留，但不作为验收硬指标。
7. 不改变现有诊断 API 响应外层结构，不破坏 `DiagnosisReport` 已有字段兼容性。

---

## 2. 当前基础与前置条件

### 2.1 当前已具备能力

根据当前代码结构，Phase 1 到 Phase 3 已经形成以下基础：

| 能力 | 当前落点 | Phase 4 复用方式 |
|---|---|---|
| Copilot API | `server-monitor/server-web/copilot/handler`、`copilot/service` | `knowledge_query` 和 `runbook.search` 复用现有 Chat 流程 |
| Tool Registry | `server-monitor/server-web/copilot/tool` | 新增只读工具 `runbook.search`，复用 schema、timeout、日志、脱敏和健康检查 |
| Diagnosis Pipeline | `server-monitor/server-web/copilot/diagnosis` | 在 `EvidenceCollector` 增加 Runbook 并行采集，在 `completedFields` 写入 `runbooks_json` |
| DiagnosisReport 模型 | `server-monitor/server-web/model/diagnosis_report.go` | 复用已有 `runbooks_json` 字段，不新增诊断表字段 |
| LLM Summarizer | `server-monitor/server-web/copilot/diagnosis/summarizer.go` | 将 `runbooks` 片段加入 Prompt，删除 Phase 3 的“Runbook 未接入”固定说明 |
| 前端诊断页 | `server-monitor/frontend/src/pages/DiagnosisDetailPage.vue` | 展示 `report.runbooks` 和 `report.evidence.runbooks` |

### 2.2 前置假设

1. `server-monitor/server-web` 仍是独立 Go module，Go 验证命令在该目录执行。
2. `server-monitor/frontend` 仍使用 Vue 3 + TypeScript + Vite。
3. Phase 4 读取本地 Markdown 文件，不依赖外部网络。
4. Runbook 文件随代码仓库或镜像发布；生产环境可通过 ConfigMap/volume 覆盖。
5. `runbook.search` 是低风险只读工具，viewer 和 admin 均可调用。
6. Runbook 内容不包含 Secret、Token、密码、真实内网凭据或不可公开的部署细节。

### 2.3 阶段边界

Phase 4 只改变知识检索与诊断上下文，不改变告警接收、规则同步、Webhook、Kafka 消费、审批和执行链路。Alertmanager Webhook 仍必须快速返回，不允许在同步路径中加载 LLM 或执行 Runbook 检索。

---

## 3. 总体实施路径

Phase 4 拆为 7 个模块推进，每个模块都有清晰文件边界和验证方式。

```text
模块 1：Runbook 内容资产与目录约定
  ↓
模块 2：Runbook Loader / Parser
  ↓
模块 3：RAGRetriever 关键词检索
  ↓
模块 4：runbook.search Tool Registry 接入
  ↓
模块 5：Diagnosis Pipeline 与 LLM Prompt 接入
  ↓
模块 6：前端诊断详情展示
  ↓
模块 7：配置、部署、联调、回归与验收
```

---

## 4. 文件规划

### 4.1 后端新增文件

| 文件 | 职责 |
|---|---|
| `server-monitor/server-web/copilot/runbook/types.go` | 定义 Runbook 文档、片段、检索请求、检索结果 |
| `server-monitor/server-web/copilot/runbook/loader.go` | 从目录读取 Markdown 文件，限制文件数量和大小 |
| `server-monitor/server-web/copilot/runbook/parser.go` | 解析标题、章节、适用告警、关键指标、关键词和正文 |
| `server-monitor/server-web/copilot/runbook/retriever.go` | 实现关键词加权排序和 Top N 截断 |
| `server-monitor/server-web/copilot/runbook/loader_test.go` | 覆盖加载、非法文件、空目录、大小限制 |
| `server-monitor/server-web/copilot/runbook/parser_test.go` | 覆盖模板解析和字段提取 |
| `server-monitor/server-web/copilot/runbook/retriever_test.go` | 覆盖精确匹配、关键词匹配、指标匹配、空结果 |
| `server-monitor/server-web/copilot/tool/runbook_tool.go` | 将 Runbook Retriever 封装为 `runbook.search` 工具 |
| `server-monitor/server-web/copilot/tool/runbook_tool_test.go` | 覆盖工具参数校验、返回结构、错误降级 |

### 4.2 后端修改文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/server-web/config/config.go` | 增加 `RunbookDir`、`RunbookMaxFiles`、`RunbookMaxFileBytes`、`RunbookSearchTopN` 配置和 env 解析 |
| `server-monitor/server-web/copilot/tool/executor.go` | `Options` 增加 `RunbookSearcher`，`Executor` 持有检索器 |
| `server-monitor/server-web/copilot/tool/readonly_tools.go` | 注册 `runbook.search`，健康检查识别该工具 |
| `server-monitor/server-web/copilot/diagnosis/evidence.go` | 增加 `ToolRunbookSearch`，EvidenceCollector 并行采集 Runbook |
| `server-monitor/server-web/copilot/diagnosis/types.go` | 将 `Runbooks []json.RawMessage` 替换或补充为明确的 `[]RunbookEvidence` |
| `server-monitor/server-web/copilot/diagnosis/summarizer.go` | Prompt 注入 Runbook 片段，并纳入 prompt hash |
| `server-monitor/server-web/copilot/diagnosis/service.go` | `completedFields` 写入真实 `runbooks_json` |
| `server-monitor/server-web/copilot/diagnosis/rule.go` | 置信度计算使用真实 Runbook 命中情况 |
| `server-monitor/server-web/api/router.go` | 启动时加载 Runbook Retriever，并传入 Tool Executor |
| `server-monitor/server-web/api/router_test.go` | 覆盖默认配置下 Copilot 仍可启动，Runbook 配置错误返回清晰错误 |

### 4.3 Runbook 内容文件

| 文件 | 覆盖告警 |
|---|---|
| `server-monitor/runbooks/high-cpu.md` | `HighCPU` |
| `server-monitor/runbooks/critical-cpu.md` | `CriticalCPU` |
| `server-monitor/runbooks/high-memory.md` | `HighMemory` |
| `server-monitor/runbooks/high-disk.md` | `HighDisk` |
| `server-monitor/runbooks/host-down.md` | `HostDown` |

### 4.4 前端修改文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/frontend/src/types/index.ts` | 增加 `RunbookSnippet` / `RunbookEvidence` 类型 |
| `server-monitor/frontend/src/pages/DiagnosisDetailPage.vue` | 展示 Runbook 命中片段、分数、关键词、来源文件和空状态 |
| `server-monitor/frontend/src/api/diagnosis.ts` | 如现有类型过窄，补齐 `runbooks` 字段类型 |

### 4.5 部署与配置文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/docker-compose.yml` | 如镜像运行无法读取仓库目录，挂载 `./runbooks:/app/runbooks:ro` |
| `server-monitor/Dockerfile` 或 `server-monitor/server-web/Dockerfile` | 如现有构建未复制 Runbook，补充只读内容复制 |
| `server-monitor/charts/server-monitor/values.yaml` | 增加 runbooks 配置开关、挂载路径和 ConfigMap 内容入口 |
| `server-monitor/charts/server-monitor/templates/...` | 仅当 Helm 已管理 server-web 部署时，增加 ConfigMap 和 volumeMount |

部署文件是否修改以实际实施时的镜像构建路径为准；如果本地二进制直接运行且可读取 `server-monitor/runbooks`，Compose/Helm 可作为 Phase 4 的后半段提交。

---

## 5. 详细实施步骤

### 5.1 模块 1：Runbook 内容资产与目录约定

**目标：** 建立第一版 Markdown 运维知识库，内容足够真实、可检索、可展示。

**目录结构：**

```text
server-monitor/runbooks/
  high-cpu.md
  critical-cpu.md
  high-memory.md
  high-disk.md
  host-down.md
```

**统一模板：**

```markdown
# HighCPU

## 适用告警
- HighCPU

## 关键词
- cpu
- load
- server_monitor_cpu_usage_percent

## 典型现象
CPU 使用率在 15m 窗口内持续高于阈值，可能伴随 load1 升高。

## 关键指标
- server_monitor_cpu_usage_percent
- server_monitor_load1
- server_monitor_process_count

## 排查步骤
1. 查看 CPU 15m 趋势，确认是否持续高位。
2. 对比 load1 和进程数量，判断是否为负载同步升高。
3. 查看同实例 24h 告警历史，判断是否周期性触发。
4. 确认近期发布、流量增长、批处理任务或异常进程。

## 建议动作
- 低风险：继续观察、通知负责人、收集进程信息。
- 中风险：扩容关联工作负载或迁移流量，必须人工确认。
- 高风险：删除资源、修改 Secret、批量重启，禁止由 AI 自动执行。

## 风险说明
任何写操作必须进入审批和审计流程；Phase 4 只提供知识建议，不执行动作。
```

**实施步骤：**

1. 新建 `server-monitor/runbooks/` 目录。
2. 为 5 类核心告警分别编写 Markdown。
3. 每个文件必须包含：
   - 一级标题。
   - `适用告警`。
   - `关键词`。
   - `典型现象`。
   - `关键指标`。
   - `排查步骤`。
   - `建议动作`。
   - `风险说明`。
4. Runbook 内容只能写通用排查知识和项目已有指标名，不写真实密钥、内网账号、生产主机凭据。
5. `CriticalCPU` 可以复用 HighCPU 的指标体系，但严重程度、响应优先级和建议动作要更保守。
6. `HostDown` 重点覆盖 `up{job="server-probe"}`、server-probe 进程、网络连通性、Prometheus scrape 状态和误报排查。

**技术要求：**

1. Markdown 文件使用 UTF-8。
2. 单文件建议小于 16KB，第一版硬限制 64KB。
3. 文件名使用小写短横线，避免空格。
4. Runbook 中的动作建议必须标注风险，不出现“直接重启”“直接删除”等无审批措辞。

**验收标准：**

1. 5 个核心 Runbook 文件存在。
2. 每个文件至少包含 5 个标准章节。
3. 每个文件至少包含 3 个关键词和 2 个关键指标。
4. `rg -n "password|token|secret|AKIA|BEGIN .*PRIVATE" server-monitor/runbooks` 不应命中敏感示例。

### 5.2 模块 2：Runbook Loader / Parser

**目标：** 启动时加载 Markdown 文件并解析为结构化文档，供检索器使用。

**核心结构：**

```go
type Document struct {
    ID             string
    Title          string
    FilePath       string
    ApplicableAlerts []string
    Keywords       []string
    Metrics        []string
    Sections       []Section
    Body           string
    UpdatedAt      time.Time
}

type Section struct {
    Heading string
    Text    string
}
```

**实施步骤：**

1. 新增 `server-web/copilot/runbook` 包。
2. `loader.go` 实现 `LoadDir(ctx, dir string, options LoadOptions) ([]Document, error)`。
3. Loader 只读取 `.md` 文件，忽略隐藏文件和子目录。
4. Loader 对每个文件做大小限制，超过 `RunbookMaxFileBytes` 返回明确错误。
5. Parser 提取：
   - 一级标题作为 `Title`。
   - `## 适用告警` 列表作为 `ApplicableAlerts`。
   - `## 关键词` 列表作为 `Keywords`。
   - `## 关键指标` 列表作为 `Metrics`。
   - 其他二级标题作为 `Sections`。
6. 对缺少 `适用告警` 的文件，使用标题作为默认告警名。
7. 对重复标题或重复告警，允许加载，但检索时按分数排序，不让启动失败。
8. Parser 不引入 Markdown 第三方依赖，使用按行扫描即可满足本阶段模板。

**错误策略：**

| 场景 | 处理 |
|---|---|
| Runbook 目录不存在 | 如果配置为空或默认目录不存在，启动成功但 retriever 为空并记录 warning |
| 单个文件无法读取 | 返回错误，避免启动后知识库半缺失且无人知晓 |
| 单个文件超限 | 返回错误，提示文件路径和大小 |
| 文件无一级标题 | 返回错误，提示 Runbook 模板不合法 |
| 文件无关键词 | 允许加载，但检索质量下降，测试中覆盖 |

**技术要求：**

1. Loader 必须接收 `context.Context`。
2. 不使用 `context.Background()` 读取外部资源。
3. 文件读取后不保留敏感原始路径到前端，只返回相对文件名。
4. Parser 逻辑保持纯函数，便于表驱动测试。

**验收标准：**

1. `LoadDir` 能加载 5 个核心 Runbook。
2. `ParseMarkdown` 能正确提取标题、适用告警、关键词和关键指标。
3. 空目录返回空文档集合，不 panic。
4. 非法模板返回可理解错误。

### 5.3 模块 3：RAGRetriever 关键词检索

**目标：** 实现轻量、可解释、无外部依赖的 Runbook 检索。

**检索请求：**

```go
type SearchRequest struct {
    AlertName string   `json:"alert_name,omitempty"`
    Keywords  []string `json:"keywords,omitempty"`
    Metrics   []string `json:"metrics,omitempty"`
    Limit     int      `json:"limit,omitempty"`
}
```

**检索结果：**

```go
type SearchResult struct {
    Title           string   `json:"title"`
    File            string   `json:"file"`
    Score           float64  `json:"score"`
    MatchedAlerts   []string `json:"matched_alerts,omitempty"`
    MatchedKeywords []string `json:"matched_keywords,omitempty"`
    MatchedMetrics  []string `json:"matched_metrics,omitempty"`
    Snippet         string   `json:"snippet"`
}
```

**排序算法：**

```text
score = alert_score + keyword_score + metric_score + title_score

alert_score:
  alert_name 精确匹配标题或适用告警       +10
  alert_name 大小写不敏感匹配正文         +3

keyword_score:
  每个关键词命中关键词列表                 +2
  每个关键词命中正文                       +1

metric_score:
  每个指标名命中关键指标列表               +5
  每个指标名命中正文                       +2

title_score:
  查询关键词命中标题                       +3
```

**Snippet 规则：**

1. 优先从命中章节中截取。
2. 如果没有命中章节，取正文前 500 个 Unicode 字符。
3. Snippet 保留 Markdown 列表语义，但去掉多余空行。
4. 单个结果返回片段不超过 800 字符，避免 Prompt 和页面过大。

**实施步骤：**

1. 实现 `Retriever`：
   - `Search(ctx context.Context, req SearchRequest) ([]SearchResult, error)`。
   - `HealthCheck(ctx context.Context) bool`。
   - `Count() int`。
2. 查询归一化：
   - trim 空白。
   - 大小写不敏感。
   - 去重。
   - 忽略空关键词。
3. `Limit` 默认 2，最大 5。
4. 如果无命中结果，返回空数组和 nil error。
5. 如果 retriever 未初始化，返回 `ErrRunbookUnavailable`，由工具层转成可控错误。
6. 表驱动测试覆盖：
   - `HighCPU` 精确命中 `high-cpu.md`。
   - `CriticalCPU` 精确命中 `critical-cpu.md`。
   - keyword=`memory` 命中 HighMemory。
   - metric=`server_monitor_disk_usage_percent` 命中 HighDisk。
   - 未命中返回空数组。

**技术要求：**

1. 检索过程只读内存，不做磁盘 I/O。
2. 文档加载后不可被调用方修改，返回结果使用拷贝。
3. 不使用全局变量保存 retriever；通过 `api/router.go` 依赖注入。
4. 不新增第三方依赖。

**验收标准：**

1. 核心告警 Top 1 命中对应 Runbook。
2. Top N 按 score 降序稳定排序。
3. Snippet 长度受控。
4. 空知识库时系统仍可启动，诊断继续，只记录 Runbook 缺失。

### 5.4 模块 4：`runbook.search` Tool Registry 接入

**目标：** 将 Runbook 检索作为统一只读工具暴露给 Copilot 和 Diagnosis Pipeline。

**工具 schema：**

```json
{
  "name": "runbook.search",
  "description": "Search local Markdown runbooks by alert name, keywords, and metrics.",
  "risk_level": "low",
  "read_only": true,
  "parameters": [
    {"name": "alert_name", "type": "string", "required": false},
    {"name": "keywords", "type": "array", "required": false},
    {"name": "metrics", "type": "array", "required": false},
    {"name": "limit", "type": "integer", "required": false, "default": 2, "min": 1, "max": 5}
  ]
}
```

**实施步骤：**

1. 在 `copilot/tool/executor.go` 增加常量：
   - `ToolRunbookSearch = "runbook.search"`。
2. 新增 `RunbookSearcher` 接口，放在使用方 `copilot/tool` 包：
   - `Search(ctx context.Context, req runbook.SearchRequest) ([]runbook.SearchResult, error)`。
   - `HealthCheck(ctx context.Context) bool`。
3. `tool.Options` 增加 `RunbookSearcher RunbookSearcher`。
4. `Executor` 增加 `runbookSearcher` 字段。
5. `registerReadOnlyTools` 注册 `newRunbookSearchTool(executor)`。
6. `runbook.search` 工具做参数解析：
   - `alert_name` 最大 128 字符。
   - `keywords` 最多 20 个，每个最大 64 字符。
   - `metrics` 最多 20 个，每个最大 128 字符。
   - `limit` 默认 2，范围 1 到 5。
7. 工具结果返回 `[]runbook.SearchResult`，并通过 Registry 统一记录 duration、success、trace。
8. `HealthCheck` 在 retriever 非 nil 且文档数量大于 0 时返回 true；空知识库返回 false 但不影响 server-web 启动。

**技术要求：**

1. 工具必须标记 `ReadOnly=true`、`RiskLevel=low`。
2. 工具不能读取请求用户无关的文件路径，只使用启动时注入的 retriever。
3. 参数校验优先交给 Registry；工具内部再做数组长度和字符串清洗。
4. `runbook.search` 不访问外部网络。
5. 工具错误不能泄露宿主机绝对路径，返回相对文件名或公共错误。

**验收标准：**

1. `/api/v1/copilot/tools` 能看到 `runbook.search`。
2. Copilot 输入“HighCPU 怎么排查”可规划或触发 Runbook 检索。
3. 参数非法时返回 Registry 的清晰错误。
4. 空知识库时工具返回可控错误，不造成 500 panic。

### 5.5 模块 5：Diagnosis Pipeline 与 LLM Prompt 接入

**目标：** 让诊断报告真正使用 Runbook 知识，而不是只展示指标和规则。

**Evidence 结构调整：**

```go
type RunbookEvidence struct {
    Title           string    `json:"title"`
    File            string    `json:"file"`
    Score           float64   `json:"score"`
    MatchedAlerts   []string  `json:"matched_alerts,omitempty"`
    MatchedKeywords []string  `json:"matched_keywords,omitempty"`
    MatchedMetrics  []string  `json:"matched_metrics,omitempty"`
    Snippet         string    `json:"snippet"`
    Source          string    `json:"source"`
    CollectedAt     time.Time `json:"collected_at"`
}
```

**实施步骤：**

1. 在 `diagnosis/evidence.go` 增加 `ToolRunbookSearch` 常量。
2. `EvidenceCollector.Collect` 从 4 个并发任务扩展为 5 个：
   - active alerts。
   - alert history。
   - host metrics。
   - prom query range。
   - runbook search。
3. Runbook 查询参数从告警上下文和已有证据构造：
   - `alert_name` 使用 `AlertContext.AlertName`。
   - `keywords` 包含 alert_name、severity、instance 中可用主机名、labels 中的 `job` / `namespace` / `alertname`。
   - `metrics` 根据告警类型映射，例如 HighCPU 包含 `server_monitor_cpu_usage_percent`、`server_monitor_load1`、`server_monitor_process_count`。
   - `limit` 使用配置 `RunbookSearchTopN`，默认 2。
4. Runbook 工具失败时写入 `collection_errors`：
   - `source=runbook.search`。
   - 诊断继续。
   - confidence 中 Runbook 分数按未命中处理。
5. `completedFields` 将 `evidence.Runbooks` 原样写入 `runbooks_json`。
6. `buildPrompt` 删除 Phase 3 固定说明，改为：
   - 有命中：注入 Top 2 Runbook 片段。
   - 无命中：注入 `runbooks: []` 和 `runbook_note: "No matching runbook found."`。
7. `compactEvidenceForPrompt` 限制 Runbook 片段数量和长度。
8. `RuleAnalyzer` 置信度计算：
   - 命中 Runbook 且 snippet 非空，`runbook_evidence_score=1.0`。
   - 未命中，`runbook_evidence_score=0.4`。
   - 工具失败，`runbook_evidence_score=0.3`。

**LLM Prompt 要求：**

1. 明确 Runbook 是参考知识，不是事实证据。
2. LLM 必须区分“指标证据显示”和“Runbook 建议”。
3. 建议动作必须保留风险等级和审批要求。
4. 不允许 LLM 把 Runbook 中的中风险操作描述成自动执行。

**技术要求：**

1. EvidenceCollector 仍遵守整体 45s timeout 和单工具 30s timeout。
2. Runbook 检索失败不得导致诊断失败。
3. `runbooks_json` 必须是合法 JSON 数组。
4. Prompt hash 必须随 Runbook 片段变化而变化，保证可追溯。
5. 报告详情中 `evidence_json.runbooks` 与 `runbooks_json` 内容保持一致或可解释一致。

**验收标准：**

1. 手动触发 HighCPU 诊断后，`runbooks_json` 包含 HighCPU Runbook 片段。
2. LLM Prompt 中包含命中片段；mock LLM 测试可断言输入存在 Runbook 内容。
3. 无 Runbook 或工具失败时，诊断仍 `completed`，并在 `collection_errors` 标注。
4. confidence 在命中 Runbook 时比未命中场景更高，其他证据相同情况下差异可由测试验证。

### 5.6 模块 6：前端诊断详情展示

**目标：** 让用户在诊断报告中看到 AI 建议引用了哪些 Runbook，以及这些片段来自哪里。

**展示区域：**

| 区域 | 内容 |
|---|---|
| Runbook 标题 | `title` |
| 来源 | `file`，只展示相对文件名 |
| 分数 | `score`，保留 1 位小数 |
| 命中信息 | `matched_alerts`、`matched_keywords`、`matched_metrics` |
| 片段 | `snippet`，支持折叠/展开 |
| 空状态 | “未命中匹配 Runbook，当前诊断仅基于告警、指标和规则分析。” |

**实施步骤：**

1. 在 `src/types/index.ts` 增加 Runbook 类型。
2. 在 `DiagnosisDetailPage.vue` 中解析：
   - 优先使用 `report.runbooks`。
   - 如果为空，尝试使用 `report.evidence?.runbooks`。
3. 新增 Runbook 展示区块，放在规则分析和建议动作之间。
4. 片段超过固定高度时折叠，避免页面被长文本撑开。
5. 命中关键词使用现有 badge / tag 风格展示，不引入新的 UI 库。
6. 错误或空状态要清晰，但不影响报告其他区域。

**技术要求：**

1. 使用现有 API client，不新增前端请求依赖。
2. 不改变诊断列表页的主要结构，只在必要时增加“Runbook 命中数”。
3. 不把 Markdown 原文作为 HTML 注入，避免 XSS；片段按纯文本展示。
4. 页面保持现有监控控制台风格，避免营销式卡片堆叠。

**验收标准：**

1. 命中 Runbook 的诊断详情能展示标题、来源、片段和命中关键词。
2. 未命中时显示空状态。
3. 前端构建通过。
4. 现有诊断详情的证据、规则、建议动作展示不回退。

### 5.7 模块 7：配置、部署、联调、回归与验收

**目标：** 让 Runbook 在本地、Docker Compose 和 Kubernetes/Helm 场景下都可明确配置和验证。

**新增配置建议：**

| 配置 | 默认值 | 敏感 | 说明 |
|---|---|---|---|
| `RUNBOOK_DIR` | `./runbooks` 或 `/app/runbooks` | 否 | Runbook Markdown 目录 |
| `RUNBOOK_MAX_FILES` | `100` | 否 | 最大加载文件数 |
| `RUNBOOK_MAX_FILE_BYTES` | `65536` | 否 | 单个 Markdown 最大字节数 |
| `RUNBOOK_SEARCH_TOP_N` | `2` | 否 | 诊断默认注入片段数量，最大 5 |

**实施步骤：**

1. 在 `config.Config` 中加入 Runbook 配置字段。
2. 在配置加载函数中解析环境变量，设置默认值和范围校验。
3. `api/router.go` 初始化流程：
   - 如果 `RUNBOOK_DIR` 为空，创建空 retriever。
   - 如果目录不存在，记录 warning，创建空 retriever。
   - 如果目录存在但文件非法，返回启动错误，避免发布坏知识库。
4. Docker Compose：
   - 本地开发优先使用源码目录 `server-monitor/runbooks`。
   - 容器运行时挂载为只读路径。
5. Helm：
   - 使用 ConfigMap 保存核心 Runbook。
   - Deployment 通过 volumeMount 挂载到 `RUNBOOK_DIR`。
6. 验证健康状态：
   - Tool Registry health check 中 `runbook.search=false` 表示无知识库，但不代表服务不可用。
   - 如果设计后续需要 `/readyz/full` 纳入 Runbook，可作为 Phase 4 后半段独立小提交。

**技术要求：**

1. 配置默认值不能破坏现有启动方式。
2. Secret 不参与 Runbook 配置。
3. Helm values 结构如需新增，必须最小化，不能重命名已有 values。
4. Compose/Helm 修改后必须运行对应配置校验。

**验收标准：**

1. 本地运行可加载源码目录 Runbook。
2. 容器运行可读取挂载目录。
3. Runbook 目录缺失不影响基础监控能力。
4. Runbook 文件非法时有明确日志或启动错误，便于定位。

---

## 6. 资源分配

### 6.1 角色与职责

| 角色 | 主要职责 | 预计投入 |
|---|---|---|
| 后端开发 | Runbook 包、Tool Registry 接入、Diagnosis 接入、配置、单元测试 | 4 人日 |
| 前端开发 | 诊断详情 Runbook 展示、类型补齐、构建验证 | 1.5 人日 |
| 运维/部署 | Compose/镜像/Helm Runbook 挂载方案和配置校验 | 1 人日 |
| 测试/验证 | 检索准确性、诊断联调、回归测试、敏感内容扫描 | 1.5 人日 |
| 产品/项目负责人 | Runbook 内容验收、阶段边界确认、演示数据确认 | 0.5 人日 |

单人执行时建议按“后端核心 4 天 + 内容和前端 2 天 + 部署联调 1 天 + 回归验收 1 天”推进，总周期约 7 到 8 个工作日。

### 6.2 环境与数据资源

| 资源 | 要求 |
|---|---|
| Runbook 内容 | 5 个核心 Markdown 文件，内容可公开、无敏感信息 |
| 测试告警 | HighCPU、CriticalCPU、HighMemory、HighDisk、HostDown 样例 |
| MySQL | 复用 `diagnosis_reports.runbooks_json`，不新增表 |
| Redis | 复用现有 Copilot/Alert 数据，不新增关键依赖 |
| Prometheus | 用于 Phase 3 指标证据回归 |
| LLM | 可选；Runbook 接入必须支持 mock LLM 和 rule-only 降级 |
| 前端构建环境 | 复用现有 Node/Vite 配置 |

---

## 7. 时间节点

以 8 个工作日为基准排期：

| 时间 | 里程碑 | 交付内容 | 验收方式 |
|---|---|---|---|
| D1 | Runbook 内容完成 | 5 个核心 Markdown 文件和目录约定 | 内容章节检查、敏感词扫描 |
| D2 | Loader / Parser 完成 | `copilot/runbook` 加载解析能力 | `go test ./copilot/runbook` |
| D3 | Retriever 完成 | 关键词检索、评分、Top N、snippet | 精确匹配和空结果单元测试 |
| D4 | Tool Registry 接入 | `runbook.search` 工具、schema、health check | `GET /api/v1/copilot/tools` 和工具单测 |
| D5 | Diagnosis 接入 | EvidenceCollector、Prompt、`runbooks_json`、confidence | Diagnosis 单元测试和 mock LLM 测试 |
| D6 | 前端展示完成 | 详情页 Runbook 展示、空状态、类型补齐 | `npm run build` |
| D7 | 部署配置完成 | env、Compose/Helm 挂载或镜像复制方案 | `docker compose config`、`helm lint` 按修改范围执行 |
| D8 | 联调与回归 | HighCPU 端到端诊断、现有功能回归、验收记录 | 后端测试、前端构建、手动联调记录 |

缓冲建议：预留 1 天处理 Runbook 文件在容器内路径不一致、前端 JSON 结构兼容和 LLM Prompt 长度控制问题。

---

## 8. 技术要求

### 8.1 后端要求

1. 遵循现有 `server-web` 包结构，Runbook 检索作为 `copilot/runbook` 独立包。
2. 接口定义放在使用方包中；`copilot/tool` 定义 `RunbookSearcher`，不要在实现包导出不必要接口。
3. 不新增第三方 Markdown、RAG 或搜索依赖。
4. 所有外部 I/O 使用调用方传入的 `context.Context`。
5. Tool Registry 继续承担参数校验、timeout、权限、trace 和日志职责。
6. Runbook 检索失败只影响知识片段，不影响诊断主流程。
7. JSON 字段写库前必须确保可 marshal，不能保存半截字符串。
8. Prompt 输入必须限制 Runbook 数量和片段长度。

### 8.2 前端要求

1. Runbook 片段按纯文本展示，不使用 `v-html`。
2. 长片段折叠，避免撑坏诊断详情布局。
3. 空状态和错误状态清晰可见，但不覆盖诊断摘要、规则分析和建议动作。
4. 类型定义与后端 JSON 字段一致，避免大量 `any`。

### 8.3 配置和部署要求

1. `RUNBOOK_DIR` 默认值必须适配本地开发。
2. 容器镜像或 Compose 必须有明确方式读取 Runbook 文件。
3. Helm 新增 values 不得改变已有 key 语义。
4. Runbook ConfigMap 不存放敏感信息。
5. Runbook 文件变更应能通过重新部署 server-web 生效；本阶段不要求热加载。

### 8.4 安全要求

1. Runbook 是只读知识，不触发写操作。
2. 建议动作只能作为文本建议进入诊断报告。
3. 中高风险动作必须保留“需要审批”措辞。
4. 工具返回值不暴露宿主机绝对路径。
5. Runbook 内容进入 LLM Prompt 前不包含 Secret、Token、Password。

### 8.5 性能要求

1. 启动加载 Runbook 目标耗时小于 1 秒。
2. 单次 `runbook.search` 目标耗时小于 50ms。
3. 默认注入 Top 2 片段，每个 snippet 不超过 800 字符。
4. EvidenceCollector 总 timeout 保持 45s，不因 Runbook 增加整体等待上限。
5. 空知识库和未命中场景不额外调用 LLM 重试。

---

## 9. 测试方案

### 9.1 单元测试

| 模块 | 测试重点 | Mock / Fixture |
|---|---|---|
| Runbook Parser | 标题、适用告警、关键词、关键指标、章节提取 | 内联 Markdown 字符串 |
| Runbook Loader | 目录加载、忽略非 md、空目录、文件超限、非法模板 | `t.TempDir()` |
| Retriever | 精确匹配、关键词匹配、指标匹配、排序、Top N、空结果 | 内存文档 |
| Runbook Tool | schema、参数解析、limit 范围、空知识库、成功返回 | fake searcher |
| EvidenceCollector | 调用 `runbook.search`、失败写入 `collection_errors`、命中写入 Runbooks | fake ToolRunner |
| RuleAnalyzer | Runbook 命中提升 confidence，失败或未命中降级 | 构造 EvidenceBundle |
| LLMSummarizer | Prompt 包含 Runbook 片段，prompt hash 随片段变化 | mock LLM |
| Diagnosis Service | `runbooks_json` 写入真实数组而不是固定空数组 | mock collector / repo |

### 9.2 集成测试

| 场景 | 验证点 |
|---|---|
| `runbook.search` 工具列表 | `GET /api/v1/copilot/tools` 包含工具 schema |
| HighCPU 手动诊断 | 报告包含 CPU 指标、规则分析和 HighCPU Runbook |
| Runbook 目录缺失 | Copilot 和 Diagnosis API 仍可用，报告标注未命中 |
| 非法 Runbook 文件 | 启动或初始化返回清晰错误 |
| LLM 不可用 | rule-only 报告仍包含 Runbook 片段 |

### 9.3 回归测试

1. `POST /api/v1/copilot/chat` 原有只读查询仍可用。
2. `GET /api/v1/copilot/tools` 原有工具仍存在。
3. `POST /api/v1/diagnosis` 仍可在无 LLM 时生成规则降级报告。
4. `GET /api/v1/diagnosis/:id` 旧报告中 `runbooks_json=[]` 时前端不报错。
5. `GET /api/v1/alerts/active`、`GET /api/v1/alert-histories`、`GET /api/v1/hosts` 不回退。
6. Alertmanager Webhook 不读取 Runbook，不增加同步延迟。

### 9.4 推荐执行命令

后端 Go 代码修改后：

```bash
cd server-monitor/server-web
goimports -w copilot/runbook/*.go copilot/tool/*.go copilot/diagnosis/*.go api/router.go config/config.go
go test ./copilot/runbook ./copilot/tool ./copilot/diagnosis ./api ./config
go test ./...
go vet ./...
```

前端修改后：

```bash
cd server-monitor/frontend
npm run build
```

Runbook 内容检查：

```bash
rg -n "password|token|secret|AKIA|BEGIN .*PRIVATE" server-monitor/runbooks
```

如果修改 Compose：

```bash
cd server-monitor
docker compose config
```

如果修改 Helm：

```bash
cd server-monitor
helm lint charts/server-monitor
```

---

## 10. 风险评估与应对措施

| 风险 | 影响 | 概率 | 应对措施 |
|---|---|---|---|
| Runbook 内容质量不足 | LLM 建议泛化，用户信任下降 | 中 | 使用统一模板；核心告警逐条验收；要求每篇包含指标、步骤、风险 |
| 检索误命中 | 诊断引用不相关知识 | 中 | 精确告警名权重最高；展示 score 和命中原因；Top N 默认 2 |
| Runbook 目录在容器内不存在 | 工具不可用，诊断缺少知识片段 | 中 | 配置默认值明确；Compose/Helm 挂载；空目录降级不阻断服务 |
| Markdown 解析过度复杂 | 代码难测、引入额外依赖 | 低 | 只支持约定模板和二级标题，按行扫描，不做完整 Markdown AST |
| Prompt 过长 | LLM 调用成本和失败率上升 | 中 | snippet 限长，Top N 限制，Prompt compact 阶段二次截断 |
| Runbook 含敏感信息 | 敏感信息进入前端或 LLM Prompt | 中 | 内容审查、敏感词扫描、禁止真实凭据、只展示相对文件名 |
| 旧报告结构兼容问题 | 前端详情页报错 | 中 | 前端兼容 `runbooks=[]`、`null`、缺字段；类型解析兜底 |
| confidence 变化引发误解 | 用户认为命中 Runbook 就是高置信根因 | 中 | 文案区分“知识建议”和“事实证据”；置信度仍由多源证据加权 |
| 阶段越界到向量库或审批 | 工期失控 | 高 | 明确 Phase 4 不做 BM25/Embedding/审批/Worker；只预留接口 |
| 测试依赖真实文件路径 | CI 或本地环境不稳定 | 低 | 单元测试使用 `t.TempDir()` 和内存文档 |

---

## 11. 验收标准

Phase 4 完成时必须满足：

1. `server-monitor/runbooks/` 至少包含 HighCPU、CriticalCPU、HighMemory、HighDisk、HostDown 五个 Runbook。
2. `runbook.search` 注册到 Tool Registry，schema 可通过 Copilot tools API 查看。
3. `runbook.search` 可按 `alert_name=HighCPU` 返回 HighCPU Runbook Top 1。
4. EvidenceCollector 在诊断时调用 `runbook.search`，命中结果进入 `EvidenceBundle.runbooks`。
5. 新生成的诊断报告 `runbooks_json` 包含命中 Runbook 片段；未命中时为合法空数组。
6. LLM Prompt 包含 Runbook 片段，且 LLM 不可用时 rule-only 报告仍可展示 Runbook。
7. 诊断详情页展示 Runbook 标题、来源文件、命中关键词、score 和 snippet。
8. 空知识库、未命中、工具失败都不会导致诊断 API 500。
9. 旧诊断报告仍可正常打开。
10. 不引入新的第三方依赖。
11. 不改变现有 API 外层响应结构、数据库 schema 兼容字段、Prometheus 指标名、Helm 既有 values 语义。
12. 后端测试、前端构建和必要配置校验有真实执行记录。

---

## 12. 建议提交拆分

Phase 4 建议拆成以下提交，便于 Review 和回滚：

1. `docs: add initial cloudops runbooks`
2. `feat: load and parse runbook markdown`
3. `feat: add keyword runbook retriever`
4. `feat: register runbook search tool`
5. `feat: attach runbooks to diagnosis reports`
6. `feat: show runbooks in diagnosis detail`
7. `chore: wire runbook deployment config`
8. `test: cover runbook diagnosis flow`
9. `docs: record phase 4 realization`

每个提交都应包含对应测试或明确说明未执行原因，不把 Runbook 内容、后端接口、前端页面和部署配置全部混成一个不可审查的大提交。

---

## 13. 阶段完成后的后续衔接

| 后续阶段 | 衔接点 |
|---|---|
| Phase 5 异步诊断 Worker | 复用已接入 Runbook 的 Diagnosis Service，Kafka 自动触发后报告天然包含知识片段 |
| Phase 6 动作审批与审计 | Runbook 的风险说明可辅助 `ActionAdvisor` 判断建议动作是否需要审批 |
| Phase 7 Kubernetes 深度接入 | 增加 K8s Runbook，如 CrashLoopBackOff、DeploymentUnavailable，并接入 K8s events/logs 工具 |
| RAG 优化阶段 | 在不改变 `RunbookSearcher` 接口的前提下，将关键词检索替换为 BM25 或 Embedding 检索 |

Phase 4 的核心价值是把“诊断报告”从纯指标和规则分析，升级为“证据 + 规则 + 运维知识”的知识增强诊断。只要 `runbook.search` 的接口、结果结构和降级策略稳定，后续自动诊断、审批和 K8s 深度集成都可以复用这一层知识能力。
