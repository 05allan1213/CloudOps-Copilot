# Demonstration Scenario

当前可重复演示的是同一 canonical CloudOps release 中的真实 bounded Scenario，不是 Compose、fixture 或旧 Golden harness。

## 1. Start and inspect

```bash
make local-up
make scenario-up
make scenario-status
make local-open
```

`scenario-status` 只有在 Kubernetes workload/traffic/fault、Prometheus sample、Alertmanager Alert、Elasticsearch Logs、Tempo Traces 与至少 1 个 Agent run 可见时才返回对应 `PASS`。Scenario identity 在 shell、Scope、Context Snapshot、Evidence 与 operation target 中保持一致。

## 2. Browser flow

在浏览器完成：

```text
Overview degradation
  -> Alert detail
  -> related Logs and exact Trace
  -> Agent Investigation with provider Evidence
  -> immutable Operation Plan
  -> Owner review and exact Authorization
  -> allowlisted recovery action
  -> current Metrics / Alert / Kubernetes / Trace Verify
  -> ResolutionReport and retained history
```

DevOps Workspace 可展示 optional delivery branch，但 GitHub/Argo 不是核心恢复前置。未配置的外部分支显示 `NOT RUN`。

## 3. Stop without deleting history

```bash
make scenario-down
make scenario-status
```

成功结果必须包含：

```text
scenario_state=inactive
scenario_write_gate=false
scenario_runtime_resources=0
scenario_stale_firing_alerts=0
```

浏览器随后回到 Live Mode，不显示 `Scenario Active` 或 `cloudops-scenario-*` runtime；retained Incident/Alert/Investigation/Plan/Verification history 仍可审计。

## 4. Evidence boundary

当前验收对象为 Scenario `scenario-20260728045922-4c81122b`；对象 ID、视口、截图、console/network、性能、accessibility 与 cleanup 结果见 [Phase 9 最终证据](evidence/phase-9-scenario/final-evidence-report.md)。

GitHub App write、human merge、Argo exact revision、hosted Actions、Registry publish/sign/attest、staging 与 production 均未由该 Scenario 隐式执行，保持 `NOT RUN`。
