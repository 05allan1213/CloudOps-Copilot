# CloudOps Copilot Phase 3 实施方案

> 方案版本：v1.0
> 制定日期：2026-05-08
> 依据文档：`docs/design.md` v3.1
> 阶段定位：告警诊断报告，在 Phase 1 Copilot Chat API 与 Phase 2 Tool Registry 基础上，支持对单条告警生成、持久化和展示结构化诊断报告。

---

## 1. 阶段目标

Phase 3 的目标是实现 CloudOps Copilot 的第一版告警诊断闭环：用户可以从告警详情、告警列表或 Copilot 对话中选择一条具体告警，手动触发诊断；后端基于真实告警上下文、历史告警、主机指标和确定性规则生成结构化诊断报告；报告持久化到 MySQL，并可在前端查看证据来源、规则分析、置信度和建议动作。

本阶段强调“基于证据的诊断”，不追求自动化执行。LLM 只负责在证据和规则分析之上进行归纳，不直接决定写操作，不读取 Secret，不直接执行 K8s 变更。

### 1.1 核心交付物

| 交付物 | 内容 | 验收标准 |
|---|---|---|
| `DiagnosisReport` 数据模型 | 保存告警上下文、证据快照、规则分析、LLM 输出、置信度和状态 | 报告可创建、更新、查询，字段可复现一次诊断输入 |
| EvidenceCollector | 聚合单条告警详情、活跃告警状态、告警历史、主机指标和补充 PromQL 结果 | 工具超时或部分失败时不中断诊断，报告标注证据缺失 |
| RuleAnalyzer | 对 HighCPU、CriticalCPU、HighMemory、HighDisk、HostDown 进行确定性分析 | 无 LLM 时仍能生成规则分析摘要 |
| LLMSummarizer | 基于 evidence、rule_analysis 生成结构化报告 | 输出经过 JSON 解析、Schema 校验和降级处理 |
| Diagnosis API | `POST /api/v1/diagnosis`、`GET /api/v1/diagnosis`、`GET /api/v1/diagnosis/:id` | 登录用户可触发和查看诊断，越权访问被拒绝 |
| Copilot 诊断衔接 | `diagnosis_request` 意图可触发单条告警诊断 | 用户输入告警名、instance 或 fingerprint 后能返回诊断结果或澄清问题 |
| 前端诊断页面 | 诊断列表、详情页、告警页入口 | 前端能展示状态、摘要、证据、规则、建议和错误信息 |
| 测试与验收记录 | 单元测试、接口测试、前端构建和回归检查 | 不影响现有告警、主机、Copilot Chat 和 WebSocket 链路 |

### 1.2 本阶段不做

1. 不实现 Markdown Runbook 检索；`runbooks_json` 字段保留为空数组或空对象，Phase 4 接入。
2. 不实现 Kafka Diagnosis Worker；Phase 3 只做手动触发和 Copilot 对话触发，Phase 5 再消费 `alert-events`。
3. 不在 Alertmanager Webhook 同步请求中调用 LLM，避免阻塞告警接收链路。
4. 不实现 PendingAction、审批、审计和写操作执行；建议动作仅作为报告中的文本化建议。
5. 不新增 Kubernetes 只读或写操作工具。
6. 不引入 LangChain、LlamaIndex、向量数据库或独立 ChatOps 微服务。
7. 不改变现有告警 Webhook、主机指标、告警历史和 Copilot Chat 的兼容响应结构。

---

## 2. 范围边界与前置条件

### 2.1 前置条件

Phase 3 默认 Phase 1 和 Phase 2 已提供以下能力：

| 前置能力 | 说明 |
|---|---|
| Copilot Chat API | `POST /api/v1/copilot/chat` 已接入登录态、会话和基础意图识别 |
| Tool Registry | 只读工具已通过统一 `ToolRegistry.Execute` 执行 |
| 只读工具 | `alert.list_active`、`alert.history`、`host.metrics`、`prom.query_range` 可用 |
| LLM Client | 已具备 OpenAI 兼容 LLM 调用、timeout、JSON 解析基础 |
| JWT / RBAC | `server-web` 已能区分登录用户和角色 |
| MySQL / Redis / Prometheus | 复用 `server-monitor` 现有连接池和客户端 |
| 前端路由 | Vue 前端已有页面、API client、类型和路由结构 |

如果 Phase 1/2 某些工具尚未完成，Phase 3 可先用接口 mock 推进模型、规则和 API，但最终验收必须回到真实工具链。

### 2.2 诊断对象范围

Phase 3 只支持“单条告警诊断”：

| 输入方式 | 必填定位信息 | 处理方式 |
|---|---|---|
| 告警详情页触发 | `fingerprint` 或 `alert_history_id` | 直接定位告警 |
| 告警列表触发 | `fingerprint` | 从活跃告警或历史表补齐上下文 |
| Copilot 对话触发 | `alert_name` + `instance`，或 `fingerprint` | 唯一匹配则诊断，多条匹配则追问 |

不支持一次性批量诊断多个告警。若用户请求“分析所有告警”，本阶段返回可选择的告警列表并要求用户确认单条告警。

### 2.3 数据来源优先级

| 数据 | 优先来源 | 辅助来源 | 说明 |
|---|---|---|---|
| 告警详情 | `alert_histories` 或 `alert:events` 中完整 Alertmanager payload | `alert:active` | 优先保留 labels、annotations、starts_at、ends_at |
| 活跃状态 | Redis `alert:active` | MySQL 最近状态 | 判断是否仍在 firing |
| 历史模式 | MySQL `alert_histories` | 无 | 统计 recurrence_count、最近触发时间 |
| 指标趋势 | Prometheus / VictoriaMetrics 查询客户端 | `host.metrics` 工具 | CPU、内存、磁盘、load、up 等 |
| Runbook | Phase 3 不接入 | Phase 4 接入 | 字段保留，报告展示“未接入 Runbook” |

---

## 3. 总体实施路径

Phase 3 拆为 7 个小模块推进，每个模块都能单独测试和单独提交。

```text
模块 1：DiagnosisReport 模型与数据访问
  ↓
模块 2：诊断请求解析与告警上下文定位
  ↓
模块 3：EvidenceCollector 多源证据采集
  ↓
模块 4：RuleAnalyzer 确定性分析与置信度计算
  ↓
模块 5：LLMSummarizer 与报告生成
  ↓
模块 6：Diagnosis API 与 Copilot 对话接入
  ↓
模块 7：前端诊断页面、联调、回归与验收
```

---

## 4. 详细实施步骤

### 4.1 模块 1：DiagnosisReport 模型与数据访问

**目标：** 建立诊断报告的持久化模型和 repository/service 基础，不接入 LLM。

**实施步骤：**

1. 在 `server-monitor/server-web/model` 中新增 `DiagnosisReport` 模型。
2. 字段与 `docs/design.md` 保持一致：
   - `alert_history_id`
   - `fingerprint`
   - `alert_name`
   - `target_kind`
   - `target_name`
   - `namespace`
   - `severity`
   - `status`
   - `summary`
   - `root_cause`
   - `evidence_json`
   - `runbooks_json`
   - `recommended_actions_json`
   - `rule_analysis_json`
   - `confidence`
   - `llm_prompt_hash`
   - `llm_model`
   - `trigger_type`
   - `created_by`
   - `created_at`
   - `updated_at`
3. 在数据库初始化或迁移入口注册模型自动迁移。
4. 在 `server-monitor/server-web/copilot/service` 或新的 `copilot/diagnosis` 子包中实现报告 repository：
   - `Create(ctx, report)`
   - `UpdateStatus(ctx, id, status, fields)`
   - `GetByID(ctx, id, user)`
   - `List(ctx, filters, page, pageSize)`
   - `FindLatestByFingerprint(ctx, fingerprint)`
5. 对 JSON 字段统一使用字符串或 `datatypes.JSON`，按项目现有 GORM 风格选择，不额外引入新依赖。
6. 增加索引：
   - `fingerprint`
   - `alert_name`
   - `status`
   - `created_by`
   - `created_at`
   - `alert_history_id`

**预计修改文件：**

| 文件/目录 | 说明 |
|---|---|
| `server-monitor/server-web/model/...` | 新增诊断报告模型 |
| `server-monitor/server-web/database/...` | 注册自动迁移或初始化 |
| `server-monitor/server-web/copilot/service/...` | 新增报告 repository/service |
| `server-monitor/server-web/copilot/service/..._test.go` | repository/service 单元测试 |

**技术要求：**

1. `status` 只允许 `pending/running/completed/failed`。
2. `confidence` 范围为 `0~1`。
3. JSON 字段保存前必须保证是合法 JSON，不能保存半截字符串。
4. `created_by` 来自 JWT user_id，不能由请求体传入。
5. 查询详情时，普通用户只能查看自己触发的报告；admin 可查看全部。

**验收标准：**

1. 可创建 `pending` 报告。
2. 可更新为 `running/completed/failed`。
3. 列表支持分页和状态过滤。
4. 非创建者访问详情返回 403，admin 例外。

### 4.2 模块 2：诊断请求解析与告警上下文定位

**目标：** 将用户输入解析为唯一告警上下文，为后续证据采集提供稳定输入。

**API 请求结构：**

```json
{
  "fingerprint": "abc123",
  "alert_history_id": 42,
  "alert_name": "HighCPU",
  "instance": "node-1:9090",
  "trigger_type": "manual"
}
```

字段规则：

1. `fingerprint`、`alert_history_id`、`alert_name + instance` 三组选项至少提供一组。
2. `trigger_type` 只能为 `manual` 或 `chat`；API 页面触发默认 `manual`。
3. 当多个告警匹配 `alert_name + instance` 时，返回 409，并给出候选告警列表。
4. 当告警不存在时，返回 404。

**实施步骤：**

1. 定义 `DiagnosisRequest`、`AlertContext`、`DiagnosisCandidate`。
2. 实现 `AlertContextResolver`：
   - 优先按 `alert_history_id` 查询 MySQL。
   - 其次按 `fingerprint` 查询活跃告警和历史告警。
   - 最后按 `alert_name + instance` 查询最近 firing 或最近历史。
3. 从告警上下文提取：
   - `fingerprint`
   - `alert_name`
   - `instance`
   - `severity`
   - `labels`
   - `annotations`
   - `starts_at`
   - `status`
4. 对 Redis `alert:active` 与 MySQL `alert_histories` 的字段差异做适配，输出统一 `AlertContext`。
5. 记录 `source` 和 `collected_at`，后续写入 `evidence_json`。
6. 单元测试覆盖唯一匹配、多条匹配、无匹配、缺少参数、非法 `trigger_type`。

**技术要求：**

1. Resolver 不直接依赖 Gin；只接收 `context.Context` 和结构化参数。
2. 查询 Redis/MySQL 必须带 timeout。
3. 不把 Alertmanager 原始 payload 直接返回前端；先做字段筛选和脱敏。
4. 如果 `alert:active` 数据字段不完整，必须从 `alert_histories` 补齐，不静默生成缺字段报告。

**验收标准：**

1. 告警详情页传 `fingerprint` 可定位告警。
2. Copilot 传 `alert_name + instance` 唯一时可定位告警。
3. 多条匹配时不随机选择，返回候选列表。
4. Resolver 输出包含证据来源和采集时间。

### 4.3 模块 3：EvidenceCollector 多源证据采集

**目标：** 基于单条告警并行采集诊断所需证据，形成统一 Evidence Bundle。

**Evidence Bundle 结构：**

```json
{
  "alert_context": {},
  "active_alerts": [],
  "metrics": [],
  "history": [],
  "runbooks": [],
  "collection_errors": [],
  "collected_at": "2026-05-08T10:00:00Z"
}
```

**实施步骤：**

1. 定义 `EvidenceBundle`、`MetricEvidence`、`HistoryEvidence`、`CollectionError`。
2. 基于 `AlertContext` 选择采集项：
   - `alert.list_active`：获取当前活跃状态和关联告警。
   - `alert.history`：查询同 fingerprint、同 alert_name、同 instance 的历史记录。
   - `host.metrics`：获取目标 instance 的基础趋势。
   - `prom.query_range`：按告警类型补充指标。
3. 为不同告警选择指标：
   - `HighCPU/CriticalCPU`：CPU、load1、process_count。
   - `HighMemory`：memory usage、available memory。
   - `HighDisk`：disk usage、mountpoint 相关指标。
   - `HostDown`：`up{job="server-probe"}`。
4. 采集并发控制：
   - 独立工具并行执行。
   - 单工具 timeout 默认 30 秒，遵守 Tool Registry schema。
   - 整体 evidence 采集 timeout 默认 45 秒。
5. 失败降级：
   - 单个工具失败写入 `collection_errors`。
   - 只要 `alert_context` 存在，诊断继续。
   - 没有任何指标时降低置信度，不返回 500。
6. 结果压缩：
   - 每类指标只保留 `max/avg/last/trend/window`。
   - 原始 Prometheus 点位不完整写入报告，避免报告过大。
7. Phase 3 `runbooks` 固定为空数组，并在报告中标注“Runbook 将在 Phase 4 接入”。

**预计修改文件：**

| 文件/目录 | 说明 |
|---|---|
| `server-monitor/server-web/copilot/service/...` | EvidenceCollector 实现 |
| `server-monitor/server-web/copilot/tool/...` | 复用 Tool Registry 执行只读工具 |
| `server-monitor/server-web/copilot/service/..._test.go` | 证据采集单元测试 |

**技术要求：**

1. 不直接绕过 Tool Registry 调用工具，保证参数校验、超时和日志一致。
2. PromQL 查询必须遵守 Phase 2 的安全限制：时间范围、步长、点数、响应大小。
3. 证据中不能包含 Secret、Token、Password。
4. `evidence_json` 必须包含每个证据块的 `source` 和 `collected_at`。

**验收标准：**

1. `HighCPU` 能采集 CPU/load/process 证据。
2. `HighMemory` 能采集内存趋势证据。
3. `HostDown` 能采集 `up` 状态证据。
4. Prometheus 超时时诊断仍完成，并在报告中标注指标不完整。

### 4.4 模块 4：RuleAnalyzer 确定性分析与置信度计算

**目标：** 在 LLM 前先生成可解释的规则分析，降低幻觉和 Token 成本。

**实施步骤：**

1. 定义 `RuleAnalyzer` 接口：
   - `Analyze(ctx, alertContext, evidence) RuleAnalysis`
2. 定义 `RuleResult`：
   - `rule`
   - `passed`
   - `detail`
   - `evidence_refs`
3. 实现规则：
   - `cpu_sustained_high`
   - `load_correlated`
   - `memory_sustained_high`
   - `disk_usage_high`
   - `host_unreachable`
   - `history_recurring`
   - `evidence_incomplete`
4. 规则示例：
   - CPU 15m avg >= 80% 且 max >= 90%，判定 CPU 持续高位。
   - load1 与 CPU 同时升高，判定负载相关。
   - 同 fingerprint 24 小时内重复触发 >= 2 次，判定重复告警。
   - 关键指标缺失时生成 `evidence_incomplete`，不假装数据正常。
5. 计算诊断置信度：
   - `alert_evidence_score` 权重 0.3。
   - `metric_evidence_score` 权重 0.3。
   - `history_evidence_score` 权重 0.2。
   - `runbook_evidence_score` 权重 0.2；Phase 3 固定按 0.4 计算，因为 Runbook 未接入。
6. 输出 `confidence` 和等级：
   - `>= 0.8`：High。
   - `0.5 ~ 0.8`：Medium。
   - `< 0.5`：Low。

**技术要求：**

1. RuleAnalyzer 必须是纯逻辑，方便表驱动测试。
2. 规则 detail 必须引用真实证据字段，不写笼统判断。
3. 规则阈值先使用设计文档默认值，不新增复杂配置；如必须配置化，单独说明并进入后续阶段。
4. 规则未覆盖的告警类型返回通用分析，而不是失败。

**验收标准：**

1. 表驱动测试覆盖 HighCPU、HighMemory、HighDisk、HostDown。
2. 缺少指标时置信度下降。
3. 历史重复触发会提高历史证据评分。
4. 无 LLM 时仍可生成规则摘要和 next_steps。

### 4.5 模块 5：LLMSummarizer 与报告生成

**目标：** 基于证据和规则生成结构化诊断报告，并保证 LLM 不可信输出不会污染系统。

**LLM 输入：**

1. 当前告警 `alert_context`。
2. 压缩后的 `evidence_json`。
3. `rule_analysis_json`。
4. 空 `runbook_snippets`，并声明 Phase 3 未接入 Runbook。
5. 输出 Schema 约束。

**LLM 输出结构：**

```json
{
  "summary": "主机 node-1 CPU 使用率持续过高，load 同步升高",
  "severity_assessment": "warning",
  "root_cause_hypotheses": [
    {
      "cause": "业务负载增加导致 CPU 消耗升高",
      "confidence": "medium",
      "evidence": ["CPU 15m avg=86.7%", "load1 同步升高"]
    }
  ],
  "recommended_actions": [
    {
      "type": "inspect_process",
      "description": "查看 CPU 占用 Top 进程",
      "risk": "low",
      "requires_approval": false
    }
  ],
  "next_steps": ["查看 CPU Top 进程", "确认近期发布或流量变化"]
}
```

**实施步骤：**

1. 定义 `DiagnosisSummary` 输出结构。
2. 实现 Prompt Builder：
   - 固定 System Prompt。
   - 注入 evidence 和 rule analysis。
   - 明确要求“只基于证据，不做无依据推测”。
   - 明确“写操作只建议，不执行”。
3. 实现 LLM 输出解析：
   - 优先解析纯 JSON。
   - 其次提取 Markdown 代码块中的 JSON。
   - 字段缺失时返回可控错误。
4. 实现降级：
   - LLM 超时：使用 RuleAnalyzer 摘要生成报告，`llm_model` 标记为空或 `rule-only`。
   - LLM 格式错误：保存 failed reason，同时生成规则降级报告。
   - LLM API 错误：报告状态可为 `completed`，但 `summary` 明确为规则分析结果，`collection_errors` 记录 LLM 失败。
5. 生成 `llm_prompt_hash`：
   - 基于 system prompt、evidence hash、rule analysis hash。
   - 不包含明文 API key 或用户 token。
6. 写入 `DiagnosisReport`：
   - `status=running` 开始。
   - 成功或规则降级成功时 `status=completed`。
   - 告警上下文无法定位或数据库不可写时 `status=failed`。

**技术要求：**

1. LLM HTTP Client 必须设置 timeout，默认 60 秒。
2. 不把 Secret、Token、Password 注入 Prompt。
3. Prompt 输入控制在 4096 tokens 以内，证据按优先级截断。
4. LLM 输出不允许创建 PendingAction；Phase 3 只保存建议动作文本。
5. 所有错误写入日志时要带上下文，但不能包含敏感内容。

**验收标准：**

1. LLM 正常时生成结构化报告。
2. LLM 超时时生成规则降级报告。
3. LLM 返回非法 JSON 时不会 500。
4. 报告中能看到 evidence、rule_analysis、confidence、recommended_actions。

### 4.6 模块 6：Diagnosis API 与 Copilot 对话接入

**目标：** 提供前端和 Copilot 可调用的诊断接口。

**API 设计：**

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `POST` | `/api/v1/diagnosis` | 登录 | 手动触发单条告警诊断 |
| `GET` | `/api/v1/diagnosis` | 登录 | 诊断报告列表 |
| `GET` | `/api/v1/diagnosis/:id` | 登录 | 诊断报告详情 |

**POST 响应：**

```json
{
  "id": 42,
  "status": "completed",
  "summary": "CPU 持续高位，load 同步升高",
  "confidence": 0.76,
  "confidence_level": "medium"
}
```

**实施步骤：**

1. 新增 Diagnosis handler。
2. 路由接入现有 JWT 中间件。
3. 请求校验：
   - 至少一种告警定位信息。
   - `trigger_type` 合法。
   - 分页参数范围合法。
4. API 触发流程：
   - 创建 `pending` 报告。
   - 更新为 `running`。
   - 执行 Resolver、EvidenceCollector、RuleAnalyzer、LLMSummarizer。
   - 更新为 `completed` 或 `failed`。
   - 返回报告摘要。
5. Copilot 接入：
   - `diagnosis_request` 意图进入 Resolver。
   - 单条匹配时触发诊断。
   - 多条匹配时返回候选告警，让用户选择。
   - 完成后在 Chat 回复中附带 `diagnosis_id` 和查看建议。
6. 错误映射：
   - 参数错误：400。
   - 告警不存在：404。
   - 多条匹配：409。
   - LLM 降级成功：200，报告中标注降级。
   - 数据库写入失败：500。

**技术要求：**

1. Handler 不堆业务逻辑，诊断编排放 service。
2. API 响应不暴露内部错误栈。
3. 触发诊断时不能阻塞 Alertmanager Webhook。
4. 本阶段手动 API 可以同步等待诊断完成；若耗时超过前端体验阈值，后续 Phase 5 改异步。

**验收标准：**

1. 登录用户可触发诊断。
2. 未登录返回 401。
3. 普通用户不能查看其他用户触发的报告。
4. Copilot 输入“帮我分析 node-1 的 HighCPU 告警”能触发或给出候选告警。

### 4.7 模块 7：前端诊断页面、联调、回归与验收

**目标：** 在现有 Vue 控制台中展示诊断报告，并从告警页面形成入口。

**实施步骤：**

1. 在 `server-monitor/frontend/src/api` 新增 `diagnosis.ts`。
2. 在 `server-monitor/frontend/src/types/index.ts` 增加诊断相关类型：
   - `DiagnosisReport`
   - `DiagnosisEvidence`
   - `RuleAnalysis`
   - `RecommendedAction`
3. 在 `server-monitor/frontend/src/pages` 新增：
   - `DiagnosisListPage.vue`
   - `DiagnosisDetailPage.vue`
4. 在路由中新增：
   - `/diagnosis`
   - `/diagnosis/:id`
5. 在告警列表或告警历史页增加“生成诊断”入口。
6. 在 Copilot 页面识别 `diagnosis_id`，展示“查看诊断报告”入口。
7. 页面展示内容：
   - 报告状态。
   - 摘要与严重程度。
   - 置信度和置信度等级。
   - 根因假设。
   - 证据来源和采集时间。
   - 规则分析。
   - 建议动作，标注风险和是否需要审批。
   - 错误或降级原因。
8. 前端体验要求：
   - 触发按钮有 loading 状态。
   - 失败时展示可理解错误。
   - 长 JSON 证据折叠显示，避免页面失控。
   - 不展示敏感字段。

**技术要求：**

1. 使用现有 API client 和鉴权方式，不新增前端请求库。
2. 页面风格保持现有监控控制台风格，不做营销式页面。
3. 大块证据使用折叠区域或表格展示，优先可读性。
4. 诊断报告中的建议动作只展示，不提供执行按钮。

**验收标准：**

1. 告警页可触发诊断并跳转详情。
2. 诊断列表分页可用。
3. 诊断详情能展示证据、规则和建议。
4. 前端构建通过，现有页面路由不回退。

---

## 5. 资源分配

### 5.1 角色与职责

| 角色 | 主要职责 | 预计投入 |
|---|---|---|
| 后端开发 | 模型、Resolver、EvidenceCollector、RuleAnalyzer、LLM、API、测试 | 6 人日 |
| 前端开发 | 诊断 API client、列表页、详情页、告警入口、Copilot 衔接 | 3 人日 |
| 测试/验证 | 单元测试、接口测试、回归测试、边界场景验证 | 2 人日 |
| 运维/部署 | 环境变量检查、Compose/Helm 影响评估、数据库迁移验证 | 1 人日 |
| 产品/项目负责人 | 验收标准确认、报告展示内容确认、阶段边界控制 | 1 人日 |

若由单人完成，建议按“后端核心 6 天 + 前端 3 天 + 联调验收 2 天”的顺序推进，总周期约 10~12 个工作日。

### 5.2 资源要求

| 资源 | 要求 |
|---|---|
| LLM API Key | 使用环境变量或 Secret 注入，不写入代码和普通文档示例 |
| MySQL | 支持新增 `diagnosis_reports` 表 |
| Redis | 复用现有连接，用于读取活跃告警和可选短缓存 |
| Prometheus / VictoriaMetrics | 可查询主机指标和告警关联指标 |
| 前端构建环境 | 复用现有 Vue 3 + TS + Vite |
| 测试数据 | 至少准备 HighCPU、HighMemory、HighDisk、HostDown 样例告警 |

---

## 6. 时间节点

以 10 个工作日为基准排期：

| 时间 | 里程碑 | 交付内容 | 验收方式 |
|---|---|---|---|
| D1 | 模型与迁移完成 | `DiagnosisReport`、repository、基础测试 | `go test` 覆盖模型和 repository |
| D2 | 告警定位完成 | Resolver 支持 fingerprint、history_id、alert_name+instance | Resolver 表驱动测试 |
| D3-D4 | 证据采集完成 | EvidenceCollector 接入 Tool Registry | mock 工具测试和 Prometheus 超时降级测试 |
| D5 | 规则分析完成 | RuleAnalyzer 和 confidence 计算 | HighCPU/HighMemory/HighDisk/HostDown 单元测试 |
| D6 | LLM 总结完成 | Prompt Builder、JSON 解析、规则降级 | Mock LLM 正常/超时/非法 JSON 测试 |
| D7 | API 完成 | POST/GET diagnosis，权限和错误映射 | httptest 接口测试 |
| D8-D9 | 前端完成 | 列表页、详情页、告警入口、Copilot 入口 | 前端构建和手动联调 |
| D10 | 回归与验收 | 端到端诊断、现有功能回归、文档记录 | 验收清单全部通过或记录未通过项 |

缓冲建议：预留 1~2 天处理现有数据字段不一致、Prometheus 查询边界和 LLM 输出不稳定问题。

---

## 7. 技术要求

### 7.1 后端技术要求

1. 复用 `server-web` 现有 Gin、GORM、Redis、Prometheus、JWT、RBAC、Logger、Trace。
2. 诊断编排必须放在 service 层，不在 handler 中堆业务逻辑。
3. 所有外部调用必须接收 `context.Context` 并设置 timeout。
4. LLM 输出必须经过结构化解析和 Schema 校验。
5. Evidence、Rule、LLM 三部分要能分别 mock，保证单元测试可控。
6. 报告必须保存证据快照，保证后续可复现。
7. 错误处理要区分用户输入错误、数据不存在、外部依赖失败和系统错误。

### 7.2 前端技术要求

1. 复用现有 API client、路由、鉴权和页面布局。
2. 诊断详情页必须展示证据来源和采集时间。
3. 报告 JSON 不直接整块铺满页面，使用结构化区域展示。
4. 建议动作只显示风险和审批需求，不提供执行入口。
5. 所有 loading、empty、error 状态都要完整。

### 7.3 安全要求

1. ChatOps/Diagnosis API 全部要求登录。
2. `created_by` 从 JWT 中读取，不能信任请求体。
3. 普通用户只能查看自己触发的诊断报告；admin 可查看全部。
4. LLM Prompt 不注入 Secret、Token、Password。
5. LLM 生成的建议不直接转为可执行动作。
6. 报告中必须标注数据来源，避免把推测包装成事实。

### 7.4 性能要求

1. 单工具 timeout 默认 30 秒。
2. EvidenceCollector 整体 timeout 默认 45 秒。
3. LLM timeout 默认 60 秒。
4. 手动诊断总耗时目标控制在 60 秒以内；超时则规则降级。
5. Prometheus 查询点数、步长和响应大小沿用 Phase 2 限制。

---

## 8. 测试方案

### 8.1 单元测试

| 模块 | 测试重点 | Mock 方式 |
|---|---|---|
| DiagnosisReport repository | 创建、更新、分页、权限过滤 | SQLite 或项目现有 DB mock 方式 |
| AlertContextResolver | 参数校验、唯一匹配、多条匹配、无匹配 | Mock alert/history reader |
| EvidenceCollector | 并发采集、单工具失败、Prometheus 超时、证据压缩 | Mock Tool Registry |
| RuleAnalyzer | 各告警规则、置信度计算、证据缺失降级 | 构造 Evidence Bundle |
| LLMSummarizer | Prompt 构造、JSON 解析、非法输出、超时降级 | Mock LLM Client |
| Diagnosis Service | pending/running/completed/failed 状态流转 | Mock resolver/evidence/rule/llm/repository |
| Handler | 401、400、404、409、200 | httptest |

### 8.2 集成测试

| 场景 | 验证点 |
|---|---|
| 手动触发 HighCPU 诊断 | 报告落库，包含 CPU/load 证据 |
| Prometheus 超时 | 报告完成，标注指标证据缺失 |
| LLM 不可用 | 规则降级报告完成 |
| 普通用户访问他人报告 | 返回 403 |
| Copilot 触发诊断 | 返回 `diagnosis_id` 和报告摘要 |

### 8.3 回归测试

1. `POST /api/v1/copilot/chat` 原有只读查询仍可用。
2. `GET /api/v1/alerts/active` 仍可用。
3. `GET /api/v1/alert-histories` 仍可用。
4. `GET /api/v1/hosts` 和主机指标页仍可用。
5. `/ws/alerts` 告警推送不受影响。
6. Alertmanager Webhook 不调用 LLM，不增加同步阻塞。

### 8.4 推荐执行命令

后端修改后默认执行：

```bash
goimports -w <本次修改的 Go 文件>
go test ./...
go vet ./...
```

如果 `server-web` 是独立 Go module，应在 `server-monitor/server-web` 目录执行：

```bash
go test ./...
go vet ./...
```

前端修改后执行：

```bash
npm run build
```

如果修改 Docker Compose、Helm 或 K8s，本阶段原则上不需要；若实际实施时触碰部署文件，再补充：

```bash
docker compose config
helm lint
kubectl apply --dry-run=client -f <file>
```

---

## 9. 风险评估与应对措施

| 风险 | 影响 | 概率 | 应对措施 |
|---|---|---|---|
| LLM 调用慢或失败 | 诊断接口等待过久，用户体验差 | 高 | 设置 timeout；失败时降级为 RuleAnalyzer 报告；Phase 5 再异步化 |
| LLM 输出非法 JSON | 报告生成失败或字段异常 | 中 | JSON 解析器提取代码块；Schema 校验；失败后规则降级 |
| Redis 活跃告警字段不一致 | 告警上下文缺失，诊断证据不完整 | 中 | Resolver 优先查历史完整 payload；证据标注 source；缺字段返回清晰错误 |
| Prometheus 查询超时 | 指标证据缺失 | 中 | 单工具 timeout；继续生成报告；降低 confidence 并展示缺失原因 |
| 诊断报告过大 | MySQL 存储和前端展示压力 | 中 | 压缩指标证据，只保存摘要统计和必要来源，不保存全量点位 |
| 用户越权查看报告 | 数据泄露 | 中 | 查询时按 `created_by` 过滤；admin 例外；handler 测试覆盖 |
| Prompt 注入 | LLM 输出被操纵 | 中 | 用户输入转义；系统 Prompt 强约束；工具执行仍走 Registry；本阶段无写操作 |
| 阶段越界 | 工期膨胀，影响可交付 | 高 | Runbook、异步 Worker、审批审计、K8s 只做字段预留和衔接说明 |
| 自动迁移影响已有表 | 启动风险 | 低 | 只新增表和索引；实施前备份本地 DB；不修改现有表字段 |
| 前端证据展示复杂 | 页面难读或卡顿 | 中 | 折叠 JSON、分块展示、限制单报告展示大小 |

---

## 10. 验收标准

Phase 3 完成时必须满足：

1. `POST /api/v1/diagnosis` 可对单条告警生成诊断报告。
2. `GET /api/v1/diagnosis` 可分页查看报告列表。
3. `GET /api/v1/diagnosis/:id` 可查看报告详情。
4. 报告持久化到 MySQL，包含告警上下文、证据快照、规则分析、置信度、建议动作和状态。
5. 报告中明确展示每类证据的数据来源和采集时间。
6. LLM 不可用时，仍能生成规则降级报告。
7. Prometheus 或部分工具失败时，诊断不中断，并在报告中标注证据不完整。
8. 普通用户不能查看他人触发的诊断报告。
9. 前端可从告警页面触发诊断，并查看诊断详情。
10. Copilot 对话可识别单条告警诊断请求，并返回诊断结果或候选告警。
11. Alertmanager Webhook 不被 LLM 阻塞。
12. 现有主机、告警、告警历史、Copilot Chat 基础能力不回退。

---

## 11. 建议提交拆分

Phase 3 建议拆成以下提交，便于回滚和 Review：

1. `feat: add diagnosis report model`
2. `feat: resolve alert context for diagnosis`
3. `feat: collect diagnosis evidence`
4. `feat: add diagnosis rule analyzer`
5. `feat: generate diagnosis report with llm fallback`
6. `feat: add diagnosis api`
7. `feat: add diagnosis frontend pages`
8. `test: add diagnosis workflow coverage`
9. `docs: record phase 3 realization`

每个提交都应包含对应测试或明确说明未执行原因，不将大范围格式化、无关重构和功能代码混在一起。

---

## 12. 阶段完成后的后续衔接

Phase 3 完成后，后续阶段按 `docs/design.md` 继续推进：

| 后续阶段 | 衔接点 |
|---|---|
| Phase 4 Runbook 检索 | 将 `runbook.search` 接入 EvidenceCollector，填充 `runbooks_json` 并加入 LLM Prompt |
| Phase 5 异步诊断 Worker | 复用 Phase 3 Diagnosis Service，改由 Kafka `alert-events` 自动触发 |
| Phase 6 动作审批与审计 | 将 Phase 3 的 `recommended_actions_json` 转换为 PendingAction 候选 |
| Phase 7 Kubernetes 深度接入 | 为 K8s 告警补充 events、logs、deployment 状态等证据 |

Phase 3 的核心价值是打通“单条告警 → 证据采集 → 规则分析 → LLM 归纳 → 报告持久化 → 前端展示”的最小闭环。只要这个闭环稳定，后续 Runbook、异步化、审批和 K8s 工具都可以在同一套诊断服务上渐进扩展。
