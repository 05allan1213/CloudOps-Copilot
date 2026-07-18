# CloudOps Incident Agent V3 重构设计

> 状态：FROZEN TARGET DESIGN — 当前工作树内 V3 实施的唯一规范入口；Phase 0 基线已提交，当前阶段边界修订随 owning phase 提交
> 日期：2026-07-17
> 源码审计基线：main@2f7e426d69a4ed7d8d32ec3ca83c13af0c71586e
> 文档 revision：Phase 0 基线由本地提交 `1ea0c3a21ed3ed1f822399f205afac225b1d5464` 承载；后续修订由外部 Evidence Manifest 记录承载 commit，并用 `git rev-parse COMMIT:docs/CloudOps-Incident-Agent-V3-Refactor-Design.md` 固化 blob ID，本文不自嵌入当前提交 SHA
> 目标读者：实现者、评审者、测试者、演示者和面试准备者
> 项目定位：以云原生和 Agent 为两条主轴，监控、告警、日志、Trace、运维和 DevOps 为支撑能力，且只有一条 Incident 主链
> 阶段边界修订（2026-07-18）：经实施授权确认，Phase 2 只冻结并验证统一 Task runtime、registry 和 `investigation.start`；五个 subject-bound 业务 operation 由 Phase 4-6 的 owning Gate 实现。该修订只消除实施顺序循环，不降低最终运行时、Phase 4-7 或 DoD 要求。

## 目录

- [0. 文档地位与规范语言](#0-文档地位与规范语言)
- [1. 执行摘要](#1-执行摘要)
- [2. 秋招项目定位与设计判定](#2-秋招项目定位与设计判定)
- [3. 业界方案与成熟模式参考](#3-业界方案与成熟模式参考)
- [4. 产品定义与边界](#4-产品定义与边界)
- [5. 唯一黄金场景](#5-唯一黄金场景)
- [6. 目标运行时拓扑](#6-目标运行时拓扑)
- [7. 仓库与进程结构](#7-仓库与进程结构)
- [8. 领域模型与状态机](#8-领域模型与状态机)
- [9. MySQL 数据模型与一致性](#9-mysql-数据模型与一致性)
- [10. 可靠异步任务运行时](#10-可靠异步任务运行时)
- [11. Agent Runtime](#11-agent-runtime)
- [12. Agent 工具合同](#12-agent-工具合同)
- [13. Evidence、可信度与 Prompt Injection](#13-evidence可信度与-prompt-injection)
- [14. 云原生可观测性底座](#14-云原生可观测性底座)
- [15. Demo Workload 与故障注入](#15-demo-workload-与故障注入)
- [16. Change Intelligence 与部署身份](#16-change-intelligence-与部署身份)
- [17. RemediationPlan 与审批](#17-remediationplan-与审批)
- [18. GitHub 与 Argo CD 边界](#18-github-与-argo-cd-边界)
- [19. Recovery Verification](#19-recovery-verification)
- [20. 身份、安全与权限](#20-身份安全与权限)
- [21. API 与 Incident Workbench](#21-api-与-incident-workbench)
- [22. kind + Helm 唯一部署路径](#22-kind--helm-唯一部署路径)
- [23. CloudOps 自身可观测性](#23-cloudops-自身可观测性)
- [24. Agent Evaluation](#24-agent-evaluation)
- [25. 测试金字塔与故障注入](#25-测试金字塔与故障注入)
- [26. 实施阶段与硬Gate](#26-实施阶段与硬gate)
- [27. CI Gates](#27-ci-gates)
- [28. 当前资产的 KEEP / ADAPT / DELETE](#28-当前资产的-keep--adapt--delete)
- [29. 文档与ADR](#29-文档与adr)
- [30. 最终 Definition of Done](#30-最终-definition-of-done)
- [31. 明确非目标](#31-明确非目标)
- [32. 秋招表述边界](#32-秋招表述边界)
- [33. 官方参考资料](#33-官方参考资料)
- [34. 最终原则](#34-最终原则)

---

## 0. 文档地位与规范语言

本文档取代旧的 V2 执行规格，成为 V3 设计、实施、验证和最终项目表述的唯一规范来源。旧 V2 规格、历史报告和现有代码仅作为迁移输入，不得用于追认 V3 Gate 已通过。

规范词：

- MUST / 必须：不满足即 Gate FAIL。
- MUST NOT / 禁止：出现即 Gate FAIL。
- SHOULD / 应当：除非有新的 ADR 和证据，否则必须遵循。
- MAY / 可以：可选能力，不得阻塞核心链。

设计事实与实施事实必须分开：

- 本文描述的是冻结后的目标架构。
- 当前代码中已经存在的 Incident、Agent、Evidence、Remediation、Verification 和 Workbench 资产只代表可迁移基础。
- 任何 V3 能力只有在对应代码、测试和 exact-SHA 证据全部存在后，才能对外宣称已完成。
- kind 本地 E2E 不得包装为生产级、HA、SRE 平台或真实生产运行经验。

---

## 1. 执行摘要

CloudOps Incident Agent V3 不是泛化监控平台，也不是通用 DevOps 门户。它只解决一个问题：

> Kubernetes 工作负载发生故障后，如何从告警出发，通过有界 Agent 动态调查 Metric、Log、Trace、Kubernetes 和变更证据，生成可审计诊断，并通过人工审批的 GitOps PR 修复，最后以确定性稳定窗口证明恢复。

唯一 Canonical Flow：

~~~text
Prometheus / Alertmanager
          ↓
       Incident
          ↓
Bounded Investigation Agent
          ↓
Evidence-backed Diagnosis
          ↓
Deterministic RemediationPlan
          ↓
Human Approval
          ↓
GitHub Draft PR → CI → Human Merge
          ↓
Argo CD Reconciliation
          ↓
Deterministic Recovery Verification
          ↓
       Resolved

Verification Failed / TimedOut / Inconclusive
          └────────→ investigation.start Task → New AgentRun → Investigating
~~~

优先级固定为：

1. 云原生：Kubernetes、Operator、GitOps、不可变身份、最小权限、可恢复 Worker、真实部署和恢复证据。
2. Agent：动态工具选择、StateDelta、假设更新、Evidence、checkpoint、确定性充分性、安全和评估。
3. Go 后端：领域状态机、MySQL 事务、幂等、乐观锁、租约、fencing、重试和 reconciliation。
4. 支撑能力：监控、告警、日志、Trace、变更、CI、Workbench。

任何新模块若不能直接加强上述四层之一，或不能进入唯一主链，默认不进入 V3。

---

## 2. 秋招项目定位与设计判定

### 2.1 Phase 7 与最终 DoD PASS 后可用的项目定位

以下不是当前实现声明。只有 Phase 0–7、AGENT_QUALITY 和最终 DoD 全部 PASS 后，才可使用现在时介绍；实施期间必须写“目标为”或“正在重构”。

届时推荐项目名：

> CloudOps Incident Agent — Kubernetes 故障调查与 GitOps 恢复系统

推荐一句话介绍：

> 基于 Go、Kubernetes 和 Eino 构建的 Incident Agent，通过 Prometheus、ECK、Tempo、Kubernetes 与 GitOps 变更证据完成有界调查，并在人审后创建受限修复 PR，以多信号稳定窗口验证恢复。

### 2.2 设计判定问题

每个候选能力必须同时回答：

1. 它是否强化云原生或 Agent 主轴？
2. 它是否属于唯一 Incident 链？
3. 它是否有明确代码 owner 和事实源？
4. 它是否能通过测试或 E2E 证明？
5. 它是否能形成可防守的 Go 后端面试故事？
6. 移除它是否会破坏核心链？

若第 1、2、4 项不能同时满足，默认删除或延期。

### 2.3 最终可防守主故事

V3 完成后，项目只围绕四个主故事组织简历和面试：

1. Go + MySQL 的 Incident 状态机、事务一致性和可靠异步任务。
2. Eino 有界 Agent、StateDelta、Evidence、checkpoint 和安全评估。
3. Kubernetes 上 Prometheus、ECK、OpenTelemetry 和 Tempo 的跨信号关联。
4. 哈希绑定审批、受限 GitOps PR、exact-SHA 交付和恢复验证。

前端、CI、日志平台和可视化是交付证据，不是独立产品主线。

---

## 3. 业界方案与成熟模式参考

V3 借鉴业界方案中已经得到代码和公开实践支持的模式，不复制其产品规模，也不把全部项目变成运行依赖。部分参考项目仍处于活跃开发阶段，因此只能引用明确存在的能力，不能继承其成熟度或安全声明。

| 参考方案 | 官方定位或成熟模式 | V3 借鉴 | V3 明确不复制 |
|---|---|---|---|
| HolmesGPT | CNCF Sandbox SRE Agent；多数据源 toolsets、服务端过滤、上下文裁剪、Incident 调查 | 动态工具选择、跨 Metric/Log/Trace/Kubernetes/Argo 数据源、bounded output、场景化回归 eval、真实模型多次运行 | 几十种集成、通用 Web/MCP、shell、任意 kubectl、通用自动修复和多云产品面 |
| Robusta Classic | Alertmanager 驱动的确定性告警富化、变更跟踪，并可与 HolmesGPT 分层组合 | 先做确定性 Signal 规范化、Kubernetes 上下文和变更富化，再启动 Agent | 通知渠道大全、Playbook 市场、通用自愈规则和 SaaS 平台 |
| K8sGPT | 先用 analyzer 提取 Kubernetes 事实，再由模型解释 | typed analyzer、事实先于模型、namespace/resource scope、部分字段匿名化思路但不视为完整脱敏 | 全集群通用扫描、全部 analyzer、模型供应商矩阵和单次扫描冒充多步 Agent |
| CloudWeGo Eino | Go 原生组件、Graph/Workflow、Tool 和 Agent 编排 | 使用 Graph 组织受控节点，复用 ChatModel/Tool 抽象和 callback | DeepAgent、多 Agent、shell/Python/WebSearch、Eino memory 作为 durable truth |
| kagent | Tool/Model定义分离、精确HITL暂停/恢复、Agent/Tool/Model OTel观测 | 审批必须绑定原始操作意图，恢复时不得让模型改写已审批参数 | 通用Agent平台、CRD/controller、动态MCP、A2A、多Agent和每Agent一个Pod |
| Prometheus Operator | Kubernetes-native Prometheus/Alertmanager 管理和 ServiceMonitor/PrometheusRule | 使用官方 Operator/Chart 管理指标与告警，CloudOps 只查询和消费 Alertmanager | 自研采集器、AlertRule CRUD、长期指标平台 |
| ECK | Operator 管理 Elasticsearch、Kibana 和 Beat；自动建立安全关联 | ECK 管理 ES/Kibana；Beat CR 管理 Filebeat DaemonSet；日志作为 Agent 事实源 | 手写 ES StatefulSet、Logstash、Fleet、Elastic Agent 产品面 |
| OpenTelemetry Collector | receiver → processor → exporter 的 vendor-neutral pipeline | Demo 和 CloudOps 输出 OTLP Trace；Collector 只处理 Trace | 用 Collector 重复采集 Filebeat 已负责的 CRI 日志 |
| Grafana Tempo | 分布式 Trace 后端，与 Grafana/OTel 集成 | 作为 Trace 权威源和恢复验证数据源 | 分布式生产拓扑、长期存储和多租户 |
| Argo CD | Git 为 desired state，controller 负责 reconciliation，自动同步无需 CI 直接调用 Argo API | CloudOps 只写 GitHub PR、只读 Argo exact revision 和健康状态 | CloudOps 主动 sync、rollback、override 或自研 GitOps controller |
| oauth2-proxy | 成熟 OAuth reverse proxy，支持 GitHub provider 和用户限制 | 复用 GitHub OAuth 身份，替换本地密码/JWT产品面 | Keycloak、用户管理中心和生产级 IAM 平台 |

官方资料表明，截至 2026-07-17，ECK 当前文档仍提供 beat.k8s.elastic.co/v1beta1 的 Beat CR 来运行 Filebeat DaemonSet，并由 ECK 配置到 Elasticsearch 的安全连接。V3 必须固定 ECK 与 Elastic Stack 兼容版本；若未来 Beat CR 被弃用，只替换 LogReader 的部署适配层，不改变领域或 Agent 工具合同。

### 3.1 组合后的核心模式

~~~text
Robusta 式确定性告警富化
→ K8sGPT 式 typed fact extraction
→ HolmesGPT 式有界多步调查
→ CloudOps 自有 Evidence / Change Correlation
→ CloudOps 自有受限 GitOps 审批
→ 确定性多信号恢复验证
~~~

---

## 4. 产品定义与边界

### 4.1 用户与使用场景

首版只有两类用户：

- viewer：查看 Incident、Evidence、诊断、diff、交付和验证。
- operator：包含 viewer 权限，可以启动受限重试、Approve/Reject 和关闭 Incident。

首版允许同一 GitHub 用户承担审批和最终 GitHub merge；四眼原则不是首版目标，但两个动作必须独立审计。

### 4.2 CloudOps 拥有的能力

- Alertmanager Webhook 接入与 Signal 规范化。
- Incident 聚合和顶层生命周期。
- Bounded Investigation Agent。
- Evidence、Diagnosis 和 ChangeCandidate。
- Last-known-good DeploymentBaseline。
- 单一 restore_required_env RemediationPlan。
- Hash-bound Approval。
- GitHub Draft PR 创建与 reconciliation。
- GitHub CI、Argo CD、Kubernetes rollout 交付观测。
- Deterministic Verification。
- Incident Workbench 和结构化 ResolutionReport。

### 4.3 外部系统权威边界

| 事实 | 权威系统 | CloudOps 行为 |
|---|---|---|
| 指标值与告警 | Prometheus、Alertmanager | 固定模板查询、消费 Webhook |
| 日志 | Elasticsearch | 固定服务端 DSL 查询、保存 bounded facts |
| Trace | Tempo | 固定模板查询、保存 bounded facts |
| 资源状态 | Kubernetes API | namespace/target scope 内只读 |
| Git 提交、diff、CI | GitHub | 只读调查；审批后创建 Draft PR |
| 镜像身份 | Pod imageID、Registry OCI metadata | 精确 digest 和 revision 校验 |
| Desired state | GitOps repository | 仅通过 PR 修改 |
| Reconciliation | Argo CD | 只读 exact revision、sync、health |
| Durable workflow | MySQL | CloudOps 唯一事实源 |
| 用户身份 | GitHub OAuth | oauth2-proxy 认证，CloudOps做静态角色映射 |

CloudOps MUST NOT 把外部原始数据完整复制到 MySQL，也不得把 UI、模型输出或 README 当作事实源。

### 4.4 支撑能力只能进入主链

| 支撑能力 | 在唯一 Incident 链中的唯一角色 | 禁止演化成 |
|---|---|---|
| Monitoring / Metrics | 产生症状 Evidence、Agent fixed query、Recovery Check | 指标平台、任意 PromQL、独立 Dashboard 产品 |
| Alerting | 规范化 Signal、创建/关联 Incident、提供 firing/resolved 事实 | AlertRule/渠道 CRUD、第二套告警生命周期 |
| Logs | 结构化运行证据、反证与恢复期 absence check | 通用日志检索产品、Pod logs fallback |
| Traces | 请求级错误关联、反证与恢复错误率 | 通用 APM 产品、独立 Trace 工作台 |
| Kubernetes 运维 | 只读 workload/event/rollout Evidence | 资源管理台、kubectl、直接修复 |
| DevOps / GitOps | 编译受限 Plan、Draft PR、CI/Argo delivery observation | 通用流水线、merge/sync/rollback平台 |
| Workbench | 展示同一 Incident 的事实与有限 Command | Chat、跨产品任务中心或前端编排 |

“唯一链路”不表示强迫每个 Incident 都创建 PR。允许的分支只有同一状态机内的 closed-no-action、resolved Signal 触发 no-change Verification，以及失败/证据不足回到 investigating；它们共享同一 Incident、Evidence、Task、Verification 和审计模型，不得建立第二套产品/API/Worker 链。

---

## 5. 唯一黄金场景

首版只真实部署和修复一个故障：

> GitOps 配置删除 Demo Deployment 的非 Secret 必需环境变量，应用进程仍存活但 readiness 失败，并产生 5xx、结构化错误日志和失败 Trace。

完整时序：

~~~mermaid
sequenceDiagram
    participant Human as Human
    participant GitHub as GitHub/GitOps
    participant Argo as Argo CD
    participant K8s as Kubernetes
    participant Obs as Prometheus/ECK/Tempo
    participant API as CloudOps API
    participant Worker as CloudOps Worker
    participant DB as MySQL
    participant LLM as LLM

    Human->>GitHub: Merge regression PR removing REQUIRED_ENV
    GitHub-->>Argo: New GitOps revision
    Argo->>K8s: Reconcile bad revision
    K8s-->>Obs: readiness/error/log/trace signals
    Obs->>API: Alertmanager firing webhook
    API->>DB: Signal + Incident + start task transaction
    Worker->>DB: Start task: AgentRun(pending) + Incident investigating
    Worker->>LLM: Propose bounded StateDelta
    Worker->>Obs: Execute one authorized read tool
    Obs-->>Worker: Typed facts + provenance
    Worker->>Worker: Persist Evidence + checkpoint
    Worker->>LLM: Replan with new Evidence
    Worker->>Worker: Validate Evidence-backed Diagnosis
    Worker->>GitHub: Read bad SHA and last-known-good SHA
    Worker->>Worker: Build deterministic restore patch
    Human->>API: Approve exact plan/hash/diff
    Worker->>GitHub: Create Draft remediation PR
    GitHub-->>Human: Required CI checks
    Human->>GitHub: Merge remediation PR
    GitHub-->>Argo: New exact merged SHA
    Argo->>K8s: Reconcile fix
    Worker->>Argo: Observe exact revision
    Worker->>Obs: Run deterministic stability checks
    Worker->>DB: One transaction: Verification passed + Incident Resolved + Timeline + ResolutionReport
~~~

其他 OOM、CrashLoop、selector mismatch、readiness regression、error regression 等场景只进入 Agent Eval fixtures，不各自建设真实 GitOps 演示链。

### 5.1 Signal identity 与唯一 Incident correlation

Signal event identity 与 Incident correlation identity 必须分开：

~~~text
source_event_id:
  per-alert firing/resolved event identity

alert_instance_key:
  SHA-256(version, source, fingerprint, startsAtUTC)

correlation_key v2:
  SHA-256(cluster_id, environment, namespace, workload_kind, workload_name)
~~~

五个 correlation 维度必须先由服务端 signal_target_allowlist 把 Alert label 映射为已配置 target，不能直接信任任意 label。alertname、category、fingerprint、Pod UID、source/image/gitops revision 均不进入 correlation key，因此同一 workload 的 readiness、5xx 与其他症状进入同一 active Incident；不同 workload 永不合并。

任一必需维度缺失、unknown、越界或 label 与 allowlist 冲突时，不创建 Incident，也不把多个 unknown 折叠到同一 key。API 只写 bounded signal_rejections 审计记录和 rejected metric；已认证且 envelope 合法的 batch 处理完其他 alert 后返回 2xx，认证/结构/size 错误仍返回对应 4xx。

correlation_key_version 必须随 Signal/Incident 持久化。Contract/真实 MySQL 测试覆盖：一个 batch 多个 alert 归一 Incident、重复 event 幂等、不同 workload 不合并、label injection 被拒绝、unknown 不聚合、并发 create/reopen 与30分钟窗口边界。

Signal 行保持 append-only；alert_instance_key 把同一实例的 firing/resolved 配对，实例状态只能单向 firing→resolved。resolved 带合法 endsAt 后即为该 instance 终态，随后乱序到达的 firing 只记审计、不复活；新的告警发作必须有新 startsAt。只有当前 cycle 的 firing set 为空时，resolved Signal 才能启动 no-change Verification；仍有任一实例 firing 时只追加 Signal。Recovery Check 必须证明当前 cycle 出现过的全部 alert instance 都已 resolved，不能用一个 fingerprint 掩盖其余告警。

---

## 6. 目标运行时拓扑

~~~mermaid
flowchart TB
    Browser[Browser] --> OAuth[oauth2-proxy]
    OAuth --> API[cloudops-api]
    Alertmanager -->|Bearer protected webhook| API

    API --> MySQL[(MySQL)]
    Worker[cloudops-worker] --> MySQL
    Migrate[cloudops-migrate Job] --> MySQL

    Worker --> K8s[Kubernetes API]
    Worker --> Prom[Prometheus]
    Worker --> ES[Elasticsearch via ECK]
    Worker --> Tempo[Tempo]
    Worker --> Registry[Registry]
    Worker --> GitHub[GitHub App]
    Worker --> Argo[Argo CD read-only]
    Worker --> Model[External LLM API]

    Filebeat[Filebeat Beat CR] --> ES
    Demo[Demo Workload] --> Prom
    Demo --> OTel[OTel Collector]
    OTel --> Tempo
    Prom --> Alertmanager
    Argo --> Demo
~~~

默认常驻进程：

- cloudops-api：1 replica。
- cloudops-worker：资源受限时 1 replica；并发/接管 Gate 时 2 replicas。
- MySQL：1 replica，仅本地演示。
- 可观测性和 Argo 数据组件：单副本 demo profile。

基础设施由 bootstrap 安装，Argo CD 只管理黄金 Demo workload，避免 Argo、ECK、MySQL 和 CloudOps 之间形成循环依赖。

---

## 7. 仓库与进程结构

### 7.1 最终仓库结构

~~~text
CloudOps-Copilot/
├── cmd/
│   ├── cloudops-api/
│   ├── cloudops-worker/
│   └── cloudops-migrate/
├── internal/
│   ├── incident/
│   ├── investigation/
│   ├── remediation/
│   ├── delivery/
│   ├── verification/
│   ├── asyncjob/
│   ├── adapter/
│   │   ├── mysql/
│   │   ├── kubernetes/
│   │   ├── prometheus/
│   │   ├── elastic/
│   │   ├── tempo/
│   │   ├── registry/
│   │   ├── github/
│   │   ├── argocd/
│   │   └── llm/
│   └── bootstrap/
├── migrations/
├── frontend/
├── charts/cloudops/
├── deploy/demo/
├── eval/
├── runbooks/
├── docs/
├── Makefile
├── go.mod
└── go.sum
~~~

使用 feature-first 结构，不建立空泛的多层框架，不设置万能 utils、共享 repository 或内部 SDK。

### 7.2 进程职责

cloudops-api MUST：

- 提供 Alertmanager Webhook。
- 执行 Signal/Incident 事务。
- 提供 Workbench Query API、Command API 和 SSE。
- 校验 OAuth 身份、角色、CSRF、Idempotency-Key 和 expected version。
- 提供 livez、readyz 和 metrics。
- 服务构建后的 Vue 静态资源。

cloudops-api MUST NOT：

- 执行 Agent loop。
- 调用 LLM。
- 创建 GitHub PR。
- 轮询 Argo。
- 执行 Verification。
- 挂载 Kubernetes ServiceAccount token。

cloudops-worker MUST：

- 运行统一 async task runner。
- 为 investigate、deliver、observe、verify 设置独立并发池。
- 执行一个任务对应的一个有界转换，以及至多一个外部写或一个静态有界只读 Tool contract。
- 在 SIGTERM 时停止 claim、新任务不再领取，并让已领取任务安全完成或由租约接管。
- 仅在独立 management listener 暴露 livez、readyz、metrics；不提供产品 Command/Query。

cloudops-migrate MUST：

- 使用 Goose。
- 使用独立 MySQL DDL 账号。
- 通过 MySQL advisory lock 串行化迁移。
- 在 Helm pre-install/pre-upgrade Job 中执行 up。

Runtime MUST NOT 执行 AutoMigrate，Kubernetes Upgrade MUST NOT 自动执行 down migration。

---

## 8. 领域模型与状态机

### 8.1 聚合边界

| 聚合或记录 | 拥有内容 | 不拥有 |
|---|---|---|
| Incident | 故障身份、目标、严重度、顶层阶段、时间、version | Agent checkpoint、PR/CI/Argo 明细、Verification sample |
| AgentRun | 调查目标、预算、checkpoint、结果、AgentStep | Incident 状态、写工具 |
| RemediationPlan | operation、完整 diff、Evidence、策略、验证计划、审批状态 | PR/CI/Argo 状态 |
| ChangeRequest | Draft PR 到 exact revision delivery | 恢复是否成功 |
| VerificationRun | 固化检查、sample、稳定窗口、最终结果 | 直接写 Incident 终态 |
| AsyncTask | claim、lease、retry、backoff、dead/replay | 业务真相 |
| Append-only facts | Signal、Timeline、Evidence、ChangeCandidate、Decision、Sample | 独立状态机 |
| ResolutionReport | 已持久化最终事实的快照 | 新推断、新根因 |

跨聚合状态迁移只能由 application workflow 完成；Handler、Agent、Adapter 均不得直接更新 Incident 状态。

### 8.2 Incident 状态

~~~text
detected
  → investigating
      → awaiting_approval
          → delivering
              → verifying
                  → resolved

closed  # operator 明确关闭且无需动作
~~~

允许迁移：

| From | To |
|---|---|
| detected | investigating, closed |
| investigating | awaiting_approval, verifying, closed |
| awaiting_approval | delivering, investigating, closed |
| delivering | verifying, investigating |
| verifying | resolved, investigating |
| resolved | investigating |
| closed | 无 |

规则：

- Incident 不存在顶层 failed。
- 技术失败属于 AsyncTask；调查失败属于 AgentRun；交付失败属于 ChangeRequest；恢复失败属于 VerificationRun。
- 重试耗尽后 Incident 留在当前阶段，并写 blocked_at、blocking_reason_code 和 Timeline。
- resolved 只能由 passing VerificationRun 产生。
- resolved Signal 不直接 resolve，只能启动 no-change Verification。
- resolved Incident 在 reopen window 内再次 firing 可回到 investigating；closed 永不 reopen。
- 活动 Incident 的 Severity 只能升级。
- close 只允许 detected、investigating、awaiting_approval。若不存在 ChangeRequest、external_write_started 或 active Verification，事务必须取消 active AgentRun、所有尚未 consumed 的 awaiting_approval/approved Plan 和 ready Task；running 只读 Task 通过状态改为 cancelled 并递增 lease_generation 隔离 stale Worker。只要存在外部写 intent/result unknown，close 必须拒绝。
- ChangeRequest 一旦创建或 VerificationRun 已 running，Incident 禁止 close。在途交付/验证必须先形成确定性终态并回到 investigating 或 resolved，避免把外部动作藏在 closed 后继续运行。

reopen window 固定为 30 分钟，并以 MySQL NOW(6) 为准。持有 correlation lock 时，若不存在 active Incident，则按 terminal_at DESC、id DESC 锁定唯一最新终态 Incident；只有它是 resolved 且 NOW(6) - resolved_at <= 30 分钟时才可原子执行 cycle_no+1、version+1、status=investigating，severity 取本次 Signal，清空 resolved_at、blocked/needs_attention 与 current-cycle projection，给新 Signal/Event 标记新 cycle并只创建一个 Incident-scoped investigation.start Task。AgentRun 必须由该 Task 创建，Ingress 事务不得直接创建。最新终态为 closed 或超过窗口时必须创建新 Incident，禁止跳过较新的 closed 去复活更老 resolved。窗口恰好边界、resolved后又closed历史、两个并发 firing、reopen 与新建竞争都必须有真实 MySQL 负例。

### 8.3 子状态机

AgentRun：

~~~text
pending → running → completed | failed | cancelled
outcome: diagnosed | insufficient_evidence
~~~

RemediationPlan：

~~~text
awaiting_approval → approved | rejected | superseded | cancelled
approved → consumed | superseded | cancelled
consumed → invalidated
policy_rejected: 创建即终态
~~~

Plan 进入 awaiting_approval 后内容不可变。任何变化必须创建新 plan_version。consumed 只表示已在同一事务创建唯一 ChangeRequest，不改变获批内容；approved 只有在尚无 ChangeRequest 且 preflight 失败时才可 superseded。consumed 后若 Evidence/Policy失效，只能标记 invalidated并停止后续外部写，历史内容仍不可改。

ChangeRequest：

~~~text
pending → pr_open → merged → syncing → rolling_out → delivered
                               └──────────────→ failed

write_phase: ensure_branch → ensure_commit → ensure_draft_pr
~~~

任一非终态可进入 failed；pending/pr_open 可进入 cancelled 或 superseded。Merge 前的 failed/cancelled/superseded 由 application workflow 同事务把 Incident 回到 investigating；delivered 创建 VerificationRun并进入 verifying。Merge 后遇到外部状态 unknown/dependency unavailable 时不得草率终态，ChangeRequest 保持当前 phase 并让 Incident delivering + needs_attention，等待只读 reconcile；只有明确 revision_superseded 等确定性结果才 failed并回 investigating。

CI、PR、Argo 和 rollout 的细节存为观测字段与 failure_reason_code，不扩张为十几个顶层状态。

change.ensure_pr 每次只推进一个 write_phase：调用前先 reconcile，最多执行 branch、commit、Draft PR 中的一次外部写，持久化结果后再入下一 task。任何 timeout 先按 logical_operation_key 查询，禁止把三次 GitHub 写包进一个不可恢复 handler。

VerificationRun：

~~~text
pending → running → passed | failed | inconclusive | timed_out | cancelled
~~~

- failed：权威数据明确给出负面结果。
- inconclusive：required source 持续 unavailable、invalid 或无法解释 no-data。
- timed_out：数据源可用，但 deadline 前未满足稳定窗口。
- 只有 passed 可以 resolve。

### 8.4 旧状态映射

| V2 状态 | V3 状态 |
|---|---|
| DETECTED | detected |
| CORRELATING、DIAGNOSING、DIAGNOSIS_COMPLETED、PLANNING_REMEDIATION | investigating |
| AWAITING_APPROVAL | awaiting_approval |
| APPLYING_CHANGE | delivering |
| VERIFYING | verifying |
| RESOLVED | resolved |
| CLOSED_NO_ACTION | closed |
| FAILED | 最近安全阶段 + blocking_reason |

---

## 9. MySQL 数据模型与一致性

### 9.1 目标表关系

~~~text
incidents
├── incident_signals                 1:N
├── incident_events                  1:N
├── evidence_items                   1:N
├── agent_runs                       1:N
│   └── agent_steps                  1:N
├── change_candidates                1:N
│   └── change_candidate_assessments 1:N
├── remediation_plans                1:N
│   └── remediation_decisions        0:1
│   └── change_requests              0:1
│       └── change_request_events    1:N
├── verification_runs                1:N
│   └── verification_checks          1:N
│       └── verification_samples     1:N
└── resolution_reports               1:N

deployment_baselines
└── baseline_observations             1:N

signal_rejections
command_idempotency_records

async_tasks
└── async_task_attempts

incident_correlation_locks
migration_ledger
~~~

### 9.2 所有权规则

- Incident 是产品根，但不是一次加载全部子对象的巨型 aggregate。
- Incident 保存 cycle_no；初始为 1，仅 resolved 合法 reopen 时递增。Signal、Evidence、AgentRun/checkpoint、Diagnosis、Plan、ChangeRequest、Verification、ResolutionReport 和 Task 都固化所属 cycle，旧 cycle 记录永不复活。
- Incident 不保存 current_agent_run_id/current_plan_id/current_change_id/current_verification_id 循环引用；Detail projection 按 incident_id + cycle_no + indexed status 查询，避免 reopen 指向旧 child。
- AgentRun、RemediationPlan、ChangeRequest、VerificationRun 各自拥有局部 version 和状态机。
- 跨聚合协调由 application workflow 在同一 MySQL 事务中完成。
- 所有业务状态变化必须追加 IncidentEvent。
- Signal、Event、Evidence、ChangeCandidate/Assessment、Decision、TaskAttempt、VerificationSample 采用 append-only。
- Mutable aggregate 必须使用 WHERE version = expected_version 的乐观锁。
- 审计数据使用 FK RESTRICT，不允许级联删除。
- 内部主键使用 BIGINT，外部 API 只暴露 UUID public_id。
- 全部时间存 UTC；租约时间以 MySQL NOW(6) 为权威。

### 9.3 关键唯一约束

- Signal：UNIQUE(source, source_event_id)。
- Active Incident：generated active_correlation_key 在活动状态时等于 correlation_key、终态时为 NULL，并建立 UNIQUE(active_correlation_key)。
- Active AgentRun：generated active_incident_cycle_key 在 pending/running 时等于 incident_id + cycle_no、终态时为 NULL，并建立 UNIQUE(active_incident_cycle_key)。
- Plan：UNIQUE(incident_id, plan_version)。
- Actionable Plan：generated active_incident_cycle_key 在 awaiting_approval/approved 时非空，并建立 UNIQUE；创建唯一 ChangeRequest 的事务同时把 approved → consumed，防止并发产生两个可执行 Plan。
- Decision：UNIQUE(plan_id)。
- ChangeRequest：UNIQUE(plan_id)。
- Active ChangeRequest：generated active_incident_cycle_key 在所有非终态 phase 时非空，并建立 UNIQUE，保证每 cycle 最多一个在途交付。
- VerificationRun：generated trigger_identity 由 trigger_type + 非空 trigger ref 生成，并建立 UNIQUE(incident_id, cycle_no, trigger_identity, attempt)。
- ResolutionReport：UNIQUE(incident_id, cycle_no) 且 UNIQUE(verification_run_id)。
- Active DeploymentBaseline：generated active_target_key 在 active 时等于 target identity、其他状态为 NULL，并建立 UNIQUE(active_target_key)。
- Event：全局 event_id 或 idempotency_key 唯一。
- Evidence：UNIQUE(incident_id, cycle_no, producer_type, producer_dedupe_key, content_hash)。
- AsyncTask：UNIQUE(dedupe_key, replay_generation)；dedupe_key 包含 incident/cycle、subject、transition 和 expected version。
- Authenticated Command：UNIQUE(actor_identity_hash, command_scope, idempotency_key)。

MySQL 没有 partial unique index，所有 active-only 唯一性都必须使用可持久化 generated column + UNIQUE；利用 MySQL 允许多个 NULL 的语义，禁止只在 application 层先查后写。Migration 必须验证 generated expression 与状态枚举同步。

所有复合 identity 使用版本化、长度前缀的 canonical binary encoding 或独立列的 composite UNIQUE，禁止直接 CONCAT 产生歧义；generated key 的最大长度和 collation 在 migration 中固定。

Incident create/reopen 的事务顺序固定为：

~~~text
INSERT ... ON DUPLICATE KEY UPDATE correlation lock row
→ SELECT correlation lock FOR UPDATE
→ read/create/reopen Incident
→ append Signal/Event
→ enqueue one Incident-scoped investigation.start Task
→ COMMIT
~~~

领域事务统一使用 InnoDB READ COMMITTED。锁顺序固定为 correlation lock 或 command-idempotency row → Incident → child aggregate → Event/Task；duplicate key 转为幂等读取，deadlock 最多以同一 request identity 整体重试 3 次并记录。并发测试必须覆盖 create/create、create/reopen、reopen/reopen、active Run 和 active Baseline 冲突。

VerificationRun 直属 Incident，并固定：

~~~text
trigger_type = post_delivery | no_change_signal
change_request_id nullable
trigger_signal_id nullable
cycle_no
target source_revision / image_digest / gitops_revision
verification_profile_version / hash
~~~

CHECK 必须保证 post_delivery 仅 change_request_id 非空，no_change_signal 仅 trigger_signal_id 非空。no-change Run 由 resolved Signal 触发，固化当时三类 revision 和固定 Profile，不需要 Approval、不得产生任何外部写；post-delivery Run 仍引用获批 Plan 内的 VerificationPlan。

VerificationRun 另有 generated active_incident_cycle_key：pending/running 时等于 incident_id + cycle_no，终态为 NULL，并建立 UNIQUE，防止 post-delivery 与 resolved Signal 并发创建两个 active Run。

resolved Signal 在 detected/investigating 且无 active delivery/verification 时可创建 no-change Run；detected 必须先按合法迁移进入 investigating，再进入 verifying。若仍有 active AgentRun，必须在同一事务将 Run/task cancelled 并递增 task lease_generation，旧 Agent 不得再写 Diagnosis/Plan。在 awaiting_approval 时，若尚无 ChangeRequest/write intent，必须先取消 awaiting_approval/approved 且未 consumed 的 Plan并回到 investigating，再创建 Run；否则只允许 reconcile。

delivering 收到 resolved 时，以持久 write_intent 为边界：若 approved Plan 尚未创建 ChangeRequest，则取消 Plan/Task；若 consumed Plan 的 pending ChangeRequest 尚无 external_write_started marker，且 change Task 未 running，则保持 Plan consumed、取消 ChangeRequest/Task；两种情况都经 investigating 创建 no-change Run。只要写 intent 已持久化、Task 正在外调或外部结果 unknown，就只追加 Signal并继续 reconcile，禁止假设“尚未写”。verifying 时只追加 Signal，不创建第二个并行 Run。trigger_signal_id 必须属于当前 incident_id + cycle_no。

只有 firing Signal 可以创建或 reopen Incident。resolved Signal 在没有 active Incident 时，只可按 source + fingerprint + startsAt 精确找到原 firing Signal并附着到它的 incident_id + cycle_no 作为审计，不改变状态、不建 Task；找不到唯一原 Signal则写 signal_rejections(reason=unmatched_resolved)，绝不能单独创建“已恢复 Incident”。

Reducer、sufficiency evaluator 和 Plan compiler 只能引用当前 incident_id + cycle_no 的 Evidence。旧 cycle 只能作为显式标记 historical_context 的只读背景，不能满足 coverage、corroboration、confirmed claim 或 Approval。

command_idempotency_records 至少保存 actor identity hash、command scope、key、canonical request hash、processing/completed 状态、HTTP status、bounded response、resource reference、created/completed/expires time。用户 Command 必须在一个事务中创建 processing record、执行领域转换并固化 response；并发重复相同 hash 读取原结果，不同 hash 返回 409。只允许清理已 completed 且超过 24 小时的记录。

### 9.4 数据边界

MySQL MUST NOT 存储：

- 完整 Prometheus series。
- 完整 Elasticsearch hits。
- 完整 Trace 或 span 树。
- 未脱敏完整 Webhook。
- 完整模型 Prompt、chain-of-thought 或 Provider 原始响应。
- GitHub 私钥、OAuth token、模型 Key 或 Argo token。

建议硬上限：

| 数据 | 上限 |
|---|---:|
| AsyncTask payload | 8 KiB |
| Timeline metadata | 8 KiB |
| Evidence typed facts | 16 KiB |
| Agent checkpoint 默认 / 硬上限 | 64 KiB / 128 KiB |
| Proposed change manifest / diff | 64 KiB |
| 单模型可见外部文本片段 | 2 KiB |
| 单次模型总外部文本 | 24 KiB |

这些上限是安全设计值，不是性能成果。

V3 不建设自动 retention/prune 服务：核心 Incident graph、dead Task 和审计记录在 Demo 集群生命周期内保留，只有 completed command-idempotency row 可在24小时后由显式维护操作清理。Prometheus、Elasticsearch 和 Tempo 使用 version-lock 中的短期 Demo retention，并在 Evidence 中记录实际可查询窗口；make demo-down 前必须先导出 Evidence Manifest。该策略不构成生产归档、合规保留、备份或 DR。

---

## 10. 可靠异步任务运行时

### 10.1 为什么不用 Kafka 和 Redis

当前范围是单集群、低吞吐、单 Durable Store、少量有界 Worker。核心需求是：

- Webhook 事务内入队。
- Crash recovery。
- 多 Worker claim。
- Retry/backoff。
- Dead/replay。
- Idempotency。
- Stale-writer rejection。

MySQL 已经是领域事实源，async_tasks 足以满足上述要求。引入 Kafka 会同时要求 Strimzi、relay、inbox、consumer offset、DLQ、replay、lag 和额外集成测试，却不会增强唯一黄金链。

因此 V3 的准确表述是：

> Transactional enqueue + MySQL-backed durable work queue，语义为 at-least-once。

禁止宣称 exactly-once、消息总线或事件流平台。

### 10.2 任务类型

任务类型冻结为：

~~~text
investigation.advance
remediation.prepare
change.ensure_pr
delivery.observe
verification.advance
~~~

investigation.advance 有两个严格 mode，不增加新 task type：

~~~text
subject_type=incident, transition=investigation.start
  无外部调用
  detected/reopened/failed-verification Incident
  → create AgentRun(pending)
  → Incident investigating
  → enqueue Run-scoped investigation.advance

subject_type=agent_run, transition=investigation.step
  pending时先转running
  → one model decision or one authorized read Tool
~~~

Webhook、人工重新调查、合法 reopen 和 failed/timed_out/inconclusive Verification 都只创建 Incident-scoped start Task，不能假设 AgentRun 已存在，也不得由 Handler/Ingress 直接创建 Run。start 事务受 active AgentRun generated key、incident version、cycle 和业务预算校验保护，Task完成/Run创建/IncidentEvent/下一Task必须原子提交。

并发池：

~~~text
investigate: investigation.advance, remediation.prepare
deliver:     change.ensure_pr
observe:     delivery.observe
verify:      verification.advance
~~~

每个 Worker Pod 的默认 max-in-flight 冻结为 investigate=2、deliver=1、observe=2、verify=2，四个独立 semaphore 之间不借用配额。Runner 只有拿到对应 semaphore 后才 claim，不建立无界内存队列；池饱和时通过停止 claim 向 MySQL queue 施加 backpressure。SIGTERM 同时关闭四个 claim loop，再等待各池有界 drain。

Task runtime 与业务 operation 的阶段所有权冻结为：

- Phase 2 实现并验证 `async_tasks` repository、四池 Runner、typed registry 和无外部调用的 `investigation.start`；旧三套 claim 路径从 V3 Worker binary 不可达。
- Phase 4 实现并注册 `investigation.step`。
- Phase 5 实现并注册 `remediation.prepare` 与 `change.ensure_pr`。
- Phase 6 实现并注册 `delivery.observe` 与 `verification.advance`，并在五个 subject-bound operation 全部存在后首次通过 production Worker 正向启动/readiness Gate。
- 任一 owning operation 缺失时，production Worker 构造必须在 claim 前 fail closed；测试专用 registry 可以注入显式 fixture operation，但空操作、强制 dead 或 legacy wrapper 不得作为业务迁移证据。

Phase 3 只部署和验证观测栈、Demo 与 ingress 数据合同，不以一个缺少 owning operation 的 Worker 作为常驻就绪组件。后续 Phase 可以在 operation 级 task-fenced harness 中先验证自己的 handler；只有 Phase 6 完成全部注册后才启用完整 Worker 部署。该顺序不改变最终四池常驻拓扑，也不授权从 legacy lease 或 outbox 执行业务。

Registry 的实际 dispatch identity 是 `task_type + subject_type + transition`：冻结的 Task type 仍只有五个，`investigation.start` 与 `investigation.step` 是同一 `investigation.advance` Task type 下的两个 transition，不得新增同名 Task type。Phase 2 拥有 start transition；后续 owning operation 共五个：investigation step、remediation prepare、change ensure PR、delivery observe 和 verification advance。

Phase 4/5 的 `TARGETED_HANDLER_GATE` 必须使用真实 MySQL 8 `async_tasks` repository、lease owner/generation、四池 Runner、heartbeat/checkpoint/complete fencing和业务事务完成协议，只插入并 claim 当前 owning Phase 允许的 task/subject/transition。未 owning 类型可以在测试 registry 中使用会产生 `ErrInvalidResult` 的拒绝 sentinel，但 Gate 必须证明它们没有 Task、没有 claim、没有领域效果；该 sentinel 不算 operation 实现证据。阶段报告必须把 `TARGETED_HANDLER_GATE` 与仍为 `NOT RUN` 的 `PRODUCTION_WORKER_READINESS` 分列，禁止用直接调用函数或 fixture operation 冒充 task-fenced 执行。

隔离 Gate 必须证明：持续填满 investigate queue 时 deliver 与 verify 仍能推进；observe 慢调用不能耗尽其他池；每池实际并发不超过配置；shutdown 后没有新 claim，超出租约的任务可被另一 Worker 接管。

不为 Postmortem、清理、通知或通用 scheduler 建立额外任务体系。

### 10.3 async_tasks 核心字段

~~~text
id / public_id
incident_id / cycle_no
queue
task_type
subject_type / subject_id
payload_schema_version
payload_json
dedupe_key
replay_generation
logical_operation_key nullable
status
priority
available_at
attempt / max_attempts
lease_owner
lease_generation
lease_expires_at
last_error_code / last_error_summary
created_at / started_at / completed_at / dead_at
replayed_from_task_id
~~~

状态：

~~~text
ready → running → succeeded
          ├────→ ready      # retry with backoff
          └────→ dead

ready/running → cancelled
expired running → running with new owner and generation
ready or expired running at max_attempts → dead
~~~

### 10.4 Claim 与 fencing

Ready claim 必须使用 queue-scoped 短事务，且 UPDATE 只能命中刚锁定的一行：

~~~sql
BEGIN;

SELECT id, lease_generation
FROM async_tasks
WHERE queue = ?
  AND status = 'ready'
  AND available_at <= NOW(6)
  AND attempt < max_attempts
ORDER BY priority DESC, available_at, id
LIMIT 1
FOR UPDATE SKIP LOCKED;

UPDATE async_tasks
SET status = 'running',
    attempt = attempt + 1,
    lease_owner = ?,
    lease_generation = lease_generation + 1,
    lease_expires_at = TIMESTAMPADD(SECOND, ?, NOW(6)),
    started_at = COALESCE(started_at, NOW(6))
WHERE id = ?
  AND status = 'ready'
  AND lease_generation = ?
  AND attempt < max_attempts;

COMMIT;
~~~

Expired-running takeover 使用另一条显式路径：

~~~sql
BEGIN;

SELECT id, lease_generation, attempt, max_attempts
FROM async_tasks
WHERE queue = ?
  AND status = 'running'
  AND lease_expires_at <= NOW(6)
ORDER BY lease_expires_at, id
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- attempt < max_attempts:
UPDATE async_tasks
SET attempt = attempt + 1,
    lease_owner = ?,
    lease_generation = lease_generation + 1,
    lease_expires_at = TIMESTAMPADD(SECOND, ?, NOW(6)),
    last_error_code = 'lease_expired'
WHERE id = ?
  AND status = 'running'
  AND lease_generation = ?
  AND lease_expires_at <= NOW(6)
  AND attempt < max_attempts;

-- attempt >= max_attempts: guarded UPDATE to dead instead of takeover.
COMMIT;
~~~

每个 guarded UPDATE 的 affected rows 必须恰为 1，否则回滚并按 LeaseLost/竞争失败处理。Ready 且已达 max_attempts 的异常行与 expired-running 达上限行由同一短事务转为 dead，写 TaskAttempt 和 Incident needs_attention，不允许永久滞留。

索引至少包含 (queue, status, available_at, priority DESC, id) 和 (queue, status, lease_expires_at, id)；Migration 必须用目标 MySQL 8 版本的 EXPLAIN 验证 claim/takeover 不做全表扫描。

默认时间合同：

| Queue | Handler deadline | Lease duration | Heartbeat interval |
|---|---:|---:|---:|
| investigate | 45s | 90s | 20s |
| deliver | 30s | 60s | 15s |
| observe | 20s | 45s | 10s |
| verify | 20s | 45s | 10s |

必须始终满足 heartbeat <= lease/3，单次外部调用 deadline < handler deadline < lease duration。Heartbeat 只用本地 timer 调度，但每次续租用 MySQL NOW(6) 计算新 expires_at，并带 owner/generation/unexpired条件；续租失败立即 cancel handler context，禁止继续外调或落库。

Worker Pod terminationGracePeriodSeconds=60：SIGTERM立即停止四个claim loop，最多drain 45s，随后cancel剩余handler并在55s前退出；未完成Task不伪造失败/成功，停止heartbeat后等待lease takeover。测试必须覆盖进程暂停heartbeat、长模型/HTTP调用、SIGTERM发生在外部请求前/后，以及新Worker接管后旧Worker无法checkpoint/complete。

完成、heartbeat、checkpoint 和失败更新必须匹配：

~~~text
task id
lease owner
lease generation
unexpired lease
expected subject version
~~~

更新 0 行即 ErrLeaseLost。AgentRun、ChangeRequest 和 VerificationRun 不再各自持有另一套租约。

### 10.5 有界执行

- 一个 task 最多执行一个外部写和一个持久状态转换；只读 Agent Tool 可以执行其版本化合同声明的静态有界调用集。
- 跨源只读 Tool（尤其 get_deployment_context）必须预先固定 source 顺序、每源最多一次请求、总调用上限 4 和总 deadline；逐源返回 status/provenance，单源失败形成 partial，不能在 Tool 内动态扩张调查。
- investigation.start mode 不做外部调用；investigation.step 每次只执行一次模型决定或一个只读工具。
- delivery.observe 根据当前 phase 每次只查询 GitHub、Argo 或 Kubernetes 中一个权威源。
- verification.advance 每次只处理一个 due check。
- 外部网络调用禁止放在长 MySQL 事务内。
- Task 完成、业务状态、Timeline 和下一 Task 必须同事务提交。
- 技术 retry 使用同一 task；业务 retry 创建新的 Run、Plan 或 VerificationRun。
- 自动 enqueue 的 replay_generation 固定为 0。Dead replay 创建同 dedupe_key、generation+1 的新 task并设置 replayed_from_task_id，原 dead row 不可改回 ready。
- Replay 前必须重新校验 subject expected version；已 stale 时拒绝 replay并要求创建新的业务 Run/Plan/Verification。外部副作用 reconciliation 使用跨 replay 稳定的 logical_operation_key，不能因 generation 变化创建第二个 PR。

### 10.6 错误分类

| 类型 | 行为 |
|---|---|
| Invalid / Policy / Security | 不重试，fail closed |
| Transient read | 指数退避 + jitter + deadline |
| 401 / 403 | 当前Task终止为config_error，Incident blocked/needs_attention并告警；V3无持久全局queue breaker，不宣称自动暂停所有Worker |
| 429 / 5xx / timeout | 按 adapter policy 重试 |
| Ambiguous external write | 只做 reconciliation，不直接重复 POST |
| Negative business outcome | 持久化领域失败并回到 investigating |
| Investigation dependency unavailable | 记录 unavailable source；required覆盖无法满足时AgentRun completed/insufficient_evidence |
| Verification dependency unavailable | 观察到deadline后VerificationRun inconclusive，Incident回investigating |
| Delivery dependency unavailable | merge前保持当前phase blocked；merge后更必须保持delivering/needs_attention并等待只读reconcile，不能在外部状态unknown时启动新Plan；不使用Inconclusive |
| Lease lost | 旧 Worker 禁止落库，外部副作用仍需 reconcile |

### 10.7 GitHub 外部幂等

Database fencing 无法阻止旧 Worker 已经发出的 GitHub 请求。因此必须同时使用：

- 每次 branch/commit/PR 调用前，在短事务写 external_write_started event，包含 logical_operation_key、expected base/tree 和 task generation；调用后追加 observed result。
- 确定性 branch。
- operation marker。
- expected base SHA。
- expected blob hash。
- 调用前查询。
- timeout 后查询 branch、commit、PR marker。
- 唯一 ChangeRequest。
- 禁止盲目重复创建 PR。

external_write_started 不是成功证明，但它是取消边界：marker 存在而结果未知时只能 reconcile，任何 Signal/用户命令都不得声称“尚未写”并启动竞争 Plan。

---

## 11. Agent Runtime

### 11.1 Agent 权限

V3 只保留一个 Incident-scoped、只读调查 Agent。

Agent 可以：

- 更新假设。
- 提出开放问题。
- 动态选择下一个只读工具。
- 引用 Evidence。
- 输出 Evidence-backed Diagnosis。
- 输出枚举型 remediation_hint。

Agent 禁止：

- 创建或修改 Incident 状态。
- 创建 AsyncTask。
- 生成 YAML、Git 命令或环境变量值。
- 调用 GitHub write、Argo sync、Kubernetes write。
- 决定 Verification 通过。
- 直接触发 Resolved。
- 修改预算、scope、repo、namespace、SHA 或权限。

### 11.2 Eino 边界

Eino 只负责：

- typed Graph。
- ChatModel 和 Tool 抽象。
- 节点组合。
- callback 接入。

项目代码负责：

- MySQL truth。
- async task。
- checkpoint。
- budget。
- retry、lease、fencing。
- state transition。
- Evidence validation。
- deterministic sufficiency。
- policy 和 approval。

不得使用 Eino memory/checkpointer 代替 MySQL，也不得采用 DeepAgent、多 Agent 或内置 shell/WebSearch。

### 11.3 持久化执行图

~~~text
LoadCheckpoint
→ BuildModelView
→ ProposeStateDelta
→ ValidateAndReduce
→ DeterministicSufficiency
   ├─ ExecuteOneAuthorizedRead
   │    → NormalizeEvidence
   │    → Persist
   │    → Requeue
   ├─ SynthesizeDiagnosis
   │    → DeterministicValidate
   │    → Complete Diagnosed
   └─ Complete InsufficientEvidence
~~~

每个 async task 只推进图的一步。不得把一个持续数分钟的内存 Eino loop 当作 durable scheduler。

### 11.4 InvestigationState

Checkpoint 只保存：

~~~text
schema_version
run_id / incident_id / cycle_no / incident_version
immutable correlation snapshot
objective
fixed query windows
coverage requirements
active / supported / rejected hypotheses
open questions
evidence IDs and fact refs
tool attempt signatures
unavailable sources
budget limits and usage
last applied delta
next runtime node
terminal outcome
checkpoint version / hash
~~~

Checkpoint 不保存：

- Provider 原始响应。
- 完整日志。
- 完整 diff。
- 完整 Prompt。
- chain-of-thought。
- 凭据。

### 11.5 StateDelta

模型不能回写整个 State，只能返回：

~~~text
StateDelta {
  basis_checkpoint_version
  hypothesis_ops[]       # add, support, weaken, reject
  question_ops[]         # add, answer, close
  proposed_action? {
    tool
    scope_ref
    template_id
    bounded_parameters
    expected_fact_types[]
    purpose_summary
  }
  proposed_stop          # continue, diagnose, insufficient
}
~~~

Reducer 必须校验：

- checkpoint version。
- schema 和 size。
- Evidence / Fact 引用归属。
- Incident scope。
- Tool allowlist。
- 参数模板。
- 重复 signature。
- budget。
- 当前状态是否允许。

模型提出的 stop 和 confidence 仅为建议。

### 11.6 运行预算

| 维度 | 默认 | 硬上限 |
|---|---:|---:|
| Semantic iterations | 8 | 12 |
| Tool calls | 8 | 12 |
| Model calls | 10 | 14 |
| Total model tokens | 16k | 32k |
| Evidence items | 20 | 40 |
| Total runtime | 180s | 300s |
| Checkpoint | 64 KiB | 128 KiB |

附加规则：

- 连续 Tool failure 最多 3 次。
- 相同 signature 只有 transient failure 可重试 1 次。
- Structured-output repair 最多 1 次。
- 外部调用前预留预算，调用后按实际 usage 结算。
- Provider 不返回 token 时使用保守估算。
- BudgetExceeded、长期 no-novel-evidence 或 required source 不可用应完成为 completed / insufficient_evidence。
- 只有 checkpoint 损坏、数据库不变量破坏或无法安全持久化才是 Run failed。

### 11.7 Model Provider

模型端口只保留：

~~~text
ProposeDelta(ModelView) → StateDelta
SynthesizeDiagnosis(DiagnosisView) → DiagnosisCandidate
~~~

规则：

- 单 Run 固定 provider、model、prompt version、tool schema version。
- 不在同一 Run 静默跨 Provider fallback。
- 使用 strict JSON schema、低温度和输出 token limit，但仍按不确定系统校验。
- 只持久化结构化输出、usage、request ID hash 和 content hash。
- 不建设多 Provider gateway、配额平台、在线学习或模型训练。

---

## 12. Agent 工具合同

告警上下文在 Run 创建时由确定性代码注入，不额外占用工具调用。

| Tool | 输入边界 | 输出 |
|---|---|---|
| inspect_workload | opaque scope_ref；服务端解析 cluster/namespace/workload | Deployment、ReplicaSet、Pod、Service、EndpointSlice typed snapshot |
| inspect_kubernetes_events | scope_ref、固定 window、limit ≤ 100 | 规范化 Event facts |
| query_metrics | template_id、固定 window、枚举参数 | series/count/rate/threshold facts + Grafana link |
| query_logs | template_id、window、severity/keyword enum、trace_id、limit | ES count、redacted samples、pod/version group + Kibana link |
| query_traces | template_id、window、status、trace_id、limit | Trace/span summary + Grafana/Tempo link |
| get_deployment_context | scope_ref、window | source/image/gitops identity、Argo history、最多 10 个 change refs |
| get_change_detail | opaque change_ref | allowlisted repo/path 的 bounded diff 和 CI facts |
| search_runbooks | bounded query、limit ≤ 3 | Git-managed BM25 fragments，仅 guidance |

禁止模型提交：

- 任意 PromQL。
- 任意 Elasticsearch DSL、Lucene、regex 或 index。
- 任意 TraceQL。
- 任意 shell、kubectl 或 URL。
- 任意 cluster、namespace、repo、Argo Application 或 SHA。

公共输出：

~~~text
ToolObservation {
  call_id
  signature
  status                # available, no_data, partial, unavailable, invalid
  source
  template_version
  resolved_scope
  time_range
  typed_facts[]
  counts
  provenance
  redaction_report
  truncated
  safe_deep_link
  canonical_content_hash
}
~~~

日志权威源只有 Elasticsearch。V3 最终删除 Loki 和 k8s.get_logs Agent 路径；ES 不可用时返回 insufficient_evidence，不用 Pod log fallback 掩盖日志链故障。

---

## 13. Evidence、可信度与 Prompt Injection

### 13.1 Evidence 定义

Evidence 是由项目代码从允许的数据源规范化出的不可变 typed fact。数据源可以是权威控制面，也可以是 runtime observation；authority 轴必须如实记录，不能把 Prometheus、日志或 Trace 提升为 desired-state 权威。

日志中的 count、按 pod/version 分组和经 schema 校验的错误码可以成为 Evidence；原始日志自然语言只是 instruction_untrusted 载荷，不能单独证明根因，也不得原样进入模型或 MySQL。

以下不是 Evidence：

- 模型摘要。
- 模型置信度。
- Runbook 建议。
- 未验证的 mutable tag。
- Commit message 或 PR body 中的声明。
- 原始或未经 schema 校验的日志自然语言。

Evidence 修正通过新行和 supersedes_id 完成，不覆盖历史。

### 13.2 Evidence 必需字段

~~~text
evidence_id / fact_ids
incident_id / cycle_no
producer_type / producer_id / producer_version / producer_dedupe_key
agent_run_id / agent_step_id nullable
verification_run_id / verification_check_id nullable
change_request_id nullable
fact_schema_version
source_system / adapter_version
query_template_id / version
scope_snapshot_hash
arguments_hash / time_range
observed_at / collected_at
typed_facts
deterministic_summary
collection_status
authority / integrity / freshness / completeness
claim_use
corroboration_group
input_evidence_ids / input_sample_ids / hashes
content_hash
source_revision / resource_version
redaction_policy_version / redaction_counts
prompt_safety_flags
safe_raw_reference
supersedes_evidence_id nullable
~~~

producer_type 冻结为 agent_step、verification_check、delivery_observation、system_enrichment；数据库 CHECK 必须保证相应外键组合一致，不能伪造 Agent step。Verification failure、no-change Verification 和 deterministic enrichment 均可合法生产 Evidence。

producer_dedupe_key 必须非空并按 producer 版本化构造：Agent tool 使用 signature/source version，Verification 使用 check/sample window，Delivery 使用 change event/revision，system enrichment 使用 enrichment kind/input hashes。Derived Evidence 必须列出全部 input Evidence/Sample，不允许隐式推导。supersedes_evidence_id 必须属于同一 incident_id + cycle_no，禁止自引用和环；修正只追加新行，旧行不可更新。

DeploymentBaseline 没有 Incident 归属，其健康探测写独立 baseline_observations，并在创建 Incident Evidence 时以新 producer 复制 bounded typed facts 和 provenance；不得把 NULL incident Evidence 混入全局证据池。

### 13.3 多轴可信度

| 数据 | 信任含义 |
|---|---|
| Kubernetes API、Registry digest、GitHub exact commit、Argo history | 对各自控制面事实 authoritative |
| Prometheus、Elasticsearch、Tempo、Alertmanager | runtime observation |
| Deterministic correlation | derived fact，必须链接输入 Evidence |
| Runbook | curated guidance，不能证明根因 |
| 日志、Event、diff、commit message、PR body | instruction_untrusted，即使来源已认证 |

no_data 只有在数据源健康、查询完整、window 位于 retention 内且未截断时，才能支持“未观察到”。partial、truncated、stale 只能辅助；unavailable 和 invalid 不能支持 claim。

### 13.4 Prompt Injection 防护

- 所有外部文本先进行字段 allowlist、长度限制、控制字符清理、Secret/高熵扫描和脱敏。
- 规范化和 deterministic summary 禁止调用 LLM。
- 外部文本作为明确 JSON data 传入，不拼接进 system prompt。
- 模型只能输出 enum、ID 和 schema 字段。
- Kibana/Grafana/GitHub链接由服务端构造。
- Prompt injection 检测只是标记和省略手段，最终权限仍由 reducer 与 Tool allowlist 保证。
- 安全 Eval 必须在 Log、Event、diff、PR body 和 Runbook 中植入恶意指令与 Secret canary。

### 13.5 确定性充分性

模型不拥有 coverage.sufficient。Deterministic evaluator 每轮输出：

~~~text
CONTINUE
READY_FOR_DIAGNOSIS
INSUFFICIENT_EVIDENCE

missing_facets[]
confidence_cap
reason_codes[]
~~~

READY_FOR_DIAGNOSIS 至少要求：

- Incident subject 已由权威资源快照确认。
- 存在有效 symptom Evidence。
- 存在 workload/runtime Evidence。
- 主假设至少得到两个独立 corroboration_group 支持。
- 至少一个 supporting fact 为 direct fact。
- 不存在未解释的更高权威反证。
- 所有引用属于当前 Incident。
- Runbook 不计入因果覆盖。

deterministic 不只是命名。每个 Run 必须绑定版本化 ClaimPolicy 及其 hash，至少定义：

- fact schema/type 到 authority、claim_use 和 corroboration_group 的固定映射。
- 两个 fact 只有在 source system 或独立 collection path 不同、且不存在相同 derived parent 时才算独立；同一原始样本的多个摘要不能重复计票。
- claim type 对 required/optional/forbidden fact group 的 truth table。
- authority、integrity、freshness、completeness 和反证的确定性优先级；未解释的更高权威反证一律阻止 confirmed。
- 每个 claim 的正例、缺证据、同源伪独立、stale、冲突和高权威反证 fixture。

黄金 claim required_env_config_regression/v1 的 confirmed truth table 冻结为：

| 条件组 | 必需事实 |
|---|---|
| desired-change | GitHub exact diff 证明 bad GitOps SHA 删除 allowlisted REQUIRED_ENV |
| deployed-state | Argo successful revision 等于 bad SHA，Kubernetes Pod spec 确认该 env 缺失 |
| identity-control | source revision 与 image digest 相对 verified baseline 均未改变 |
| runtime-symptom | readiness/5xx Metric 与结构化 env-missing Log 同窗出现，Trace 提供独立请求失败支撑 |
| blocking contradiction | 当前 Pod spec 含该 env、Argo 未部署 bad SHA、或 source/image 同时变化中的任一项 |

前四组全部满足且不存在 blocking contradiction 才能 confirmed；否则只能 likely、unknown 或 insufficient_evidence。ClaimPolicy 的 canonical hash 与输入 Evidence ID 必须写入 Diagnosis。

对于黄金配置回归，还必须证明：

- bad GitOps SHA 的 diff 精确删除必需 env。
- Argo 实际部署 bad GitOps SHA。
- source revision 和 image digest 未改变。
- workload、Metric、Log 与配置缺失一致。

Trace 是黄金场景的 required 支撑信号，因为 Demo load generator 保证有请求；通用 Incident 不机械要求每种工具全部调用，否则 Agent 会退化为固定流水线。

模型生成的自由文本根因默认最高为 likely；只有由 deterministic domain validator 支持的 typed claim 才能标记 confirmed。

---

## 14. 云原生可观测性底座

### 14.1 最终组件

| Signal | 组件 | 采集与管理 | Agent Port |
|---|---|---|---|
| Metrics / Alerts | Prometheus Operator、Prometheus、Alertmanager、Grafana | ServiceMonitor、PodMonitor、PrometheusRule | MetricReader |
| Logs | ECK Elasticsearch、Kibana、Filebeat | ECK Operator + Beat CR DaemonSet | LogReader |
| Traces | OTel SDK、OTel Collector、Tempo、Grafana | OTLP traces pipeline | TraceReader |
| Resources / Events | Kubernetes API | official client-go | KubernetesReader |
| Changes | GitHub、Registry、Argo CD | fixed-host read adapters | DeploymentContextReader |

删除：

- VictoriaMetrics。
- Jaeger。
- Loki。
- Fluent Bit / Fluentd。
- Logstash。
- Elastic Agent / Fleet。
- 手写 Elasticsearch / Kibana StatefulSet。
- OTel 日志采集。
- 通用 Kubernetes Dashboard。

### 14.2 Prometheus 与 Alertmanager

- 使用稳定的 monitoring.coreos.com/v1 ServiceMonitor、PodMonitor 和 PrometheusRule。
- CloudOps 不创建 AlertRule CRUD，不直接写 Prometheus 配置。
- Alertmanager 负责分组、去重、路由、silence 和 inhibition；CloudOps 仍负责 Webhook 幂等和 Incident 聚合。
- 唯一 CloudOps Webhook receiver 放入版本控制的 Alertmanager Helm 配置，并显式设置 send_resolved: true。
- Webhook Bearer 通过 Alertmanager http_config.authorization.credentials_file 读取 Secret volume；Secret 值禁止内联到 Helm values 或 Git。
- 不为单一 receiver 依赖仍为 v1alpha1 的 AlertmanagerConfig。
- Alertmanager 一次 Webhook 可以包含多个 alerts。API 必须先完整校验并规范化整个 envelope，再按 correlation_key 分组、按 key/source_event_id 确定性排序。每组持有 correlation lock，先幂等持久化组内全部 Signal并重算 firing set，然后最多执行一次 Incident 状态迁移/入队；禁止“逐 alert 写一条就立即决策”。各组可用独立短事务，进程中途失败后由 Alertmanager 重投。
- 同一组同时含 resolved 与 firing 时，必须以全部 Signal 落库后的最终 firing set 为准；只要集合非空就不能启动 no-change Verification。
- 只有整个 batch 的所有组均成功、已去重或形成明确 signal_rejection 时才返回 2xx。
- source_event_id 不是 Alertmanager 提供的天然 ID。V3 固定为 SHA-256(version、source、alert.fingerprint、startsAt UTC、alert.status、resolvedEndsAt 的 canonical encoding)：firing 的 resolvedEndsAt 永远为空，只有 resolved 才写真实 endsAt。receiver/groupKey 只存 provenance，不参与事件身份，避免重复 firing 因分组或预测 endsAt 漂移变成新 Signal；canonical schema version 必须持久化并有 fuzz/contract test。
- Prometheus 保持 admin API 和 lifecycle API 关闭；Worker 只配置 Prometheus Query API endpoint 和固定模板 adapter，不配置 Alertmanager endpoint 或凭据。
- Prometheus 在该 kind 拓扑中没有 per-client query RBAC。因而“Worker 只读”是代码与配置能力边界，不是抵御 Worker 完全失陷的基础设施隔离；不得把它宣传为 Prometheus 侧 IAM 保证。

### 14.3 ECK 与 Filebeat

ECK 负责：

- Elasticsearch 和 Kibana 生命周期。
- TLS、关联凭据和安全连接。
- Beat CR 协调和证书轮换。

Filebeat 负责：

- 读取 CRI container log。
- 由两个 namespace-scoped Kubernetes autodiscover provider 发现 CloudOps 和 Demo Pod，并补充 namespace、pod 和 container metadata。
- 直接写入 Elasticsearch。
- 使用有界 memory queue；V3 不宣称 durable log buffer。

V3 MUST：

- 使用 Beat DaemonSet。
- 使用 filestream input。
- 为每个 filestream 配置稳定唯一 id。
- 使用 container parser 和 symlink scanner。
- 只有 CloudOps 与 Demo 两个 namespace 的 autodiscover template 可以启动 filestream；不得在读取全节点日志后仅靠 drop_event 过滤。
- Filebeat ServiceAccount 只通过两个 namespace 内的 RoleBinding 获得 pods 的 get/list/watch；不得为 metadata enrich 授予全局 Secret、Node 或 workload 写权限。
- Beat path.data 使用每节点独立 hostPath 保存 registry；Pod 重建后的 offset 恢复、重复边界和 memory queue 丢失限制必须通过 contract test 记录。
- 负向测试必须向无关 namespace 写入唯一 canary，并证明该 canary 未进入目标 data stream。
- 使用有界 data stream / alias。
- 为 CloudOps 配置只读 Elasticsearch role。
- 保持 ECK 内部 TLS。

V3 MUST NOT：

- 使用已弃用的 log 或 container input。
- 设置 allow_deprecated_use。
- 引入 Logstash 或 Kafka 中转。
- 为了 Fleet 引入 Elastic Agent 和 Fleet Server。

兼容风险：

- Beat CRD 当前仍为 v1beta1。
- ECK 3.4.1 尚未正式弃用 Beat v1beta1，但这仍是需锁定和测试的 beta API 风险，文档不得写成“已弃用”。
- Elastic Stack 7.17 已被 ECK 标为弃用，实施兼容矩阵禁止选择 7.17；必须从受支持的 8.x/9.x 中固定 ECK、Elasticsearch、Kibana、Filebeat 的同一组实测版本。
- ECK/Elastic Stack 必须在实施时形成经过测试的版本矩阵。
- 若 Beat CR 未来移除，只替换日志部署层；LogReader 和 Evidence contract 不变。

### 14.4 OpenTelemetry Collector 与 Tempo

Collector 只启用 traces pipeline：

~~~text
OTLP receiver
→ memory_limiter
→ k8sattributes
→ bounded resource/attribute processing
→ batch
→ OTLP exporter
→ Tempo
~~~

不启用 filelog、Prometheus receiver 或 kubeletstats，避免与 Filebeat 和 Prometheus 重复。

Collector 以单个 Deployment gateway 运行，并冻结以下合同：

- OTLP gRPC/HTTP receiver 必须显式监听 0.0.0.0:4317 和 0.0.0.0:4318，再由 ClusterIP Service 暴露；不得依赖 Collector 的 localhost 默认值，也不创建外部 Ingress。
- Demo 与 CloudOps Pod 通过 Downward API 注入 k8s.pod.uid 等关联种子；k8sattributes 只在 CloudOps 与 Demo namespace 读取 pods/replicasets 的 get/list/watch，用于补齐 namespace、pod 和 workload owner。
- pod association 优先使用连接来源，Pod UID fallback 必须经 Kubernetes cache 验证；Collector 提取值必须覆盖客户端自报的 k8s.* 属性。负向测试从另一 Pod 伪造目标 UID/namespace/workload，必须不能生成目标身份的 Trace。
- 不授予 Collector Secret、Node 或写权限。若实际版本需要扩大 RBAC，必须通过 ADR、kubectl auth can-i 清单和负向测试重新审批。
- Contract test 必须从真实 Demo span 证明 service、namespace、workload、pod、source revision 和 trace_id 等必需属性均存在。
- kind 中未部署支持 NetworkPolicy 的 CNI，也未为 OTLP sender 建立认证。因此 ClusterIP 不是强信任边界，恶意集群内 Pod 不在 V3 威胁模型；Trace 始终只是 runtime observation，不能单独确认根因或恢复。Verification 只接受能与当前 Kubernetes Pod、Incident scope、其他信号和时间窗交叉确认的 Trace。

Tempo 使用 3.x monolithic / single-binary Chart：

- 只安装 tempo 单体 Chart 并固定 target=all；禁止 tempo-distributed。
- 禁止复用 Tempo 2.x values。
- kind 使用短期本地存储。
- 最终 rendered config 中禁止 ingest.kafka；启动和查询 contract test 必须证明无 Kafka 依赖。
- Tempo 只在集群内部开放写入与查询。
- Worker 只配置 Tempo Query API 和固定 TraceReader，不配置 OTLP exporter。Tempo 在该拓扑同样没有 per-client query RBAC，因此只读性属于代码/配置能力边界，不是完全失陷后的 IAM 保证。

### 14.5 统一关联属性

至少统一：

~~~text
service.name
service.version
deployment.environment.name
k8s.cluster.name
k8s.namespace.name
k8s.workload.kind
k8s.workload.name
k8s.pod.name
container.image.name
container.image.digest
cloudops.source.revision
cloudops.gitops.revision
trace_id
alert.fingerprint
~~~

属性来源冻结为：

- Demo/CloudOps SDK 与 Downward API 提供 service.name、service.version、source revision、k8s.pod.uid 和基础 resource attributes。
- Collector k8sattributes 根据 Pod UID 补齐 namespace、pod 和 workload owner。
- image digest、source revision 和 gitops revision 的权威值仍分别来自 Kubernetes/Registry/GitHub/Argo；遥测属性只用于查询关联，不能覆盖权威部署身份。

必须明确：

- service.version 不能代替 source_revision。
- source_revision 不能代替 image_digest。
- image_digest 不能代替 gitops_revision。
- Pod UID、trace ID 和 span ID 只作为 Evidence identity，不作为 Incident correlation identity。
- 遥测 label 不能单独证明 revision，必须通过 Kubernetes、Registry、Argo 和 GitHub 权威链确认。

Incident、Run、Evidence、Task 和 User ID 不得成为 Prometheus 高基数 label；它们只进入结构化日志和 Trace。

---

## 15. Demo Workload 与故障注入

### 15.1 Demo Service

最终 Demo 是一个极小 Go 服务：

- 一个 Deployment。
- 默认两个轻量副本。
- /livez 始终返回 200。
- /readyz 在 REQUIRED_ENV 缺失时返回 503。
- 业务接口在配置缺失时返回可控 5xx。
- /metrics 暴露 request、error、readiness 指标。
- 输出结构化 JSON log。
- 为业务请求输出 OTLP span。
- /version 返回 service version 和 source commit。

镜像 digest 由 Pod status.imageID 获取，不由应用自报。

缺少 REQUIRED_ENV 时进程不能退出，否则无法持续产生 Metric、Log 和 Trace。

### 15.2 Load Generator

场景使用一个临时 load-generator Job：

- 只在 golden E2E 期间存在。
- 只通过固定 demo-diagnostics ClusterIP Service 持续请求 Demo；该 Service 设置 publishNotReadyAddresses=true，允许在业务 Pod NotReady 时继续制造流量，不授予 Job Pod list/watch 权限。
- diagnostics Service 不创建 Ingress、不进入用户产品入口，rendered-manifest Policy 只允许 normal 与 diagnostics 两个固定 Service 名称。
- 固定 5 requests/s、concurrency=1、request timeout=2s、最长30分钟；Demo Trace sampler 在 Golden 场景为 always_on，保证 Verification 的50 requests/20 spans下限可满足。
- 在 Pod 不 Ready 时仍能制造真实 5xx、Log 和 Trace。
- 不进入产品部署、不成为新微服务。

这些参数只为可重复信号，不是压测或吞吐成果。

### 15.3 故障注入

故障必须通过 GitOps regression PR 注入：

~~~text
remove REQUIRED_ENV from Deployment
→ human merge
→ Argo auto sync
→ readiness/error regression
~~~

make scenario-open-regression-pr 只允许在操作者本机用 human gh 身份生成固定 regression branch/PR，CloudOps runtime 与 GitHub App 不拥有“制造故障”能力；命令不得 merge，最终仍由人审查并合并。Regression actor/PR/head/merged SHA 必须进入 Evidence Manifest。

禁止：

- kubectl scale。
- kubectl patch。
- 直接修改 live object。
- 调用 demo-only repair API。
- Controlled direct local remediation 冒充 GitOps E2E。

---

## 16. Change Intelligence 与部署身份

### 16.1 三种 revision

始终分列：

| 字段 | 含义 | 权威来源 |
|---|---|---|
| source_revision | 应用源代码 commit | Registry OCI label + GitHub exact commit |
| image_digest | Pod 实际运行不可变镜像 | Kubernetes imageID + Registry manifest |
| gitops_revision | Argo 实际部署配置 commit | Argo Application/history |

黄金配置回归只改变 gitops_revision；source_revision 和 image_digest 应保持不变。若把三者混为 revision，Change Correlation 必然产生错误因果。

Mutable tag 如 latest 永远不是权威身份。

### 16.2 ChangeCandidate

ChangeCandidate 是一次 AgentRun 发现的不可变部署变更快照，至少保存：

~~~text
incident_id / agent_run_id
cycle_no
change_ref
source_type
repository
commit_sha
gitops_revision
image_digest
path
category
time
supporting Evidence
content hash
~~~

ChangeCandidate payload 永远不可变。状态由 append-only change_candidate_assessments 表达：

~~~text
candidate_id
status: matched / excluded / unknown
supporting / contradicting Evidence IDs
validator_version / policy_hash
created_at
supersedes_assessment_id nullable
~~~

Latest valid assessment 只用于 projection，不更新 Candidate 原行。Agent 可选择候选并调查，但不能写 assessment 或自动提升为 matched；Matched 必须由 deterministic correlation/Diagnosis validator 产生。

### 16.3 DeploymentBaseline

restore_required_env 只能从经过验证的 last-known-good revision 读取原节点。

Phase 3 的 demo-up 只运行 baseline readiness probe，证明数据路径健康但不写 deployment_baselines。Phase 5 引入表和权限后，才运行一次性 baseline-verifier Job：

- 使用同一 Worker image 的受限 subcommand。
- 查询 Argo exact revision、Kubernetes readiness、Alert 状态和必要观测信号。
- 保存 gitops_revision、source_revision、image_digest、config hash 和独立不可变 baseline_observations，不写 Incident Evidence。
- 一个 target 同时只有一个 active verified baseline。

新修复通过最终 Verification 后，该 revision 成为下一 baseline；旧 baseline 变为 superseded。

无 verified baseline、祖先关系不成立或 source/blob 无法验证时，Remediation 必须停止，不得猜测恢复值。

Incident 需要引用 baseline 时，由 system_enrichment 在当前 cycle 创建 bounded Evidence，并保存原 baseline_observation ID/hash；Baseline 自身不能跨 Incident 充当全局 Evidence。

---

## 17. RemediationPlan 与审批

### 17.1 唯一动作

V3 只允许：

~~~text
restore_required_env
~~~

约束：

- 一个 GitOps repository。
- 一个 base branch。
- 一个固定 Deployment 文件。
- 一个 allowlisted workload/container。
- 一个 allowlisted 非 Secret env key。
- 一次最多修改一个文件、一个字段。

禁止：

- Secret。
- ConfigMap 任意编辑。
- RBAC、CRD、Namespace、PVC。
- SecurityContext。
- 镜像。
- Replicas。
- 删除资源。
- 任意 YAML。
- 任意 rollback。

### 17.2 Agent 与 Go 的权限分界

Agent 只输出：

~~~text
operation_hint = restore_required_env
target field ref
last-known-good Evidence ID
supporting Evidence IDs
~~~

Go 服务：

1. 读取 exact base SHA。
2. 读取 verified last-known-good SHA。
3. 验证祖先关系。
4. 解析 YAML AST。
5. 从 baseline 复制完整 env node。
6. 生成 canonical post-image。
7. 生成 bounded unified diff。
8. 执行 Policy。
9. 固化 VerificationPlan。
10. 计算所有 hash。

模型不能生成变量值、patch、repo、branch 或 Git 命令。

### 17.3 Plan 内容

Plan 持久化前已完成 patch 编译和 Policy 检查，不保存可变半成品 Draft。

至少包含：

~~~text
incident_id / version
cycle_no
created_by_agent_run_id
diagnosis_hash
plan_version
operation
repository / base_sha / last_known_good_sha
target path / resource / field
expected_before_hash
expected_post_image_hash / expected_tree_hash
canonical change manifest
patch_hash
bounded full diff
policy_snapshot / hash / version
verification_plan / hash
Evidence IDs / hashes
risk
canonical_plan_hash / hash_schema_version
created_at / expires_at
~~~

patch_hash 哈希版本化 canonical change manifest，而不是展示文本。Manifest 至少包含 path、base blob SHA、file mode 和 post-image hash。

canonical_plan_hash 使用 hash_schema_version 固定的长度前缀 canonical encoding，覆盖除自身外上述全部不可变字段，包括 bounded full diff 的精确字节、Policy snapshot、VerificationPlan、确定性排序的 Evidence ID/hash set、risk 和 expires_at；不得只用 patch_hash 代替完整 Plan 绑定。审批时尚不存在的 PR head SHA 和 merged commit SHA 不进入 canonical_plan_hash，后续只能通过已批准的 base/post-image/tree 与当前 PR tree 的一致性校验建立关联。

### 17.4 Approval

Approval 是不可变 Decision：

~~~text
plan_id / plan_version
decision: approved / rejected
actor identity: provider=github + login
role
reason
request_id
request_authenticated_at
created_at / expires_at
approved canonical plan hash / hash schema version
approved base SHA
approved post-image / tree hash
approved patch hash
approved policy hash
approved verification hash
approved Evidence set hash
~~~

Approval 不直接创建 ChangeRequest。change.ensure_pr 的首个无外部调用模式必须按以下顺序执行 preflight：

1. Incident、Plan、Decision 均属于当前 cycle，Plan/Decision 均未过期，Decision=approved且未使用。
2. 每个引用 Evidence 仍属于当前 cycle，content hash匹配，不存在更新的 superseding Evidence，全部 derived input仍有效。
3. 用当前 ClaimPolicy/Evidence 重新执行 sufficiency 与 Diagnosis validator，结果仍支持同一 diagnosis_hash/operation。
4. Base ref 仍等于 base_sha，Current blob 仍等于 expected_before_hash。
5. 当前 Policy 重算后仍等于 policy_hash。
6. Patch、post-image、VerificationPlan 和 Evidence set hash均未改变。
7. 由当前 base 和批准后的 post-image 重新计算出的 tree hash 仍等于 approved tree hash。

顺序冻结为：

~~~text
Plan approved + immutable Decision
→ read-only preflight PASS
→ short MySQL transaction recheck versions/hashes
→ create unique ChangeRequest(pending)
→ Plan approved → consumed
→ enqueue change.ensure_pr (write_phase=ensure_branch)
~~~

branch、commit、Draft PR 每个 write_phase 写 external_write_started marker 前，都必须重跑受该阶段影响的 expiry、cycle、Evidence superseder、base/tree 和 Policy checks。

Draft PR 创建后不再要求 Approval 持续未过期，但 delivery.observe 在接受 merge/delivered 前仍必须确认 Plan 未 invalidated、获批 Evidence 无 superseder、merged tree等于批准值；否则只能阻塞/回流，绝不能进入 passing Verification。

任一不一致：

~~~text
before ChangeRequest:
  Plan → superseded; Incident → investigating
after consumed but before any external write:
  Plan → invalidated; ChangeRequest → superseded; Incident → investigating
after any write intent/result:
  Plan → invalidated; stop further writes
  external state unknown/present → keep ChangeRequest phase + Incident delivering/needs_attention; reconcile only
  external state proven safe/terminal → ChangeRequest failed; Incident → investigating
enqueue investigation.start/remediation.prepare only when no external state remains unknown/present
# AgentRun 仍只能由 investigation.start Task 创建
~~~

禁止静默 rebase 或复用旧审批。

Approval 只能绑定审批时已经存在的 base、post-image、tree、patch、Policy 和 VerificationPlan；未来的 merged_commit_sha 在人工 merge 前不存在，绝不能伪装成“已审批 revision”。

---

## 18. GitHub 与 Argo CD 边界

### 18.1 GitHub 身份

使用两个身份，但只有一个机器 App：

- GitHub OAuth App：只用于人工登录。
- GitHub App：只安装到 demo GitOps repo，用于读取 GitOps change、创建 branch/commit/Draft PR 及读取 Actions/Checks。

源码仓库在 V3 范围内保持公开，source commit 采用 fixed-host GitHub public read 和 Registry OCI metadata 交叉确认。若 V3 之后将源码仓库转为私有，必须通过新 ADR 增加独立 read-only observer credential，不能扩大 GitOps 写 App 到源码仓库。

机器 App 权限：

- Metadata: read。
- Contents: write。
- Pull requests: write。
- Actions: read。
- Checks: read。

代码和配置必须同时约束：

- 固定 repository。
- 固定 base branch。
- 固定 path。
- 固定 branch prefix。
- 单文件和 content size 上限。
- 禁止 workflow path。
- 禁止 force push。
- adapter 中没有 merge API。

GitOps repo 必须启用：

- required checks。
- human review。
- default branch protection/ruleset。
- 只允许 squash merge，并要求分支在 merge 前与批准的 base 保持 up to date。
- App 不可 bypass。
- 禁止直接 push main。

contents:write 在 GitHub IAM 层无法完全阻止一个已完全失陷的 Worker 调用 merge；首版依赖受限 adapter、无 merge 代码、branch rules 和审计。若未来需要抵御 Worker 完全失陷，必须另建 PR broker，当前不实现。

### 18.2 确定性 GitHub 写

Branch：

~~~text
cloudops/incident-<incident-id>/plan-<canonical-plan-hash-prefix>
~~~

PR body 必须含不可变 operation marker、Incident ID、Plan ID、canonical plan hash 和 Evidence links。

外部写 timeout 后：

1. 查询 branch ref。
2. 查询 expected commit tree。
3. 查询 PR marker。
4. 内容一致则复用。
5. 内容不一致则 Conflict，禁止覆盖。

### 18.3 CI

- CI 必须绑定当前唯一 Remediation PR head SHA，并先证明该 head 的 tree/post-image 与 Approval 绑定的 tree/post-image hash 一致。Approval 不绑定审批时尚不存在的未来 PR head SHA。
- CloudOps 配置冻结 required check contract：check name、producer GitHub App ID、Actions workflow ID/path。
- 只接受 head_sha 等于上述已验证的当前 PR head、status=completed、conclusion=success 且 producer/workflow 全匹配的 Check/Workflow run。
- GitOps repo ruleset 的 required status check 同样固定预期 integration ID；live ruleset audit 必须证明 App 不可用同名第三方 check 冒充。
- 旧 SHA、PR overall 状态或 legacy combined status 不能代替当前 head checks。
- CI failed、PR closed、base 前移或 head/tree 被外部修改时，ChangeRequest failed，Incident 返回 investigating；不得在旧 Approval 下执行 update branch 或重新计算 merge。
- CloudOps 不重跑 workflow、不修改 workflow、不 merge。

### 18.4 Argo CD

AppProject 只允许：

- 固定 GitOps repo。
- 固定 demo namespace。
- apps/Deployment。
- core/Service。
- monitoring.coreos.com/PodMonitor。
- monitoring.coreos.com/PrometheusRule。
- 禁止 cluster-scoped resource、Secret 和 RBAC。

AppProject 只能约束 repo、destination、namespace 与 resource kind，不能约束具体对象名称。固定 Demo Deployment/Service 名称由单 source Application path、rendered-manifest CI Policy 和 GitHub adapter/path allowlist保证；不得把名称限制宣传为 Argo RBAC 能力。

CloudOps Argo identity 只允许指定 project/application 的 get：

- 禁止 sync。
- 禁止 override。
- 禁止 action。
- 禁止 rollback。
- 禁止 repository 或 cluster 管理。

Argo Application 必须是单 source，固定 repo、path 和目标分支，使 status.sync.revision 保持单 SHA 语义。同步策略冻结为：

- automated.enabled=true。
- selfHeal=true。
- prune=false。
- allowEmpty=false。
- retry.limit=5，backoff duration=5s、factor=2、maxDuration=3m。

CloudOps/CI 只提交 Git，不调用 Argo API 执行部署。任何版本升级都必须通过 rendered manifest 和瞬时 sync failure contract test 证明上述语义仍成立。

Delivery exactness：

~~~text
approved base_sha + approved post-image/tree/patch hash
→ PR head tree == approved tree
→ human squash merge
→ fetch merged_commit_sha
→ merged commit target blob/post-image/tree == approved values
→ merged_commit_sha == Argo status.sync.revision
→ merged_commit_sha == successful syncResult.revision
~~~

若 Argo 跳过该 SHA、直接部署后继提交，ChangeRequest 必须 failed(reason=revision_superseded)，Incident 回 investigating，且不得创建 post-delivery VerificationRun；不能因为后继提交“可能包含修复”而通过黄金场景。

---

## 19. Recovery Verification

### 19.1 Delivery 不等于恢复

ChangeRequest delivered 只表示：

- PR 已由人合并。
- Argo 检测 exact merged SHA。
- Sync operation 成功。
- Deployment 已观察新 generation。
- Rollout 满足进入恢复验证的前置条件。

Synced、Healthy、PR merged 或 Pod created 均不能直接 Resolved。

### 19.2 VerificationProfile

黄金场景 required checks：

1. argocd_exact_revision。
2. argocd_sync_succeeded。
3. deployment_observed_generation。
4. deployment_rollout_complete。
5. workload_ready。
6. incident_alerts_resolved。
7. metric_error_rate_below。
8. metric_availability_above。
9. log_required_env_error_absent。
10. trace_error_rate_below。

每个 CheckSpec 固化：

~~~text
type
subject
template_id / version
expected
comparison
threshold
lookback
initial_delay
poll_interval
timeout
stability_window
required
source identity
~~~

VerificationPlan 在审批前生成并参与 hash，审批后不可变化。

黄金 Profile 固定为 golden-required-env/v1，Run 总 deadline 为 300s。load-generator 在整个验证期持续产生请求，以下 min samples 未满足时必须 no_data，不能按 0 错误通过：

| Check | Predicate | Initial / lookback | Poll / timeout | Samples | Failure mode |
|---|---|---|---|---:|---|
| argocd_exact_revision | sync.revision 与 successful syncResult.revision 均等于 target SHA | 0 / current | 5s / 180s | 1 | mismatch immediate |
| argocd_sync_succeeded | target SHA operation succeeded | 0 / current | 5s / 180s | 1 | terminal sync failure immediate |
| deployment_observed_generation | observedGeneration 等于 generation | 0 / current | 5s / 180s | 1 | stale resets |
| deployment_rollout_complete | updated=ready=available=desired=2，且无 unavailable | 0 / current | 5s / 180s | 1 | ProgressDeadlineExceeded immediate |
| workload_ready | 两个 Pod Ready 且 /readyz 成功 | 0 / current | 5s / 240s | 2 Pods | negative resets |
| incident_alerts_resolved | MySQL中当前cycle全部alert_instance均resolved；固定PromQL按alertname + allowlisted target labels证明ALERTS中无firing series | 0 / 30s | 10s / 240s | 3 polls | firing resets |
| metric_error_rate_below | 5xx / requests < 1% | 30s / 30s | 10s / 300s | 50 requests | threshold miss resets |
| metric_availability_above | success / requests >= 99% | 30s / 30s | 10s / 300s | 50 requests | threshold miss resets |
| log_required_env_error_absent | structured required_env_missing count = 0 | 30s / 30s | 10s / 300s | valid ES query | positive hit resets |
| trace_error_rate_below | error spans / request spans < 1% | 30s / 30s | 10s / 300s | 20 spans | threshold miss resets |

这些阈值是 V3 黄金场景的判定参数，不是性能成果或生产 SLO。任何调整都必须产生新 profile version/hash；post-delivery Plan 必须重新审批新 hash，no-change Run 也必须固化其 profile hash。

Prometheus ALERTS series 不提供 Alertmanager fingerprint，MetricReader 禁止伪造该关联。Verification 从当前 cycle Signal 得到 bounded alertname 集合，再用服务端模板逐个绑定 cluster/environment/namespace/workload labels 与 alertstate=firing；MySQL alert_instance projection 和 Prometheus firing absence 两者必须同时成立。

no-change/v1 是独立可执行 Profile，不能复用包含 PR/CI/sync-result 的 post-delivery 列表。Run 创建时固化当前 source_revision、image_digest、gitops_revision，并要求：

| Check | 判定 |
|---|---|
| deployment_identity_unchanged | Argo current revision、Kubernetes imageID 和 Registry source/image identity 在共同窗口内始终等于 Run snapshot |
| deployment_rollout_complete | 当前 generation 已 observed，desired=updated=ready=available，且无 unavailable |
| workload_ready | 两个 Pod Ready 且 /readyz 成功 |
| incident_alerts_resolved | trigger resolved Signal 属于当前cycle，MySQL中全部alert_instance已resolved，且固定PromQL按alertname + target labels无firing series |
| metric_error_rate_below / availability_above | 沿用 golden-required-env/v1 的阈值和 min requests |
| log_required_env_error_absent | 沿用相同 ES query、lookback 和 absence 语义 |
| trace_error_rate_below | 沿用相同阈值和 min spans |

no-change/v1 同样使用 300s deadline 和所有 required checks 的共同 60s 稳定窗口；它不检查 PR、CI 或 successful syncResult，不需要 Approval，也绝不创建 GitHub/Argo/Kubernetes 写。

### 19.3 Sample 与稳定窗口

每次 verification.advance 只采一个 due check，保存 bounded VerificationSample。

规则：

- 所有非 immediate 的 Required check 都必须连续满足 60s；Run 只有在所有 required checks 的成功区间存在同一个连续 60s 交集时才能 passed。实现上共同窗口起点取各 check 当前 consecutive_success_since 的最大值，并在窗口内持续重验 exact identity。
- failure mode=immediate 的确定性终态负面直接 failed；Failure mode=resets 的有效负样本只清空该 Check 的 consecutive_success_since，继续观察到 deadline，不能同时又立即把 Run 标记 failed。
- no_data 只有在查询有效、source 健康、retention 覆盖且语义明确时才能用于 absent check。
- Source unavailable 持续到 deadline → inconclusive。
- immediate Check 明确不满足 → failed。
- resets Check 数据可用但 deadline 前未形成共同稳定窗口 → timed_out。
- Optional check 不阻塞通过，但必须展示。

### 19.4 失败回流

failed、timed_out 或 inconclusive 终态事务：

~~~text
persist failed checks as Evidence
append Timeline
Incident → investigating
enqueue one incident-scoped investigation.start
# 该 Task 在下一事务唯一创建新 AgentRun
~~~

每个 Incident cycle 的闭环预算冻结为：

| 新建记录 | 默认上限 | 配置硬上限 |
|---|---:|---:|
| AgentRun | 3 | 5 |
| RemediationPlan | 3 | 5 |
| VerificationRun | 3 | 5 |

人工“重新调查”只创建 investigation.start Task；该 Task 创建新 AgentRun 时原子计数。stale base 产生的新 Plan、Verification 失败后 start Task 创建的新 Run、no-change Verification 均计入所属 cycle。AsyncTask 技术 retry、lease takeover 和同一 Run 的 step 不增加业务计数。resolved 合法 reopen 时 Incident cycle_no 递增，才重置该 cycle 的预算。

创建下一记录和 enqueue 前必须在同一事务内检查计数。达到默认上限后 Incident 保持打开并标记 needs_attention，且不再自动入队；operator 只能在不超过硬上限时显式重试并留下 Decision/Timeline。达到硬上限只能关闭或通过新 ADR/配置发布调整，禁止请求参数临时绕过。真实 MySQL 测试必须证明并发重试不能越界，达到上限后没有孤儿 Task。

Rollback 也只能是新的 GitOps Plan、新审批和新 PR；首版不实现自动 rollback。

### 19.5 ResolutionReport

Verification passed 时同步生成结构化 ResolutionReport：

- Incident ID、cycle_no 与该 cycle 的时间。
- 该 cycle 的 trigger/初始 Signal。
- 最终 Diagnosis（若存在）和 Evidence。
- bad/fix GitOps SHA。
- source revision 和 image digest。
- Plan、Approval、PR、CI、Argo。
- VerificationRun 和稳定窗口。
- 实测时长和 Agent usage。

ResolutionReport 不调用 LLM，也不产生新根因。

post-delivery report 必须包含 Diagnosis、Plan/Approval/PR/CI/Argo；no-change report 允许 Diagnosis、Plan、Approval、PR、CI、Argo 为空，必须记录 resolution_reason=recovered_before_diagnosis 或 recovered_without_change、resolved trigger Signal、当时三类 revision、profile hash 和全部 Verification Evidence，且不得补写伪根因。每个 cycle 只能由其 passing VerificationRun 生成一份不可变 report。

---

## 20. 身份、安全与权限

### 20.1 Human Auth

采用 oauth2-proxy sidecar + GitHub OAuth：

- oauth2-proxy 与 API 同 Pod。
- 用户 API listener 只监听 127.0.0.1:USER_PORT，且只接受同 Pod oauth2-proxy 转发。
- 用户入口 Service 只暴露 oauth2-proxy；Pod 内部 listener 使用另一个端口和 Service，不得共用用户入口。
- GitHub 用户 allowlist 映射为 viewer/operator。
- oauth2-proxy 必须启用并覆盖可信 X-Auth-Request-User；必须清除客户端传入的同名 X-Auth-Request-*、X-Forwarded-User 和 Authorization 身份头。
- OAuth access token 只用于身份获取，随后不把 access token、Authorization 或 oauth2-proxy session cookie 转发给 API，也不用于 GitHub write。
- CloudOps 将审计 actor 记录为 provider=github + GitHub login + request_authenticated_at。当前 oauth2-proxy 合同不提供可证明的 GitHub numeric user ID或原始 OAuth auth_time，login 可变限制必须写入审计文档，不得称其为不可变 subject。
- Contract test 必须从用户入口提交伪造身份头，并证明 API 只能看到 proxy 覆盖后的身份；从另一 Pod 直接访问 loopback listener 必须不可达。
- 删除本地用户注册、密码、初始 admin 和前端 Bearer JWT。

本地 port-forward 使用 HTTP 时允许 Secure=false cookie，但必须明确为 Demo 例外，不能作为生产 TLS 设计。

Session 与 CSRF ownership 冻结为：

- oauth2-proxy 独占 HttpOnly / SameSite session cookie 和 OAuth state 校验；API 不解析该 cookie。
- `GET /api/v3/session/csrf` 由 API 根据可信 `provider/login`、签发时间、过期时间和随机 nonce 返回短期签名 token；前端只保存在内存，不写 localStorage/cookie。
- Mutating request 必须把 token 放入 `X-CSRF-Token`。API 校验签名、当前可信 identity、有效期和 Origin；proxy 必须允许该 header 通过，但清除任何客户端身份 header。
- CSRF token 不是 OAuth credential，不能用于 Query、身份切换或 GitHub 调用。伪造 identity/header、跨用户复用、过期 token、缺 Origin 和跨站请求都必须有负向 contract test。

Mutating API 必须有：

- oauth2-proxy 拥有的 HttpOnly / SameSite session cookie。
- 当前 session 来自 oauth2-proxy 已完成 OAuth state 校验的登录流程；mutation 本身不重复携带 OAuth state。
- API 拥有并绑定可信 identity 的短期 CSRF token。
- Origin check。
- Idempotency-Key。
- expected version/hash。
- Audit actor。

### 20.2 Webhook Auth

Alertmanager Webhook：

- API 进程在 Pod 地址的独立 INTERNAL_PORT 监听，使用独立 ClusterIP Service 暴露；该 listener 只注册 /webhooks/alertmanager、/livez、/readyz、/metrics，绝不注册用户 Query/Command。
- /webhooks/alertmanager 单独执行 Bearer middleware；Probe/metrics 不要求该 Bearer，但响应必须无配置、依赖详情、ID或Secret。用户入口 Service 不暴露 INTERNAL_PORT。
- 不经过 OAuth。
- 使用独立 Bearer secret。
- Alertmanager 通过 credentials_file 读取挂载的 Secret，CloudOps 从独立只读文件挂载读取同一值。
- 常量时间比较。
- Request body limit。
- Read timeout。
- Source event idempotency。
- Redaction 后再持久化。

kind 默认没有 NetworkPolicy 强制隔离，Webhook Service 对集群内 Pod 可达；Bearer、body/time limit、幂等和输入校验才是实际安全边界。未来若要求网络隔离，必须先引入真实支持 NetworkPolicy 的 CNI 及负向连通性测试。

### 20.3 Machine Identity

| Workload | 权限 |
|---|---|
| cloudops-api | 不挂载 K8s token；MySQL DML；OAuth config |
| cloudops-worker | demo namespace 只读 K8s；MySQL DML；ES 只读角色；Prometheus/Tempo 固定查询 adapter；GitHub App；Argo read |
| cloudops-migrate | 无 K8s API 权限；独立 MySQL DDL |
| baseline-verifier Job | 独立 SA 和 MySQL 用户；Demo K8s、Argo、Prometheus、ES、Tempo、Registry 只读；仅 deployment_baselines/baseline_observations 写；无 GitHub App 与 LLM Key |
| oauth2-proxy | GitHub OAuth client；不拥有 GitHub repo write |
| Filebeat | 仅日志读取与 Elastic write；只在 CloudOps/Demo 两个 namespace 对 pods get/list/watch |
| OTel Collector | 仅 Trace pipeline；因 V3 必启 k8sattributes，只在 CloudOps/Demo namespace 对 pods/replicasets get/list/watch；无 Secret、Node 或写权限 |

Worker Kubernetes RBAC 仅允许：

- pods get/list。
- deployments get/list。
- replicasets get/list。
- services get/list。
- endpointslices get/list。
- events get/list。

禁止：

- secrets、configmaps、nodes。
- pods/log。
- exec、attach、port-forward。
- create、patch、update、delete。

### 20.4 Secrets

- Secret 预创建；Helm values 只引用 Secret 名称。
- 不引入 Vault 或 External Secrets。
- GitHub private key、LLM Key、Argo token、Elastic credentials 使用只读文件挂载。
- Installation token 只在内存短期缓存。
- API、Worker、Migrate 使用不同 MySQL账号。
- Evidence、Log、Trace、checkpoint 和 metrics 禁止出现 Secret。
- 所有外部 URL 必须由 fixed-host config 提供，模型不能构造，防止 SSRF。

### 20.5 Container Hardening

CloudOps 自身容器：

- runAsNonRoot。
- readOnlyRootFilesystem。
- allowPrivilegeEscalation false。
- drop ALL capabilities。
- seccomp RuntimeDefault。
- 不需要 K8s API 的容器 automountServiceAccountToken false。

Filebeat读取宿主日志所需的root/hostPath权限必须作为第三方例外单独记录，不得用来放宽CloudOps容器。

V3 kind 默认网络不宣称 NetworkPolicy 隔离，也不为了这一点额外引入 Calico/Cilium。未来若加入必须有真实支持该能力的 CNI 和负向网络测试。

---

## 21. API 与 Incident Workbench

### 21.1 API 约束

V3 使用 /api/v3。采用代码层 Command/Query separation，不引入 Event Sourcing、CQRS框架或第二数据库。

External ingestion：

~~~text
POST /webhooks/alertmanager                  # 独立内部 port
~~~

它使用 Bearer、canonical source_event_id 和 Signal 唯一键幂等，不要求用户 Idempotency-Key 或 expected version。

Authenticated Commands：

~~~text
POST /api/v3/incidents/{id}/investigations
POST /api/v3/incidents/{id}/close
POST /api/v3/remediation-plans/{id}/decisions
~~~

Authenticated session：

~~~text
GET /api/v3/session/csrf
~~~

Queries：

~~~text
GET /api/v3/incidents
GET /api/v3/incidents/{id}
GET /api/v3/incidents/{id}/signals
GET /api/v3/incidents/{id}/timeline?after_id=
GET /api/v3/incidents/{id}/evidence
GET /api/v3/incidents/{id}/investigations
GET /api/v3/incidents/{id}/remediation-plans
GET /api/v3/incidents/{id}/delivery
GET /api/v3/incidents/{id}/verifications
GET /api/v3/incidents/{id}/resolution-report
~~~

Incident event stream：

~~~text
GET /api/v3/incidents/{id}/events            # SSE
~~~

规则：

- Incident Query GET 只能读 MySQL projection，不能访问外部系统或触发 reconciliation。`/api/v3/session/csrf` 只签发本地短期 token，不读取外部系统或改变领域状态。
- Authenticated Commands 必须包含 Idempotency-Key，以及适用 aggregate 的 expected version/hash。
- 相同 key + 相同 payload 返回原结果。
- 相同 key + 不同 payload → 409。
- Stale version/hash → 409。
- 非法业务迁移 → 422。
- 成功持久化并入队 → 202。
- 只暴露 public UUID。
- 错误使用 application/problem+json、稳定 error code 和 request/trace ID。
- 不暴露 lease、checkpoint、Prompt、Raw Result、Secret 或内部 numeric ID。
- Timeline、Evidence、Step 使用 cursor pagination。
- `/events` 明确是 SSE，不是 JSON Event Query；它从持久化 IncidentEvent 单调 ID 恢复，支持 Last-Event-ID，只发送 Incident-scoped refresh hint。
- 不使用 WebSocket。
- 提供手写 OpenAPI 3.1 和 contract tests，不引入生成式 API框架。
- Dead task replay 只允许内部 CLI，不进入 Workbench。

角色矩阵：

- viewer 可以读取全部 Query/SSE，包括完整 diff、所有 hash、Policy、Decision、PR/CI/Argo 和 Verification；不能执行 Command。
- operator 包含 viewer 权限，只能执行本节列出的重新调查、close 和 Approve/Reject Command。
- V3 产品 API 不存在本地 admin 用户、用户管理或第三种产品角色。

### 21.2 两页 Workbench

只保留：

~~~text
/incidents
/incidents/:id
~~~

Incident List：

- active/recent Incident。
- status、severity、service 三个筛选。
- 当前阶段、诊断摘要、needs_attention、updated_at。

Incident Detail 四区：

1. What happened：Signal、Kubernetes状态、最近变更。
2. Investigation：假设、工具步骤、Evidence、Diagnosis。
3. Remediation & Delivery：完整diff、Policy、Approve/Reject、PR/CI/Argo。
4. Recovery：Checks、稳定窗口、ResolutionReport。

外部 deep link：

- Metric → Grafana。
- Log → Kibana。
- Trace → Grafana/Tempo。
- Change/PR/CI → GitHub。
- Delivery → Argo CD。

前端 MUST NOT：

- 计算领域状态。
- 调用 LLM。
- 生成查询语言。
- 编排 Worker。
- 执行 kubectl。
- 编辑 YAML。
- 隐藏 Inconclusive/NOT RUN。

删除通用Chat、Platform Health、Kubernetes浏览器、独立Metric/Log/Trace页面、任意终端和高级CRUD。

---

## 22. kind + Helm 唯一部署路径

### 22.1 命令面

~~~text
make preflight
make demo-up
make scenario-open-regression-pr
make e2e-gitops
make demo-down
~~~

### 22.2 Bootstrap 顺序

~~~text
preflight
→ kind
→ namespaces / storage / resource policy
→ ECK Operator
→ Elasticsearch / Kibana / Filebeat
→ kube-prometheus-stack
→ Tempo monolithic / OTel Collector
→ Argo CD
→ MySQL
→ migration Job
→ oauth2-proxy / cloudops-api / cloudops-worker
→ AppProject / Application / Demo workload
→ Phase 3 baseline readiness probe；Phase 5+ baseline-verifier Job
~~~

上述为最终 Phase 6+ 完整 profile。阶段 profile 冻结为：

- Phase 3：安装 platform、MySQL、migration Job、CloudOps API internal webhook listener、Argo Application/Demo 与观测资产；`cloudops-worker` 和 oauth2-proxy 禁用，用户 API Service 不暴露。API 仍不挂载 Kubernetes token。
- Phase 4：继续使用 Phase 3 部署 profile；真实模型只进入离线 Eval 与 `TARGETED_HANDLER_GATE`，不启用 incomplete production Worker。
- Phase 5：增加 oauth2-proxy/GitHub OAuth、GitHub App、baseline-verifier 和审批/PR相关 API；subject-bound operation 使用 targeted Gate，production Worker仍不宣称 ready。
- Phase 6：五个 subject-bound operation 全部注册后启用完整 `cloudops-worker`，首次执行 production Worker startup/readiness Gate并开放完整两页 Workbench。

Chart/Make profile 必须显式渲染上述开关并有负向 contract test，禁止依赖“缺 Secret 后 CrashLoop”来表达阶段禁用。

### 22.3 所有权

Platform bootstrap 管理：

- kind。
- ECK和观测栈。
- Argo CD。
- MySQL。
- namespace/storage。

CloudOps Helm Chart 只管理：

- API。
- Worker。
- Migrate。
- oauth2-proxy sidecar config。
- Service / RBAC。
- ServiceMonitor / PrometheusRule。
- ConfigMap和Secret references。

Argo CD 只管理：

- Demo Deployment、normal Service 和 demo-diagnostics Service。
- Demo PodMonitor 与 PrometheusRule；其资源类型必须与 AppProject allowlist 完全一致。

Load Generator 的唯一 owner 是 Golden E2E harness：以 Helm test hook/fixture 创建，在测试结束时按 hook delete policy 清理。它不属于 Argo Application desired state，不进入产品 Chart 常驻资源，也不得用 raw kubectl 创建。

Prometheus 的发现合同冻结为：

- ServiceMonitor、PodMonitor 和 PrometheusRule 统一带 cloudops.io/monitoring=enabled。
- kube-prometheus-stack 显式配置 serviceMonitorSelector、podMonitorSelector、ruleSelector 及对应 namespaceSelector，只选择 CloudOps 和 Demo namespace。
- 将各类 NilUsesHelmValues 行为显式关闭，不能依赖 kube-prometheus-stack 的 release label 隐式匹配。
- Contract test 必须通过 Prometheus targets API 和 rules API 证明 CloudOps/Demo target 与 rule 已加载，并证明无标签 canary 未被选择。

禁止：

- Docker Compose作为完整演示路径。
- raw k8s作为平行部署源。
- deploy-k8s兼容脚本。
- kubectl直接注入或修复。
- 另一套Fast Demo架构。

### 22.4 版本锁定与风险

所有第三方：

- Chart version固定。
- Chart包SHA256固定。
- 镜像digest固定。
- 禁止latest。
- 升级必须通过真实数据写入/查询和E2E Gate。

2026-07-17 调研快照仅供选版输入，不直接视为最终兼容矩阵：

| 组件 | 调研时官方版本 |
|---|---:|
| Prometheus Operator | v0.92.1 |
| ECK | v3.4.1 |
| OTel Collector releases | v0.156.0 |
| Tempo | v3.0.2 |
| Argo CD | v3.4.5 |
| oauth2-proxy | v7.15.3 |

实施阶段必须形成一个经过实际安装和数据路径验证的版本锁文件。Elastic 组合不得选择已被 ECK 标为弃用的 7.17；Beat CR v1beta1 在 ECK 3.4.1 尚未正式弃用，但仍必须作为 beta API 风险测试和记录。

### 22.5 Demo 资源边界

目标初始预算：

~~~text
总 requests 约 5 GiB
总 limits 约 10 GiB
另加 kind / Kubernetes 开销
~~~

具体最低CPU、可用内存和余量必须在首次干净部署测量后冻结。

Preflight 必须检查：

- CPU、可用内存、swap和磁盘。
- Docker、kind、kubectl、helm、jq、gh。
- 网络与镜像/Chart可达性。
- OAuth、GitHub App、模型和Repo配置。
- 已存在的同名kind。
- 旧Compose和端口冲突。

Preflight 同样按阶段 profile 收敛：Phase 3 不要求 LLM、GitHub OAuth 或 GitHub App凭据，只检查平台工具、Chart/镜像网络、Argo/GitOps repo和由人创建 regression PR所需的 `gh` 身份；Phase 4增加模型配置；Phase 5增加OAuth/App/ruleset配置；Phase 6沿用全部配置。任何未在当前 profile启用的后续凭据不得阻塞较早 Gate。

发现冲突直接FAIL，不自动删除或停止用户现有环境。

本地入口使用kubectl port-forward，不安装Ingress。ECK内部TLS保留；localhost HTTP/OAuth cookie例外必须明确记录。

数据组件单副本，不宣称HA、生产SLO、持久备份或DR。

---

## 23. CloudOps 自身可观测性

### 23.1 Health

- /livez：仅表示进程存活。
- API /readyz：只检查MySQL可达、schema/CUTOVER marker受当前binary支持、user/internal listener已初始化；API没有task执行循环，禁止检查“核心Worker loop”。
- Worker /readyz：检查MySQL、schema/CUTOVER marker、四个claim loop、semaphore和heartbeat scheduler已初始化；不要求queue非空。
- oauth2-proxy使用自身health/readiness；Migrate是一次性Job，只以退出码表达成功，不伪造常驻readyz。
- GitHub、Argo、Elasticsearch、Tempo或模型短暂故障不得让Pod进入重启风暴。
- 完整依赖健康通过metrics和受限诊断输出展示，不增加Platform Health产品页。

### 23.2 Metrics

必须覆盖：

- Webhook accepted/rejected/deduplicated。
- Signal-to-Incident latency。
- Incident state count。
- Queue depth、oldest age、claim、retry、dead、lease lost。
- Handler duration和error class。
- AgentRun outcome、step、tool/model latency、token、budget。
- Duplicate/invalid tool signature。
- Evidence created/truncated/redacted/rejected。
- GitHub/Registry/Argo/Prometheus/ES/Tempo adapter latency、401/403/429/5xx。
- Approval、Policy和superseded。
- PR reconciliation。
- Delivery phase。
- Verification check、sample、stability和outcome。
- MySQL pool。
- Build SHA和schema version。

Prometheus label禁止使用Incident、Run、Task、Evidence、User、PR等高基数ID。

### 23.3 Structured Logs

日志可以包含：

~~~text
request_id
trace_id
incident_id
run_id
task_id
task_attempt
lease_generation
plan_id
change_request_id
verification_run_id
worker_id
error_code
duration
~~~

日志禁止包含：

- Authorization/cookie。
- GitHub/Argo/LLM/Elastic token。
- Prompt。
- 完整Evidence。
- 完整diff。
- 外部原始文本。

### 23.4 Tracing

CloudOps自身输出：

~~~text
http.request
webhook.ingest
async_task.claim
async_task.handle
agent.run
agent.decision
agent.tool.call
agent.evidence.normalize
agent.state.reduce
agent.sufficiency.evaluate
agent.checkpoint.persist
remediation.compile
github.reconcile
delivery.observe
verification.sample
~~~

Span只记录ID、枚举、hash、计数、耗时和error code，不记录查询、参数、结果、Prompt或Secret。

CloudOps自身告警不得触发CloudOps对自身自动修复，避免递归闭环和同栈失明。

Alertmanager route 必须把 CloudOps 自身告警排除在 Incident receiver 之外，signal_target_allowlist 也只包含黄金 Demo target；contract test 注入 CloudOps target alert，必须不创建 Incident/AgentRun。

---

## 24. Agent Evaluation

### 24.1 E2E 与 Eval 分工

- Golden E2E：证明真实组件和唯一链路能够集成。
- Agent Eval：证明动态调查、假设更新、反证、降级、安全和引用质量。
- DemoModel或固定fixture只能证明harness，不证明智能。

### 24.2 Dataset

Eval数据至少覆盖：

- REQUIRED_ENV配置缺失。
- CrashLoopBackOff。
- OOMKilled。
- Readiness regression。
- Service selector mismatch。
- Application error regression。
- 相同症状不同根因。
- 有干扰的recent change。
- 错误但时间更近的提交。
- source/image/gitops revision冲突。
- mutable tag。
- no-data。
- timeout。
- Prometheus/ES/Tempo其中一个不可用。
- 多个数据源相互矛盾。
- Budget exhausted。
- Malformed structured output。
- Checkpoint crash/replay。
- Log/Event/diff/PR body/Runbook prompt injection。
- Secret canary。
- Cross-namespace/repo scope escape。
- Write-tool request。

每个case包含：

~~~text
Incident snapshot
Tool fixture keyed by signature
Oracle facts
Required/forbidden evidence groups
Acceptable root causes
Expected diagnosed/insufficient outcome
Budget
Allowed multiple investigation paths
~~~

不得锁死唯一工具顺序，否则只是在测试固定pipeline。

### 24.3 当前可冻结硬Gate

以下必须全部为0：

- 执行写工具。
- Scope escape。
- Secret/canary泄漏。
- 服从prompt injection。
- 引用不存在或不属于Incident的Evidence。
- Unsupported confirmed claim。
- 接受重复/非法tool signature。
- Budget越界。

以下必须为100%：

- Checkpoint replay一致性。
- Plan/Approval hash验证。
- Deterministic reducer状态迁移。

Root-cause accuracy、insufficient-evidence precision/recall、citation recall和工具效率阈值必须在Dataset冻结后跑真实模型baseline，再通过ADR制定。禁止提前编造漂亮数字。

该 ADR 产生独立的 AGENT_QUALITY Gate：

- Dataset、oracle、metric 实现和切分先冻结并计算 hash，阈值不得针对最终结果回填。
- 阈值必须同时覆盖 root-cause accuracy、insufficient-evidence precision/recall、citation recall、平均/上界 Tool calls；不得只用一个总分掩盖弱项。
- 固定 provider/model/prompt/tool schema 后，每个随机性 case 至少独立运行 3 次；ADR 明确按单次、多数票或全重复通过中的哪一种聚合，不能在结果出现后切换。
- 必须同时超过 ADR 中的非 Agent/固定 pipeline baseline，并满足本节全部安全零违规 Gate。
- 缺凭据、配额或外部模型时报告 NOT RUN；AGENT_QUALITY 非 PASS 时，Phase 4 Gate、Phase 5 及后续阶段、最终 DoD 和“Agent Eval 已验证”表述均不得通过。

### 24.4 执行层级

- PR：reducer、tool contract、fixture model和安全负例。
- Main/Nightly：真实模型offline replay，凭据存在时执行。
- Manual：完整kind golden E2E。

真实模型case应重复执行并记录provider、model、prompt/tool schema hash和每次结果。缺凭据为NOT RUN。

模型Judge最多用于辅助分析，不能作为唯一评分者。

---

## 25. 测试金字塔与故障注入

### 25.1 Unit

- Incident和子状态机。
- StateDelta schema、reducer和sufficiency。
- Tool argument validation。
- Evidence normalization、hash、redaction。
- Correlation和revision identity。
- YAML AST patch。
- Policy和Approval hash。
- Verification evaluator和stability window。
- Config validation。
- Error classification。

### 25.2 Fuzz / Property

- Alertmanager normalization。
- Label/annotation redaction。
- JSON schema。
- YAML path/AST。
- Idempotency key。
- State transition。
- Canonical hashing。
- External URL/host allowlist。

### 25.3 Real MySQL Integration

必须使用真实 MySQL 8：

- Goose 00001至最新 migration。
- Duplicate Webhook。
- Concurrent Incident ingestion。
- Incident-scoped investigation.start并发只创建一个AgentRun和一个Run-scoped Task。
- 同batch resolved/firing不同顺序产生相同最终firing set和状态。
- Generated-key Active Incident/AgentRun/Plan/ChangeRequest/VerificationRun/Baseline唯一性。
- 30m reopen窗口边界、cycle隔离和并发reopen。
- AsyncTask SKIP LOCKED。
- 多Worker claim。
- Lease takeover。
- Stale writer rejection。
- Task retry/dead/replay generation和stale replay拒绝。
- Command idempotency同key同/不同payload并发。
- Agent checkpoint transaction。
- Evidence producer idempotency、supersedes约束和跨cycle拒绝。
- Approval stale hash。
- Delivery/no-change并发只能创建一个active Verification。
- Verification samples、阈值边界和共同稳定窗口。
- 业务闭环预算并发不越界且无孤儿Task。
- 无passing VerificationRun时任何路径都无法Resolved。

### 25.4 Adapter Contract

- Kubernetes typed bounds和RBAC拒绝。
- Alertmanager send_resolved、credentials_file、多alert normalization和canonical source_event_id。
- Prometheus fixed template、admin/lifecycle关闭、targets/rules selector正负例。
- Elasticsearch filestream数据、namespace canary隔离、registry重启边界、bounded DSL和redaction。
- OTel 0.0.0.0 receiver、ClusterIP、k8sattributes字段和最小RBAC。
- Tempo monolithic target=all、无ingest.kafka和Trace查询。
- Registry OCI labels与digest。
- GitHub exact commit/diff及check name + producer + workflow合同。
- GitHub branch/commit/Draft PR分相write reconciliation。
- Argo单source、automated policy、bounded retry、exact revision和read-only RBAC。
- OAuth伪造header覆盖、token不转发和loopback边界。
- Webhook独立listener、Bearer和非NetworkPolicy限制。
- baseline-verifier独立SA/DB用户和无GitHub/LLM凭据。

### 25.5 Fault Injection

必须覆盖：

- Worker claim后崩溃。
- 旧Worker与takeover Worker重叠。
- Heartbeat暂停、handler deadline和SIGTERM drain/takeover。
- Checkpoint写前/后崩溃。
- investigate队列饱和时deliver/verify继续推进。
- GitHub branch/commit/PR请求成功但响应丢失。
- PR branch被外部修改。
- Base SHA变化。
- Required check失败。
- Argo延迟、失败或跳过revision。
- Kubernetes rollout中途失败。
- ES不可用。
- Tempo不可用。
- Alert resolved延迟。
- resolved Signal与post-delivery Verification并发。
- Stability window中途回退。
- LLM timeout、429、5xx、invalid JSON。
- Prompt injection和Secret canary。

### 25.6 UI

- Vue lint、typecheck、unit、build。
- Incident List和Detail状态。
- Complete diff。
- Approve/Reject。
- 409 stale plan。
- Inconclusive/needs_attention。
- SSE reconnect和Last-Event-ID。
- Keyboard和基本accessibility。

---

## 26. 实施阶段与硬Gate

每个阶段必须独立完成、验证、记录后停止。不得因后续代码存在而追认前置Gate。

### Phase 0 — V3规范与基线

范围：

- 本文档。
- `docs/architecture.md` 与可执行 KEEP/ADAPT/DELETE。
- `docs/migration-ledger.md`。
- `docs/risk-register.md`。
- `docs/evidence/phase-0-baseline-audit-report.md`。
- 第29节列出的12项最低决策ADR；ADR只冻结目标决定，不冒充实现证据。
- 当前源码/模块/数据库/部署/CI live audit。

Gate：

- 当前SHA和工作树来源明确。
- V2历史文档不再是normative。
- V3组件类别、权威边界、产品范围和non-goal无冲突。第三方精确版本/Chart兼容矩阵必须保持明确待测，并由Phase 3真实安装Gate冻结；Phase 0不得编造兼容结论。
- 未修改业务行为。

### Phase 1 — Root Module 与进程边界

范围：

- 机械提升root Go module。
- 吸收现有pkg。
- 建立cloudops-api、worker、migrate。
- Typed config、health、graceful shutdown。
- 保持现有功能可构建。

Gate：

- root go test ./...、race、vet、build通过。
- 三个binary可构建。
- API中没有Agent/Delivery/Verification goroutine。
- Runtime无AutoMigrate。
- 不存在平行V3实现。

### Phase 2 — Async Runtime 与领域收敛

范围：

- async_tasks / attempts。
- Incident状态压缩。
- 统一唯一lease。
- 建立五类 Task 的 typed registry、唯一 claim path 和 handler 注入合同；Phase 2 只实现无外调的 Incident-scoped `investigation.start`，subject-bound 业务 operation 按本节修订后的 Phase 4-6 所有权实现。
- API v3 Command/Query skeleton。

Gate：

- 真实MySQL并发、takeover、stale writer通过。
- Ready claim、expired takeover与dead转换均只更新目标Task，EXPLAIN使用目标索引。
- 四个池的max-in-flight、backpressure、饥饿隔离和shutdown Gate通过。
- API/Worker readiness合同分离，lease/heartbeat/deadline/termination关系配置校验与故障测试通过。
- Command idempotency与replay generation并发Gate通过。
- 在 V3 compatibility binary 的代码与测试配置中，旧三套lease claim入口不可达，V3 task是唯一claim path；registry 在任一 owning operation 缺失时必须于 claim 前 fail closed，且不能用 no-op/dead/legacy wrapper 冒充迁移。legacy字段保持只读兼容，直到Phase 7B独立contract migration才删除。该条是代码/测试Gate，不是 live data cutover授权；CUTOVER_V3前禁止旧Worker与V3 Worker并发运行，实际task/state conversion和marker仍在Phase 7A执行。
- Duplicate Webhook不重复Incident/start Task；并发claim同一start Task不重复AgentRun，multi-alert每个correlation最多一次状态决策。
- Graceful shutdown可恢复。

### Phase 3 — 云原生可观测性与Demo

范围：

- Prometheus Operator/Alertmanager/Grafana。
- ECK/ES/Kibana/Filebeat filestream。
- OTel Collector/Tempo monolithic。
- Demo Service和load generator。
- 统一identity。
- Baseline readiness probe，不创建 DeploymentBaseline。

Gate：

- 空kind可安装。
- Metric、Log、Trace、K8s四条真实数据路径可查询。
- 同一请求可按workload/version/revision/trace关联。
- REQUIRED_ENV regression产生真实告警。
- Alertmanager firing/resolved和multi-alert重投合同通过。
- Prometheus target/rule真实加载，Filebeat无关namespace canary未入库，OTel必需属性齐全。
- Tempo rendered config无Kafka且单体启动/查询通过。
- 资源峰值被测量。

### Phase 4 — Investigation Agent

范围：

- StateDelta。
- task-fenced `investigation.step` operation 注册到统一 registry；不恢复 AgentRun 自有 lease/claim。
- 八个工具。
- Evidence trust。
- Deterministic sufficiency。
- Checkpoint/replay。
- Prompt injection guard。
- Eval v1。

八个 Tool contract 在 Phase 4 全部冻结并进入 fixture/eval。`inspect_*`、Metric/Log/Trace 和 Runbook 可使用 Phase 3 已验证的真实只读 adapter；`get_deployment_context` 与 `get_change_detail` 在 Phase 4 只使用 versioned fixture/现有只读 adapter证明 Agent contract，不得宣称真实 GitHub App/Argo Change 链已通过。真实 Change/三类 revision/GitHub/Argo 合同由 Phase 5 启用和验证。

Gate：

- Observation真实影响下一轮工具。
- 无写工具、scope escape、Secret泄漏、无效引用。
- Eval Dataset、oracle和non-Agent/model baseline冻结。
- AGENT_QUALITY 阈值 ADR 已在冻结数据集后产生，真实模型按固定重复次数执行并达到阈值。
- 缺真实模型凭据时 AGENT_QUALITY 明确 NOT RUN，Phase 4 Gate 不得 PASS，并阻止 Phase 5 及后续阶段；fixture PASS 不能替代。

### Phase 5 — Change 与 GitOps Remediation

范围：

- DeploymentBaseline。
- baseline-verifier Job 与独立最小权限身份。
- 三类revision。
- ChangeCandidate。
- restore_required_env。
- task-fenced `remediation.prepare` 与分相 `change.ensure_pr` operation 注册到统一 registry。
- Complete diff。
- Policy和Approval。
- GitHub App Draft PR。
- OAuth身份。

Gate：

- 无审批零外部写。
- Stale base/hash/policy全部拒绝。
- Approval绑定base/post-image/tree，squash merge后目标blob/tree与批准值等价。
- 任意crash点只有一个有效PR。
- GitHub App installation scope 的 repo 外访问被拒绝；ruleset 负例证明 App 不能绕过 main 保护；adapter/policy 负例证明正常 Worker 拒绝其他 path。完全失陷 Worker 不在首版威胁模型，不能声称 GitHub IAM 提供 path 级限制。
- PR head required checks的name、producer App和workflow均绑定正确。

### Phase 6 — Delivery、Verification 与 Workbench

范围：

- Argo exact revision。
- task-fenced `delivery.observe` 与 `verification.advance` operation 注册到统一 registry；五个 subject-bound operation 完整后启用 production Worker 并验证正向 startup/readiness。
- Delivery observation。
- Verification profiles/samples/stability。
- ResolutionReport。
- 两页Workbench。
- 将现有 `server-monitor/frontend/` 在同一改造中迁到 root `frontend/`；禁止保留平行UI。

Gate：

- Synced/Healthy不能直接Resolved。
- Passed/Failed/TimedOut/Inconclusive语义严格。
- golden-required-env/v1 与 no-change/v1 的版本/hash、阈值边界、min samples 和共同60s窗口全部有证据。
- resolved Signal、delivery并发时只有一个active Verification；no-change不产生任何外部写。
- 失败只入队一个 Incident-scoped investigation.start，并由该 Task 唯一创建新AgentRun。
- 每个cycle最多一个ResolutionReport，no-change不伪造Diagnosis/Plan/PR。
- Approval、Evidence、Delivery和Verification均能从Incident Detail查看。

### Phase 7 — Golden E2E、Cutover 与 Contract

范围：

- Phase 7A（Release A）：真实GitHub OAuth/App、CI、Argo、LLM。
- Phase 7A（Release A）：quiesce、legacy task/state conversion、CUTOVER_V3 marker和完整故障→恢复。
- Phase 7A（Release A）：Exact-SHA evidence与完整迁移审计导出；不删除legacy schema/claim资产。
- Phase 7B（Release B）：只有Release A的Golden与审计被独立接受后，才执行contract migration和Legacy删除；禁止与首次cutover同一个release。
- 3至5分钟证据讲解脚本；它不是系统恢复耗时或 SLO。

Gate：

- Phase 7A（Release A）：AGENT_QUALITY 为 PASS。
- Phase 7A（Release A）：无direct K8s repair和demo bypass。
- Phase 7A（Release A）：Exact merged SHA被Argo实际部署，Required stability checks全部通过。
- Phase 7A（Release A）：当前exact SHA重新构建和验证，旧binary marker拒绝启动；expand/backfill/cutover/Golden ledger单元全部PASS，`CONTRACT-V3`保持NOT RUN。
- Phase 7B（Release B）：使用新的exact SHA重新构建/验证后，执行`CONTRACT-V3`并删除Kafka、Redis、legacy claim/schema/service、Compose/raw部署和旧UI。
- Phase 7B（Release B）：Cleanup后工作树/集群/容器状态明确，且仍能重跑核心Golden smoke。Phase 7只有A、B均PASS才整体PASS。

---

## 27. CI Gates

### PR_FAST

- gofmt/goimports。
- go vet。
- golangci-lint。
- Go unit/fuzz。
- Frontend lint/typecheck/unit/build。
- Migration静态检查。
- Helm lint/template/schema。
- kubeconform。
- promtool。
- actionlint/shellcheck。
- Secret scan。
- Policy/RBAC/Auth负例。

### PR_INTEGRATION

- 真实MySQL。
- Race。
- AsyncTask并发和崩溃恢复。
- Adapter contract。
- Agent fixture eval。
- Prompt injection/Secret/scope安全集。
- GitHub reconciliation fixtures。

### MAIN

- exact-SHA image build。
- OCI revision/source/version。
- govulncheck。
- Trivy。
- kind core smoke，在资源允许时运行。

### MANUAL_GOLDEN

- 完整kind/ECK/Tempo/Argo。
- 真实模型。
- GitHub OAuth和App。
- Regression PR和人工merge。
- Remediation Draft PR、required checks和人工merge。
- Argo exact SHA。
- Recovery Verification。

SBOM/Cosign可保留在tag release可选工作流，但不是V3核心叙事和主Gate。

缺凭据、资源、branch protection、模型或外部系统时，MANUAL_GOLDEN必须报告NOT RUN，不能使用mock或旧SHA证据替代。

---

## 28. 当前资产的 KEEP / ADAPT / DELETE

当前基线仍是多module和V2增量实现。本表描述迁移意图，不表示已经执行。

### KEEP / MIGRATE

| 资产 | 处理 |
|---|---|
| 00001至00006 Goose migrations | 保持历史不可变，新增forward migrations |
| Incident Signal规范化、correlation lock、Timeline | 迁移到root module并压缩状态 |
| Eino typed graph骨架 | 适配StateDelta和task-per-step |
| AgentRun、AgentStep、Evidence、checkpoint | 保留数据思想，删除领域lease |
| Registry OCI metadata和exact digest/revision/source校验 | 保留并成为DeploymentContext |
| Remediation Policy、YAML AST、plan/patch hash | 扩展完整diff和全部审批绑定 |
| GitHub write isolation和reconciliation思想 | 收敛到一个repo/operation |
| VerificationProfile、fixed template、stability window | 替换Loki为Elastic并补Inconclusive/Sample |
| Incident Workbench两条路由、SSE | 收敛成四区并补审批 |

### ADAPT

| 当前能力 | V3 |
|---|---|
| server-web单进程运行全部Worker | API/Worker进程分离 |
| agent_runs/change_requests/verification_runs各自lease | async_tasks唯一lease |
| outbox_events | 归档后删除；async_tasks 独立新建，不从 outbox 派生 |
| 11个Incident状态 | 7个用户可见状态 |
| Delivery十几个平级状态 | 局部phase + observation + reason |
| Model coverage.sufficient | Deterministic sufficiency evaluator |
| GraphState整体模型交互 | StateDelta |
| k8s.get_logs/Loki | ECK Elasticsearch LogReader |
| rollback_image/set_replicas | restore_required_env only |
| local user/password/JWT | oauth2-proxy + GitHub OAuth |
| 只读Remediation UI | Complete diff + Approve/Reject |
| controlled direct demo | real GitOps E2E |
| Postmortem model/流程 | deterministic ResolutionReport |

### DELETE

- server-probe。
- alert-service。
- nested pkg module。
- Redis。
- Kafka。
- VictoriaMetrics。
- Jaeger。
- Fluent Bit。
- Loki。
- 手写ES/Kibana。
- Docker Compose完整演示。
- raw k8s平行部署。
- /api/v2/demo/**。
- direct kubectl scale/patch repair。
- generic Alert/Action/Copilot Chat路径。
- host monitoring。
- AlertRule/NotificationChannel CRUD。
- legacy AutoMigrate。
- local user management。
- Argo Rollouts。
- 旧文档路径分裂。

### 28.4 Forward migration 与 cutover 合同

00001–00006 永远不可修改。V3 只新增 forward migrations，并按 expand → backfill → cutover → contract 执行：

1. Expand：先增加 V3 表、generated keys、nullable compatibility fields 和 migration ledger；旧 binary 在这一阶段仍可启动，但不得读到新状态枚举。
2. Backfill：在旧 Worker 仍是唯一执行者时，批量迁移 immutable facts、cycle_no=1、child references 和 projection；每批保存 source count、target count、ID range、hash、PASS/FAIL。
3. Quiesce：进入明确维护窗口，停止 Webhook ingress 和用户 mutation，停止旧 Worker claim，等待旧 lease 到期；禁止在外部写处于 unknown 时继续。
4. Task conversion：持有 cutover lock，先按发布状态和event type归档全部 legacy outbox；它不是job queue，禁止直接生成Task。cutover前已存在且验证通过的V3 async_tasks原样保留；新增conversion Task只能由非终态 AgentRun/ChangeRequest/VerificationRun 的versioned converter产生，并对保留Task执行anti-join，绝不原地改名。
5. State conversion：在同一维护窗口按确定性表压缩 Incident/child 状态，写 IncidentEvent 和 blocking reason。
6. Cutover：写不可逆 CUTOVER_V3 schema marker，启动仅 V3 Worker，恢复 API ingress；旧 binary 看到 marker 必须拒绝启动。
7. Contract：完整 Golden E2E 和审计导出通过后的后续 migration 才能删除 legacy claim path、lease column 和表；删除不得与首次切换同一个 release。

Legacy outbox 归档规则：

| 当前真实字段条件 | V3 处理 |
|---|---|
| published_at IS NULL | 归档为 legacy_outbox_unpublished，记录event type/schema/count/hash，不创建task |
| published_at IS NOT NULL | 归档为 legacy_outbox_published，不创建task |
| unknown event type/schema 或 invalid payload | Cutover FAIL，禁止丢弃、猜测或启动新 Worker |
| 任何看似承载外部operation的event | Cutover FAIL；先增加显式versioned mapper并reconcile，仍不得从payload文本推断或直接生成task |

当前审计证明 outbox 接口只有Add/PendingCount，没有relay、claim或mark-published，因此 attempts/last_error也不能推导ready/running/dead。每一种 legacy event type 必须有版本化archive mapping和fixture。归档前后要证明每个outbox row恰有一个archive record；Task唯一性由child converter单独证明。

非终态 child 转换必须在 outbox 归档后，对已存在的target Task做anti-join，保证每个 subject 恰有一个 Task：

- pending/running AgentRun 只有通过 versioned checkpoint converter 后才能 → investigation.advance。
- 只有完整 Draft PR 已存在或 PR 已 merged 的 ChangeRequest 才可先 reconcile，再创建纯只读 delivery.observe。
- 仅有 branch/commit、没有完整 Draft PR 的 legacy ChangeRequest 必须 failed/superseded + needs_attention，保留外部artifact审计但绝不补写 commit/PR。
- 尚未发生任何外部写、但只绑定 legacy Approval 的 ChangeRequest → cancelled/failed，旧 Plan superseded，Incident 回 investigating，禁止创建 PR。
- pending/running VerificationRun 只有通过 versioned profile/check/sample converter 后才能 → verification.advance。

dedupe_key 必须包含 legacy subject ID/version/cycle、expected transition和converter version；ledger分别记录outbox-archived-published、outbox-archived-unpublished、existing-target-task、subject-derived、conversion-failed与anti-join-skipped数量，不存在outbox-derived-task类别。

Converter 合同：

- Agent checkpoint converter 固定 source/target schema version、canonical hash 和字段映射，验证 incident/cycle、Evidence归属、tool signature、budget usage 与 next node；不能把 V2 整体 GraphState 直接当作 V3 StateDelta checkpoint。
- 转换失败时旧 AgentRun → cancelled，原 checkpoint/hash进入只读legacy archive，并只创建一个Incident-scoped investigation.start Task；由该Task创建全新V3 Run，不得在旧Run上续跑。
- Verification converter 必须验证 trigger、三类revision、profile/check语义、sample单位和stability状态。V2 Loki或不兼容Profile默认不可转换：旧Run → cancelled，Incident → investigating/needs_attention，由新调查决定后续，禁止直接verification.advance。
- 每次转换记录 converter version、input/output hash、PASS/FAIL和reason；转换fixture必须包含缺字段、stale Evidence、跨cycle、旧Loki Check与损坏checkpoint负例。
- 所有成功转换的Run/Task/Evidence都标记 migrated_legacy=true；即使后续终态正确，也不得作为Phase 4/7、Agent Quality或Golden E2E的新实现证据。

若未来审计发现任何 legacy outbox 类型代表 branch/commit/PR 写，cutover必须先FAIL并新增显式mapper；没有V3完整hash Approval时仍只能对已存在的完整 Draft PR/merged state做只读delivery.observe，绝不能生成change.ensure_pr或补齐下一外部写。

Legacy delivery.observe 只为收敛和审计外部状态，不能基于旧Approval创建V3 post-delivery Verification或ResolutionReport；外部状态明确后Incident回investigating + legacy_delivery_observed。若当前resolved Signal随后触发全新no-change/v1，它必须标记 migrated_legacy_context，且该结果不得作为V3 Golden E2E/简历闭环证据。

Legacy Incident FAILED 绝不直接映射 resolved。映射优先级固定为：

~~~text
active VerificationRun        → verifying + legacy_failed_blocked
ChangeRequest with observed external write → delivering + legacy_failed_blocked
legacy Plan/Approval without external write → supersede Plan + investigating + legacy_approval_incomplete
otherwise                     → investigating + legacy_failed_blocked
~~~

Legacy Incident RESOLVED 只有在兼容converter验证其 passing Verification、trigger和三类revision后才保留resolved，并标记migrated_legacy；否则必须映射为investigating + legacy_resolution_unverified + needs_attention，不能用resolved Signal或旧Postmortem补证据。

Legacy Approval 不具备 V3 的完整 base/post-image/tree/policy/verification hash，任何路径都不得复用它创建新外部写；必须生成新 Plan 和新人工审批。映射后统一 needs_attention；除只读 reconcile/verification外不自动入队，先由 migration audit 验证 child state。旧 postmortem/narrative 不能冒充 V3 ResolutionReport：00006 数据原 ID、内容 hash 和时间必须保留为只读 legacy_postmortem_archive，只供内部审计导出，不进入 Evidence、Diagnosis 或 Workbench 的 V3 report projection。

Rollback 边界：

- CUTOVER_V3 之前可以回滚 compatibility binary，但只能使用向前兼容 schema。
- CUTOVER_V3 写入、新状态产生或 V3 task 被 claim 后，禁止 Helm/database 回滚到旧 binary；失败只允许 forward fix。
- 删除 legacy lease/表前必须证明旧 Deployment/Job image 全部不存在、旧 binary marker test 拒绝启动、legacy active lease 为 0，且所有 pre-contract ledger 单元均 PASS；`CONTRACT-V3` 只有在本次删除和 post-check 全部通过后才能标记 PASS。
- 任何 count/hash、外部副作用或状态映射不一致都使 cutover FAIL，并保持 ingress 关闭；不得以手工改库掩盖。

---

## 29. 文档与ADR

仓库内只使用docs/，不再并存doc/和docs/。

必须维护：

~~~text
README.md
docs/CloudOps-Incident-Agent-V3-Refactor-Design.md
docs/architecture.md
docs/agent-runtime.md
docs/reliability.md
docs/security.md
docs/api.md
docs/demo.md
docs/migration-ledger.md
docs/risk-register.md
docs/adr/
docs/evidence/<exact-sha>/manifest.md
~~~

最低ADR：

1. V3产品边界与唯一Incident Flow。
2. Root module与API/Worker/Migrate。
3. MySQL async_tasks与拒绝Kafka/Redis。
4. Prometheus + ECK/Filebeat + OTel/Tempo。
5. StateDelta、Evidence和deterministic sufficiency。
6. Deployment identity和last-known-good baseline。
7. restore_required_env与hash-bound approval。
8. GitHub App、Argo read-only和exact-SHA。
9. GitHub OAuth/oauth2-proxy和RBAC。
10. kind/Helm唯一部署路径与资源边界。
11. Eval、Gate和claim safety。
12. Legacy cutover和删除策略。

Phase 0 必须创建上述12项决策ADR并标记为目标架构决定；没有对应代码、测试和exact-SHA证据时，ADR不得写成“已实施”。`docs/agent-runtime.md`、`docs/reliability.md`、`docs/security.md`、`docs/api.md`、`docs/demo.md` 和 exact-SHA manifest 由各自 owning Phase 在其Gate前补齐，不是Phase 0用空文档抢占的交付物。

Evidence Manifest至少记录：

- 承载实现与本文档的 CloudOps source exact SHA。
- CloudOps API/Worker image digest 及对应 OCI source revision。
- GitOps regression PR head SHA、bad merged SHA、remediation PR validated current head SHA、Approval-bound tree/post-image hash、fix merged SHA。
- Argo 实际 deployed revision 与 successful syncResult revision。
- Chart/Operator/Kubernetes版本。
- Commands。
- PASS/FAIL/NOT RUN。
- 每个 source/GitOps SHA 对应的 GitHub Actions run URL 和 required check conclusion。
- Incident/AgentRun/Evidence/Plan/PR/Argo/Verification IDs。
- Agent model/prompt/tool schema hash。
- Tool calls、tokens、E2E耗时等真实测量值。
- 资源峰值。
- 已知限制和未运行项。

Golden E2E 的系统耗时必须另行实测：从 bad regression merge webhook 时间、Incident detected、Agent完成、人工审批等待、fix merge、Argo sync 到 Verification passed 分段记录。Bootstrap、镜像/Chart下载、模型延迟和两次人工等待分别报告，不得用3至5分钟讲解目标替代恢复数据。

---

## 30. 最终 Definition of Done

### Product

- 只有Incident List和Incident Detail两个主页面。
- 只有一个黄金故障场景。
- 只有restore_required_env一个修复动作。
- 全流程都可在一个Incident中审计。

### Cloud Native

- 一个root Go module和三个入口。
- 空kind集群可重复部署。
- 使用官方Operator/Chart。
- Metric、Log、Trace、Kubernetes和Change真实关联。
- Git为desired state，Argo为唯一reconciler。
- CloudOps没有直接Kubernetes写权限。
- Resource、RBAC、Secret和Health边界真实验证。

### Agent

- Agent根据Evidence缺口动态选择工具。
- Observation影响后续Hypothesis和Action。
- StateDelta由deterministic reducer应用。
- 每一步可checkpoint并在Worker接管后恢复。
- Diagnosis只引用合法Evidence。
- 能安全返回insufficient_evidence。
- Write/scope/secret/injection安全Gate为0违规。
- Eval独立于E2E。
- 冻结数据集上的真实模型重复运行达到 AGENT_QUALITY 阈值，结果可按 exact model/prompt/tool hash 重放。

### Go Backend

- Webhook事务原子创建Signal、Incident、Timeline和Task。
- 多Worker通过SKIP LOCKED并发claim。
- Lease generation拒绝stale writer。
- Duplicate/retry/crash不产生重复领域效果。
- GitHub ambiguous write通过reconciliation处理。
- Runtime没有AutoMigrate。
- Root test/race/vet/build全通过。

### GitOps 与 Recovery

- 无审批零外部写。
- UI展示完整diff和全部hash。
- Base变化后旧审批失效。
- 只有一个有效Draft PR。
- Required CI绑定经验证且tree/post-image等于Approval-bound hash的当前唯一PR head SHA。
- Argo实际部署exact merged SHA。
- Required Verification稳定通过后才Resolved。
- 失败/超时/Inconclusive真实回流并入队唯一investigation.start，再由该Task创建新AgentRun。

### Delivery

- PR Fast和Integration通过。
- Manual Golden在最终exact SHA真实运行。
- 旧SHA证据不复用。
- Legacy服务、组件和部署捷径全部删除。
- 在预热且已有完整 Incident 证据的环境中，3至5分钟讲解脚本可重复；它不计 bootstrap、镜像拉取、真实模型、人工等待或 live recovery。
- README、设计、Evidence和简历表述一致。

---

## 31. 明确非目标

V3不做：

- 多集群、多租户、多环境。
- HA、生产SLO、生产备份/DR。
- 自研Operator、CRD或controller。
- Kafka、Redis、Vector DB。
- Loki、Jaeger、VictoriaMetrics。
- Logstash、Fleet、Elastic Agent。
- Argo Rollouts。
- 自动merge、主动Argo sync、自动rollback。
- 模型或用户执行任意 YAML、Git 命令、kubectl 或 shell；唯一例外是服务端 allowlisted GitHub adapter 按获批 manifest 写受限 branch/commit/Draft PR。
- Secret、RBAC、CRD、PVC修复。
- 通用Chat、长期会话记忆、多Agent。
- 动态MCP/Tool插件平台。
- 任意PromQL、ES DSL、TraceQL。
- WebSearch。
- 在线学习或模型训练。
- AI Infra、模型Gateway和多Provider路由。
- host monitoring。
- AlertRule/NotificationChannel CRUD。
- 通用Kubernetes控制台。
- 通用DevOps执行平台。
- 前端工作流编排。
- 将kind Demo描述为生产系统。

---

## 32. 秋招表述边界

只有在对应Gate真实通过后，才可以形成以下能力表述：

- Go/MySQL：事务一致性、乐观锁、可靠任务队列、租约fencing和崩溃恢复。
- Agent：Eino Graph、动态工具选择、Evidence、StateDelta、checkpoint，以及冻结数据集上真实模型重复运行且质量/安全 Gate 均通过的 Eval 和 guardrail。
- Cloud Native：Kubernetes、Helm、Prometheus Operator、ECK、OTel/Tempo、Argo CD。
- DevOps：受限GitOps PR、CI、exact-SHA部署追踪和恢复验证。

禁止表述：

- 生产级云原生平台。
- 支撑高并发或大规模集群。
- 自研大模型。
- AI Infra。
- 全自动自愈。
- 多集群SRE平台。
- 生产HA/DR。
- 已上线真实生产环境。

必须真实测量后才能写：

- Agent准确率。
- Eval案例数和通过率。
- E2E恢复耗时。
- Tool调用数。
- Token消耗。
- Lease接管时长。
- Webhook并发吞吐。
- 资源峰值。
- 测试覆盖率。

---

## 33. 官方参考资料

### Agent 与 Incident

- HolmesGPT: https://github.com/HolmesGPT/holmesgpt
- HolmesGPT toolsets: https://github.com/HolmesGPT/holmesgpt/tree/3bccb1d4ffb9aff21561f79bbfe43c8c8602f6a3/docs/data-sources/builtin-toolsets
- HolmesGPT context management: https://github.com/HolmesGPT/holmesgpt/blob/3bccb1d4ffb9aff21561f79bbfe43c8c8602f6a3/docs/reference/context-management.md
- HolmesGPT evaluations: https://github.com/HolmesGPT/holmesgpt/blob/3bccb1d4ffb9aff21561f79bbfe43c8c8602f6a3/docs/development/evaluations/index.md
- HolmesGPT deployment verification: https://github.com/HolmesGPT/holmesgpt/blob/3bccb1d4ffb9aff21561f79bbfe43c8c8602f6a3/docs/operator/deployment-verification.md
- Robusta Classic: https://github.com/robusta-dev/robusta
- Robusta read-only ServiceAccount: https://github.com/robusta-dev/robusta/blob/eb350a423163bac5f157a79773bc7afdc85e8ed1/docs/setup-robusta/read-only-service-account.rst
- K8sGPT: https://github.com/k8sgpt-ai/k8sgpt
- K8sGPT current README and anonymization boundary: https://github.com/k8sgpt-ai/k8sgpt/blob/492af39e14a5fe646527afa3bba7d2edcbc75d0c/README.md#key-features
- K8sGPT analyzers: https://github.com/k8sgpt-ai/k8sgpt/blob/492af39e14a5fe646527afa3bba7d2edcbc75d0c/pkg/analysis/analysis.go
- K8sGPT privacy: https://docs.k8sgpt.ai/reference/guidelines/privacy/
- kagent HITL: https://github.com/kagent-dev/kagent/blob/b2b86c9f0d3aad901c83c0663548d22472d02fdb/docs/architecture/human-in-the-loop.md
- kagent types: https://github.com/kagent-dev/kagent/blob/b2b86c9f0d3aad901c83c0663548d22472d02fdb/docs/architecture/crds-and-types.md
- kagent telemetry attributes: https://github.com/kagent-dev/kagent/blob/b2b86c9f0d3aad901c83c0663548d22472d02fdb/go/adk/pkg/telemetry/attributes.go
- Eino: https://github.com/cloudwego/eino
- Eino Graph/Workflow: https://www.cloudwego.io/docs/eino/core_modules/chain_and_graph_orchestration/

### Metrics、Logs 与 Traces

- Prometheus Operator introduction: https://prometheus-operator.dev/docs/getting-started/introduction/
- Prometheus Operator RBAC: https://prometheus-operator.dev/docs/platform/rbac/
- Alertmanager: https://prometheus.io/docs/alerting/latest/alertmanager/
- ECK: https://www.elastic.co/docs/deploy-manage/deploy/cloud-on-k8s
- ECK Beats configuration: https://www.elastic.co/docs/deploy-manage/deploy/cloud-on-k8s/configuration-beats
- ECK Beats quickstart: https://www.elastic.co/docs/deploy-manage/deploy/cloud-on-k8s/quickstart-beats
- Filebeat filestream migration: https://www.elastic.co/docs/reference/beats/filebeat/migrate-to-filestream
- ECK deprecations: https://www.elastic.co/docs/release-notes/cloud-on-k8s/deprecations
- OpenTelemetry Collector: https://opentelemetry.io/docs/collector/
- OTel Collector configuration: https://opentelemetry.io/docs/collector/configuration/
- Tempo architecture: https://grafana.com/docs/tempo/latest/introduction/architecture/
- Tempo Helm: https://grafana.com/docs/tempo/latest/set-up-for-tracing/setup-tempo/deploy/kubernetes/helm-chart/
- Tempo 3 migration: https://grafana.com/docs/tempo/latest/set-up-for-tracing/setup-tempo/migrate-to-3/

### GitOps、Identity 与 Kubernetes

- Argo CD automated sync: https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/
- Argo CD projects: https://argo-cd.readthedocs.io/en/stable/user-guide/projects/
- Argo CD RBAC: https://argo-cd.readthedocs.io/en/stable/operator-manual/rbac/
- GitHub App permissions: https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app
- GitHub rulesets: https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets
- oauth2-proxy overview: https://oauth2-proxy.github.io/oauth2-proxy/configuration/overview/
- oauth2-proxy GitHub provider: https://oauth2-proxy.github.io/oauth2-proxy/configuration/providers/github/
- Kubernetes RBAC: https://kubernetes.io/docs/reference/access-authn-authz/rbac/

---

## 34. 最终原则

V3的成功不以组件数量、页面数量或自动化动作数量衡量，而以一个exact-SHA、可重复、可审计的真实链路衡量：

~~~text
真实告警
→ 唯一Incident
→ 动态Agent调查
→ 合法Evidence
→ 确定性受限Plan
→ Hash-bound人工审批
→ 唯一Draft PR
→ 人工merge
→ Argo exact revision
→ 多信号稳定验证
→ Resolved
~~~

任何绕过Git、审批、Evidence、exact revision或Verification的捷径，都不属于V3完成路径。
