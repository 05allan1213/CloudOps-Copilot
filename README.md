# CloudOps-Copilot

CloudOps-Copilot 是运行在本机 Kubernetes 上的云原生运维工作台。当前可运行基线以 Incident 为入口，把 MySQL 中保留的 Signal、Timeline、Evidence、Investigation、Remediation、Delivery 与 Verification 投影到同一个 Workbench，并通过唯一的 `/api/v1` 合同读写。

当前实施权威是 [CloudOps 全栈实施规范](docs/CloudOps-Implementation-Spec.md) 与 [实施任务书](docs/CloudOps-Implementation-Taskbook.md)。[实施状态](docs/evidence/cloudops-implementation-status.md) 记录逐任务的 `PASS`、`FAIL` 和 `NOT RUN`。带产品代际标签的旧设计、报告和 ADR 只保留为历史迁移输入，不描述当前产品合同。

## 当前可用能力

- Local Owner 模式：应用只通过 `127.0.0.1` port-forward 暴露，无登录、session 或浏览器 token。
- 单一 V1 API：OpenAPI 位于 [`docs/api-v1-openapi.yaml`](docs/api-v1-openapi.yaml)，实现位于 `internal/api`。
- Incident Workbench：当前产品路由为 `/incidents` 与 `/incidents/:incidentId`，直接读取 MySQL projection。
- 独立进程：`cloudops-api`、`cloudops-worker`、`cloudops-migrate`；API 不运行后台 Provider 工作。
- 单一语义数据基线：`migrations/00001_cloudops_baseline.sql`；旧本地领域数据经一次性转换保留。
- 可恢复本地生命周期：kind、Helm、MySQL、备份、恢复、重启和诊断都由顶层 `make local-*` 管理。
- Provider fail-closed：本地默认 Worker 处于 standby，不领取需要未配置 Provider 的任务，也不伪造结果。

十 Workspace、Settings、Infrastructure、Monitoring、Logs、Traces、Alerts、Agent、Verify 与 DevOps 的目标能力仍按任务书逐项实施。未在状态文件标记为 `DONE` 的能力不得从本 README 推断为已完成。

## 本地运行

需要 Linux、Docker、kind、kubectl、Helm、Go、Node.js/npm、curl、jq、OpenSSL、Git 和 `rg`。首次启动需要构建镜像并拉取固定的 kind/MySQL 依赖。

```bash
make local-up
```

默认入口是 `http://127.0.0.1:18080`。端口被占用时可为当前命令指定另一个 loopback 端口：

```bash
CLOUDOPS_LOCAL_PORT=18081 make local-up
```

生命周期命令只操作固定的 `cloudops-local` kind 集群、`cloudops-system` namespace 和 `cloudops` Helm release：

```bash
make local-status
make local-open
make local-logs COMPONENT=api
make local-restart
make local-doctor
make local-down
```

`local-down` 停止工作负载但保留 MySQL 数据、配置、secret 和备份。私有运行状态位于 Git 忽略且权限受限的 `.cloudops/`。

## 备份与恢复

```bash
make local-backup
make local-restore BACKUP=.cloudops/backups/<backup-id>
make local-reset
```

备份包含 checksummed schema/data 与所需本地配置。恢复先导入隔离 staging database，核对格式、checksum、schema identity 和逐表 count，再切换活动库；失败时保留活动数据并执行 rollback。`local-reset` 必须 backup-first 并显式确认，不能作用于其他集群或 namespace。

## 运行时结构

| 组件 | 所有权 |
|---|---|
| `cloudops-api` | 静态 UI、`/api/v1` Query/Command、用户健康检查；独立内部 listener 接收带 bearer 的 Alertmanager webhook |
| `cloudops-worker` | MySQL async task runtime；Provider 未配置时以 standby 方式保持可诊断且不领取任务 |
| `cloudops-migrate` | 启动前应用唯一 forward-only Goose baseline，并验证最终 schema version |
| MySQL | Incident、Evidence、任务、决策、变更与验证等 durable truth；API/Worker/Migrate 使用不同身份 |
| Helm | API、Worker、Migrate Job、MySQL、Service、ServiceAccount/RBAC 与可选 observability CR |

更完整的所有权与数据流见 [Architecture](docs/architecture.md)，安全边界见 [Security](docs/security.md)，HTTP 合同见 [API](docs/api.md)。

## 开发检查

实现期间按变更范围运行 focused checks；大任务完成后再执行真实浏览器到 Provider 联调。

```bash
make check-naming
make test-go
make test-frontend
make helm-contracts
make docker-build
```

`make check` 是完整本地代码与静态合同集合。Fixture Playwright 只证明 presentation 行为，不能替代 UI -> `/api/v1` -> Provider 的真实验收。

## 目录

```text
cmd/                    process entrypoints
internal/api/           sole public V1 transport contract
internal/asyncjob/      MySQL-backed task runtime
internal/bootstrap/     process composition and Provider gateway
internal/infra/         Provider and persistence adapters
migrations/             one semantic baseline
frontend/               Vue Incident Workbench
charts/cloudops/        sole deployable Helm chart
runbooks/               bounded investigation knowledge
scripts/                lifecycle and contract checks
docs/                   current specification, ADRs, API and evidence
```
