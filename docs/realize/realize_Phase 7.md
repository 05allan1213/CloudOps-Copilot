# CloudOps Copilot Phase 7 实施方案

> 方案版本：v1.0
> 制定日期：2026-05-11
> 依据文档：`docs/design.md` v3.1
> 阶段定位：Kubernetes 深度接入，在 Phase 2 Tool Registry、Phase 3 诊断报告、Phase 4 Runbook、Phase 5 异步诊断 Worker、Phase 6 动作审批与审计基础上，补齐 K8s 只读证据采集和审批后的受控写操作执行能力。

---

## 1. 阶段目标

Phase 7 的目标是把 `chatops` 原型中的 Kubernetes 查询能力正式合并到 `server-monitor/server-web` 的 Copilot 架构中：通过 `client-go` 建立可配置、可测试、可降级的 K8s Client；将 Pod、Deployment、Service、Node、Event、Log 查询注册为低风险只读工具；将 K8s 资源状态纳入 ChatOps 和 Diagnosis Evidence；并在 Phase 6 的 PendingAction / AuditLog / Action Policy 已完成的前提下，谨慎开放 `k8s.restart_deployment` 和 `k8s.scale_deployment` 两个中风险写操作的真实执行器。

本阶段坚持安全优先：LLM 不能直接调用写操作，所有写操作必须先进入 PendingAction，由 admin 审批后才能执行；执行前还要二次校验白名单、参数范围、RBAC、namespace 约束和当前资源状态；执行结果、失败原因、前置状态和 trace_id 必须写入 AuditLog，并尽量发布 `operation-events` 与 WebSocket 状态。

### 1.1 核心交付物

| 交付物 | 内容 | 验收标准 |
|---|---|---|
| K8s 配置与 Client | `K8S_ENABLED`、`K8S_IN_CLUSTER`、`K8S_KUBECONFIG`、允许 namespace、日志行数、超时等配置；基于 `client-go` 初始化 typed client | 默认关闭真实 K8s 接入；开启后配置错误启动失败；单元测试可注入 fake client |
| K8s 只读 Service | Pod、Deployment、Service、Node、Event、Log 的结构化查询能力 | 所有外部调用使用请求 ctx；返回 DTO 不包含 Secret、Env、Token 等敏感字段 |
| Tool Registry 工具 | `k8s.get_pods`、`k8s.get_deployments`、`k8s.get_services`、`k8s.get_nodes`、`k8s.get_events`、`k8s.get_logs` | `/api/v1/copilot/tools` 能看到工具 schema；viewer 可调用只读工具；参数错误由 Registry 拒绝 |
| Diagnosis 接入 | EvidenceCollector 可按告警上下文采集 K8s 资源、事件和日志摘要 | K8s 证据不可用时诊断继续，报告标注 evidence 不完整，不阻塞 LLM/rule-only 输出 |
| 真实 ActionExecutor | Phase 6 的 `K8sExecutor` 从 disabled executor 升级为可选真实执行器 | 未审批不能执行；非 admin 不能执行；restart/scale 成功、失败、超时均写 AuditLog |
| 最小 RBAC 与部署 | Docker Compose 本地 kubeconfig 支持；K8s/Helm 提供独立 ServiceAccount、Role、RoleBinding、环境变量 | 写操作 ServiceAccount 只具备 get/list/watch pods/events/services/nodes、get/update/patch deployments、get/update scale 的最小权限 |
| 前端增强 | Copilot / 诊断详情 / Action 详情展示 K8s 证据、资源状态、执行前后状态 | 用户能看到 K8s 证据来源、namespace、资源名、采集时间和错误降级说明 |
| 验证闭环 | 单元测试、fake client 集成测试、权限测试、前端构建、部署模板校验 | K8s 工具可用；写操作受审批/审计控制；现有监控、诊断、审批能力不回退 |

### 1.2 本阶段不做

1. 不开放删除 Namespace、PVC、Secret、ConfigMap、Node drain、批量重启、任意 kubectl apply 等高风险动作。
2. 不让 LLM 直接执行 K8s 写操作；LLM 只能生成建议或触发 PendingAction 创建流程。
3. 不把 kubeconfig、Bearer Token、Secret 内容、Pod 环境变量原文传给 LLM、前端或审计日志。
4. 不新增独立 K8s 微服务；K8s 能力嵌入 `server-web`，复用 Gin、JWT/RBAC、Tool Registry、Trace、日志和审计。
5. 不引入 controller-runtime、operator 框架或复杂工作流引擎；第一版只使用 `client-go` typed client。
6. 不改变 Phase 6 的 PendingAction 外部 API、状态机和权限模型。
7. 不改变现有告警 Webhook 同步链路；Webhook 仍不能等待 K8s 查询、LLM 或写操作。
8. 不要求本地开发环境必须有真实集群；fake client 和 disabled 模式必须能完成主要测试。

---

## 2. 当前基础与前置条件

### 2.1 当前已具备能力

| 能力 | 当前落点 | Phase 7 复用方式 |
|---|---|---|
| Tool Registry | `server-monitor/server-web/copilot/tool` | 注册 K8s 只读工具，复用 schema、timeout、权限、脱敏和执行日志 |
| Diagnosis Pipeline | `server-monitor/server-web/copilot/diagnosis` | 增加 K8s Evidence 采集，不改变已有 alert/metric/history/runbook 证据 |
| Action 审批与审计 | `server-monitor/server-web/copilot/action`、`model/pending_action.go`、`model/audit_log.go` | 复用 PendingAction、AuditLog、Policy、Action API、WebSocket 状态 |
| K8s 执行接口 | `server-monitor/server-web/copilot/action/k8s_executor.go` | 将 `DisabledK8sExecutor` 替换为可配置的真实 `client-go` executor |
| 前端审批页 | `server-monitor/frontend/src/pages/ActionsPage.vue`、`ActionDetailPage.vue`、`AuditLogsPage.vue` | 展示 K8s action 的参数、执行结果、失败原因和前置状态 |
| chatops 原型 | `chatops/server/service/k8s.go` | 迁移 Pod/Deployment/Service/Node 查询思路，但改为无全局状态、结构化返回、ctx 透传和可测试实现 |
| 部署体系 | `server-monitor/docker-compose.yml`、`server-monitor/k8s/`、`server-monitor/charts/server-monitor/` | 增加 K8s 配置、RBAC、ServiceAccount 和 Helm values |

### 2.2 当前缺口

1. `server-monitor/server-web/go.mod` 当前还没有显式引入 `k8s.io/client-go`、`k8s.io/apimachinery`、`k8s.io/api`。
2. `chatops` 原型使用包级全局 `k8sClient` 和 `context.TODO()`，不适合迁入正式服务。
3. Phase 6 当前已有 `K8sExecutor` 接口和 disabled executor，但真实 restart/scale 执行尚未接入。
4. 现有 Tool Registry 已支持只读工具，但还没有 K8s resource schema、namespace 白名单和日志截断策略。
5. 诊断 Evidence 当前以告警、指标、历史、Runbook 为主，还缺少 Pod 状态、Deployment 副本、Event 和日志摘要。
6. 部署侧尚未定义 ChatOps 专用 ServiceAccount / RBAC，不能直接让 `server-web` 使用过大的默认集群权限。

### 2.3 前置假设

1. Phase 6 已完成并通过：PendingAction、AuditLog、Action API、Action Policy、WebSocket `action_status` 均可用。
2. `server-web` 仍是 Copilot、Diagnosis 和 Action 的统一后端入口。
3. `admin` / `viewer` 角色模型不变；只读 K8s 工具允许登录用户调用，写操作只允许 admin 审批和执行。
4. 部署环境可能是三种形态：本地 Docker Compose + kubeconfig、集群内运行 + ServiceAccount、测试环境 fake client。
5. K8s 资源名称遵循 DNS label / subdomain 约束，namespace 和 resource name 必须强校验。
6. 生产建议默认 `K8S_WRITE_ENABLED=false`，只有在 RBAC、审计、回滚预案完成后才开启真实写操作。

### 2.4 阶段边界

Phase 7 只扩展 Kubernetes 数据源和受控执行能力，不改变告警接收、Kafka 自动诊断、Runbook 检索、LLM Provider、用户/角色体系和审批状态机。任何新增 K8s 配置、依赖、权限和部署模板都必须能单独回滚，且默认不扩大现有运行权限。

---

## 3. 总体实施路径

Phase 7 拆为 9 个模块推进，每个模块都有独立验证点。

```text
模块 1：依赖、配置与 K8s Client 工厂
  ↓
模块 2：K8s 只读 Service 与 DTO 脱敏
  ↓
模块 3：K8s 只读工具注册到 Tool Registry
  ↓
模块 4：Diagnosis Evidence 接入 K8s 上下文
  ↓
模块 5：真实 K8s ActionExecutor
  ↓
模块 6：RBAC、部署配置与最小权限
  ↓
模块 7：前端 K8s 证据与动作执行展示
  ↓
模块 8：观测、降级、限流与安全校验
  ↓
模块 9：联调、回归、验收与交付
```

---

## 4. 文件规划

### 4.1 后端新增文件

| 文件 | 职责 |
|---|---|
| `server-monitor/server-web/copilot/k8s/types.go` | 定义 K8s DTO、查询请求、错误类型、配置快照 |
| `server-monitor/server-web/copilot/k8s/client.go` | K8s client 工厂：in-cluster、kubeconfig、disabled、fake 注入 |
| `server-monitor/server-web/copilot/k8s/service.go` | Pod/Deployment/Service/Node/Event/Log 查询服务 |
| `server-monitor/server-web/copilot/k8s/sanitize.go` | 统一脱敏、字段裁剪、日志截断、Event message 长度限制 |
| `server-monitor/server-web/copilot/k8s/service_test.go` | 使用 fake client 覆盖列表、空结果、错误、ctx cancel |
| `server-monitor/server-web/copilot/k8s/sanitize_test.go` | 覆盖 Secret、Token、env、annotation 和日志截断 |
| `server-monitor/server-web/copilot/tool/k8s_tool.go` | 将 K8s 只读能力封装为 Tool Registry 工具 |
| `server-monitor/server-web/copilot/tool/k8s_tool_test.go` | 覆盖工具参数校验、权限、结果结构、disabled 降级 |
| `server-monitor/server-web/copilot/action/k8s_client_executor.go` | 真实 `K8sExecutor`：restart deployment、scale deployment |
| `server-monitor/server-web/copilot/action/k8s_client_executor_test.go` | 使用 fake client 覆盖执行成功、资源不存在、冲突、ctx 超时 |

### 4.2 后端修改文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/server-web/go.mod` / `go.sum` | 新增 `k8s.io/client-go v0.36.0`、`k8s.io/apimachinery v0.36.0`、`k8s.io/api v0.36.0` |
| `server-monitor/server-web/config/config.go` | 新增 K8s 配置、默认值和 `Validate()` 规则 |
| `server-monitor/server-web/copilot/tool/executor.go` | Options 增加 `K8sService` 或 `K8sReader` 依赖 |
| `server-monitor/server-web/copilot/tool/readonly_tools.go` | 注册 `k8s.*` 只读工具并纳入健康检查 |
| `server-monitor/server-web/copilot/diagnosis/types.go` | 增加 `K8sEvidence`、`K8sResourceSnapshot`、`K8sLogSnippet` |
| `server-monitor/server-web/copilot/diagnosis/evidence.go` | 按告警上下文和 target_kind 采集 K8s 证据 |
| `server-monitor/server-web/copilot/diagnosis/summarizer.go` | 将 K8s 摘要加入 Prompt，避免注入原始长日志 |
| `server-monitor/server-web/copilot/diagnosis/rule.go` | 规则分析使用 Pod phase、Deployment ready replicas、Events 判断 K8s 异常 |
| `server-monitor/server-web/copilot/action/policy.go` | 确认 namespace/name/replicas 校验覆盖真实执行入口；必要时增加 namespace 白名单 |
| `server-monitor/server-web/copilot/action/service.go` | 根据配置注入真实 executor 或 disabled executor |
| `server-monitor/server-web/api/router.go` | 初始化 K8s service，传入 Tool Executor、Diagnosis 和 Action Service |
| `server-monitor/server-web/api/middleware/metrics.go` | 增加 K8s 工具调用、错误、超时、执行结果指标 |
| `server-monitor/server-web/docs/swagger.yaml` / `docs.go` / `swagger.json` | 更新 K8s 工具、Action 执行结果、错误响应说明 |

### 4.3 前端修改文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/frontend/src/types/index.ts` | 增加 `K8sPodSummary`、`K8sDeploymentSummary`、`K8sEventSummary`、`K8sEvidence` |
| `server-monitor/frontend/src/api/copilot.ts` | 如工具结果类型过窄，补齐 `k8s.*` 结果类型 |
| `server-monitor/frontend/src/api/diagnosis.ts` | 补充 `k8s_evidence` 字段类型 |
| `server-monitor/frontend/src/pages/CopilotPage.vue` | 渲染 K8s 工具结构化结果，避免长日志挤压对话布局 |
| `server-monitor/frontend/src/pages/DiagnosisDetailPage.vue` | 展示 K8s 资源状态、事件摘要、日志片段和采集错误 |
| `server-monitor/frontend/src/pages/ActionDetailPage.vue` | 展示 restart/scale 执行前后状态、replicas、错误和审计入口 |
| `server-monitor/frontend/src/pages/AuditLogsPage.vue` | 支持筛选 `k8s.restart_deployment`、`k8s.scale_deployment` |

### 4.4 部署与配置文件

| 文件 | 修改内容 |
|---|---|
| `server-monitor/docker-compose.yml` | 可选挂载只读 kubeconfig；注入 `K8S_ENABLED=false` 默认值 |
| `server-monitor/k8s/configmap.yaml` | 增加 K8s 读取和写操作配置 |
| `server-monitor/k8s/web.yaml` | 注入环境变量、ServiceAccountName、必要 volume |
| `server-monitor/k8s/rbac.yaml` 或新增 `server-monitor/k8s/chatops-rbac.yaml` | 定义 ChatOps 专用 ServiceAccount、Role、RoleBinding |
| `server-monitor/charts/server-monitor/values.yaml` | 增加 `k8sIntegration.*` values |
| `server-monitor/charts/server-monitor/templates/configmap.yaml` | 输出 K8s 配置 |
| `server-monitor/charts/server-monitor/templates/server-web.yaml` | 注入 ServiceAccount、env、volumeMount |
| `server-monitor/charts/server-monitor/templates/chatops-rbac.yaml` | Helm 管理最小 RBAC |

---

## 5. 配置与依赖设计

### 5.1 新增环境变量

| 环境变量 | 默认值 | 类型 | 说明 | 敏感 |
|---|---:|---|---|---|
| `K8S_ENABLED` | `false` | bool | 是否启用 K8s 只读工具和诊断证据 | 否 |
| `K8S_WRITE_ENABLED` | `false` | bool | 是否启用审批后的真实 K8s 写操作 | 否 |
| `K8S_IN_CLUSTER` | `true` | bool | 是否优先使用集群内 ServiceAccount | 否 |
| `K8S_KUBECONFIG` | 空 | string | 本地/Compose 使用的 kubeconfig 路径；为空时使用 in-cluster 或默认 kubeconfig | 是，路径本身不敏感但文件内容敏感 |
| `K8S_ALLOWED_NAMESPACES` | 空 | csv | 允许访问的 namespace；为空表示只允许 `default`，不表示全量开放 | 否 |
| `K8S_DEFAULT_NAMESPACE` | `default` | string | 用户未指定 namespace 时的默认值 | 否 |
| `K8S_REQUEST_TIMEOUT_SECONDS` | `10` | duration | 单次 K8s API 调用超时 | 否 |
| `K8S_LOG_TAIL_LINES` | `100` | int | `k8s.get_logs` 默认日志行数 | 否 |
| `K8S_LOG_MAX_BYTES` | `32768` | int | 单次日志返回最大字节数 | 否 |
| `K8S_EVENT_LIMIT` | `50` | int | `k8s.get_events` 默认返回条数 | 否 |
| `K8S_MAX_REPLICAS` | 复用 `ACTION_MAX_REPLICAS` | int | `scale_deployment` 最大副本数；优先与 Phase 6 配置保持一致 | 否 |

### 5.2 配置校验规则

1. `K8S_WRITE_ENABLED=true` 时，`ACTION_APPROVAL_ENABLED=true` 且 `ACTION_EXECUTION_ENABLED=true`，否则启动失败。
2. `K8S_WRITE_ENABLED=true` 时，`K8S_ENABLED=true`，否则启动失败。
3. `K8S_ALLOWED_NAMESPACES` 不能为空时，每个 namespace 必须符合 K8s namespace 命名规范。
4. `K8S_DEFAULT_NAMESPACE` 必须在允许 namespace 列表中；如果列表为空，则默认只允许 `default`。
5. `K8S_REQUEST_TIMEOUT_SECONDS` 必须在 `[1, 60]`。
6. `K8S_LOG_TAIL_LINES` 必须在 `[1, 1000]`。
7. `K8S_LOG_MAX_BYTES` 必须在 `[1024, 262144]`。
8. `K8S_EVENT_LIMIT` 必须在 `[1, 200]`。
9. 生产 Helm 默认 `K8S_WRITE_ENABLED=false`，除非 values 明确开启并创建 RBAC。

### 5.3 依赖新增说明

Phase 7 需要新增 Kubernetes 官方 Go SDK：

| 依赖 | 版本 | 用途 | 必要性 |
|---|---|---|---|
| `k8s.io/client-go` | `v0.36.0` | typed client、in-cluster config、kubeconfig 加载、fake client 测试 | K8s 官方客户端，替代 shell 调用 `kubectl` |
| `k8s.io/apimachinery` | `v0.36.0` | `metav1`、types、runtime schema | client-go 必需配套依赖 |
| `k8s.io/api` | `v0.36.0` | Pod、Deployment、Service、Event 等 API 类型 | client-go typed client 必需 |

不使用 `kubectl` shell 命令的原因：

1. 不易做超时、审计、结构化错误和单元测试。
2. kubeconfig 和命令输出脱敏困难。
3. 容器镜像需要额外安装 kubectl，增加供应链和版本漂移风险。
4. typed client 能使用 fake client 做无集群测试，符合当前 Go 测试策略。

---

## 6. 数据结构与工具 Schema

### 6.1 K8s Evidence 数据结构

```go
type Evidence struct {
    Enabled       bool                 `json:"enabled"`
    Namespace     string               `json:"namespace,omitempty"`
    TargetKind    string               `json:"target_kind,omitempty"`
    TargetName    string               `json:"target_name,omitempty"`
    Pods          []PodSummary         `json:"pods,omitempty"`
    Deployments   []DeploymentSummary  `json:"deployments,omitempty"`
    Services      []ServiceSummary     `json:"services,omitempty"`
    Nodes         []NodeSummary        `json:"nodes,omitempty"`
    Events        []EventSummary       `json:"events,omitempty"`
    Logs          []LogSnippet         `json:"logs,omitempty"`
    Errors        []EvidenceError      `json:"errors,omitempty"`
    CollectedAt   time.Time            `json:"collected_at"`
}
```

字段原则：

1. DTO 只保留诊断需要的状态摘要，不返回完整 Kubernetes object。
2. Pod 不返回 env、volume secret、service account token、image pull secret。
3. Event message 最长 512 字符，避免把异常栈或敏感输出直接塞入 LLM。
4. Log snippet 默认 100 行、最大 32KB，并做 Secret/Token/Password 模式脱敏。
5. 所有 DTO 带 `namespace`、`name`、`resource_version` 或 `collected_at`，便于审计和复现。

### 6.2 Tool Registry 工具清单

| 工具名 | 风险 | 权限 | 参数 | 返回 |
|---|---|---|---|---|
| `k8s.get_pods` | low / readonly | viewer、admin | `namespace`、`label_selector`、`field_selector`、`limit` | `[]PodSummary` |
| `k8s.get_deployments` | low / readonly | viewer、admin | `namespace`、`name` 可选、`label_selector`、`limit` | `[]DeploymentSummary` |
| `k8s.get_services` | low / readonly | viewer、admin | `namespace`、`label_selector`、`limit` | `[]ServiceSummary` |
| `k8s.get_nodes` | low / readonly | viewer、admin | 无；可选 `limit` | `[]NodeSummary` |
| `k8s.get_events` | low / readonly | viewer、admin | `namespace`、`involved_kind`、`involved_name`、`limit` | `[]EventSummary` |
| `k8s.get_logs` | low / readonly | viewer、admin | `namespace`、`pod_name`、`container` 可选、`tail_lines` | `LogSnippet` |

参数限制：

1. `namespace` 未传时使用 `K8S_DEFAULT_NAMESPACE`。
2. namespace 必须在 `K8S_ALLOWED_NAMESPACES` 中。
3. `limit` 默认 50，最大 200。
4. `label_selector` 只允许 Kubernetes label selector 的安全字符集：字母、数字、`-`、`_`、`.`、`/`、`=`, `!`, `,`, `(`, `)`。
5. `field_selector` 第一版只允许 `metadata.name=`、`status.phase=`，避免任意复杂查询。
6. `pod_name`、`deployment_name`、`service_name` 必须符合 K8s DNS 命名规则。

### 6.3 写操作白名单

| 动作 | 风险 | 前置状态 | 执行方式 | 成功标准 |
|---|---|---|---|---|
| `k8s.restart_deployment` | medium | PendingAction 已 approved，namespace/name 合法，Deployment 存在 | patch Deployment template annotation `kubectl.kubernetes.io/restartedAt=<RFC3339>` | Deployment generation 变化或 patch 成功，AuditLog 记录前置 replicas/ready 状态 |
| `k8s.scale_deployment` | medium | PendingAction 已 approved，replicas 在 `[1, ACTION_MAX_REPLICAS]` | 使用 AppsV1 Scale subresource 更新 replicas | scale 更新成功，AuditLog 记录 old_replicas/new_replicas |

禁止动作：

1. delete namespace / pod / pvc / secret / configmap。
2. patch arbitrary object。
3. exec into pod。
4. port-forward。
5. apply raw YAML。
6. 修改 Secret、ServiceAccount、ClusterRole、RoleBinding。

---

## 7. 详细实施步骤

### 7.1 模块 1：依赖、配置与 K8s Client 工厂

**目标：** 先建立默认关闭、可注入、可测试的 K8s 接入边界。

**实施步骤：**

1. 在 `server-monitor/server-web/go.mod` 添加 Kubernetes 官方依赖，版本与 `chatops/server/go.mod` 对齐为 `v0.36.0`。
2. 在 `config.Config` 新增第 5.1 节配置字段。
3. 在 `Load()` 中解析环境变量，沿用项目现有 `configutil` 风格。
4. 在 `Validate()` 中加入第 5.2 节校验，特别是写操作必须依赖审批和执行开关。
5. 新增 `copilot/k8s/client.go`：
   - `NewClient(cfg Config) (*Client, error)`。
   - `K8S_ENABLED=false` 时返回 disabled client，不访问 kubeconfig。
   - `K8S_IN_CLUSTER=true` 时优先 `rest.InClusterConfig()`。
   - 本地模式使用 `K8S_KUBECONFIG`，为空时可尝试 `$HOME/.kube/config`，失败返回清晰错误。
6. 为测试暴露 `NewServiceWithClient(kubernetes.Interface, Config)`，避免单测依赖真实集群。

**技术要求：**

1. 不使用包级全局 K8s client。
2. 不在业务代码中使用 `context.Background()` 或 `context.TODO()` 调 K8s API；必须从 handler、diagnosis、worker 或 action executor 传入 ctx。
3. 每次 K8s API 调用使用 `context.WithTimeout(ctx, cfg.K8SRequestTimeout)` 包裹。
4. kubeconfig 路径和加载错误可以记录日志，但不能输出 kubeconfig 内容。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./config ./copilot/k8s
```

**通过标准：**

1. 默认 `K8S_ENABLED=false` 时现有 `server-web` 启动路径不受影响。
2. `K8S_WRITE_ENABLED=true` 但审批/执行配置未开启时配置测试失败。
3. fake client 测试不访问真实 kubeconfig。

### 7.2 模块 2：K8s 只读 Service 与 DTO 脱敏

**目标：** 将 Kubernetes object 转换为可展示、可诊断、可给 LLM 使用的安全摘要。

**实施步骤：**

1. 在 `copilot/k8s/types.go` 定义：
   - `PodSummary`
   - `DeploymentSummary`
   - `ServiceSummary`
   - `NodeSummary`
   - `EventSummary`
   - `LogSnippet`
   - `EvidenceError`
2. 在 `service.go` 实现：
   - `ListPods(ctx, QueryOptions) ([]PodSummary, error)`
   - `ListDeployments(ctx, QueryOptions) ([]DeploymentSummary, error)`
   - `ListServices(ctx, QueryOptions) ([]ServiceSummary, error)`
   - `ListNodes(ctx, QueryOptions) ([]NodeSummary, error)`
   - `ListEvents(ctx, EventQuery) ([]EventSummary, error)`
   - `GetPodLogs(ctx, LogQuery) (LogSnippet, error)`
3. Pod 摘要字段包含：
   - namespace、name、phase、ready containers、restart count、node name、pod IP、start time、owner kind/name。
4. Deployment 摘要字段包含：
   - namespace、name、replicas、ready_replicas、updated_replicas、available_replicas、strategy、conditions。
5. Service 摘要字段包含：
   - namespace、name、type、cluster_ip、ports，不返回 external secrets。
6. Node 摘要字段包含：
   - name、ready、roles、kubelet version、cpu/memory capacity summary、conditions。
7. Event 摘要按 `lastTimestamp/eventTime` 倒序，限制条数，message 截断到 512 字符。
8. Logs 读取只使用 `TailLines` 和 `LimitBytes`，不支持全量日志。
9. `sanitize.go` 实现脱敏：
   - 键名包含 `secret`、`token`、`password`、`authorization` 的值统一替换为 `[REDACTED]`。
   - 日志中疑似 Bearer token、AK/SK、private key 块统一替换。

**技术要求：**

1. 所有 list 查询必须设置 `Limit`，避免一次返回过大。
2. 日志查询只允许指定 Pod，不允许按 namespace 拉取全部 Pod 日志。
3. 默认不查询 previous logs；如果后续需要，单独设计参数和审计。
4. 对 `IsNotFound`、`IsForbidden`、`IsUnauthorized` 做错误分类，返回前端友好错误，不泄露内部栈。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/k8s
```

**通过标准：**

1. fake client 中的 Pod/Deployment/Service/Node/Event 能转换为 DTO。
2. ctx cancel 后查询快速返回。
3. 日志和 annotation 中的敏感字段被脱敏。
4. 空 namespace、非法名称、越界 limit 均返回明确错误。

### 7.3 模块 3：K8s 只读工具注册到 Tool Registry

**目标：** 让 Copilot 可以通过统一 Registry 调用 K8s 只读能力。

**实施步骤：**

1. 新增 `copilot/tool/k8s_tool.go`，为 6 个只读工具分别定义 schema。
2. 在 `tool.Executor` 的 Options 中增加 `K8sService`。
3. `K8S_ENABLED=false` 时：
   - 工具可以不注册；或注册后返回 `k8s integration disabled`。
   - 推荐第一版不注册，避免 LLM 看到不可用工具。
4. 在 `readonly_tools.go` 中注册：
   - `k8s.get_pods`
   - `k8s.get_deployments`
   - `k8s.get_services`
   - `k8s.get_nodes`
   - `k8s.get_events`
   - `k8s.get_logs`
5. Registry 权限：
   - 所有 K8s 只读工具 `ReadOnly=true`、`RiskLevel=low`。
   - viewer/admin 均可调用。
6. 工具结果返回结构化 JSON，禁止返回原型中的自然语言字符串。
7. 更新 Copilot Prompt 工具列表，使 LLM 知道 K8s 工具只读、不得编造集群状态。

**技术要求：**

1. 参数校验在 Registry 层和 K8s service 层都要做，防止绕过。
2. 工具超时使用 Tool Registry timeout 与 K8s request timeout 双重保护。
3. 工具执行日志只记录脱敏后的 args hash 和结果摘要，不记录日志正文。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/tool
```

**通过标准：**

1. 工具 schema 完整，风险等级为 low，read_only 为 true。
2. 非法 namespace、非法 selector、日志行数越界被拒绝。
3. disabled 模式不会导致 Copilot 初始化失败。

### 7.4 模块 4：Diagnosis Evidence 接入 K8s 上下文

**目标：** 让告警诊断报告能够结合 K8s 资源状态、事件和日志摘要，而不是只看主机指标。

**实施步骤：**

1. 在 `diagnosis/types.go` 增加 `K8sEvidence` 字段：
   - `namespace`
   - `target_kind`
   - `target_name`
   - `pods`
   - `deployments`
   - `events`
   - `logs`
   - `errors`
2. 在诊断请求归一化中识别：
   - `target_kind=k8s_pod`
   - `target_kind=k8s_deployment`
   - `namespace`
   - `resource_name`
3. EvidenceCollector 采集策略：
   - Deployment 告警：查 Deployment、关联 Pod、相关 Events。
   - Pod 告警：查 Pod、Pod Events、最近日志。
   - Node 告警：查 Node、相关 Events，不读取全部 Pod 日志。
   - 主机类告警无 K8s 目标时跳过 K8s 证据。
4. K8s 采集错误只写入 `errors`，不让整个诊断失败。
5. `summarizer.go` Prompt 中只注入摘要：
   - Deployment ready/desired。
   - Pod phase/restarts。
   - 最近 5 条 warning events。
   - 最多 20 行脱敏日志摘要。
6. `rule.go` 增加确定性规则：
   - Deployment `ready_replicas < replicas` → 标记 rollout/deployment 异常。
   - Pod `CrashLoopBackOff` 或 restart count 持续增加 → 标记容器重启风险。
   - Event 出现 `FailedScheduling`、`ImagePullBackOff`、`BackOff` → 标记对应原因。
7. 前端诊断详情展示 K8s evidence，并明确采集失败原因。

**技术要求：**

1. K8s Evidence 是可选证据，不能影响主诊断报告持久化。
2. 诊断 prompt hash 必须包含脱敏后的 K8s 摘要，便于缓存和审计复现。
3. 不把完整 Pod YAML、Secret、ConfigMap、长日志传给 LLM。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/diagnosis
```

**通过标准：**

1. K8s disabled 时原有诊断测试不回退。
2. fake K8s evidence 能进入报告 JSON。
3. K8s API Forbidden/Timeout 时报告仍可 completed 或 rule-only completed，并标注 evidence error。

### 7.5 模块 5：真实 K8s ActionExecutor

**目标：** 在 Phase 6 审批链路之后，接入真实 restart/scale 执行能力。

**实施步骤：**

1. 新增 `copilot/action/k8s_client_executor.go` 实现现有接口：

```go
type K8sExecutor interface {
    RestartDeployment(ctx context.Context, namespace, name string) (ActionResult, error)
    ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) (ActionResult, error)
}
```

2. `RestartDeployment` 流程：
   - 校验 namespace/name。
   - `Get` 当前 Deployment，记录 old generation、replicas、ready replicas。
   - patch `spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"]` 为当前 UTC RFC3339。
   - 返回 ActionResult，包含 target、message、old/new annotation、replicas 摘要。
3. `ScaleDeployment` 流程：
   - 校验 namespace/name/replicas。
   - `GetScale` 当前 scale，记录 old replicas。
   - 更新 scale replicas。
   - 返回 ActionResult，包含 old_replicas/new_replicas。
4. 执行前继续复用 Phase 6 `Policy.ValidateExecute()`，防止跳过审批直接调用 executor。
5. `ActionExecutionEnabled=false` 或 `K8S_WRITE_ENABLED=false` 时继续使用 `DisabledK8sExecutor`。
6. 执行失败分类：
   - NotFound：状态 failed，错误摘要 `deployment not found`。
   - Forbidden/Unauthorized：状态 failed，提示 RBAC 不足。
   - Conflict：状态 failed，提示资源版本冲突，可重试。
   - Timeout：状态 failed，提示 K8s API timeout。
7. AuditLog 记录：
   - action_type。
   - namespace/name。
   - params 脱敏 JSON。
   - result_json 中的 old/new replicas 或 restart annotation。
   - error_message 摘要。
   - trace_id。

**技术要求：**

1. restart/scale 只能作用于 Deployment。
2. replicas 必须在 `[1, ACTION_MAX_REPLICAS]`。
3. 不允许 scale 到 0，避免 AI 建议直接停止服务。
4. 不等待 rollout 完成；第一版只确认 API 更新成功，前端可通过只读工具查看后续状态。
5. 不吞掉执行错误；错误必须更新 PendingAction 和 AuditLog。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/action
```

**通过标准：**

1. 未审批 action 无法执行。
2. approved restart 能 patch fake Deployment annotation。
3. approved scale 能更新 fake Scale replicas。
4. NotFound/Forbidden/Conflict/Timeout 均进入 failed 审计路径。

### 7.6 模块 6：RBAC、部署配置与最小权限

**目标：** 让 K8s 集群内运行时使用最小权限，而不是复用默认高权限账号。

**实施步骤：**

1. 新增或修改原生 K8s 清单：
   - `ServiceAccount cloudops-copilot`
   - `Role cloudops-copilot-readonly`
   - `RoleBinding cloudops-copilot-readonly`
   - 如启用写操作，再增加最小写权限 Role。
2. 只读权限：
   - `get/list/watch` pods。
   - `get/list/watch` deployments。
   - `get/list/watch` services。
   - `get/list/watch` events。
   - `get/list/watch` nodes 如果使用 ClusterRole；否则第一版可不开放 node 查询或只在允许环境启用。
   - `get` pods/log。
3. 写权限：
   - `get/patch/update` deployments。
   - `get/update` deployments/scale。
   - 不授予 delete、create、update secrets、update roles。
4. `server-monitor/k8s/web.yaml` 指定 `serviceAccountName: cloudops-copilot`。
5. Helm values 增加：

```yaml
k8sIntegration:
  enabled: false
  writeEnabled: false
  inCluster: true
  allowedNamespaces:
    - default
  defaultNamespace: default
  requestTimeoutSeconds: 10
  logTailLines: 100
  logMaxBytes: 32768
  eventLimit: 50
  rbac:
    create: true
```

6. Helm 模板根据 values 创建 ServiceAccount、Role/ClusterRole、RoleBinding/ClusterRoleBinding。
7. Compose 默认不挂载 kubeconfig；本地调试需要显式挂载只读 kubeconfig 并设置 `K8S_ENABLED=true`。

**技术要求：**

1. 默认部署不扩大权限。
2. Helm 和原生 K8s 配置必须保持同名环境变量。
3. kubeconfig 文件不得进入镜像，不写入 Git。
4. 如果启用 Node 查询需要 ClusterRole，必须在 values 中显式开启，并在文档中说明权限扩大。

**验证命令：**

```bash
helm lint server-monitor/charts/server-monitor
kubectl apply --dry-run=client -f server-monitor/k8s/
```

如果本地没有 Kubernetes API 或 Helm，必须在实施报告中说明未验证原因。

**通过标准：**

1. 配置默认关闭 K8s 写操作。
2. RBAC 不包含 delete secret、delete pvc、delete namespace 等高危权限。
3. Helm 渲染出的 env 与 configmap 键名一致。

### 7.7 模块 7：前端 K8s 证据与动作执行展示

**目标：** 让用户能看懂 K8s 证据和执行结果，而不是只看到 JSON。

**实施步骤：**

1. 在 `types/index.ts` 增加 K8s DTO 类型。
2. `CopilotPage.vue`：
   - 对 `k8s.get_pods` 等工具结果渲染为表格或紧凑列表。
   - 日志结果使用可折叠文本块，默认只展开摘要。
3. `DiagnosisDetailPage.vue`：
   - 新增 K8s Evidence 区块。
   - 展示 Deployment ready/desired、Pod phase/restarts、Warning Events、日志片段。
   - K8s disabled 或采集失败时展示降级原因。
4. `ActionDetailPage.vue`：
   - 对 restart/scale 显示 namespace、deployment、replicas、执行前状态、执行后结果。
   - failed 状态展示错误摘要和审计 trace id。
5. `AuditLogsPage.vue`：
   - 动作类型筛选包含 K8s action。
   - request/result JSON 保持脱敏和折叠展示。

**技术要求：**

1. 不新增大面积营销式页面；保持现有控制台信息密度。
2. 长日志必须折叠，避免页面卡顿或遮挡。
3. 所有错误展示面向运维用户，避免暴露内部堆栈。
4. viewer 可以看只读诊断证据，但不能看到审批执行按钮。

**验证命令：**

```bash
cd server-monitor/frontend
npm run build
```

**通过标准：**

1. 前端类型检查和构建通过。
2. K8s evidence 为空、失败、成功三种状态都有可读展示。
3. admin 和 viewer 的按钮/入口权限符合 Phase 6 权限矩阵。

### 7.8 模块 8：观测、降级、限流与安全校验

**目标：** K8s 接入失败时可追踪、可降级，不拖垮 Copilot 和 Diagnosis 主链路。

**实施步骤：**

1. 增加指标：
   - `cloudops_k8s_tool_requests_total{tool,result}`
   - `cloudops_k8s_tool_duration_seconds{tool}`
   - `cloudops_k8s_action_requests_total{action,result}`
   - `cloudops_k8s_action_duration_seconds{action}`
2. 日志字段统一：
   - `tool`
   - `namespace`
   - `resource`
   - `trace_id`
   - `duration_ms`
   - `result`
3. 降级策略：
   - K8s disabled：不注册工具或返回 disabled。
   - K8s timeout：工具返回失败，诊断继续。
   - RBAC forbidden：明确提示权限不足，写审计。
   - logs too large：截断并标记 `truncated=true`。
4. 安全扫描：
   - 对工具输出做敏感字段脱敏。
   - 对 AuditLog request/result 做脱敏。
   - 对 LLM Prompt 只注入摘要，不注入原始 object。
5. 限流：
   - 复用 Copilot/API 现有限流。
   - `k8s.get_logs` 可增加更低频限制，避免大量日志读取。

**验证命令：**

```bash
cd server-monitor/server-web
go test ./copilot/k8s ./copilot/tool ./copilot/diagnosis ./copilot/action
```

**通过标准：**

1. 禁用、超时、Forbidden、NotFound、日志过大均有测试覆盖。
2. 指标和日志不包含敏感原文。
3. K8s 工具失败不会导致 Copilot 进程 panic。

### 7.9 模块 9：联调、回归、验收与交付

**目标：** 证明 Phase 7 与前面阶段集成后形成完整闭环。

**联调场景：**

| 场景 | 步骤 | 期望结果 |
|---|---|---|
| K8s disabled 回归 | 默认配置启动 `server-web` | Copilot、诊断、审批已有能力不回退；K8s 工具不可见或返回 disabled |
| 只读查询 | `POST /api/v1/copilot/chat` 问 “default 有哪些 pod” | LLM 或规则路由到 `k8s.get_pods`，返回结构化 Pod 摘要 |
| 诊断接入 | 手动诊断 `target_kind=k8s_deployment` 的告警 | 报告包含 Deployment、Pod、Event 证据；K8s 错误被标注 |
| 创建待审批动作 | 诊断建议 restart deployment | 只创建 PendingAction，不执行 |
| 审批后 restart | admin approve + execute | Deployment annotation 更新，PendingAction executed，AuditLog success |
| 审批后 scale | admin approve + execute replicas=2 | Scale subresource 更新，AuditLog 记录 old/new replicas |
| 权限不足 | 使用无写权限 ServiceAccount 执行 | PendingAction failed，AuditLog failure，错误摘要为 forbidden |
| 非 admin 操作 | viewer 调用 approve/execute | 返回 403，AuditLog denied 或安全日志记录 |

**回归命令：**

```bash
cd server-monitor/server-web
go test ./...
go vet ./...

cd ../frontend
npm run build

cd ..
docker compose config
helm lint charts/server-monitor
kubectl apply --dry-run=client -f k8s/
```

**通过标准：**

1. 所有 Go 单元测试通过。
2. 前端构建通过。
3. Compose/Helm/K8s 配置校验通过，或明确记录本地工具缺失导致未运行。
4. 默认配置不真实访问 Kubernetes。
5. 真实或 fake K8s 联调证明只读工具和审批后写操作路径可用。

---

## 8. 资源分配

### 8.1 人员角色

| 角色 | 人数 | 职责 |
|---|---:|---|
| 后端 Go 工程师 | 1 | K8s client/service/tool/diagnosis/action executor 主实现、单元测试 |
| 前端工程师 | 1 | Copilot、诊断详情、Action/Audit 页面 K8s 展示 |
| DevOps / 平台工程师 | 1 | ServiceAccount、RBAC、Compose/K8s/Helm 配置、集群联调 |
| 测试 / 验收负责人 | 1 | fake client 场景、权限场景、端到端验收、回归报告 |

如果只有单人实施，按模块顺序串行推进；优先完成后端只读工具和 fake client 测试，再接真实写操作和部署模板。

### 8.2 工作量估算

| 模块 | 预计人日 | 主要产出 |
|---|---:|---|
| 模块 1：依赖、配置、Client 工厂 | 1.0 | 配置、client 初始化、fake 注入 |
| 模块 2：只读 Service 与 DTO | 1.5 | DTO、查询、脱敏、单测 |
| 模块 3：Tool Registry 接入 | 1.0 | 6 个只读工具、schema、tool tests |
| 模块 4：Diagnosis Evidence | 1.0 | K8s evidence、prompt 摘要、rule 扩展 |
| 模块 5：真实 ActionExecutor | 1.5 | restart/scale、执行失败审计、fake tests |
| 模块 6：RBAC 与部署 | 1.0 | K8s YAML、Helm values/templates、Compose 配置 |
| 模块 7：前端展示 | 1.0 | 类型、页面展示、构建 |
| 模块 8：观测、安全、限流 | 0.5 | 指标、日志、降级、安全测试补齐 |
| 模块 9：联调与验收 | 1.0 | 回归命令、集成场景、验收记录 |

总计约 9.5 人日。单人实施建议预留 2 周，避免把真实集群权限和写操作压缩到一天内完成。

---

## 9. 时间节点

以 2026-05-12 开始实施为基准，建议排期如下：

| 日期 | 阶段 | 目标 | 验收输出 |
|---|---|---|---|
| 2026-05-12 | Day 1 | 模块 1：依赖、配置、client 工厂 | `go test ./config ./copilot/k8s` 通过；默认 disabled 不影响启动 |
| 2026-05-13 | Day 2 | 模块 2 上半：Pod/Deployment/Service/Node DTO 与查询 | fake client 只读查询测试通过 |
| 2026-05-14 | Day 3 | 模块 2 下半：Event/Log、脱敏、错误分类 | 日志截断、Forbidden/NotFound/Timeout 测试通过 |
| 2026-05-15 | Day 4 | 模块 3：Tool Registry 接入 | `go test ./copilot/tool` 通过；工具 schema 可列出 |
| 2026-05-18 | Day 5 | 模块 4：Diagnosis Evidence | `go test ./copilot/diagnosis` 通过；报告包含 K8s evidence |
| 2026-05-19 | Day 6 | 模块 5：真实 ActionExecutor | `go test ./copilot/action` 通过；fake restart/scale 成功和失败路径覆盖 |
| 2026-05-20 | Day 7 | 模块 6：RBAC 与部署 | `helm lint`、`kubectl apply --dry-run=client` 通过或记录环境限制 |
| 2026-05-21 | Day 8 | 模块 7：前端展示 | `npm run build` 通过；页面展示 K8s evidence/action result |
| 2026-05-22 | Day 9 | 模块 8-9：安全、观测、联调、回归 | `go test ./...`、`go vet ./...`、联调场景完成 |
| 2026-05-25 | 缓冲 | 修复回归问题、补充文档和验收记录 | Phase 7 closeout 报告，给出提交建议 |

检查点：

1. Day 4 结束前只读工具必须完成；否则不进入写操作。
2. Day 6 结束前 ActionExecutor 必须通过 fake client 测试；否则不开放真实集群写权限。
3. Day 8 前端构建失败时停止联调，先修复类型和页面问题。
4. Day 9 任一审批/审计失败路径不完整，Phase 7 不能标记完成。

---

## 10. 技术要求

### 10.1 Go 实现要求

1. `client-go` 调用必须使用传入的 `context.Context`，并叠加超时。
2. 不使用包级全局 client；通过 constructor 注入。
3. 不在 handler 内堆业务逻辑；K8s 查询放在 `copilot/k8s` service，工具层只做参数适配。
4. 不新增大接口；只在使用方定义小接口，例如 `K8sReader`、`K8sExecutor`。
5. 不吞掉错误；service 层返回分类错误，handler/tool/action 层转换为用户可读响应。
6. 不 panic；初始化错误由启动流程返回，业务错误按工具失败或 action failed 处理。
7. 不把敏感字段写入日志、Prompt、审计或前端。
8. 所有测试使用 fake client、httptest、内存对象，不依赖真实集群。

### 10.2 安全要求

1. 默认关闭 K8s 接入和 K8s 写操作。
2. 只读工具也必须做 namespace 白名单校验。
3. 写操作必须满足：PendingAction approved、admin/system actor、`ACTION_EXECUTION_ENABLED=true`、`K8S_WRITE_ENABLED=true`、Policy 校验、RBAC 成功。
4. 高风险动作禁止进入 PendingAction。
5. AuditLog 必须覆盖 success/failure/denied/timeout。
6. RBAC 默认最小权限，不使用 cluster-admin。
7. Node 查询如需要 ClusterRole，必须独立开关，默认关闭或只读最小化。

### 10.3 性能要求

1. K8s 单工具默认超时 10s。
2. 日志最大 100 行 / 32KB。
3. Event 默认最多 50 条。
4. Diagnosis 中 K8s 证据采集与其他证据并行，但失败不阻塞主报告。
5. 对重复 Copilot 查询可沿用现有短 TTL 缓存，避免连续拉取日志。

### 10.4 兼容性要求

1. 不修改现有 API 外层响应结构；只兼容新增字段。
2. 不修改 Phase 6 PendingAction 状态机。
3. 不修改已有 Prometheus metric name。
4. 不修改 Go version。
5. 不改变 Dockerfile 基础镜像。
6. 不引入 LangChain、controller-runtime 或额外数据库。

---

## 11. 风险评估与应对措施

| 风险 | 影响 | 概率 | 应对措施 | 触发时处理 |
|---|---|---:|---|---|
| K8s RBAC 权限过大 | 可能造成越权操作 | 中 | 默认关闭写操作；独立 ServiceAccount；Role 最小权限；审查模板 | 立即关闭 `K8S_WRITE_ENABLED`，回滚 RBAC，保留 AuditLog 排查 |
| LLM 诱导执行危险动作 | 可能绕过人工判断 | 中 | LLM 只生成建议；Tool Registry 和 Action Policy 白名单；高风险动作禁止 pending | 拒绝动作并写审计，补充 prompt 和 policy 测试 |
| kubeconfig 泄露 | 集群凭据泄露 | 低 | 不把 kubeconfig 入镜像/入 Git；Compose 只读挂载；日志不打印内容 | 立即吊销凭据，轮换 ServiceAccount token |
| 日志包含敏感信息 | 敏感数据进入 LLM/前端/审计 | 中 | 日志截断、脱敏、默认低行数；Prompt 只注入摘要 | 关闭 `k8s.get_logs`，补充脱敏规则，清理测试数据 |
| K8s API 慢或不可用 | Copilot/诊断延迟上升 | 中 | 单工具超时、并行采集、失败降级 | 标记 evidence error，保持报告可生成 |
| Scale 参数错误 | 服务容量异常 | 中 | replicas 范围 `[1, ACTION_MAX_REPLICAS]`；审批页展示 old/new | 执行失败或人工再次 scale 回原值，审计记录变更 |
| Restart 造成业务抖动 | Deployment 滚动重启影响服务 | 中 | 必须 admin 审批；展示 ready replicas 和风险；不支持批量重启 | 观察 rollout，必要时人工 rollback |
| client-go 依赖膨胀 | go.mod/go.sum 变化较大 | 高 | 只引入官方必需依赖；不升级无关依赖；说明原因 | 若依赖冲突，暂停并单独评估版本 |
| Node 查询需要 ClusterRole | 权限范围扩大 | 中 | Node 工具单独开关；默认可先不注册或只在 admin 可见 | 禁用 `k8s.get_nodes`，保留 namespace 级工具 |
| 前端长日志卡顿 | 页面体验变差 | 中 | 后端限制大小；前端折叠渲染 | 降低 `K8S_LOG_MAX_BYTES`，改为摘要显示 |

---

## 12. 验收标准

Phase 7 完成必须同时满足以下条件：

1. 默认配置下，K8s 接入关闭，现有监控、Copilot、诊断、Runbook、自动诊断、审批审计能力不回退。
2. 开启 `K8S_ENABLED=true` 后，`k8s.get_pods`、`k8s.get_deployments`、`k8s.get_services`、`k8s.get_events`、`k8s.get_logs` 至少在 fake client 或测试集群中可用。
3. K8s 只读工具全部经过 Tool Registry schema 校验、权限校验、超时控制和脱敏。
4. Diagnosis 能在 K8s 目标存在时采集 K8s evidence；K8s 失败时诊断仍能降级完成。
5. `k8s.restart_deployment` 和 `k8s.scale_deployment` 只有 approved PendingAction 才能执行。
6. viewer 不能创建、审批、执行写操作。
7. 执行成功、执行失败、权限拒绝、超时都写入 AuditLog。
8. 部署模板默认不授予高危权限，RBAC 不包含 delete Namespace/PVC/Secret。
9. 前端能展示 K8s evidence、action 执行结果和审计记录。
10. 验证命令有明确结果；未运行项必须记录原因。

---

## 13. 建议提交拆分

Phase 7 建议拆为小提交，便于回滚和 review：

```bash
git add server-monitor/server-web/go.mod server-monitor/server-web/go.sum server-monitor/server-web/config server-monitor/server-web/copilot/k8s
git commit -m "feat: add k8s client configuration"

git add server-monitor/server-web/copilot/tool server-monitor/server-web/copilot/k8s
git commit -m "feat: register k8s readonly copilot tools"

git add server-monitor/server-web/copilot/diagnosis
git commit -m "feat: include k8s evidence in diagnosis"

git add server-monitor/server-web/copilot/action
git commit -m "feat: execute approved k8s actions"

git add server-monitor/k8s server-monitor/charts/server-monitor server-monitor/docker-compose.yml
git commit -m "chore: add k8s integration deployment config"

git add server-monitor/frontend/src
git commit -m "feat: show k8s evidence and action results"
```

实际提交前应根据最终改动文件调整 `git add` 范围；不要把 kubeconfig、临时日志、构建产物或真实集群凭据加入提交。

---

## 14. Phase 7 完成定义

Phase 7 只有在以下四类闭环都完成后，才能标记为结束：

1. **功能闭环：** K8s 只读工具、诊断证据、审批后 restart/scale 能在 fake client 或测试集群中跑通。
2. **安全闭环：** namespace 白名单、RBAC、Action Policy、AuditLog、脱敏和默认关闭策略均生效。
3. **部署闭环：** Compose、原生 K8s、Helm 的配置键一致，默认不扩大权限。
4. **验证闭环：** 后端测试、前端构建、部署模板校验和关键联调场景均有明确结果。

若真实集群不可用，Phase 7 可以先达到“fake client + dry-run 配置校验完成”的开发完成状态，但不能声称生产联调完成；最终上线前必须在受控测试集群中验证 RBAC、只读查询、审批后 restart/scale 和审计链路。
