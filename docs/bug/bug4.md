# Phase 4 代码审查结果

审查范围：`b30b3d8..2012ab6`（29 文件，+1104/-47 行）
审查日期：2026-05-09

---

## 优点

1. **包边界清晰，无第三方依赖** — `copilot/runbook` 包只依赖标准库，parser 按行扫描，retriever 纯内存计算
2. **文档克隆防止外部变异** — `retriever.go:36` 的 `cloneDocuments` 在 `NewRetriever` 时深拷贝 slice，加载后文档不可被调用方修改
3. **Context 传播正确** — `loader.go:17` 的 `LoadDir` 接收 `ctx`，目录遍历和文件读取循环中多次检查 `ctx.Err()`；启动时使用 `context.Background()` 正确
4. **Tool schema 完全匹配方案** — `runbook_tool.go:16-36` 参数定义与方案一致，`ReadOnly=true`、`RiskLevel=low` 标记正确
5. **HealthCheck 语义准确** — `runbook_tool.go:69-74` 同时检查 retriever 非 nil 和 `Count() > 0`，空知识库返回 false 但不影响启动
6. **EvidenceCollector 降级策略正确** — `evidence.go:156-168` runbook 检索失败写入 `collection_errors`，诊断继续
7. **前端安全做法** — `DiagnosisDetailPage.vue:154` 使用 `<pre>{{ runbook.snippet }}</pre>` 纯文本展示，避免 XSS；`<details>` 实现折叠/展开
8. **Prompt hash 随 Runbook 内容变化** — `summarizer.go:177` 的 `sha256.Sum256` 覆盖了包含 runbooks 的完整 prompt

---

## 问题

### Critical — 无

### Important（应修复）

#### 1. Confidence 评分与方案偏差

- **位置**: `rule.go:129-165`
- **方案要求**: 命中 Runbook 且 snippet 非空时 `runbook_evidence_score=1.0`，未命中 `0.4`，工具失败 `0.3`
- **实现**: 使用加法模型 — 命中 `0.1`，未命中 `0.04`，工具失败 `0.03`，替换原来硬编码的 `0.08`
- **差异**: 命中 Runbook 时总 confidence 最高 0.9（0.3+0.3+0.2+0.1），未命中 0.84，差异仅 0.06。方案设想的差异远大于此（1.0 vs 0.4）
- **影响**: 方案文档明确列出三个分数值，实现采用了完全不同的计分逻辑，后续维护者参考方案文档会产生误解
- **修复**: 按方案调整 `runbookEvidenceScore` 返回独立的 runbook 置信度值，或更新方案文档反映实际加法模型

#### 2. `planToolCall` 改变了 IntentAlertQuery 的默认行为

- **位置**: `executor.go:227-234`
- **问题**: 原来 `IntentAlertQuery` 直接路由到 `alert.list_active`，现在当 `alert_name` 非空时路由到 `runbook.search`
- **影响**: 用户说"查看 HighCPU 告警"，原来会列出活跃告警，现在会搜索 Runbook。改变 Copilot 对已有意图的行为，可能导致用户预期与实际结果不符
- **修复**: 将 runbook 检索限制在诊断流程中，不改变 Copilot Chat 的意图路由；或保留 `alert.list_active` 作为首选，让 runbook 检索在诊断流程中自动触发

#### 3. Helm/K8s 部署缺少 Runbook 文件的 volume mount

- **位置**: `charts/server-monitor/templates/server-web.yaml`、`k8s/web.yaml`
- **问题**: 配置了 `RUNBOOK_DIR` 等环境变量，但没有 volumeMount 将 Runbook 文件挂载到容器中
- **影响**: 方案要求"Helm 使用 ConfigMap 保存核心 Runbook，通过 volumeMount 挂载"。当前仅 Dockerfile 的 `COPY runbooks ./runbooks` 保证镜像内有文件，但无法通过 ConfigMap 覆盖 Runbook 内容
- **修复**: 在 Helm values 中增加 runbook ConfigMap 配置，在 server-web.yaml 中增加 volume 和 volumeMount；或文档中说明当前仅支持镜像内嵌方式

#### 4. 测试覆盖不足

- **位置**: 仅 `runbook_test.go`、`runbook_tool_test.go`
- **方案列出 8 个独立测试文件，实际只有 2 个**
- **缺失测试场景**:
  - Loader 空目录、目录不存在、context 取消
  - Parser 无关键词（允许加载但质量下降）、无适用告警（使用标题兜底）
  - Retriever 空 retriever 调用 Search 返回 `ErrUnavailable`、limit 超过 maxLimit 的截断
  - EvidenceCollector 调用 runbook.search 后写入 `collection_errors` 和 `Runbooks`
  - RuleAnalyzer confidence 在命中/未命中/失败时的分数差异
  - LLMSummarizer prompt 中包含 runbook 片段

---

### Minor（建议改进）

#### 1. `snippetFor` 可能变异入参

- **位置**: `retriever.go:143`
- **说明**: `terms := append([]string{alertName}, keywords...)` — 若 `keywords` 底层数组有剩余容量，`append` 可能修改原始 slice。当前实际不会出问题（`keywords` 来自 `normalizeTerms` 返回的新 slice），但防御性编程建议明确拷贝

#### 2. `runbookKeywords` 包含 instance 作为关键词

- **位置**: `evidence.go:444-452`
- **说明**: `alert.Instance` 通常是 `ip:port` 格式（如 `192.168.1.10:9100`），作为关键词传入 Runbook 检索不会命中任何内容，属于噪音
- **建议**: 从关键词列表中移除 `alert.Instance`，或仅提取 hostname 部分

#### 3. 前端 `runbooks` 计算属性的回退逻辑

- **位置**: `DiagnosisDetailPage.vue:18-22`
- **说明**: 优先使用 `report.runbooks`，回退到 `report.evidence?.runbooks`。`runbooks_json` 写入的是 `evidence.Runbooks`，两者内容相同，双重来源增加理解成本
- **建议**: 统一为一个来源，或在注释中说明回退原因（兼容旧报告格式）

#### 4. `RUNBOOK_DIR` 默认值不一致

- **位置**: `config.go:338` 默认 `../runbooks`，Dockerfile/Compose/Helm 均为 `/app/runbooks`
- **说明**: 本地开发时 `server-web` 工作目录是 `server-monitor/server-web`，`../runbooks` 指向 `server-monitor/runbooks`，正确。Dockerfile 设置 `RUNBOOK_DIR=/app/runbooks` 环境变量覆盖 config 默认值，不会冲突。但值得在注释中说明两个路径的适用场景

---

## 建议

1. **对齐 confidence 评分文档** — 更新方案文档或调整 `runbookEvidenceScore` 函数，使两者一致
2. **重新考虑 `planToolCall` 中的 runbook 路由** — 将 runbook 检索限制在诊断流程中，不改变 Copilot Chat 的意图路由
3. **补充测试** — 至少为 EvidenceCollector 的 runbook 集成、RuleAnalyzer 的 confidence 变化、LLMSummarizer 的 prompt 注入添加测试
4. **Helm 补充 volume mount** — 如果需要 ConfigMap 覆盖 Runbook，需在 Helm 模板中增加 volume/volumeMount 配置

---

## 结论

**可以合并，但建议先处理 Important #1 和 #2。**

核心功能实现完整：5 个 Runbook 文件、loader/parser、retriever、tool、diagnosis 集成、前端展示和部署配置均到位，`go test` 和 `go vet` 通过。主要问题是 confidence 评分与方案文档的偏差、`planToolCall` 改变了已有意图路由的行为、以及测试覆盖不足。建议至少修复路由优先级问题并补充 confidence 评分对齐后再合并。

---

## Codex 复核与修复结论（2026-05-09）

### 已确认属实并修复

1. **Important #1 Confidence 评分与方案偏差**
   - 复核结论：属实。当前实现中 `runbookEvidenceScore` 使用 `0.1 / 0.04 / 0.03` 加法分数，和 Phase 4 方案中的 `1.0 / 0.4 / 0.3` 独立分数不一致。
   - 修复：`runbookEvidenceScore` 改为返回 Phase 4 方案分数，并在总 confidence 中按 `0.2` 权重计入，保留现有 alert、metric、history 的加权结构。
   - 回归测试：新增 `TestRunbookEvidenceScoreMatchesPhase4Plan`、`TestConfidenceScoreWeightsRunbookEvidence`，已先确认 RED 后修复。

2. **Important #2 `planToolCall` 改变 `IntentAlertQuery` 默认行为**
   - 复核结论：属实。`IntentAlertQuery` 只要带 `alert_name` 就会路由到 `runbook.search`，导致“查看 HighCPU 告警”不再走活跃告警查询。
   - 修复：`IntentAlertQuery` 恢复固定路由到 `alert.list_active`；Runbook 检索继续由诊断流程和 `IntentMetricQuery` 的知识类查询触发。
   - 回归测试：新增 `TestExecuteAlertQueryWithAlertNameKeepsActiveAlertRoute`，已先确认 RED 后修复。

3. **Important #3 Helm/K8s 部署缺少 Runbook volume mount**
   - 复核结论：属实。Compose 已挂载 `./runbooks:/app/runbooks:ro`，但 raw K8s 与 Helm 仅配置了 `RUNBOOK_DIR=/app/runbooks`，没有 ConfigMap/volumeMount 覆盖入口。
   - 修复：raw K8s 新增 `monitor-runbooks` ConfigMap 并挂载到 `server-web:/app/runbooks`；Helm 新增 `runbooks` values、`runbooks-configmap.yaml` 模板，并在 `server-web` Deployment 中挂载。

4. **Minor #2 `runbookKeywords` 包含 instance 噪声**
   - 复核结论：属实。`alert.Instance` 通常为 `ip:port`，作为 Runbook 关键词价值很低。
   - 修复：`runbookKeywords` 不再把 `alert.Instance` 加入关键词，保留 `alert_name`、severity、`alertname/job/namespace` labels。
   - 回归测试：新增 `TestRunbookKeywordsSkipInstanceNoise`，已先确认 RED 后修复。

### 复核后未按原文修复

1. **Important #4 测试覆盖不足**
   - 原文“实际只有 2 个测试文件”不属实。`server-web` 下存在被 `.gitignore` 隐藏的多组 `_test.go`，包括 `copilot/runbook/runbook_test.go`、`copilot/tool/runbook_tool_test.go`、`copilot/diagnosis/evidence_test.go`、`rule_test.go`、`summarizer_test.go` 等。
   - 本次仍补充了与修复直接相关的 4 个回归测试。

2. **Minor #1 `snippetFor` 可能变异入参**
   - 复核结论：当前说法不成立。`append([]string{alertName}, keywords...)` 的目标 slice 来自新建字面量，不会修改 `keywords` 的底层数组。
   - 本次不改。

3. **Minor #3 前端 `runbooks` 回退逻辑**
   - 复核结论：这是 Phase 4 方案明确要求的兼容逻辑，用于优先使用 `report.runbooks`，为空时回退 `report.evidence?.runbooks`。
   - 本次不改。

4. **Minor #4 `RUNBOOK_DIR` 默认值不一致**
   - 复核结论：当前行为可解释。本地默认 `../runbooks` 指向源码目录，容器/Compose/K8s/Helm 通过环境变量覆盖为 `/app/runbooks`。
   - 本次不改。

### 验证记录

- `go test ./copilot/tool ./copilot/diagnosis`：通过
- `go test ./...`（`server-monitor/server-web`）：通过
- `helm template server-monitor ./charts/server-monitor`：通过
- `helm lint ./charts/server-monitor`：通过（仅提示 Chart.yaml 建议配置 icon）
- `python3` YAML 解析 `k8s/configmap.yaml`、`k8s/web.yaml`：通过
- `git diff --check`：通过
- `kubectl apply --dry-run=client`：未通过本机集群发现接口，报 `invalid character '<' looking for beginning of value`；已用 Helm 与 YAML 解析作为本地替代校验
