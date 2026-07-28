# CloudOps-Copilot

CloudOps-Copilot 是运行在本机 Kubernetes 上的统一云原生运维平台。十个 Workspace 通过唯一的 `/api/v1` 合同共享 Operational Scope、资源身份和绝对时间窗口；Incident 协调 Alert、Investigation、Decision、Operation、Verification 与 ResolutionReport，但不承载整个产品。

当前实施权威是 [CloudOps 全栈实施规范](docs/CloudOps-Implementation-Spec.md) 与 [实施任务书](docs/CloudOps-Implementation-Taskbook.md)。[实施状态](docs/evidence/cloudops-implementation-status.md) 和 [Phase 9 最终证据报告](docs/evidence/phase-9-scenario/final-evidence-report.md) 逐项记录 `PASS`、`FAIL` 与 `NOT RUN`。带产品代际标签的旧设计、报告和 ADR 仅保留历史 provenance，不描述当前产品合同。

## 当前能力

- Local Owner：应用仅通过 `127.0.0.1` port-forward 暴露，无 login、session 或浏览器 token。
- 十 Workspace：Overview、Infrastructure、Monitoring、Alerts、Logs、Traces、Agent、Incidents、DevOps 与 Settings 均可导航、deep link 和浏览器 Back/Forward。
- Cloud-native Evidence：当前 kind 集群的 Kubernetes topology、Prometheus Metrics、Alertmanager Alerts、Elasticsearch Logs 与 Tempo Traces 使用真实 Provider 数据，不以 fixture 补位。
- Agent：Investigation/Consultation、Evidence、Knowledge、Action Card、Operation Plan、exact Authorization 与 audit history 持久化到 MySQL；模型没有授权能力。
- Incident 与 Operation：Owner 可审查 immutable Plan，精确授权后执行 allowlisted operation，并以当前 Metrics、Alert、Kubernetes、Logs/Trace Evidence 完成 Verify。
- Scenario：`scenario-up/status/down` 在 `demo` Namespace 创建真实 bounded workload、traffic 与 fault；关闭时仅删除 Scenario runtime，保留 tagged CloudOps history。
- 可恢复生命周期：kind、Helm、MySQL、observability stack、备份、恢复、重启和诊断均由顶层 `make local-*` 管理。
- Fail closed：GitHub、Argo、hosted、staging 或 production 等未配置/未授权分支明确显示 `NOT RUN`，不会生成假成功。

## 本地运行

需要 Linux、Docker、kind、kubectl、Helm、Go、Node.js/npm、curl、jq、OpenSSL、Git 和 `rg`。首次启动会构建本地镜像并拉取或加载固定依赖。

```bash
make local-up
```

默认入口为 `http://127.0.0.1:18080`。端口被占用时可为当前命令选择另一个 loopback 端口：

```bash
CLOUDOPS_LOCAL_PORT=18081 make local-up
```

生命周期命令只操作固定的 `cloudops-local` kind 集群、`cloudops-system` Namespace 和 `cloudops` Helm release：

```bash
make local-status
make local-open
make local-logs COMPONENT=api
make local-restart
make local-doctor
make local-down
```

`local-down` 停止工作负载但保留 MySQL 数据、配置、secret、Evidence 与备份。私有运行状态位于 Git 忽略且权限受限的 `.cloudops/`。完整操作合同见 [Operations](docs/operations.md)。

## 真实 Scenario

```bash
make scenario-up
make scenario-status
# 在浏览器完成 Observe -> Detect -> Investigate -> Decide -> Act -> Verify
make scenario-down
make scenario-status
```

Scenario 使用唯一 identity 标记其 Kubernetes runtime、Alert、Context Snapshot、Agent 与 Operation history。`scenario-down` 必须得到 `scenario_state=inactive`、`scenario_runtime_resources=0`、`scenario_write_gate=false`，且 retained history 不减少。它不删除普通 Live Mode 数据，也不授权 GitHub/Argo 或其他外部写入。

## 备份与恢复

```bash
make local-backup
make local-restore BACKUP=.cloudops/backups/<backup-id>
make local-reset
```

恢复先导入隔离 staging database，核对格式、checksum、schema identity 与逐表 count，再切换活动库；失败时保留活动数据并 rollback。`local-reset` 必须 backup-first 且显式确认，不能作用于其他集群或 Namespace。

## 运行时结构

| 组件 | 所有权 |
|---|---|
| `cloudops-api` | 静态 UI、`/api/v1` Query/Command、用户健康检查；独立内部 listener 接收带 bearer 的 Alertmanager webhook |
| `cloudops-worker` | MySQL async task runtime、bounded Provider read 与受 authority/write-gate 约束的 allowlisted operation |
| `cloudops-migrate` | 启动前应用 forward-only Goose migration，并验证最终 schema version |
| `cloudops-demo` | Scenario workload/traffic 镜像；只由 canonical Helm release 在 Scenario active 时实例化 |
| MySQL | Incident、Evidence、任务、配置、决策、Operation 与 Verification 的 durable truth |
| Helm | 唯一 API/Worker/Migrate/MySQL/observability/Scenario 部署源 |

更完整的数据流见 [Architecture](docs/architecture.md)，安全边界见 [Security](docs/security.md)，HTTP 合同见 [API](docs/api.md)，可靠性合同见 [Reliability](docs/reliability.md)。

## 开发检查

```bash
make test-go
make test-race
make vet
make lint-go
make build-go
make test-frontend
make build-frontend
make helm-contracts
make kubeconform
make check-naming
```

`make check` 汇总本地代码与静态合同。Fixture Playwright 只能证明 presentation；真实完成声明必须有当前 UI -> `/api/v1` -> Provider evidence。

## 目录

```text
cmd/                    process entrypoints
internal/api/           sole public V1 transport contract
internal/asyncjob/      MySQL-backed task runtime
internal/bootstrap/     process composition and Provider boundary
internal/infra/         Provider and persistence adapters
migrations/             forward-only semantic schema
frontend/               Vue CloudOps Web application
charts/cloudops/        sole deployable Helm chart
runbooks/               bounded investigation knowledge
scripts/                lifecycle and contract checks
docs/                   current specification, ADRs, API and evidence
```
