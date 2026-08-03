# CloudOps-Copilot 前端细节优化实施方案

> 状态：`PLAN_APPROVED=YES`
>
> Owner 共同理解确认：2026-08-02（Asia/Shanghai）
>
> 目标仓库：`/home/monody/k8s/CloudOps-Copilot`
>
> 规划时代码基线：`main@a8076f97a8b252d595f5040a618cd4c408873fe1`；实施开始时必须重新解析当前 HEAD 与工作树
>
> 视觉基线：当前已完成的 CPA 风格 CloudOps 前端实际运行效果
>
> 本文档性质：独立的前端细节优化执行方案，不是前端重建方案，也不是视觉改版方案
>
> 本次授权：`DOCUMENT_CREATION_ONLY`；前端代码实施为 `NOT_STARTED`，必须由 Owner 单独启动
>
> 外部写入：`NOT_AUTHORIZED`；不得 push、创建 PR、发布或部署

## 0. 文档定位与使用方式

### 0.1 独立执行边界

本文档只负责既有前端的以下细节优化：

- 动效语言、页面节奏和状态过渡。
- 点击、加载、成功、失败、断线和后台任务反馈。
- 前端缓存、请求协调、实时数据绘制和页面现场恢复。
- 路由加载、专业渲染器生命周期、列表渲染和浏览器性能。
- 只读的前端性能测量与结果报告。

本文档不替代 `docs/CloudOps-CPA-Frontend-Rebuild-Implementation-Plan.md`。后者继续定义已经完成的页面结构、领域能力、API/SSE/Router 合同、安全语义与最终集成边界；本文档把这些结果作为冻结基线，只在其上实施细节精修。

### 0.2 冲突解释顺序

出现冲突时按以下顺序处理：

1. Owner 最新明确指令。
2. 本文档对前端细节优化的范围、已批准决定与验收边界。
3. 当前工作树和 Owner 已认可的实际运行效果。
4. `docs/CloudOps-CPA-Frontend-Rebuild-Implementation-Plan.md` 中仍适用于功能、合同和安全的要求。
5. 已接受 ADR 与历史 evidence，仅作为约束和 provenance。

若细节优化需要改变永久 UI、布局、基础样式、路由、领域模型或后端合同，必须停止并报告；不得自行扩大范围。

### 0.3 状态词

执行和报告只使用以下状态：

- `PASS`：在当前精确工作树或提交上实际运行并通过。
- `FAIL`：已运行且没有满足本文档合同。
- `NOT RUN`：未运行、不在本轮范围、条件不足或没有授权。
- `BACKEND_GAP`：真实实现需要的后端合同不存在；不得以前端假状态掩盖。

没有实际测量的性能、没有 Owner 明确给出的视觉判断、没有运行的长时或大数据测试，都不能被写成 `PASS`。

## 1. 目标与不可变边界

### 1.1 最终目标

本轮目标不是让前端重新变得“更像 CPA”，而是在现有视觉已经确定的前提下补齐展示所需的节奏、反馈、连续性和流畅性。

产品优先级为：

```text
FRONTEND_PURPOSE=PRESENTATION_FIRST
VISUAL_CHARACTER=QUIET_BUT_ALIVE
VISUAL_TONE=RESTRAINED_PREMIUM
OWNER_VISUAL_DECISION_ONLY=YES
```

展示优先不等于允许失真。权限、审批、高风险操作、数据新鲜度、Provider 状态和服务端结果必须保持真实；不得用动画、缓存或乐观状态伪造成功、实时性或系统健康。

### 1.2 永久视觉冻结

以下内容不得因本轮实施发生有意变化：

- 页面信息架构、路由结构和导航分组。
- Shell、Sidebar、Header、Toolbar、Inspector、Dock 和弹层的永久布局。
- 页面网格、区域顺序、间距、尺寸、字体层级、圆角、基础色板与表面样式。
- 已存在的控件类型、业务字段、表格列、页面入口和用户能力。
- Light/Dark 的既有视觉结果。

允许的可见变化只包括不改变稳定几何结构的瞬态反馈：

- `opacity`、小幅 `transform`、已有 Token 范围内的颜色或边框强调。
- 图标与文字在预留宽度内的 loading/success/error 状态替换。
- 已有状态区、通知收件箱和 Inspector 中的真实进度或新鲜度反馈。
- 图表、Atlas、Waterfall、实时日志和 Agent 流式内容的真实数据驱动过渡。

不得通过细节优化新增卡片、重排页面、修改基础造型、加入装饰背景、引入第二套 UI 系统或增加自动演示界面。

### 1.3 技术与写入边界

- 继续使用 Vue 3、Vite、TypeScript、Vue Router、Pinia、Nuxt UI、Lucide、Three.js、uPlot 和 TanStack Vue Virtual。
- 不引入第二套通用组件库，不手绘基础控件或图标。
- 不修改后端、OpenAPI、数据库、认证、Router 路径、SSE 事件合同或安全语义。
- 不加入生产 fixture、假流量、假百分比、假打字速度或演示专用数据路径。
- 写操作继续以服务端结果为准；本文档不授权真实 Provider 或集群写入验证。

## 2. 已批准决定总表

以下决定已经由 Owner 逐项确认，实施时不得重新解释为另一个方向：

```text
MOTION_TONE=QUIET_BUT_ALIVE
MOTION_PURPOSE=EXPLAIN_REAL_CHANGE
PAGE_ENTRANCE=STAGED_TWO_OR_THREE_BEATS
PAGE_ENTRANCE_TOTAL_MS=300_TO_450
CACHED_RETURN_REPLAYS_ENTRANCE=NO
CONTINUOUS_FOCAL_MOTION_MAX_PER_PAGE=1
CONTINUOUS_MOTION_REQUIRES_REAL_DATA=YES
SEMANTIC_DATA_TRANSITIONS=YES
THEME_TRANSITION=COORDINATED_200_TO_300_MS
SCROLL_REVEAL=PRIMARY_SECTIONS_ONCE
CONTEXT_LINKED_HIGHLIGHT=YES
POINTER_DEPTH=ATLAS_AND_PRIMARY_FOCAL_ONLY
COLD_START_BRAND_REVEAL_MS=ABOUT_300
DOMAIN_SIGNATURE_MOTION=ONE_PER_DOMAIN
AUTO_DEMO_MODE=NO
FAKE_TYPEWRITER=NO

CACHE_MODEL=STALE_WHILE_REVALIDATE
STALE_DATA_MUST_BE_LABELLED=YES
WRITE_RESULT_AUTHORITY=SERVER
REALTIME_LIST_INSERTION=USER_CONTROLLED
LOCAL_FEEDBACK_FIRST=YES
GLOBAL_NOTIFICATION=BACKGROUND_OR_IMPORTANT_RESULT_ONLY
ERROR_FEEDBACK=TIERED_BY_IMPACT
LONG_TASKS=ROUTE_INDEPENDENT_AND_TRACEABLE
FAKE_PROGRESS_PERCENT=NO
LOADING_FEEDBACK=PROGRESSIVE_BY_WAIT_TIME
PENDING_LOCK_SCOPE=LOCAL_AND_CONFLICTING_ACTIONS_ONLY
CONFIRMATION_STRENGTH=RISK_TIERED
DESKTOP_NOTIFICATION=OPT_IN_BACKGROUND_CRITICAL_ONLY
NOTIFICATION_SOUND_DEFAULT=OFF
REALTIME_DISCONNECT=KEEP_CONTENT_AND_MARK_STALE
REALTIME_RENDERING=BATCHED_WITHOUT_DATA_LOSS
LIVE_LOG_FOLLOW=ONLY_WHEN_AT_BOTTOM
CHART_UPDATE=REAL_CADENCE_APPEND
AGENT_STREAMING=REAL_CHUNKS_BATCH_RENDERED
OFFLINE_MODE=READ_ONLY

CACHE_POLICY=CENTRALIZED_BY_DATA_TYPE
PERSISTENT_PREFERENCES=SAFE_ALLOWLIST_ONLY
BUSINESS_WORKSPACE_STATE=SESSION_ONLY
SENSITIVE_DATA_PERSISTENCE=FORBIDDEN
NON_SENSITIVE_DRAFT_RETENTION_HOURS=24
QUERY_CACHE=BOUNDED_LRU
REQUEST_INPUT=DEBOUNCED_WHERE_CONTINUOUS
OBSOLETE_REQUESTS=CANCELLED
LATEST_RESULT_WINS=YES
PAGE_FOCUS_REFRESH=STALE_DATA_ONLY

INITIAL_LOAD=LIGHTWEIGHT
PREFETCH=INTENT_AND_CAPABILITY_AWARE
HEAVY_RENDERERS=ROUTE_AND_VIEWPORT_LAZY
HIDDEN_PAGE_RENDERING=PAUSED
PERFORMANCE_DEGRADATION=VISUALS_BEFORE_DATA
PERFORMANCE_DEGRADATION_NOTICE=ONLY_IF_DATA_EXPERIENCE_CHANGES

AI_VISUAL_REVIEW=NOT_RUN
SCREENSHOT_CAPTURE=NOT_RUN
OWNER_LIVE_VISUAL_REVIEW=REQUIRED
PERFORMANCE_REFERENCE_MACHINE=OWNER_CURRENT_COMPUTER
PERFORMANCE_DATA=CURRENT_REAL_DATA_ONLY
PERFORMANCE_ROUTE_COVERAGE=ALL_ROUTES_BASIC_HEAVY_ROUTES_DEEP
PERFORMANCE_CACHE_MODES=COLD_AND_WARM
PERFORMANCE_ROUTE_TOUR_COUNT=1
PERFORMANCE_METRIC_RETENTION_HOURS=24
LARGE_DATA_PERFORMANCE=NOT_RUN
LONG_SESSION_MEMORY=NOT_RUN
```

## 3. 统一动效合同

### 3.1 Motion Token

所有页面必须消费共享 Motion Token，不得继续在页面内自由散落互不一致的时长和缓动。

| 用途 | 目标 | 约束 |
| --- | --- | --- |
| 点击确认 | `<= 100ms` | 立即出现 pressed/pending 反馈，不等待网络 |
| Hover、Pressed、局部状态替换 | `120ms` 左右 | 不改变控件尺寸，不模糊文字 |
| Inspector、Sidebar、局部结构过渡 | `120-200ms` | 可中断，动画期间仍可操作 |
| Light/Dark 协调切换 | `200-300ms` | DOM、uPlot、Three.js 与 Overlay 同步 |
| 冷启动品牌揭示 | 约 `300ms` | 只利用真实加载时间，不新增 Splash |
| 首次页面层级入场 | `300-450ms` 总计 | 两到三拍，不逐卡片或逐行播放 |
| 真实状态持续信号 | 数据驱动 | 每页最多一个，数据停止时动画停止 |

`frontend/src/styles/tokens.css` 继续是 Motion primitive 和 semantic token 的生产入口。页面允许使用领域语义 Token，但不得出现无法解释的任意时长和 easing。

### 3.2 页面入场节奏

首次进入一个真实工作区时按以下顺序编排：

1. Shell、Sidebar 和 Header 保持稳定，不重复入场。
2. 页面标题、Scope 和核心状态进入。
3. 核心指标、主图表或主工作区进入。
4. 次要详情与技术信息在同一总时长内完成，不形成长尾动画。

返回已经缓存的页面时立即恢复现场，不重播整套入场。页面内主要章节只在本次页面会话第一次进入视野时轻微揭示；滚回该区域不得重复播放。

### 3.3 空间关系

- 一级页面切换使用短淡化和极小纵向位移。
- Inspector、详情和 Slideover 使用现有方向关系进入，关闭后回到原触发对象。
- 行内展开从触发位置自然展开，不推动无关区域反复跳动。
- Dialog 只做轻微淡入，不做大幅缩放、弹跳或旋转。
- Sidebar 标签与宽度变化协调完成，主内容只发生一次连续调整。
- Hover 不抬升表格行；按钮只提供轻微按压感。

### 3.4 数据变化

- 数值直接更新为真实结果，只对变化位置做一次短暂强调。
- 不从 `0` 重新计数，不让整张卡片闪动，不无限脉冲严重状态。
- 相同指标、相同对象和连续时间窗口允许平滑衔接。
- 查询身份已经变化时使用短暂交叉淡化；旧结果只能明确标记为“上次结果”。
- 表格、列表和技术 ID 使用稳定尺寸与 tabular numbers，动态值不得推动布局。
- 高频连续变化先合并，再进行一次可感知更新。

### 3.5 reduced motion

所有新增 Motion 必须有 `prefers-reduced-motion: reduce` 等价行为：

- 移除持续流动、视差、自动跟随动画和非必要位移。
- 状态变化、错误、权限、加载和完成语义必须继续可见。
- Atlas 使用静态关系表达；图表直接呈现最终数据。
- 不允许以“减少动效”为理由隐藏数据或控件。

## 4. 领域标志动作

共享 Motion 语法保持一致，每个领域只允许一个主要标志动作。除表中动作外，页面保持安静。

| 领域 | 标志动作 | 真实触发 | 停止条件 |
| --- | --- | --- | --- |
| Shell | 冷启动品牌揭示与协调主题切换 | 首次真实加载、主题切换 | 缓存启动、过渡完成 |
| Overview | Operational signal 流动 | 真实活动、状态传播 | 无活动、数据 stale、页面隐藏 |
| Atlas / Infrastructure | 关系流动、选中路径与轻微指针深度 | 真实拓扑、选择、状态变化 | 无活动、失焦、reduced motion、WebGL 降级 |
| Monitoring | 曲线末端按真实 cadence 延伸 | 新采样到达 | Hover/键盘锁定时保持读数，页面隐藏时暂停绘制 |
| Alerts | 已有行状态原地变化提示 | Alert 真实状态变化 | 一次提示完成；新行保持排队 |
| Logs | 实时游标与底部跟随 | 用户位于底部且 live stream 正常 | 用户向上阅读、断线、页面隐藏 |
| Traces | Waterfall 层级展开与 Span 关联高亮 | Trace/Span 选择 | 选择稳定后停止，不持续扫动 |
| Agent | 真实流式 chunk 与当前执行状态 | SSE chunk、真实阶段变化 | 完成、失败、断线或用户离开底部 |
| Incidents | 生命周期与时间线阶段推进 | Incident 真实状态或资源事件 | 当前状态稳定后停止 |
| DevOps | Authority、Execution、Verification 因果链推进 | 真实阶段变化 | 最终状态到达或断线 |
| Settings | Diff、校验和 apply 阶段揭示 | 本地 Draft、真实校验和服务端结果 | 状态稳定后停止；不伪造 apply 进度 |

任何页面没有真实数据时，使用现有 Empty/Unavailable 状态；不得为了展示补造持续动画。

## 5. 异步反馈合同

### 5.1 加载分级

每个异步操作遵守同一反馈阶梯：

1. 点击后立即在触发控件上显示 pressed 或 pending，防止重复执行。
2. 很快完成的请求直接显示结果，不闪现骨架屏。
3. 等待明显增加时，只在受影响区域显示与最终结构一致的 skeleton/loading。
4. 长等待显示真实阶段名称和已用时间；没有后端百分比时不得显示百分比。
5. 已有缓存时保留旧内容，只显示低干扰的后台刷新状态。

Skeleton 使用统一、低对比度的轻微呼吸，不使用明显扫光；reduced motion 下静止。

### 5.2 成功反馈

- 高频轻操作就地反馈，能在触发位置说明结果时不发全局 Toast。
- Copy、刷新、筛选和局部状态变更使用预留宽度内的短暂图标/文字替换。
- 后台任务、跨页面任务和关键写操作完成后使用现有全局通知能力。
- 一次性成功反馈恢复正常后，不得清除持续性的业务状态。

### 5.3 错误分级

| 错误范围 | 反馈位置 | 行为 |
| --- | --- | --- |
| 字段或输入错误 | 字段附近 | 说明原因与修正方式，保留输入 |
| 局部读取/刷新失败 | 对应区域 | 保留旧内容，标记 stale/error，提供重试 |
| 权限不足 | 被拒绝的操作位置 | 说明所需权限，不清空页面 |
| 后台任务失败 | 全局通知 + 原任务位置 | 可返回失败现场，保留 request/trace identity |
| 冲突、高风险失败、可能造成错误判断 | 不可忽略确认层 | 展示目标、范围、服务端当前状态与下一步 |

错误不得只显示“失败”。已知原因、请求身份、Trace ID、是否可重试和下一步必须渐进披露。

### 5.4 Pending 与确认

- 只锁定当前提交按钮及互斥操作，不能冻结整个页面。
- 整体配置 apply 只锁定对应编辑工作区。
- 超时后先确认服务端实际结果，再决定是否使用同一 idempotency identity 重试。
- 低风险操作不滥用确认弹窗；中风险确认目标与范围；高风险明确 Scope、对象、后果和可逆性。
- Undo 和 Cancel 只有后端真实支持时才可出现。

### 5.5 长任务

Agent Investigation、Monitoring 执行、Provider Test、配置 apply、修复和 Verification 等长任务必须：

- 立即确认服务端已经受理。
- 展示真实阶段、已用时间和当前连接状态。
- 允许用户离开页面，任务继续由现有全局生命周期跟踪。
- 完成或失败后通过全局通知返回 canonical 结果位置。
- 连接中断时保留已知阶段，不伪造完成或失败。

## 6. 实时数据合同

### 6.1 稳定视野

- 已存在的列表行可以原地更新，并对真正变化的字段做一次短暂提示。
- 新行不得强制插入当前视野；统一进入 pending queue，显示新增数量，由用户主动载入。
- 严重异常可以立即触发应用内或已授权桌面通知，但不能抢 Focus、移动鼠标下内容或强制切页。
- 用户选中、编辑、复制或阅读的行不得被实时排序夺走。

### 6.2 批量绘制

实时事件完整接收，视觉更新可以短暂合并：

- 同一实体的连续投影只绘制最新状态，同时保留合同要求的完整历史记录。
- 严重异常通知不因批处理被延迟。
- 批处理结束后显示新增数量和最新更新时间。
- 不得用降频隐藏数据丢失；达到合同或内存上限时必须明确报告。

### 6.3 Logs、Charts 与 Agent

- Live Logs 仅在用户位于底部时自动跟随；向上阅读后暂停画面并累计新日志，恢复时直接定位最新内容，不回放追赶动画。
- uPlot 按真实采样 cadence 追加；Hover 或键盘选中时保持 Tooltip 和读数稳定。
- 页面隐藏时停止无意义图表绘制，返回后一次性呈现最新窗口，不回放离开期间动画。
- Agent 按真实 SSE chunk 分批渲染 Markdown；不模拟字符级打字速度。
- Agent 向上阅读时停止自动滚动，保留新内容计数；断线保留已收到内容并标记未完成。

### 6.4 断线与恢复

- 短暂断线只在现有实时状态区提示；持续断线升级为明确警告和手动重试入口。
- 断线期间保留最后内容并标记“停止实时更新/可能已过期”。
- 后台使用有限指数退避重连，不制造通知风暴。
- 恢复连接后先按 cursor/revision/identity 补齐或重新投影，再取消 stale 标记。
- 恢复反馈说明断线时长和补回数量；无法证明补齐时继续保持 stale。

### 6.5 桌面通知

- 桌面通知必须由用户主动授权。
- 页面可见时优先使用应用内反馈。
- 页面隐藏时只为严重异常发送桌面通知；声音默认关闭。
- 重复事件按 Scope、资源 identity 和事件 identity 合并数量与最新时间。
- 点击通知必须恢复正确 Scope 并进入 canonical 事件详情。

## 7. 缓存与页面现场

### 7.1 统一缓存层

不得继续由每个页面和组件自行创建不一致的缓存策略。实现统一 typed cache，至少包含：

- `user identity`、Operational Scope、route/domain、query identity 和 contract version 组成的 key。
- `data`、`updatedAt`、`freshUntil`、`staleReason`、request identity 和可选 revision/cursor。
- request 去重、AbortController、latest-result-wins 和后台 revalidation。
- 有界 LRU 淘汰；当前页面、执行中任务和草稿不得被普通 query cache 淘汰。
- logout、用户变化和 Scope 切换时的精确清理。

缓存只改善连续展示，不能成为新的业务真相来源。

### 7.2 默认新鲜度分层

| 数据类别 | 默认策略 | 关键限制 |
| --- | --- | --- |
| 实时连接、权限、审批、安全状态 | 可展示 last-known，操作前重新确认 | 不以缓存授权写操作 |
| Monitoring、Alerts、Logs、Traces 当前查询 | 短时 fresh，后台更新 | live 状态由 stream/cursor 决定，不只看 TTL |
| Incident、资源和普通列表 | 数十秒级复用 | 状态变化由 SSE/通知主动失效 |
| Provider 摘要、配置历史和低频元数据 | 分钟级复用 | apply/test 成功后立即失效或更新 |
| 固定历史时间范围查询 | 可延长复用 | Scope、Provider 或 query identity 改变即隔离 |

具体 TTL 由统一 policy table 管理，并通过真实请求频率与当前 Owner 机器性能调优；页面不得内联任意 TTL。

### 7.3 存储边界

| 存储范围 | 允许内容 | 禁止内容 |
| --- | --- | --- |
| 内存 cache | 当前 API 结果、历史查询结果、页面投影 | 无界增长 |
| Session 状态 | 路由身份、筛选、scroll、selected entity、Inspector 状态 | Secret、Token、原始 Logs、Evidence 正文、权限结果 |
| 长期偏好 | Theme、Sidebar、无敏感常用筛选、表格显示偏好 | 业务响应正文和身份越界数据 |
| 24 小时 Draft | allowlist 后的非敏感配置草稿、base revision/hash、保存时间 | Secret、Token、凭据值、自动 apply 标记 |

Draft 恢复必须提供恢复、查看 Diff 或丢弃选择；超过 24 小时后只能查看和丢弃。配置 revision 已变化时先处理冲突，不能自动合并或 apply。

### 7.4 页面返回与重新聚焦

- 返回页面时先从 cache 和 session 状态恢复内容、筛选、scroll、selected entity 与 Inspector。
- 有缓存时不重复显示全页 skeleton，也不重播首次页面入场。
- 浏览器重新聚焦时只刷新已 stale、断线或高优先级的数据。
- 相同请求统一去重并分批启动，严重状态和实时连接优先。
- 新数据到达后只提示变化区域，不重置阅读现场。

### 7.5 请求与重试

- 连续文本输入短暂 debounce；Enter 和明确命令立即执行。
- 新 query identity 出现时取消旧请求，只允许最新 identity 更新页面。
- 相同读取请求共享 in-flight Promise，避免组件并发重复读取。
- 可恢复读取错误有限自动重试并逐步延长间隔。
- 权限、参数、冲突和确定性合同错误不自动重试。
- 写操作默认不自动重发；只有幂等合同与服务端结果确认充分时才允许安全重试。

### 7.6 离线

完全离线时保留当前会话中可用的缓存内容并明确标记离线，只允许查看、筛选、复制等本地只读行为。所有写操作、审批和高风险检查禁用并解释原因；不得创建恢复网络后自动执行的写队列。

## 8. 性能优化合同

### 8.1 加载与代码分割

- Shell 和当前路由保持最小首屏依赖。
- Three.js、Atlas、uPlot 重型适配、虚拟化扩展和 Settings 专业编辑区域按路由或真实需要加载。
- 用户 Hover、Focus 导航项或浏览器空闲时，允许预加载最可能的下一路由。
- 弱网、省流或能力不足时减少或关闭预加载。
- 不得在启动时一次加载所有公开路由。

### 8.2 视口激活

- 首屏内容优先；不可见的复杂图表在接近视口时准备，并预留稳定尺寸。
- 通过 hash、目录或 canonical link 直达时立即激活目标区域。
- 离开页面后缓存轻量数据和现场，暂停或 dispose Three.js、uPlot Observer、页面专属 RAF、Timer 和 SSE 消费者。
- 全局通知与已受理后台任务可以继续运行，但不可见的专业画布不得继续无意义绘制。

### 8.3 大列表

- Logs 继续使用 TanStack Vue Virtual。
- 其他真实大列表优先使用服务端筛选、cursor 和虚拟化，不把全部 DOM 节点一次挂载。
- 搜索、复制、选择和定位必须保持完整业务语义，不能只处理当前可见 DOM。
- 达到后端限制或前端安全上限时明确提示，不静默截断。

本轮只使用当前真实数据进行性能测量，因此不能据此声明 `10k Logs`、`2.5k Spans`、`20k rows` 等大数据档位通过。

### 8.4 运行时调度

- 高频实时事件先进入有界队列，再按 animation frame 或短窗口合并 DOM 更新。
- 避免在同一帧交错读取布局与写入样式。
- `will-change` 只在动画前临时启用，完成后移除。
- DOM 动画优先使用 compositor-friendly 的 `opacity` 与 `transform`。
- 若真实数据压力导致掉帧，按顺序减少预加载、非必要 Motion、图表绘制频率、标签密度和 Atlas 视觉质量。
- 不得优先丢弃严重事件、隐藏数据错误或降低操作正确性。

### 8.5 自动降级反馈

只关闭非必要动画、预加载或装饰质量时保持静默；当自动降级影响刷新频率、实时性、可见数据量或完整性时，必须显示原因、当前影响和恢复方式。

## 9. 性能预算与参考环境

### 9.1 Owner 参考机器

本轮正式性能结论只针对以下当前 Owner 环境：

```text
Manufacturer/Model: Lenovo 21N5
CPU: Intel Core i7-14650HX
Host Memory: 32 GB
GPU: Intel UHD Graphics + NVIDIA GeForce RTX 4060 Laptop GPU
Display: 3200 x 2000
Power: plugged in, normal daily power mode
Graphics mode: hybrid graphics
Browser: current Windows Chrome with hardware acceleration enabled
Data: current real CloudOps/Provider data only
```

运行性能验证前必须记录实际 Chrome 版本、浏览器缩放、Windows 显示缩放、可见 viewport、当前 HEAD、生产构建 identity 和运行时 URL。缺少这些事实时不得给出可复现的性能 `PASS`。

### 9.2 体验预算

| 指标 | 目标 | 说明 |
| --- | --- | --- |
| 点击视觉确认 | `<= 100ms` | 与后端完成时间分开测量 |
| 缓存页面恢复 | 约 `<= 200ms` | 内容先可用，后台 revalidate 不阻塞 |
| 常规过渡 | `120-200ms` | 不包括首次页面整体编排 |
| 首次页面编排 | `300-450ms` | 不阻止提前交互，不逐项拉长 |
| INP | `<= 200ms` | 继承 ADR 0037 目标 |
| LCP | `<= 2.5s` | 冷启动与 warm cache 分开报告 |
| CLS | `<= 0.1` | 动态内容、字体、Skeleton 不得造成明显跳动 |
| 重型可视化与滚动 | 目标接近 `60fps` | 报告掉帧、Long Task 与输入阻塞，不用主观描述替代 |
| 初始 Shell JavaScript | `<= 300 KiB gzip` 目标 | Three.js 与领域重型代码必须 lazy |

性能数字是本轮执行方可以判断的验收对象；审美、节奏是否“好看”和最终视觉效果不由执行方判定。

### 9.3 前端性能遥测边界

允许记录 route timing、interaction latency、Long Task、layout shift、cache hit/miss、reconnect、前端错误和版本 identity。禁止记录：

- Secret、Token、Authorization header 和 Cookie。
- Logs 正文、Trace payload、Evidence、Agent 对话和用户输入。
- PromQL/查询正文、配置 Draft 内容和技术详情正文。
- 会话录屏、DOM 内容回放或第三方行为画像。

原始性能样本和聚合结果最长保留 24 小时。当前后端没有合适接收合同时，只保留本地测量结果并记录 `BACKEND_GAP`；不得偷偷发送到第三方。

## 10. 实施阶段

本轮共享文件和跨页面行为高度耦合，默认串行实施。不得先让各页面自由添加动画，再在最后统一。

### 10.1 阶段 0：基线与清单

1. 重新记录 HEAD、工作树、前端依赖和生产构建结果。
2. 清点现有 Motion、Timer、RAF、Observer、EventSource、localStorage 和页面级 `Map` cache。
3. 在不截图的前提下记录 cold/warm 路由时序、Long Task、CLS、内存和重型渲染生命周期基线。
4. 确认所有公开路由当前可进入，阻塞性 Console error 为零。

退出条件：

```text
DETAIL_OPTIMIZATION_BASELINE=PASS
CURRENT_PERFORMANCE_BASELINE=PASS|FAIL
SCREENSHOT_CAPTURE=NOT RUN
```

### 10.2 阶段 1：共享 Motion 与异步反馈

主要责任范围：

```text
frontend/src/styles/tokens.css
frontend/src/style.css
frontend/src/components/layout/
frontend/src/components/workspace/
frontend/src/composables/useLatestAsync.ts
frontend/src/composables/useRealtimeCleanup.ts
frontend/src/composables/useCopyFeedback.ts
```

实施：

- 统一 Motion Token、easing、reduced-motion 和临时状态替换。
- 建立首次页面编排、缓存返回跳过、section reveal 与 Theme 同步合同。
- 统一 loading、success、error、pending、stale 和 background refresh 反馈。
- 保留 Nuxt UI primitive 的 Focus、ARIA、Dialog 和 Overlay 行为。

### 10.3 阶段 2：统一缓存与请求协调

实施：

- 提取 typed cache policy、identity、fresh/stale、LRU 和精确清理。
- 扩展 `useLatestAsync` 或等价共享层，支持 request dedupe、background revalidate、Abort 和 latest-result-wins。
- 统一 session 现场、长期安全偏好和 24 小时非敏感 Draft 边界。
- 把现有 Incident 页面级缓存迁移为共享合同的首个消费者，再逐域迁移。

迁移必须保持 API 参数、URL、cursor、revision、Scope 和错误 identity 不变。

### 10.4 阶段 3：实时反馈与后台生命周期

实施：

- 统一 SSE 断线、重连、stale、补齐和事件批处理语义。
- 建立稳定列表 pending queue 和 user-controlled insert。
- 统一长任务跨路由跟踪、全局通知与 canonical 返回位置。
- 实现 tab visibility、page scope cleanup 和后台绘制暂停。

后端缺少 cursor、replay、task identity、cancel 或安全重试合同时明确记录 `BACKEND_GAP`，不得在前端伪造。

### 10.5 阶段 4：领域标志动作

按以下顺序串行实施并做 focused checks：

1. Shell、Overview。
2. Infrastructure、Atlas。
3. Monitoring、Logs、Traces。
4. Alerts、Incidents。
5. Agent、DevOps、Settings。

每个领域只实现第 4 节定义的一个标志动作，并复用共享 Motion/Feedback/Cache 合同。页面不得自行创建第二套速度、状态或缓存体系。

### 10.6 阶段 5：加载与运行时优化

- 校验 route chunks、intent prefetch 和重型依赖 lazy loading。
- 激活 viewport-aware rendering 和 hidden-page pause。
- 检查 Virtualizer、Observer、RAF、Timer、EventSource 和 Three/uPlot dispose。
- 在 Owner 当前机器上调优 batching、Atlas quality 和图表 cadence。
- 保留真实数据和视觉完整性，先移除无价值成本。

### 10.7 阶段 6：性能验证与 Owner 交接

执行方只验证性能和工程合同，不做视觉结论：

1. 所有公开路由做一次基础 cold/warm 性能巡查。
2. Atlas、Monitoring、Logs、Traces、Agent、Inspector 和 Theme 做深入时序、帧率、Long Task、CLS、内存与 cleanup 检查。
3. 使用当前真实数据，不创建压力 fixture，不截图。
4. 报告实际指标、失败、未运行项和无法证明的边界。
5. 由 Owner 在同一台电脑上现场操作并独立决定视觉是否通过。

## 11. 验证合同

### 11.1 工程护栏

每个阶段运行与改动相关的最小检查：

- `git diff --check`。
- 修改 TS/Vue 的 scoped lint。
- 一次 `npm run typecheck`。
- Motion、cache、request identity、realtime cleanup、visibility 和 reduced-motion focused Vitest。
- 依赖、lockfile、Vite 或构建入口变化时运行 `npm run build` 与 bundle budget。

这些检查证明代码合同，不构成视觉验收。

### 11.2 浏览器性能检查

浏览器自动化可以使用 Performance API、PerformanceObserver、Chrome Performance trace、CDP metrics 和自定义 timing marks，但必须关闭截图采集。不得生成或保存页面截图、filmstrip 或视觉 diff。

全路由基础检查：

- 首次路由 chunk 和 warm route timing。
- 点击反馈延迟、缓存恢复和后台 revalidation。
- Console/Page error、失败请求、未清理请求与明显 CLS。
- 路由离开后的 RAF、Timer、Observer、EventSource 和 renderer cleanup。

重型流程深入检查：

- Atlas 非空渲染、选择、visibility pause 和 dispose；非空只使用 Canvas 像素统计，不保存截图。
- Monitoring 图表更新、Hover 稳定、后台暂停和返回补齐。
- Logs live follow/pause、批量追加和虚拟列表滚动。
- Traces Waterfall 展开、Span 选择和大 DOM 风险。
- Agent 真实 SSE chunk batching、Markdown reflow 和离底暂停。
- Inspector、Sidebar、Theme 切换的 frame、CLS 与输入阻塞。

### 11.3 本轮明确不运行

```text
AI_VISUAL_REVIEW=NOT RUN
SCREENSHOT_CAPTURE=NOT RUN
VISUAL_REGRESSION_SCREENSHOT_DIFF=NOT RUN
AUTOMATED_DEMO_MODE=NOT RUN
SYNTHETIC_LARGE_DATA_PERFORMANCE=NOT RUN
LARGE_DATA_PERFORMANCE=NOT RUN
FIFTEEN_MINUTE_ROUTE_SOAK=NOT RUN
LONG_SESSION_MEMORY=NOT RUN
MULTI_BROWSER_PERFORMANCE=NOT RUN
MOBILE_PERFORMANCE=NOT RUN
WRITE_PATH_E2E=NOT RUN
PUSH_PR_RELEASE_DEPLOY=NOT RUN
```

一次完整路由巡查不能证明不存在内存泄漏或长时 SSE 问题；当前真实数据不能证明大数据性能。报告必须保留这些 `NOT RUN`。

## 12. 完成定义

只有同时满足以下条件，本轮前端细节优化实现才可报告完成：

- 永久 UI、布局和基础样式没有被有意改变。
- 所有新增 Motion 使用共享 Token，支持 reduced motion，并可被用户交互中断。
- 每页最多一个真实数据驱动焦点，领域标志动作没有扩散成装饰动画。
- 缓存、实时更新和异步反馈不伪造服务端状态或数据新鲜度。
- 敏感内容没有进入持久缓存，24 小时 Draft 边界生效。
- 页面返回、断线、后台恢复和新数据到达不破坏阅读现场。
- 不可见的 Three.js、uPlot、RAF、Observer、Timer 和页面专属连接按合同暂停或释放。
- 当前 Owner 机器上的 cold/warm 性能巡查已实际完成并诚实报告。
- 没有截图或 AI 视觉 `PASS` 被用作证据。
- Owner 已在自己的电脑上现场操作，并独立给出最终视觉决定。

阶段状态模板：

```text
FRONTEND_DETAIL_OPTIMIZATION=COMPLETE|INCOMPLETE
SHARED_MOTION_SYSTEM=PASS|FAIL|NOT RUN
ASYNC_FEEDBACK_SYSTEM=PASS|FAIL|NOT RUN
CENTRAL_CACHE_POLICY=PASS|FAIL|NOT RUN
REALTIME_PRESENTATION=PASS|FAIL|NOT RUN
HEAVY_RENDERER_LIFECYCLE=PASS|FAIL|NOT RUN
TYPECHECK=PASS|FAIL|NOT RUN
FOCUSED_TESTS=PASS|FAIL|NOT RUN
CURRENT_REAL_DATA_PERFORMANCE=PASS|FAIL|NOT RUN
OWNER_VISUAL_DECISION=PASS|FAIL|NOT RUN
AI_VISUAL_REVIEW=NOT RUN
SCREENSHOT_CAPTURE=NOT RUN
LARGE_DATA_PERFORMANCE=NOT RUN
LONG_SESSION_MEMORY=NOT RUN
WRITE_PATH_E2E=NOT RUN
EXTERNAL_DELIVERY=NOT RUN
```

本文档完成不自动提升最终真实前后端集成、写路径、发布或生产就绪状态。后续 Gate 仍由当前 CPA 重建方案及 Owner 新授权控制。
