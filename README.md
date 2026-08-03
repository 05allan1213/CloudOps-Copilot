# CloudOps-Copilot

CloudOps-Copilot 是运行在本机 Kubernetes 上的云原生运维平台。Overview、Infrastructure、Monitoring、Alerts、Logs、Traces、Agent、Incidents、DevOps 和 Settings 共享同一套 `/api/v1` 合同、Operational Scope、资源身份与绝对时间窗口。

平台将 Kubernetes 状态、Prometheus Metrics、Alertmanager Alerts、Elasticsearch Logs 和 Tempo Traces 汇入可追溯的 Evidence Plane。Incident 协调 Investigation、Decision、Operation、Verification 与 ResolutionReport；Agent 只能使用有界工具提出建议，任何写操作仍由 Owner 对精确内容授权。

## Capabilities

- Local Owner：仅通过 `127.0.0.1` port-forward 访问，不提供公网或多用户模式。
- Provider-backed workspaces：展示真实 Provider 数据，并明确区分 unavailable、partial 与 stale 状态。
- Durable operations：Incident、Evidence、Agent、配置、任务、授权、Operation 和 Verification 持久化到 MySQL。
- Controlled changes：Operation Plan 不可变；执行前重验精确 Hash、Authorization、配置修订和 Provider precondition。
- Demonstration Scenario：在 `demo` Namespace 创建有界 workload、traffic 与 fault，完整覆盖 Observe -> Detect -> Investigate -> Decide -> Act -> Verify。
- Recoverable lifecycle：kind、Helm、MySQL、Provider、备份、恢复、重启和诊断由顶层 `make` 命令管理。

## Local Runtime

需要 Linux、Docker、kind、kubectl、Helm、Go、Node.js/npm、curl、jq、OpenSSL、Git 和 `rg`。

```bash
make local-up
```

默认入口为 `http://127.0.0.1:18080`。端口被占用时可以为当前命令指定另一个 loopback 端口：

```bash
CLOUDOPS_LOCAL_PORT=18081 make local-up
```

常用生命周期命令：

```bash
make local-status
make local-open
make local-logs COMPONENT=api
make local-restart
make local-doctor
make local-down
```

运行状态、凭据和备份位于 Git 忽略且权限受限的 `.cloudops/`。`local-down` 停止工作负载，但保留持久数据和备份。完整边界见 [Operations](docs/operations.md)。

## Demonstration Scenario

```bash
make scenario-up
make scenario-status
# 在浏览器完成 Observe -> Detect -> Investigate -> Decide -> Act -> Verify
make scenario-down
```

Scenario 使用唯一身份标记 Kubernetes runtime、Alert、Context Snapshot、Agent 与 Operation history。`scenario-down` 只删除 Scenario runtime，并关闭对应 write gate；普通 Live Mode 数据和可审计 history 不受影响。详见 [Demonstration Scenario](docs/demo.md)。

## Backup And Restore

```bash
make local-backup
make local-restore BACKUP=.cloudops/backups/<backup-id>
make local-reset
```

Restore 先导入隔离 staging database，核对 checksum、schema identity 和逐表 row count，再切换活动库。Reset 要求 backup-first 和显式确认。

## Runtime Components

| Component | Responsibility |
| --- | --- |
| `cloudops-api` | 静态 UI、`/api/v1` Query/Command、健康检查和内部 Alertmanager webhook listener |
| `cloudops-worker` | MySQL async task runtime、Provider read 和受授权约束的 allowlisted operation |
| `cloudops-migrate` | 应用 forward-only Goose migration 并验证 schema |
| `cloudops-demo` | Scenario workload、traffic 和 telemetry |
| MySQL | Domain、Evidence、task、configuration、decision、operation 与 verification truth |
| Helm | API、Worker、Migrate、MySQL、observability stack 和 Scenario 的唯一部署源 |

## Development

```bash
make test-go
make test-race
make test-frontend
make frontend-e2e-stable
make check-capability-matrix
make check
```

Fixture Playwright 验证确定性的前端交互合同；真实集成测试验证 browser -> `/api/v1` -> API/Worker -> MySQL/Provider -> refreshed UI。能力覆盖清单位于 `frontend/tests/real-integration/capabilities.json`。

## Documentation

- [Domain](docs/domain.md)：领域对象、权限边界与用户界面用语。
- [Architecture](docs/architecture.md)：运行拓扑、进程所有权和数据流。
- [API](docs/api.md)：V1 transport 与 mutation 合同。
- [Agent Runtime](docs/agent-runtime.md)：Agent、Evidence 和任务执行边界。
- [Operations](docs/operations.md)：本地生命周期、Scenario、备份与诊断。
- [Security](docs/security.md)：Local Owner、secret 和写入权限边界。
- [Reliability](docs/reliability.md)：持久化、fencing、恢复与失败行为。
- [Risk Register](docs/risk-register.md)：长期风险和控制措施。

## Repository Layout

```text
cmd/                    process entrypoints
internal/api/           public V1 transport contract
internal/asyncjob/      MySQL-backed task runtime
internal/bootstrap/     process composition and Provider boundaries
internal/infra/         Provider and persistence adapters
migrations/             forward-only semantic schema
frontend/               Vue application and browser tests
charts/cloudops/        deployable Helm chart
eval/                   versioned Agent evaluation datasets
runbooks/               bounded investigation guidance
scripts/                lifecycle and contract checks
docs/                   product and operator documentation
```
