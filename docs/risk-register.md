# Risk Register

本表只记录当前语义产品和未闭合风险。`CONTROLLED` 限定到明确证据边界；`OPEN` 与 `NOT RUN` 不能被局部测试覆盖。

| ID | Severity | Risk | Current control / owner | Status |
|---|---|---|---|---|
| ARCH-001 | CRITICAL | 平行 API/runtime/schema/deploy path 再次出现 | 唯一 `/api/v1`、root module、semantic baseline、`charts/cloudops`、`make check-naming` | CONTROLLED for Task 0 source/runtime |
| ARCH-002 | HIGH | 主动文档引用已删除实现或把未来能力写成完成 | 当前 README/architecture/API/security/reliability/demo/status 以实施规范和 live tree 为准 | CONTROLLED for rewritten docs |
| DATA-001 | CRITICAL | schema 转换丢失 Incident/Evidence 或破坏 hash/provenance | private backup、隔离 import、count/FK/hash/provenance audit、rollback restore | CONTROLLED for current local dataset |
| DATA-002 | HIGH | 未来 migration 重新引入平行 chain 或 runtime mutation | one semantic baseline；Migrate-only DDL；API/Worker tests/Chart boundaries | OPEN across future tasks |
| LIFE-001 | CRITICAL | lifecycle 命令误操作其他集群或持久数据 | 固定 cluster/context/namespace/release、broad-path rejection、backup-first reset | CONTROLLED locally |
| LIFE-002 | HIGH | port-forward 与无关进程端口冲突 | loopback-only、端口检测、`CLOUDOPS_LOCAL_PORT` override | CONTROLLED; operator selects free port |
| SECRET-001 | CRITICAL | DB/Provider/backup secret 进入 Git、UI、日志或 evidence | `.cloudops/` private permissions、workload-scoped Secret、no response/log contract | CONTROLLED for Task 0; rotation remains future work |
| PROVIDER-001 | CRITICAL | 未配置 Provider 的任务被领取或产生假成功 | default standby runner；Provider Gateway validates complete config before claim | CONTROLLED in local default; live providers NOT RUN |
| PROVIDER-002 | CRITICAL | Kubernetes/GitHub/Argo/LLM 越权写入 | K8s write disabled/read RBAC；write identity/approval/hash/allowlist boundaries | source controls present; live negatives NOT RUN |
| API-001 | HIGH | Local Owner 无 auth 被错误暴露到 LAN/Internet | only supported access is `127.0.0.1` port-forward；remote mode requires new ADR | CONTROLLED for managed local entry |
| API-002 | HIGH | browser mutation 被跨源/replay/stale request 触发 | Origin、idempotency、expected version/hash、body bounds | source/runtime checks required per task |
| UI-001 | HIGH | fixture 或静态截图冒充真实 UI/API/Provider 集成 | status requires Browser/Network/Data/Provider/Console evidence after each task | OPEN until each task MCP run |
| RELEASE-001 | HIGH | 普通源码 push 意外写 Registry 或部署 | CI read-only/build-only；publish workflows manual dispatch only | CONTROLLED for branch push |
| RELEASE-002 | HIGH | hosted scan/sign/attest/cleanup 被本地静态检查冒充 | hosted workflow evidence stays distinct and exact-SHA | NOT RUN |
| SCENARIO-001 | HIGH | 旧 Golden/Compose flow 被当作当前完整 Scenario | current demo states Task 9 commands absent；history is non-normative | OPEN, Task 9 |

逐任务 evidence 和 blocker 见 [实施状态](evidence/cloudops-implementation-status.md)。风险状态必须随实际实现和验证更新，不能仅因代码存在改为 `CONTROLLED`。
