# Domain

CloudOps-Copilot 使用一套统一语言描述云原生观测、事件响应、Agent 调查和受控变更。公开 API、前端文案、持久化对象与操作文档应使用这里的语义。

## Product Boundary

| Term | Meaning |
| --- | --- |
| CloudOps Operations Platform | 统一承载运维可见性、事件响应、Agent 调查、受控变更和平台配置的产品。Incident 只是其中一个领域。 |
| Local Owner | 通过本机 loopback 使用全部 Workspace 的唯一 Owner。该身份不代表任何 Provider credential。 |
| Workspace | 具有独立 route、上下文和操作的一级产品区域。 |
| Operational Scope | 当前 cluster、environment、Namespace 集和 time range 组成的查询边界。 |
| Context Link | 携带精确资源身份和绝对时间窗口的内部导航关系，或指向 allowlisted Provider 资源的受控链接。 |
| Provider-backed Capability | 由 CloudOps 呈现和治理，但事实或外部效果仍由 Kubernetes、Prometheus、Alertmanager、Elasticsearch、Tempo 等 Provider 所有的能力。 |
| Live Mode | 所有资源、观测、Alert、Evidence 和 Agent 结果均来自配置的真实 Provider 与 CloudOps domain record。 |
| Demonstration Scenario | 显式启用的项目自有 workload 和受控 fault；它产生真实 Provider telemetry，但不冒充普通 Live Mode 数据。 |

## Signals And Response

| Term | Meaning |
| --- | --- |
| Signal | Provider 接收的不可变观测。Signal 是输入事实，不是用户管理的 Alert 或 Incident 生命周期。 |
| Alert | 可被 acknowledge、silence、investigate、correlate 或 resolve 的时限性运维条件。 |
| Alert Acknowledgement | Owner 已知悉 Alert 的持久记录；它不抑制通知，也不表示恢复。 |
| Alert Silence | 对匹配 Alert 的限时 Provider-backed 通知抑制；它不改变 firing/resolved 事实。 |
| Incident | 协调 investigation、remediation 和 recovery verification 的响应案例，可关联多个 Alert。 |
| Incident Cycle | Incident 内一次隔离的响应尝试，只包含可归因于该次尝试的 Alert、调查、决策和恢复证明。 |
| Recovery Verification | 所有必需观测在同一稳定窗口内持续通过后，对 Incident 恢复作出的权威判断。 |
| ResolutionReport | 绑定一个 Incident Cycle、最终 Recovery Verification 和可归因 history 的不可变恢复证明。 |
| Owner Notification | 需要 Owner 关注的 Alert 状态、Agent 结果、授权请求或 Operation 结果的持久且去重通知。 |

Alert、Incident 和 Notification 是不同对象。收到 Signal 不会自动创建 Incident；acknowledge、silence、resolve 和 close 也不能互相替代。

## Evidence And Agent

| Term | Meaning |
| --- | --- |
| Evidence | 有界、经过清理且可归因的观测，包含 source identity、source/collection time、查询上下文、schema/content hash 与 provenance。 |
| Cloud-Native Evidence Plane | 按资源身份和时间关联 Kubernetes、Metrics、Alert、Logs 与 Traces 的 Provider-backed 事实集合。 |
| Agent Investigation | 由 Alert 或 Incident 触发的持久 Agent run，包含有界 steps、tool calls、Evidence 和诊断结果。 |
| Agent Consultation | Owner 发起并持久化的诊断对话，只使用显式 Context Snapshot 和授权的只读工具。 |
| Context Snapshot | 在确定时刻绑定 Operational Scope、资源、时间窗、Query Definition、Evidence 引用和 Configuration Revision 的不可变上下文。 |
| Knowledge Item | Owner 确认、版本化且有 scope 的可复用经验；它不能证明系统当前状态。 |
| Runbook Guidance | Git 管理的有界操作指导；它可以指导调查，但不是 Evidence 或执行权限。 |

模型输出不是 Evidence。Agent 没有 Provider identity、Owner authority 或外部写权限，也不能通过文本、URL、Context Link 或工具结果获得这些权限。

## Queries And Operations

| Term | Meaning |
| --- | --- |
| Query Definition | 版本化、有界的只读 Provider 查询。 |
| Query Authorization | Owner 对一次精确查询或一个固定 Query Definition 版本授予的可撤销权限。 |
| Query Execution | 绑定 actor、Provider、Scope、Configuration Revision、time range 和资源上限的审计执行。 |
| Operation Plan | 绑定 target、parameters、diff、preconditions、risk、expiry 和 verification intent 的不可变外部效果提案。 |
| Action Authorization | Owner 对一个精确 Action Card 或 Operation Plan material hash 的许可。任何 material change 都需要新授权。 |
| Operation Execution | 对一个未过期、已授权且 precondition 仍成立的精确操作进行的审计尝试。 |
| Operation Verification | 使用当前 post-effect observation 判断 Operation 是否达到目标；它不能替代 Incident Recovery Verification。 |
| Change Freeze | Owner 对精确 Workload target 设置的可逆变更阻止状态。 |
| Configuration Revision | Operational Configuration 的不可变版本；进行中的工作继续绑定其原始 Revision。 |

## User Interface Language

- 中文用于导航、命令、标签、校验、空状态、错误和恢复动作。
- 保留 Kubernetes Kind、协议、query language、source field、command、identifier 和原始 Provider 文本。
- 使用 `CloudOps` 或 `CloudOps-Copilot`，不引入代际名称或只围绕 Incident 的品牌。
- 使用 Agent、Incident、Alert、Evidence、Provider、Scope、Operations Atlas、Context Link、Revision 等规范词。
- 状态文案必须区分 `accepted`、`dispatched`、`observed`、`verified`、`partial`、`stale` 和 `unavailable`。
- 审计、Authorization、Operation、Verification 和 Revision History 展示精确 UTC 时间。
- 命令文案描述真实效果；确认对话框必须标明 target、effect、authority、exact hash/version、不可逆后果和恢复限制。
- API 错误按失败事项、影响范围、精确身份、request/trace ID 和下一步操作呈现。
- 图标使用 Lucide，不使用 emoji 充当控件或状态标记。
- 不向 UI、日志、Evidence 或文档暴露 secret、credential、原始敏感配置或隐藏推理过程。
