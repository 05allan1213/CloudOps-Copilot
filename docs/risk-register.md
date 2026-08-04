# Risk Register

本表记录需要长期维护的产品风险。控制措施描述当前设计约束，不代表任意环境或未来变更自动满足这些约束。

| ID | Severity | Risk | Control |
| --- | --- | --- | --- |
| ARCH-001 | Critical | 出现平行 API、runtime、schema 或 deploy path | 唯一 `/api/v1`、root Go module、forward-only schema、`charts/cloudops` 和 semantic naming check |
| ARCH-002 | High | 文档、OpenAPI、runtime route 与 typed client 漂移 | 保留少量 current-state docs，并由 capability matrix、contract tests 和 link checks 联合验证 |
| DATA-001 | Critical | Schema、restore 或 retention 操作丢失 Domain/Evidence 数据 | Private checksummed backup、staging restore、row-count/schema validation 和 rollback |
| DATA-002 | High | 多进程修改 schema 或破坏 durable truth | Migrate-only forward migration；API/Worker 禁止 AutoMigrate；schema contract tests |
| LIFE-001 | Critical | Lifecycle 命令误操作其他集群或持久数据 | 固定 cluster/Namespace/release target，拒绝 broad override，reset 要求 backup-first 和确认 |
| LIFE-002 | Medium | Port-forward 在 rollout 或命令返回后失效 | Status/doctor 分层诊断；允许对 canonical Service 重建 loopback forward |
| SECRET-001 | Critical | DB/Provider secret 进入 Git、UI、日志、Evidence 或备份元数据 | `.cloudops/` 权限、workload-scoped Secret、write-only Settings API、redaction 和 no-value projections |
| PROVIDER-001 | Critical | Provider unavailable 被表现为成功 | 显式 partial/stale/unavailable、bounded queries、source identity 和禁止 fixture fallback |
| PROVIDER-002 | Critical | Kubernetes、GitHub、Argo 或 LLM 越权写入 | 分离 read/suggestion/effect authority，绑定 exact hash，并在 effect time 重验 precondition 和 write gate |
| API-001 | High | 无登录的 Local Owner 应用被暴露到 LAN 或 Internet | 仅支持 `127.0.0.1` port-forward；公开或多用户模式需要新的认证和架构设计 |
| API-002 | High | Browser mutation 被跨源、重放或 stale request 触发 | Origin、idempotency、expected version/hash、body bounds 和 stable Problem Details |
| UI-001 | High | Fixture 或静态页面检查被误认为真实集成 | 将 fixture tests 与 browser -> API -> MySQL/Provider -> refreshed UI tests 分开 |
| UI-002 | Medium | 大数据、移动端、Canvas 或离线状态退化 | Virtualization、bounded scrollers、responsive constraints、structured fallback 和 retry states |
| RELEASE-001 | High | 普通源码 push 意外发布镜像或部署 | CI build-only；Golden publish 使用独立的显式 dispatch workflow |
| RELEASE-002 | High | 镜像来源、漏洞扫描或不可变身份校验失效 | Exact source revision labels、digest validation、pre-publish scan 和 pinned workflow dependencies |
| SCENARIO-001 | High | Scenario runtime 清理不完整或污染 Live Mode | `scenario-down` 验证 runtime 为零、write gate 关闭、RBAC 移除且 history 保留 |
| EXTERNAL-001 | High | 本地恢复结果被扩大为外部 Provider 或生产成功 | 每个外部 effect 都需要独立 credential、Plan、Authorization、execution 和 verification |

风险控制相关变更应补充相应 code、contract 或 integration test；一次运行生成的截图、日志和报告不作为仓库内长期状态。
