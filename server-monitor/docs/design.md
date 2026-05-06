# 云原生可观测监控平台 — 整体设计方案

## 一、项目定位

基于 Go + Prometheus + Grafana + AlertManager + Kubernetes 实现的云原生可观测监控平台，支持主机资源指标采集、服务健康状态监控、告警规则触发、实时告警推送和可视化大盘展示。项目通过 Helm 部署到 Kubernetes 集群，并结合 Ingress、ConfigMap、Secret、HPA 完成云原生化改造。

### 核心链路

```
指标采集 → 指标存储 → 大盘展示 → 告警触发 → 实时推送
```

### 设计原则

- 监控指标数据不落入关系型数据库，基于 Prometheus TSDB 进行时序化采集、存储与查询
- 每个阶段独立可运行，不依赖后续阶段的组件
- 中间件引入必须服务于监控核心功能，不为炫技而引入
- 云原生优先：容器化部署、声明式配置、弹性伸缩
- Kafka 只承载事件，不承载原始指标；指标长期存储走 Prometheus remote_write → VictoriaMetrics

---

## 二、现有项目分析

### 当前架构

```
server-probe → MySQL ← server-web
```

### 现有组件

| 组件 | 技术栈 | 功能 |
|------|--------|------|
| server-probe | Go + gopsutil + Prometheus client | 采集 CPU/内存，5 秒轮询写入 MySQL，暴露 /metrics |
| server-web | Go + Gin + GORM | 从 MySQL 读取指标，拼接 HTML 表格展示 |
| MySQL | 8.0 | 存储监控指标（server_metrics 表） |
| Docker | Dockerfile 多阶段构建 | 两个服务各自有 Dockerfile |
| Docker Compose | docker-compose.yml | MySQL + probe + web 本地编排 |
| K8s | Deployment/Service/ConfigMap/Secret/Ingress/HPA | 基础 K8s 部署清单 |
| Prometheus | ConfigMap + Deployment + Service NodePort | 采集 probe /metrics |
| Grafana | Deployment + Service NodePort | 可视化（未与 Prometheus 集成） |
| GitHub Actions | CI Pipeline | 推送镜像到 DockerHub |

### 核心问题

| 问题 | 说明 |
|------|------|
| MySQL 存时序数据 | 关系型数据库不适合高频时序写入，无降采样、无保留策略 |
| 指标维度单一 | 只采集 CPU/内存，缺少磁盘、网络、进程、服务指标 |
| 前端简陋 | Go 代码拼接 HTML 字符串，meta refresh 刷新，无图表 |
| 无告警闭环 | Prometheus + Grafana 只是看板，没有主动告警能力 |
| 无实时推送 | 前端靠 meta refresh 轮询，不是真正的实时 |
| 无缓存层 | 每次查询直接打 MySQL/后端，高频刷新压力大 |
| 架构紧耦合 | probe 直写 MySQL，web 直读 MySQL，无法扩展消费者 |
| K8s 配置散乱 | 裸 YAML 管理，无 Helm，不可复用 |
| 无结构化日志 | fmt.Println 输出，无法检索和分析 |
| 无链路追踪 | 微服务间调用无法追踪 |

---

## 三、整体架构设计

### 最终架构（四阶段完成后）

```
云原生可观测与智能运维平台
│
├── 基础中间件
│   ├── Kafka             事件总线（仅承载事件，不承载原始指标）
│   ├── Redis             缓存 / 限流 / Pub/Sub 多副本广播
│   └── VictoriaMetrics   指标长期存储（通过 Prometheus remote_write）
│
├── 指标监控链路 Metrics
│   ├── server-probe      自研 Exporter，暴露 /metrics
│   ├── Prometheus        指标采集与告警规则计算
│   ├── VictoriaMetrics   remote_write 长期存储
│   ├── Grafana           监控大盘（Provisioning 自动配置）
│   └── AlertManager      指标告警通知
│
├── 日志链路 Logs
│   ├── Fluent Bit        DaemonSet 采集 Pod 日志
│   ├── Elasticsearch     日志索引与检索
│   └── Kibana            日志查询与可视化
│
├── 链路追踪 Traces
│   ├── OpenTelemetry SDK 服务埋点
│   └── Jaeger            Trace 存储与查询展示
│
├── 告警与事件链路
│   ├── Kafka             告警事件 / 操作事件 / AI 分析任务等异步事件流转
│   ├── alert-service     告警去重/聚合/状态管理/上下文增强
│   └── AlertManager      基础设施告警
│
├── 应用服务
│   ├── server-probe      采集指标 + /metrics 暴露（DaemonSet）
│   ├── server-web        API / gRPC / Redis 缓存 / WebSocket
│   ├── alert-service     Kafka 消费 → 告警去重/聚合/状态管理/上下文增强
│   └── frontend          Vue3 + ECharts + WebSocket
│
├── Kubernetes 部署能力
│   ├── Helm              应用模板化部署
│   ├── Ingress           统一入口路由
│   └── HPA               服务弹性伸缩
│
└── CI/CD
    └── GitHub Actions    构建镜像、推送镜像、部署到 K8s
```

### 关键架构决策

#### server-probe 部署形态

server-probe 以 **DaemonSet** 方式部署到每个 Kubernetes Node 上，每个节点运行一个 probe 实例，通过 hostPath 挂载 /proc、/sys 等宿主机目录采集节点资源指标，并暴露 /metrics 供 Prometheus 采集。

如果本地开发环境无法使用 DaemonSet，则降级为 Deployment 单实例模式，用于演示指标采集链路。

#### 指标链路 vs 事件链路

```
Metrics 链路（指标数据）：
server-probe → Prometheus scrape → remote_write → VictoriaMetrics

Events 链路（事件数据）：
server-web / alert-service → Kafka → event consumers

Kafka 用于告警事件、操作事件、AI 分析任务等异步事件，不用于替代 Prometheus 的指标采集链路。
```

---

## 四、四阶段演进路线

### 第一阶段：云原生监控闭环

**目标**：跑通指标采集 → 存储 → 展示 → 告警 → 推送的核心闭环

**技术栈**：

```
Go (Gin) + Redis + Prometheus + Grafana + AlertManager + Vue3 + ECharts + WebSocket + K8s + Helm
```

**架构**：

```
server-probe (DaemonSet) → /metrics → Prometheus → Grafana
                                        ↓
                                   Prometheus Rules
                                        ↓
                                   AlertManager → Webhook → server-web
                                        ↓                    ↓
                                   server-web ← Redis ← Prometheus HTTP API
                                        ↓
                                   Redis Pub/Sub → 所有 server-web Pod
                                        ↓
                                   WebSocket → frontend
```

**关键决策**：
- 去掉 MySQL，监控指标走 Prometheus TSDB
- server-probe 以 DaemonSet 部署，采集每个 Node 的宿主机指标
- Redis 缓存热点数据，减少 Prometheus 查询压力
- Redis Pub/Sub 解决 server-web 多副本 WebSocket 广播问题
- 告警闭环：Prometheus Rules → AlertManager → Webhook → Redis Pub/Sub → WebSocket → 前端
- 前端不直接传 PromQL，后端维护 PromQL 白名单模板

**暂不引入**：Kafka、VictoriaMetrics、Fluent Bit、Elasticsearch、Kibana、OpenTelemetry、Jaeger、MySQL

### 第二阶段：补日志链路

**目标**：建立统一的日志采集、存储、查询体系

**新增组件**：

| 组件 | 职责 |
|------|------|
| Fluent Bit | DaemonSet 采集 Pod 日志 |
| Elasticsearch | 日志索引与全文检索 |
| Kibana | 日志查询与可视化 |

**改造内容**：
- 所有服务输出结构化 JSON 日志（zap/slog）
- 日志中预留 trace_id 字段，为第三阶段链路追踪做准备
- Grafana 关联 ES 数据源，实现指标 + 日志联动查询

### 第三阶段：补链路追踪 + 事件驱动

**目标**：建立分布式链路追踪能力，引入事件驱动架构

**新增组件**：

| 组件 | 职责 |
|------|------|
| OpenTelemetry SDK | 统一埋点，自动注入 trace_id |
| Jaeger | Trace 存储、查询、展示 |
| Kafka | 事件总线（仅承载事件，不承载原始指标） |
| VictoriaMetrics | 指标长期存储，通过 Prometheus remote_write |
| alert-service | Kafka 消费 → 告警去重/聚合/状态管理/上下文增强 |

**改造内容**：
- Prometheus remote_write → VictoriaMetrics 长期存储
- 所有服务集成 OTel SDK，请求链路可视化
- Kafka 承载告警事件、操作事件等异步事件流
- 告警从"Prometheus 单一来源"扩展为"Kafka 事件 + Prometheus 规则"双通道
- **注意**：指标链路不变，仍为 probe → Prometheus → VictoriaMetrics，Kafka 不参与指标传输

### 第四阶段：智能化增强

**目标**：引入业务管理能力和智能运维

**新增组件**：

| 组件 | 职责 |
|------|------|
| MySQL | 用户/权限/配置/告警历史等业务数据 |
| ChatOps Agent | 告警摘要、日志关联、排障建议 |

**改造内容**：
- 用户登录 / JWT / RBAC
- 主机分组管理
- 告警规则配置管理（界面化）
- 通知渠道配置
- 告警历史归档
- MySQL 职责边界：存业务配置和管理数据，不存原始监控指标

---

## 五、数据存储策略

### 第一阶段

| 数据类型 | 存储位置 |
|---------|---------|
| CPU/内存/磁盘/网络指标 | Prometheus TSDB |
| 接口 QPS/耗时/错误率 | Prometheus TSDB |
| 服务健康状态 | Prometheus + Redis 缓存最新状态 |
| Dashboard 汇总数据 | Redis 缓存 |
| 告警实时事件 | AlertManager + Redis（active + events + Pub/Sub） |
| 告警历史记录 | 第四阶段再做 |
| 用户/权限/配置 | 第四阶段再做 |

### 第三阶段后

| 数据类型 | 存储位置 |
|---------|---------|
| 监控指标（长期） | VictoriaMetrics（通过 Prometheus remote_write） |
| 监控指标（短期） | Prometheus TSDB |
| 服务日志 | Elasticsearch |
| 链路数据 | Jaeger |
| 业务配置/用户 | MySQL |
| 热点缓存/实时状态 | Redis |
| 事件流 | Kafka（仅事件，不含原始指标） |

---

## 六、中间件选型依据

| 中间件 | 选型理由 | 替代方案 | 为什么不选替代 |
|--------|---------|---------|--------------|
| Kafka | 行业标准消息队列，大厂必备，Go 有 sarama/confluent-kafka-go | NATS | NATS 轻量但简历含金量低 |
| VictoriaMetrics | 兼容 Prometheus 协议，云原生监控领域最火 | Thanos | Thanos 组件太多（Sidecar+Store+Compactor+Query+MinIO），demo 承载不了 |
| Redis | 缓存绝对标准，go-redis 极其成熟 | — | 无可替代 |
| Elasticsearch | 日志检索绝对标准，倒排索引，面试高频 | Loki | Loki 轻量但市场占有率远不如 ES，面试深度不够 |
| Fluent Bit | CNCF 项目，C 编写极轻量，K8s 原生 DaemonSet | Filebeat | Filebeat 较重且非 CNCF，Fluent Bit 更云原生 |
| Jaeger | CNCF 毕业项目，链路追踪标准 | Zipkin | Zipkin 功能弱，社区活跃度低 |
| OpenTelemetry | CNFC 可观测性统一标准，统一 Metrics/Traces/Logs 埋点 | Jaeger SDK | Jaeger SDK 已逐步弃用，官方推荐 OTel |
| AlertManager | Prometheus 生态标配 | — | 无可替代 |
| etcd | 不引入 | — | K8s 底层已有 etcd，再部署一套是重复建设 |

---

## 七、微服务清单

### 第一阶段（2 个后端 + 1 个前端）

| 服务 | 职责 | 技术栈 | 部署形态 |
|------|------|--------|---------|
| server-probe | 主机指标采集 + 暴露 /metrics | Go + gopsutil + Prometheus client | DaemonSet（生产）/ Deployment（开发） |
| server-web | API + Redis 缓存 + AlertManager Webhook + WebSocket | Go + Gin + go-redis + gorilla/websocket | Deployment + HPA |
| frontend | 可视化大盘 + 告警面板 | Vue3 + ECharts + WebSocket | Deployment |

### 第三阶段后（3 个后端 + 1 个前端）

| 服务 | 职责 | 技术栈 |
|------|------|--------|
| server-probe | 采集指标 + /metrics 暴露 | Go + gopsutil + OTel |
| server-web | API + gRPC + Redis 缓存 + WebSocket | Go + Gin + gRPC + OTel |
| alert-service | Kafka 消费 → 告警去重/聚合/状态管理/上下文增强 | Go + Kafka Consumer + OTel |
| frontend | 可视化大盘 + 告警面板 | Vue3 + ECharts + WebSocket |

**注意**：第三阶段不再有 storage-service，因为指标链路走 Prometheus → remote_write → VictoriaMetrics，不需要 Kafka 消费者写 VM。

---

## 八、简历表述建议

### 项目描述

> 基于 Go + Prometheus + Grafana + AlertManager + Kubernetes 实现云原生可观测监控平台，支持主机资源指标采集、服务健康状态监控、告警规则触发、实时告警推送和可视化大盘展示。针对监控指标高频写入、按时间窗口聚合查询的特点，放弃传统 MySQL 指标落库方案，基于 Prometheus TSDB 实现指标采集、存储、PromQL 查询与告警规则计算，降低系统存储复杂度并提升云原生部署一致性。项目通过 Helm 部署到 Kubernetes 集群，并结合 Ingress、ConfigMap、Secret、HPA 完成云原生化改造。

### 项目亮点

```
1. 自研 server-probe Exporter 以 DaemonSet 部署采集每个 K8s Node 宿主机指标，
   基于 Prometheus TSDB 存储监控指标，避免将高频时序指标写入 MySQL，
   降低关系型数据库存储压力。

2. 基于 Prometheus Rules + AlertManager 实现指标告警闭环，
   通过 Webhook 接入 server-web，使用 Redis Pub/Sub 支持多副本 WebSocket 广播，
   并使用 WebSocket 向前端实时推送告警。

3. 封装 Prometheus HTTP API，基于 PromQL 模板实现指标查询白名单，
   避免前端直接透传任意 PromQL；
   使用 Redis 缓存主机最新状态和 Dashboard 聚合数据，
   减少前端高频刷新对后端和 Prometheus 查询接口的压力。

4. 使用 Helm 将后端、前端、Prometheus、Grafana、AlertManager
   部署到 Kubernetes，结合 Ingress 和 HPA 完成云原生化改造。
```

---

## 九、Kubernetes 部署架构

### 命名空间规划

```
monitoring          监控组件（Prometheus、Grafana、AlertManager）
server-monitor      应用服务（probe、web、frontend）
```

### Helm Chart 结构

```
server-monitor/
├── Chart.yaml
├── values.yaml
├── templates/
│   ├── server-probe/
│   │   ├── daemonset.yaml       DaemonSet 部署（生产模式）
│   │   ├── deployment.yaml      Deployment 部署（开发模式）
│   │   ├── service.yaml
│   │   └── configmap.yaml
│   ├── server-web/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── configmap.yaml
│   │   ├── secret.yaml
│   │   └── hpa.yaml
│   ├── frontend/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── configmap.yaml
│   ├── redis/
│   │   ├── deployment.yaml
│   │   └── service.yaml
│   ├── prometheus/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── configmap.yaml
│   │   └── rules-configmap.yaml
│   ├── grafana/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   ├── configmap.yaml
│   │   └── dashboards-configmap.yaml   Grafana Provisioning Dashboard JSON
│   ├── alertmanager/
│   │   ├── deployment.yaml
│   │   ├── service.yaml
│   │   └── configmap.yaml
│   ├── ingress.yaml
│   └── _helpers.tpl
└── values/
    ├── dev.yaml
    └── prod.yaml
```

---

## 十、CI/CD 流程

```
开发者 push 代码到 main 分支
        ↓
GitHub Actions 触发
        ↓
├── promtool check config  校验 Prometheus 配置
├── promtool check rules   校验告警规则
├── 构建 server-probe 镜像
├── 构建 server-web 镜像
├── 构建 frontend 镜像
└── 推送到 DockerHub
        ↓
手动/自动部署到 K8s
        ↓
helm upgrade --install server-monitor ./chart
```

---

## 十一、工程化规范

### 健康检查与优雅关闭

```
server-web:
- GET /healthz    存活检查
- GET /readyz     就绪检查（检查 Redis 和 Prometheus 连通性）
- 支持 graceful shutdown，关闭时主动断开 WebSocket 连接

server-probe:
- GET /healthz    存活检查
- GET /metrics    指标暴露 + 就绪检查
```

### 容器安全配置

```yaml
securityContext:
  runAsNonRoot: true
  readOnlyRootFilesystem: true
  allowPrivilegeEscalation: false
```

注意：server-probe 以 DaemonSet 部署并挂载宿主机 /proc、/sys 时，安全配置需单独处理，不能简单套用普通 Web 服务模板。

### Grafana Provisioning

Grafana 通过 provisioning 自动配置 Prometheus DataSource 和 Dashboard JSON，避免手动进入页面配置。Helm 部署后 Grafana 自动拥有数据源和大盘。

### Prometheus 配置校验

CI 流程中使用 promtool 校验配置：
- `promtool check config prometheus.yml`
- `promtool check rules alerts.yml`

### Makefile 命令

```makefile
make dev              本地开发启动
make test             运行测试
make docker-build     构建 Docker 镜像
make compose-up       Docker Compose 启动
make compose-down     Docker Compose 停止
make helm-install     Helm 部署到 K8s
make helm-uninstall   Helm 卸载
make promtool-check   校验 Prometheus 配置
```
