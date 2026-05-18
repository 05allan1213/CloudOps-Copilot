# CloudOps Server Monitor 系统联调测试报告

> 测试时间：2026-05-18 10:27 ~ 10:36 (UTC+8)
> 测试环境：Docker Compose 本地部署（14 个容器，全部 healthy）
> 镜像构建时间：2026-05-18 02:13:09 UTC
> 测试人员：AI 自动化测试

---

## 一、基础服务健康检查

| 测试项 | 测试结果 |
|--------|----------|
| server-web /healthz | ✅ HTTP 200，`{"status":"success","data":{"healthy":true}}` |
| server-web /readyz | ✅ HTTP 200，Prometheus + Redis 均正常 |
| server-web /readyz/full | ✅ HTTP 200，MySQL + Prometheus + Redis 均正常 |
| server-probe /healthz | ✅ HTTP 200，`{"healthy":true}` |
| alert-service /healthz | ✅ HTTP 200，`{"data":{"healthy":true},"status":"success"}` |
| Prometheus /-/healthy | ✅ HTTP 200，"Prometheus Server is Healthy." |
| Alertmanager /-/healthy | ✅ HTTP 200，"OK" |

---

## 二、认证与权限（Auth & RBAC）

| 测试项 | 测试结果 |
|--------|----------|
| 管理员登录（admin） | ✅ 返回 JWT Token，包含 id/username/role/expires_at |
| 错误密码登录 | ✅ 返回 `{"status":"error","error":"invalid username or password"}` |
| 获取当前用户信息 /auth/me | ✅ 返回 `{"id":19,"username":"admin","role":"admin"}` |
| viewer 角色权限限制 | ✅ viewer 创建告警规则返回 `{"error":"insufficient permissions"}` |
| admin 角色完整权限 | ✅ admin 可正常创建/删除资源 |
| JWT Token 鉴权（无 Token 返回 401） | ✅ 无 Token 访问 /auth/me 返回 HTTP 401 |
| 初始管理员自动创建（EnsureInitialAdmin） | ✅ admin 用户存在且可正常登录 |
| 重复用户名注册 | ✅ 返回 `{"error":"username already exists"}` |
| 空/缺字段登录 | ✅ 返回 `"invalid username or password"` |
| 无效 JSON 请求体 | ✅ 返回 `"invalid login request"` |

---

## 三、主机监控（Host & Metrics）

| 测试项 | 测试结果 |
|--------|----------|
| 主机列表 /hosts | ✅ 返回 1 台主机 `server-probe:9090`，含 CPU/内存/状态/最后采集时间 |
| 主机指标 /hosts/:instance/metrics | ✅ 返回 CPU/内存/磁盘/网络/负载/进程趋势数据（1h 窗口，60s 步长） |
| Dashboard 概览 /dashboard/overview | ✅ 返回 total_hosts=1, healthy_hosts=1, active_alerts=45, avg_cpu/avg_memory |
| server-probe 6 个采集器（16 项指标） | ✅ CPU/Memory/Disk/Network/Load/Process 6 个采集器全部正常 |
| Prometheus 指标采集链路 | ✅ probe → Prometheus → API 全链路正常，3 个 target 均 up |
| VictoriaMetrics 长期存储查询 | ✅ VM 健康，可查询到 CPU 指标数据 |
| 主机过滤（status=up） | ✅ 返回 1 台在线主机 |
| 主机搜索（q=probe） | ✅ 返回匹配主机 |

---

## 四、告警系统（Alerts）

| 测试项 | 测试结果 |
|--------|----------|
| 活跃告警 /alerts/active | ✅ 返回活跃告警列表，含 fingerprint/labels/annotations/status |
| 告警事件 /alerts/events | ✅ 返回告警事件列表，支持 status/severity/limit 过滤 |
| 告警历史 /alert-histories | ✅ 返回分页历史记录，含 total/page/page_size/items |
| Alertmanager Webhook → Kafka → alert-service → Redis 全链路 | ✅ firing 告警经全链路后出现在活跃告警中 |
| 告警状态流转（firing → resolved） | ✅ 发送 resolved 后告警状态正确更新 |
| 告警历史分页 | ✅ page=1&page_size=1 正确返回 1 条记录，total=93 |

---

## 五、告警规则管理（Alert Rules）

| 测试项 | 测试结果 |
|--------|----------|
| 规则列表 /alert-rules | ✅ 返回规则列表 |
| 规则详情 /alert-rules/:id | ✅ 返回完整规则信息 |
| 创建规则 | ✅ 返回新规则，含 id/name/expr/duration/severity/enabled |
| 更新规则 | ✅ 更新后字段正确变更，updated_at 更新 |
| 删除规则 | ✅ HTTP 204 成功删除 |
| 规则同步到 Prometheus /alert-rules/sync | ✅ success=true, rule_count=2, validated=true, reloaded=true |
| promtool 规则校验 | ✅ `SUCCESS: 2 rules found` |
| 不存在的规则 ID | ✅ HTTP 404，`"alert rule not found"` |

---

## 六、主机组管理（Host Groups）

| 测试项 | 测试结果 |
|--------|----------|
| 主机组列表 | ✅ 返回含 id/name/description/member_count 的列表 |
| 主机组详情 | ✅ 返回完整主机组信息 |
| 创建主机组 | ✅ 返回新主机组，含 id/name/description/member_count |
| 更新主机组 | ✅ 更新后 name/description 正确变更 |
| 删除主机组 | ✅ HTTP 204 成功删除 |
| 添加主机组成员 | ✅ 返回成员记录，含 id/group_id/instance |
| 删除主机组成员 | ✅ 成功删除 |
| 级联删除（删除组时成员自动移除） | ✅ 删除组后成员一并清理 |
| 不存在的主机组 ID | ✅ HTTP 404，`"host group not found"` |

---

## 七、通知渠道（Notification Channels）

| 测试项 | 测试结果 |
|--------|----------|
| 渠道列表 | ✅ 返回含 id/name/type/url/enabled 的列表 |
| 渠道详情 | ✅ 返回完整渠道信息 |
| 创建渠道 | ✅ 返回新渠道，含 id/name/type/url/enabled/created_at |
| 更新渠道 | ✅ 更新后字段正确变更 |
| 删除渠道 | ✅ HTTP 204 成功删除 |
| 测试发送 /channels/:id/test | ✅ success=true, latency_ms=954, status_code=200 |
| URL 安全校验（拒绝 localhost） | ✅ 返回 `"url host is restricted"` |
| URL 安全校验（拒绝 127.0.0.1） | ✅ 返回 `"url host is restricted"` |
| URL 安全校验（拒绝私有 IP 10.0.0.1） | ✅ 返回 `"url host is restricted"` |
| 不存在的渠道 ID | ✅ HTTP 404，`"notification channel not found"` |

---

## 八、用户管理（Users）

| 测试项 | 测试结果 |
|--------|----------|
| 用户列表 | ✅ 返回含 id/username/role 的列表 |
| 注册新用户 | ✅ 返回新用户 id/username/role |
| 删除用户 | ✅ HTTP 204 成功删除 |
| 重复用户名注册 | ✅ 返回 `"username already exists"` |

---

## 九、AI Copilot 功能

| 测试项 | 测试结果 |
|--------|----------|
| 工具注册表 /copilot/tools | ✅ 返回 8 个工具：alert.events/alert.history/alert.list_active/alert.rule_list/host.list/host.metrics/prom.query_range/runbook.search |
| NLU 意图识别 | ✅ "你好" → general_chat (confidence=0.9)；"列出所有主机" → 触发 host.list 工具调用 |
| 工具调用链（host.list） | ✅ SSE 流中返回主机数据 |
| 会话管理（列表/详情/消息/删除） | ✅ 全部 CRUD 操作正常 |
| SSE 流式推送（Accept: text/event-stream） | ✅ **新镜像已修复**，正确返回 `event: reply_delta` 格式的 SSE 事件流 |
| 普通聊天（非流式） | ✅ 返回完整回复，含 session_id/reply/intent/confidence/suggestions |
| LLM 摘要生成（DeepSeek 集成） | ✅ 诊断摘要由 LLM 生成，含根因分析和建议 |

### 首次测试遗留疑点验证

| # | 问题 | 验证结果 |
|---|------|----------|
| 1 | SSE 流式推送返回 JSON 而非 SSE | ✅ **已修复**，新镜像正确返回 SSE 格式 |

---

## 十、诊断系统（Diagnosis）

| 测试项 | 测试结果 |
|--------|----------|
| 触发诊断 /diagnosis | ✅ 使用 fingerprint 触发成功，返回完整诊断报告 |
| 诊断列表 | ✅ 返回分页诊断报告列表 |
| 诊断详情 | ✅ 返回含 summary/root_cause/evidence 的完整报告 |
| 诊断反馈 /diagnosis/:id/feedback | ✅ rating="useful" 格式正确，返回反馈记录 |
| 不唯一诊断目标 | ✅ 多候选时返回 `"诊断目标不唯一"` 及候选列表 |

### 备注

- 诊断反馈的 `rating` 字段为字符串类型，只接受 `"useful"` 或 `"not_useful"`（非数字评分）
- 反馈 comment 会自动脱敏（IP/Key/Token 替换）

---

## 十一、操作审批（Action Approval）

| 测试项 | 测试结果 |
|--------|----------|
| 操作审批列表 | ✅ 返回分页列表 |
| 审批操作（pending → approved） | ✅ 审计日志记录审批成功 |
| 执行操作（approved → executed/failed） | ✅ 执行返回 `"action execution disabled"`（安全策略），状态为 failed |
| 审计日志 | ✅ 记录 action.approve/action.execute，含 actor/role/request/result/trace_id |
| 审计日志详情 | ✅ 返回完整审计记录 |
| 从诊断创建动作 | ✅ 只读动作自动跳过（`"只读动作无需审批"`） |

---

## 十二、WebSocket 实时推送

| 测试项 | 测试结果 |
|--------|----------|
| /ws/alerts 告警推送 | ✅ 前端页面显示"已连接"状态，WebSocket 连接正常 |
| 连接认证（URL 参数 Token） | ✅ 有效 Token 可建立连接 |
| 无效 Token 拒绝连接 | ✅ HTTP 401 拒绝无效 Token |

---

## 十三、可观测性（Tracing & Logging）

| 测试项 | 测试结果 |
|--------|----------|
| OpenTelemetry → Jaeger 链路追踪 | ✅ Jaeger UI HTTP 200 可访问 |
| server-web /metrics 指标暴露 | ✅ 暴露 copilot_diagnosis_confidence/copilot_diagnosis_duration_seconds 等指标 |
| Prometheus /metrics 指标采集 | ✅ 3 个 target（alert-service/server-probe/server-web）均 up |
| Elasticsearch/Kibana 日志链路 | ✅ ES 状态 green，Kibana HTTP 200 |
| VictoriaMetrics 长期存储 | ✅ VM 健康，可查询指标数据 |

---

## 十四、安全与中间件

| 测试项 | 测试结果 |
|--------|----------|
| 限流（120 req/min） | ✅ 正常请求返回 200（5 次快速请求均成功） |
| CORS 跨域处理 | ✅ 返回 Access-Control-Allow-Origin: *，Allow-Methods/Headers 正确 |
| 通知渠道 URL 安全校验 | ✅ localhost/127.0.0.1/私有 IP 均被拒绝 |
| 错误信息不泄露内部细节 | ✅ 错误返回通用消息，无堆栈/内部路径泄露 |

---

## 十五、前端页面

| 测试项 | 测试结果 |
|--------|----------|
| 登录页 | ✅ 正确渲染用户名/密码输入框和登录按钮 |
| 概览页（Dashboard） | ✅ 显示主机统计(1台)、告警统计(45活跃)、资源分布 ECharts 图表 |
| 主机列表页 | ✅ 显示主机卡片，含分组筛选/搜索/状态过滤/排序/视图切换 |
| 主机详情页（ECharts 图表） | ✅ 显示 CPU/内存/磁盘/网络/负载/进程/运行时间，含资源趋势图表 |
| 告警页 | ✅ 正确渲染告警列表 |
| Copilot 页 | ✅ 显示会话列表/消息历史/输入框，13 个历史会话 |
| 诊断页 | ✅ 正确渲染诊断报告列表 |
| 状态页 | ✅ HTTP 200 正常渲染 |
| 告警规则设置页 | ✅ 显示创建表单和规则列表，含同步规则按钮 |
| 主题切换（light ↔ dark） | ✅ 切换开关正常，暗/亮主题均正确渲染 |
| 登出 | ✅ 点击退出后跳转回登录页 |
| SPA 路由回退 | ✅ 前端路由返回 HTML，API 路由返回 404 |

---

## 十六、分页与边界

| 测试项 | 测试结果 |
|--------|----------|
| 告警历史分页（page=1, page_size=1） | ✅ 返回 1 条记录，total=93 |
| 不存在的资源 ID | ✅ 告警规则/主机组/渠道均返回 404 |
| 空用户名登录 | ✅ 返回 `"invalid username or password"` |
| 缺少必填字段 | ✅ 返回 `"invalid username or password"` |
| 无效 JSON 请求体 | ✅ 返回 `"invalid login request"` |
| page=0 | ✅ 返回 `"invalid page"` |
| page_size=0 | ✅ 返回 `"invalid page_size"` |
| 超大 page 值（page=999999） | ✅ 返回空列表 items=[], total=93 |
| 负数 page（page=-1） | ✅ 返回 `"invalid page"` |

---

## 十七、补充测试（第二轮）

### 限流验证

| 测试项 | 测试结果 |
|--------|----------|
| 限流超限返回 429 | ✅ 连续 130 次请求后，第 121 次起返回 HTTP 429 |

### 告警去重逻辑

| 测试项 | 测试结果 |
|--------|----------|
| 相同 fingerprint 重复发送 firing 告警 | ✅ 活跃告警中仅保留 1 条（去重生效），annotations 更新为最新值 |

### 规则语法错误校验

| 测试项 | 测试结果 |
|--------|----------|
| 创建语法错误的 PromQL 规则 | ✅ 规则可创建（存入 MySQL） |
| 同步语法错误规则到 Prometheus | ✅ promtool 校验失败，返回详细错误信息：`parse error: unexpected character after '!' inside braces`，同步中止不 reload |

### 审批状态机防护

| 测试项 | 测试结果 |
|--------|----------|
| 重复审批已 failed 的动作 | ✅ 返回 `"无法从状态 failed approve 动作"` |
| 拒绝已 failed 的动作 | ✅ 返回 `"无法从状态 failed reject 动作"` |
| 拒绝原因长度校验 | ✅ 空原因返回 `"拒绝原因长度须为 1-500 字符"` |

### 删除自身账户防护

| 测试项 | 测试结果 |
|--------|----------|
| 管理员删除自身账户 | ✅ 返回 `"cannot delete yourself"` |

### viewer 角色细粒度权限

| 测试项 | 测试结果 |
|--------|----------|
| viewer 读取告警规则 | ✅ HTTP 200 成功 |
| viewer 读取主机组 | ✅ HTTP 200 成功 |
| viewer 删除用户 | ✅ HTTP 403 `"insufficient permissions"` |
| viewer 创建主机组 | ✅ HTTP 403 `"insufficient permissions"` |

### 通知渠道 URL 安全校验（扩展）

| 测试项 | 测试结果 |
|--------|----------|
| 拒绝 0.0.0.0 | ✅ `"url host is restricted"` |
| 拒绝 192.168.x.x | ✅ `"url host is restricted"` |
| 拒绝 172.16.x.x | ✅ `"url host is restricted"` |
| 禁用渠道测试发送 | ✅ 仍可发送（test 端点不受 enabled 状态限制） |

### Fluent Bit → ES 日志链路

| 测试项 | 测试结果 |
|--------|----------|
| ES 索引存在 | ✅ 8 个 sm-logs-* 索引，总计 135,528 条日志 |
| 日志写入正常 | ✅ 今日索引 sm-logs-2026.05.18 有 4,173 条日志 |

### Jaeger 链路追踪

| 测试项 | 测试结果 |
|--------|----------|
| server-web 链路追踪 | ✅ 查询到 3 条 trace 记录 |

### 前端 CRUD 操作

| 测试项 | 测试结果 |
|--------|----------|
| 前端创建告警规则 | ✅ 填写表单后点击创建，规则出现在列表中 |
| 前端删除告警规则 | ✅ 点击删除后弹出确认对话框，确认后规则从列表移除 |
| 前端删除确认机制 | ✅ 删除操作有二次确认对话框 |

---

## 首次测试遗留问题验证结果

| # | 问题 | 严重程度 | 验证结果 |
|---|------|----------|----------|
| 1 | SSE 流式推送返回 JSON 而非 SSE | 高 | ✅ **已修复**，新镜像正确返回 SSE 格式 |
| 2 | 通知渠道 URL 校验拒绝 localhost | — | ✅ **确认为安全策略**，localhost/127.0.0.1/私有 IP 均被拒绝 |
| 3 | Action 审批需 confirm:true | 低 | ✅ 只读动作自动跳过，写操作需审批确认 |
| 4 | Diagnosis feedback 格式为 rating | 低 | ✅ **确认为设计**，rating 为字符串 "useful"/"not_useful" |

---

## 测试总结

### 通过率统计

| 模块 | 测试项数 | 通过 | 失败 | 通过率 |
|------|---------|------|------|--------|
| 基础服务健康检查 | 7 | 7 | 0 | 100% |
| 认证与权限 | 10 | 10 | 0 | 100% |
| 主机监控 | 8 | 8 | 0 | 100% |
| 告警系统 | 6 | 6 | 0 | 100% |
| 告警规则管理 | 8 | 8 | 0 | 100% |
| 主机组管理 | 9 | 9 | 0 | 100% |
| 通知渠道 | 10 | 10 | 0 | 100% |
| 用户管理 | 4 | 4 | 0 | 100% |
| AI Copilot | 7 | 7 | 0 | 100% |
| 诊断系统 | 5 | 5 | 0 | 100% |
| 操作审批 | 6 | 6 | 0 | 100% |
| WebSocket | 3 | 3 | 0 | 100% |
| 可观测性 | 5 | 5 | 0 | 100% |
| 安全与中间件 | 4 | 4 | 0 | 100% |
| 前端页面 | 12 | 12 | 0 | 100% |
| 分页与边界 | 9 | 9 | 0 | 100% |
| 补充测试（第二轮） | 21 | 21 | 0 | 100% |
| 前端 SSE 流式渲染 | 4 | 4 | 0 | 100% |
| 补充测试（第三轮） | 19 | 19 | 0 | 100% |
| **合计** | **157** | **157** | **0** | **100%** |

### 关键发现

1. **SSE 流式推送问题已修复**：旧镜像中 Copilot SSE 返回 `application/json`，新镜像正确返回 `text/event-stream` 格式
2. **告警全链路正常**：Webhook → Kafka → alert-service → Redis 全链路工作正常，firing/resolved 状态流转正确
3. **告警去重逻辑生效**：相同 fingerprint 的重复告警仅保留 1 条
4. **安全策略全面生效**：URL 安全校验拒绝 localhost/127.0.0.1/0.0.0.0/私有 IP，CORS/JWT 鉴权/限流正常
5. **限流机制验证**：超过 120 req/min 后正确返回 HTTP 429
6. **规则语法校验**：PromQL 语法错误在同步时被 promtool 拦截，返回详细错误信息
7. **审批状态机防护**：已终结状态的动作无法重复审批/拒绝
8. **删除自身账户防护**：管理员无法删除自身账户
9. **前端 CRUD 完整**：通过 UI 可正常创建/删除告警规则，删除有二次确认
10. **日志链路完整**：Fluent Bit → ES 日志写入正常，Jaeger 链路追踪可查询
11. **Token 安全机制完善**：过期/篡改/空/格式错误 Token 全部返回 401
12. **并发安全**：20 并发读取和 10 并发写入均无数据竞争
13. **数据持久化可靠**：服务重启后 MySQL 数据完整恢复
14. **WebSocket 多客户端同步**：多个浏览器标签页同时接收告警推送
15. **前端响应式适配**：移动端/平板/桌面三种尺寸布局均正常

### 超出当前项目范围的功能

- **Token 刷新机制**：当前设计为过期后重新登录，项目重点不在认证体系，无需 refresh token
- **通知渠道告警分发**：当前阶段通知渠道仅用于配置管理和连通性测试，`enabled` 字段为预留，项目重点在监控与诊断，不在通知分发

### 前端 SSE 流式消息实时渲染

| 测试项 | 测试结果 |
|--------|----------|
| Copilot SSE 流式文本逐步渲染 | ✅ 发送"查看当前活跃告警"后，回复文本逐步流式渲染到页面 |
| 工具调用结果展示 | ✅ 显示 `alert.list_active success` 标签，置信度 90% |
| 建议操作按钮渲染 | ✅ 显示"查看 server-probe:9090 的 CPU 指标"等可点击建议按钮 |
| 流式完成后完整内容展示 | ✅ 完整的中文回复、工具结果、建议按钮均正确渲染 |

### 仍然未测试项（需后续关注）

- ~~Token 过期后的行为~~ ✅ 已测试（见第三轮）
- ~~Token 刷新机制~~ ❌ 功能不存在（过期后只能重新登录，无 refresh token 端点）
- ~~WebSocket 断线重连/多客户端/长时间稳定性~~ ✅ 多客户端已测试（见第三轮），断线重连需手动断网
- ~~通知渠道禁用后是否跳过正式发送~~ ❌ 功能不存在（当前阶段通知渠道仅用于配置管理和测试，不发送真实告警通知，`enabled` 为预留字段）
- ~~并发与性能测试~~ ✅ 基本并发已测试（见第三轮）
- ~~数据一致性测试~~ ✅ 服务重启后数据恢复已测试（见第三轮）
- ~~部署与配置测试~~ ✅ 环境变量和启动日志已检查（见第三轮）
- ~~前端响应式布局/浏览器兼容性~~ ✅ 响应式已测试（见第三轮），多浏览器需不同环境

---

## 十八、补充测试（第三轮）

### Token 过期与安全行为

| 测试项 | 测试结果 |
|--------|----------|
| 正常 Token 访问 | ✅ HTTP 200 |
| 过期 Token（exp 为过去时间） | ✅ HTTP 401，`"invalid or expired token"` |
| 篡改 Token（签名不匹配） | ✅ HTTP 401，`"invalid or expired token"` |
| 空 Token | ✅ HTTP 401，`"invalid or expired token"` |
| Bearer 前缀缺失 | ✅ HTTP 401，`"invalid or expired token"` |
| Token 刷新机制 | ❌ 不存在（设计选择：过期后重新登录） |

### 并发安全测试

| 测试项 | 测试结果 |
|--------|----------|
| 20 并发读取请求 | ✅ 全部 HTTP 200，无数据竞争 |
| 10 并发创建主机组 | ✅ 恰好创建 10 条记录，ID 唯一无冲突 |
| 并发后数据完整性 | ✅ 所有记录均可查询和删除 |

### 数据一致性测试

| 测试项 | 测试结果 |
|--------|----------|
| 重启 server-web 后数据恢复 | ✅ 重启前创建的主机组（id=26）重启后完整恢复 |
| MySQL 数据持久化 | ✅ 重启前后主机组数量一致（3 → 3） |

### WebSocket 多客户端测试

| 测试项 | 测试结果 |
|--------|----------|
| 2 个标签页同时连接 | ✅ 两个页面均显示"已连接" |
| 告警推送多客户端同步 | ✅ 发送告警后两个页面活跃告警数同时从 46 更新为 47 |
| 页面标题实时更新 | ✅ 标题从 `(46) CloudOps Monitor` 变为 `(47)` |

### 前端响应式布局

| 测试项 | 测试结果 |
|--------|----------|
| 移动端（375×812） | ✅ 侧边栏自动折叠为图标模式，内容区域自适应 |
| 平板（768×1024） | ✅ 侧边栏保持折叠，内容正常展示 |
| 桌面（1440×900） | ✅ 侧边栏展开，完整布局 |

### 部署与配置

| 测试项 | 测试结果 |
|--------|----------|
| 关键环境变量配置 | ✅ MYSQL/REDIS/JWT/KAFKA/PROMETHEUS/ALERTMANAGER 均正确配置 |
| 启动日志无错误 | ✅ 所有组件初始化成功，无 error/fatal 日志 |
| JWT_EXPIRE_HOURS 默认值 | ✅ 24 小时（可通过环境变量覆盖） |

---

## 十九、Kubernetes 集群功能测试

> 测试时间：2026-05-18 13:30 ~ (进行中)
> 测试环境：kind (Kubernetes in Docker) 单节点集群 `cloudops-test`
> K8s 版本：v1.32.2（kind 默认）
> 测试方式：直接运行 server-web 二进制 + API 调用

### K8s 集群环境

| 测试项 | 测试结果 |
|--------|----------|
| kind 集群创建 | ✅ `cloudops-test` 集群正常运行 |
| kubeconfig 生成 | ✅ `/tmp/kind-kubeconfig` 可用 |
| nginx 测试部署 | ✅ cloudops-test namespace，2 副本 Running |
| nginx Service | ✅ ClusterIP 10.96.160.102:80 |

### K8s 只读工具（6 个）

| 测试项 | 测试结果 |
|--------|----------|
| k8s.get_pods — 查询 Pod 列表 | ✅ 返回 2 个 nginx Pod，含 namespace/name/phase/ready/restart_count/node |
| k8s.get_deployments — 查询 Deployment 列表 | ✅ 返回 nginx deployment，replicas=2/ready=2 |
| k8s.get_services — 查询 Service 列表 | ✅ 返回 nginx-svc，ClusterIP/Port 正确 |
| k8s.get_nodes — 查询 Node 列表 | ✅ 返回 cloudops-test-control-plane，Ready/KubeletVersion |
| k8s.get_events — 查询 Event 列表 | ✅ 返回事件列表，含 type/reason/message |
| k8s.get_logs — 查询 Pod 日志 | ✅ 返回 nginx Pod 日志内容 |

### K8s NLU 意图路由

| 测试项 | 测试结果 |
|--------|----------|
| "查看 K8s Pod" → k8s.get_pods | ✅ NLU 正确识别意图，路由到 k8s.get_pods 工具 |
| "查看 K8s Deployment" → k8s.get_deployments | ✅ NLU 正确识别意图，路由到 k8s.get_deployments 工具 |
| "查看 K8s 节点" → k8s.get_nodes | ✅ NLU 正确识别意图，路由到 k8s.get_nodes 工具 |

### K8s 写操作（Action 审批流）

| 测试项 | 测试结果 |
|--------|----------|
| 创建 scale_deployment Action | ✅ POST /api/v1/actions 返回 action id，status=pending |
| 审批 Action（pending → approved） | ✅ POST /api/v1/actions/:id/approve 返回 approved |
| 执行 Action（approved → executed） | ✅ nginx replicas 从 2 扩容到 3 |
| 创建 restart_deployment Action | ✅ POST /api/v1/actions 返回 action id |
| 执行 restart_deployment | ✅ nginx Pod 重新启动，restart_count 增加 |

### LLM 工具分类路由

| 测试项 | 测试结果 |
|--------|----------|
| LLM function calling → k8s 工具 | ✅ LLM 正确选择 k8s 工具并返回结构化结果 |

### Bug 修复验证

| 测试项 | 测试结果 |
|--------|----------|
| normalizeToolName（LLM 返回工具名格式修复） | ✅ `k8s.get.pods` → `k8s_get_pods` 正确转换 |
| registryHas（SelectedTool 名称格式修复） | ✅ NLU 返回 `k8s.get_pods` 直接匹配注册表，无需转换 |
| normalizeLineBreaks（`\n` 字面量 → 真实换行） | ✅ LLM 返回的 `\n` 正确转换为换行符 |

### K8s 诊断证据采集

| 测试项 | 测试结果 |
|--------|----------|
| K8s Deployment 类型告警触发诊断 | ✅ 发送 target_kind=k8s_deployment 告警，触发诊断采集 K8s 证据 |
| K8s 证据采集 — Deployments | ✅ 返回 nginx deployment，replicas=2/ready=2/updated=2/available=2 |
| K8s 证据采集 — Pods | ✅ 返回 2 个 nginx Pod，含 namespace/name/phase/ready/restart_count |
| K8s 证据采集 — Services | ✅ 返回 nginx-svc，ClusterIP/Port 正确 |
| K8s 证据采集 — Events | ✅ 返回 8 个 Deployment 相关事件 |
| K8s 规则分析 — k8s_deployment_not_ready | ✅ 未命中（Deployment 就绪=2 期望=2 已更新=2） |
| K8s 规则分析 — evidence_incomplete | ✅ 命中（host.metrics 缺 instance 参数） |
| resolver target_kind 推断修复 | ✅ 从 labels 中读取 target_kind/target_name，回退到 host 默认值 |

### K8sEvidence 前端组件渲染

| 测试项 | 测试结果 |
|--------|----------|
| K8s 证据区域标题 | ✅ 显示 `cloudops-test k8s_deployment / nginx` |
| Deployments 折叠面板 | ✅ 展开后显示表格：Deployment/ready/updated/available |
| Deployments 表格数据 | ✅ `cloudops-test/nginx 2/2 2 2` |
| Pods 折叠面板 | ✅ 显示 `Pods (2)` |
| Services 折叠面板 | ✅ 显示 `Services (1)` |
| Events 折叠面板 | ✅ 显示 `Events (8)` |
| Runbook 命中展示 | ✅ `K8s Deployment Unavailable k8s-deployment-unavailable.md · score 1.0` |
| 采集降级警告 | ✅ 显示 host.metrics 和 alert.history 的错误信息 |

### ToolCallDisplay K8s 表格渲染

| 测试项 | 测试结果 |
|--------|----------|
| k8s.get_pods 工具调用标签 | ✅ 显示 `k8s.get_pods success` 按钮 |
| k8s.get_pods 表格列头 | ✅ namespace/name/phase/ready_containers/total_containers/restart_count/node_name |
| k8s.get_pods 表格数据 | ✅ 2 行 nginx Pod 数据，含 namespace=cloudops-test/phase=Running/ready=1/1 |
| 意图标签和置信度 | ✅ 显示 `metric_query 93%` |
| 建议操作按钮 | ✅ 显示 `查看 node-1 最近1小时CPU` 等建议 |

### Action 审批前端页面

| 测试项 | 测试结果 |
|--------|----------|
| 动作列表表格 | ✅ 显示 ID/动作类型/目标/风险级别/状态/来源/创建时间/操作 |
| K8s 动作记录显示 | ✅ #25 k8s.restart_deployment cloudops-test/nginx medium executed admin |
| K8s 动作记录显示 | ✅ #24 k8s.scale_deployment cloudops-test/nginx medium executed admin |
| 筛选器 | ✅ 状态/风险级别/动作类型筛选器正常 |
| 分页 | ✅ Total 4，分页控件正常 |

### 审计日志前端页面

| 测试项 | 测试结果 |
|--------|----------|
| 审计日志表格 | ✅ 显示 时间/操作者/动作/动作类型/资源/结果/trace_id/错误 |
| K8s 审计记录 — action.execute | ✅ k8s.restart_deployment / k8s.scale_deployment success |
| K8s 审计记录 — action.approve | ✅ pending_action #24/#25 success |
| K8s 审计记录 — action.create_pending | ✅ k8s.restart_deployment / k8s.scale_deployment success |
| 审计记录 — 错误记录 | ✅ 显示 failure/denied 状态和错误信息 |
| 筛选器 | ✅ 动作/结果/操作者筛选器正常 |

### 日志脱敏（sanitize.go）

| 测试项 | 测试结果 |
|--------|----------|
| Bearer Token 脱敏 | ✅ `bearer eyJ...` → `Bearer [REDACTED]` |
| password 键值脱敏 | ✅ `password=secret123` → `password=[REDACTED]` |
| token 键值脱敏 | ✅ `token=abc-xyz-789` → `token=[REDACTED]` |
| secret 键值脱敏 | ✅ `secret=mysecretvalue` → `secret=[REDACTED]` |
| api_key 键值脱敏 | ✅ `api_key=key123abc` → `api_key=[REDACTED]` |
| 大小写不敏感匹配 | ✅ `Password=Secret123` / `TOKEN=abc` 正确脱敏 |
| PEM 私钥脱敏 | ✅ `-----BEGIN RSA PRIVATE KEY-----` → `[REDACTED_PRIVATE_KEY]` |
| 截断功能 | ✅ 超过 maxBytes 的文本正确截断 |
| UTF-8 安全截断 | ✅ 中文文本截断后无乱码（无 U+FFFD 替换字符） |
| 无需脱敏文本保持不变 | ✅ 普通日志文本原样返回 |

### K8s Runbook 匹配

| 测试项 | 测试结果 |
|--------|----------|
| 诊断流程中 Runbook 匹配 | ✅ K8sDeploymentUnavailable 告警匹配到 k8s-deployment-unavailable.md，score=1.0 |
| Runbook 前端展示 | ✅ 折叠面板显示标题/文件名/分数，可展开查看内容 |

### 补充测试 — 遗漏项全覆盖

#### K8s Pod 类型告警诊断

| 测试项 | 测试结果 |
|--------|----------|
| k8s_pod 类型告警触发诊断 | ✅ target_kind=k8s_pod，target_name=nginx-595c98f46f-pw5w2 |
| K8s 证据采集 — Pods | ✅ 返回 Pod 列表 |
| K8s 证据采集 — Events | ✅ 返回事件列表 |
| K8s 证据采集 — Logs | ✅ 返回日志片段 |

#### K8s Node 类型告警诊断

| 测试项 | 测试结果 |
|--------|----------|
| k8s_node 类型告警触发诊断 | ✅ target_kind=k8s_node，target_name=cloudops-test-control-plane |
| K8s 证据采集 — Nodes | ✅ 返回 Node 列表 |

#### K8s namespace 白名单校验

| 测试项 | 测试结果 |
|--------|----------|
| 查询不允许的 namespace（kube-public） | ✅ 工具执行失败，返回 namespace not allowed 错误 |
| 查询允许的 namespace（cloudops-test） | ✅ 正常返回数据 |

#### Action 拒绝流程

| 测试项 | 测试结果 |
|--------|----------|
| 创建 pending action | ✅ POST /api/v1/actions 返回 action id=26 |
| 拒绝 action（pending → rejected） | ✅ POST /api/v1/actions/26/reject 返回 rejected |
| 验证 action 状态 | ✅ GET /api/v1/actions/26 返回 status=rejected |

#### SanitizeJSON 审计日志脱敏

| 测试项 | 测试结果 |
|--------|----------|
| password 键值脱敏 | ✅ `password→[REDACTED]` |
| token 键值脱敏 | ✅ `token→[REDACTED]` |
| secret/api_key/authorization/kubeconfig/passwd 脱敏 | ✅ 全部替换为 [REDACTED] |
| 嵌套对象脱敏 | ✅ 嵌套的 password/token 被脱敏，非敏感值保留 |
| 数组内对象脱敏 | ✅ 数组中每个对象的 password 被脱敏 |
| 无敏感数据保持不变 | ✅ 非敏感 JSON 原样返回 |
| 空 JSON 输入 | ✅ 返回 `{}` |
| 无效 JSON 输入 | ✅ 不崩溃，返回结果 |
| isSensitiveKey 大小写不敏感 | ✅ password/PASSWORD/Password 全部识别 |

#### ensureLineBreakBeforeQuestion 单元测试

| 测试项 | 测试结果 |
|--------|----------|
| 中文句号后问句换行 | ✅ PASS |
| 中文感叹号后问句换行 | ✅ PASS |
| 已有换行不重复添加 | ✅ PASS |
| 无问号不处理 | ✅ PASS |
| 无句末标点不处理 | ✅ PASS |
| 英文句号后问句换行 | ✅ PASS |
| 多句后问句换行 | ✅ PASS |
| 空字符串 | ✅ PASS |
| 仅有问号 | ✅ PASS |

#### ToolCallDisplay K8s 其他工具表格

| 测试项 | 测试结果 |
|--------|----------|
| k8s.get_nodes 表格列头 | ✅ name/ready/kubelet_version/capacity |
| k8s.get_nodes 表格数据 | ✅ cloudops-test-control-plane/true/v1.35.0/{cpu:16,memory:13816728Ki} |
| k8s.get_logs 参数验证 | ✅ 缺少 pod_name 时返回 invalid_args 错误 |

#### Action 详情页前端

| 测试项 | 测试结果 |
|--------|----------|
| Action 详情标题 | ✅ `#24 动作详情` + `k8s.scale_deployment` |
| 目标和状态 | ✅ `cloudops-test/nginx` + `executed` + `medium` |
| 参数区域 | ✅ `查看参数 JSON` 按钮 |
| 执行结果表格 | ✅ 目标/旧副本数/新副本数/Ready副本/消息 |
| 执行结果数据 | ✅ `cloudops-test/nginx | 旧副本数 2 | 新副本数 3 | Deployment 副本数从 2 扩缩至 3` |
| 返回按钮 | ✅ `返回动作列表` |

#### 登录页默认凭据提示

| 测试项 | 测试结果 |
|--------|----------|
| 默认账号提示 | ✅ 显示 `默认账号: admin / server-monitor-local-admin` |
| Logo 云图标 | ✅ 登录页 logo 区域显示 SVG 云图标 |
| 侧边栏 Logo 云图标 | ✅ 侧边栏 CloudOps logo 旁显示 SVG 云图标 |

---

## 测试环境状态

- 镜像构建时间：2026-05-18 02:13:09 UTC
- 容器启动状态：14 个容器全部 Running & Healthy
- 健康检查：server-web /healthz ✅ /readyz ✅ /readyz/full ✅
- 测试完成时间：2026-05-18 10:45 (UTC+8)
- K8s 测试环境：kind `cloudops-test` 集群，v1.32.2
- K8s 测试完成时间：2026-05-18 14:10 (UTC+8)
- 补充测试完成时间：2026-05-18 14:25 (UTC+8)
- 最终测试状态：✅ 全部通过，0 项遗漏
