# Risk Register

本表只记录当前语义产品与未闭合风险。`CONTROLLED` 限定到明确证据边界；`OPEN` 与 `NOT RUN` 不能被局部测试覆盖。

| ID | Severity | Risk | Current control / owner | Status |
|---|---|---|---|---|
| ARCH-001 | CRITICAL | 平行 API/runtime/schema/deploy path 再次出现 | 唯一 `/api/v1`、root module、forward-only schema、`charts/cloudops`、semantic naming check | CONTROLLED locally |
| ARCH-002 | HIGH | 文档引用已删除实现或把历史能力写成当前事实 | current-state docs + Phase 9 report；历史报告只保留 exact-run provenance | CONTROLLED for Phase 9 convergence |
| DATA-001 | CRITICAL | schema/restore 丢失 Incident/Evidence 或破坏 hash/provenance | private backup、staging restore、count/FK/hash/provenance audit、rollback | CONTROLLED locally |
| DATA-002 | HIGH | migration 或 runtime mutation 破坏单一 truth | Migrate-only forward migration、API/Worker no AutoMigrate、schema tests | CONTROLLED in current tree; future drift remains OPEN |
| LIFE-001 | CRITICAL | lifecycle 命令误操作其他集群或持久数据 | fixed target、broad override rejection、backup-first reset | CONTROLLED locally |
| LIFE-002 | MEDIUM | port-forward 在 rollout/command return 后漂移 | status/doctor 单独检测；允许对 canonical Service 建立前台 loopback | CONTROLLED operationally; lifecycle robustness remains OPEN |
| SECRET-001 | CRITICAL | DB/Provider secret 进入 Git、UI、日志或 Evidence | `.cloudops/` permissions、workload-scoped Secret、write-only Settings API、no-value evidence | CONTROLLED locally; rotation remains OPEN |
| PROVIDER-001 | CRITICAL | unavailable Provider 产生假成功 | explicit partial/stale/unavailable、bounded query、Provider identity、no fixture fallback | CONTROLLED for local Providers |
| PROVIDER-002 | CRITICAL | Kubernetes/GitHub/Argo/LLM 越权写入 | three-layer authority、exact hash、effect-time revalidation、Scenario-only RBAC/write gate | CONTROLLED for local Scenario; external writes NOT RUN |
| API-001 | HIGH | Local Owner 无 auth 被错误暴露到 LAN/Internet | only supported access is `127.0.0.1` port-forward | CONTROLLED for supported mode |
| API-002 | HIGH | browser mutation 被跨源/replay/stale request 触发 | Origin、idempotency、expected version/hash、body bounds | CONTROLLED in current contracts |
| UI-001 | HIGH | fixture/截图冒充 UI/API/Provider integration | current browser/network/data/provider/console evidence required | CONTROLLED for Phase 9 Scenario |
| UI-002 | MEDIUM | 大数据/移动端/Canvas/离线状态退化 | virtual lists、bounded scrollers、320–1440 matrix、structured fallback、offline retry evidence | CONTROLLED for tested matrix |
| RELEASE-001 | HIGH | 普通源码 push 意外写 Registry/部署 | CI read-only/build-only；publish workflows explicit dispatch | CONTROLLED for branch push |
| RELEASE-002 | HIGH | hosted scan/sign/attest/cleanup 被本地静态检查冒充 | exact-SHA hosted evidence kept separate | NOT RUN |
| SCENARIO-001 | HIGH | Scenario runtime 清理不完整或污染 Live Mode | `scenario-down` asserts runtime 0/write gate false/RBAC no/history retained；browser Live Mode check | CONTROLLED for current run |
| EXTERNAL-001 | HIGH | 本地 Kubernetes recovery 被扩大为 GitHub/Argo/staging/production PASS | final report keeps every external branch explicit `NOT RUN` | NOT RUN |

逐项证据与 blocker 见 [实施状态](evidence/cloudops-implementation-status.md) 和 [Phase 9 最终证据](evidence/phase-9-scenario/final-evidence-report.md)。
