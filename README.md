# CloudOps

## 项目概述

CloudOps 的核心项目位于 `server-monitor/`，它是一套面向学习、演示和小规模实验环境的监控与智能运维平台，覆盖指标采集、告警管理、实时推送、可视化展示、Copilot 辅助诊断，以及 Docker Compose / Kubernetes / Helm 多种部署方式。

项目当前聚焦三条主线：

- 监控链路：`server-probe` 采集主机指标，Prometheus 负责抓取与查询，Alertmanager 负责告警路由，VictoriaMetrics 用于长期存储。
- 告警链路：`server-web` 接收告警 Webhook 并写入 Kafka，`alert-service` 消费事件后写入 Redis，再由前端与 WebSocket 消费。
- Copilot 链路：`server-web` 提供 NLU、Runbook 检索、诊断和 Kubernetes 工具入口，可按配置接入 LLM 与集群能力。

适用场景：

- 云原生监控体系的端到端学习与参考
- 本地开发、测试或演示环境的快速部署
- 中小规模主机监控、告警联动和 AI 辅助排障实践

## 架构

```mermaid
graph LR
    subgraph 采集层
        Probe[server-probe\n主机指标采集]
    end

    subgraph 监控与存储
        Prom[Prometheus\n采集与查询]
        AM[Alertmanager\n告警路由]
        VM[VictoriaMetrics\n长期存储]
    end

    subgraph 应用层
        Web[server-web\nAPI + WebSocket + Copilot]
        Redis[(Redis)]
        MySQL[(MySQL)]
        Runbook[Runbooks]
        LLM[LLM / AI Provider]
        K8S[Kubernetes API]
    end

    subgraph 事件流转
        Kafka{{Kafka}}
        AlertSvc[alert-service\n事件消费]
    end

    subgraph 展示层
        FE[Frontend]
        Grafana[Grafana]
    end

    subgraph 可观测性
        FB[Fluent Bit]
        ES[(Elasticsearch)]
        Kibana[Kibana]
        Jaeger[Jaeger]
    end

    Probe -->|metrics| Prom
    Prom -->|query| Web
    Prom -->|remote_write| VM
    Prom -->|alert| AM
    AM -->|webhook| Web
    Web -->|produce| Kafka
    Kafka -->|consume| AlertSvc
    AlertSvc -->|read/write| Redis
    Web --> MySQL
    Web --> Redis
    Web --> Runbook
    Web -. optional .-> LLM
    Web -. optional .-> K8S
    Web -->|HTTP / WS| FE
    Prom --> Grafana
    Probe --> FB
    Web --> FB
    AlertSvc --> FB
    FB --> ES
    ES --> Kibana
    Probe --> Jaeger
    Web --> Jaeger
    AlertSvc --> Jaeger
```

## 项目目录

```text
server-monitor/
├── alert-service/            # 告警事件消费服务
├── charts/server-monitor/    # Helm Chart
├── docker/                   # Compose、Prometheus、Grafana、Jaeger、Fluent Bit 等配置
├── docs/                     # 说明文档与辅助资料
├── frontend/                 # 前端工程
├── k8s/                      # 原始 Kubernetes 清单
├── pkg/                      # 共享 Go 包
├── runbooks/                 # Runbook 知识库
├── scripts/                  # 辅助脚本
├── server-probe/             # 监控探针
├── server-web/               # Web API、WebSocket、Copilot 服务
├── .env.example              # 本地配置参考
├── docker-compose.yml        # 本地编排入口
└── Makefile                  # 构建、测试、部署命令入口
```

补充说明：

- `server-web/internal/` 包含配置、路由、handler、service、infra 和 Copilot 相关实现。
- `docker/` 目录维护 Prometheus、AlertManager、Grafana、Jaeger、Fluent Bit、Elasticsearch 初始化等运行时配置。
- `charts/server-monitor/` 与 `k8s/` 分别提供 Helm 和原始清单两套集群部署入口。

## 快速开始

### 本地 Compose

```bash
cd server-monitor
make env-init
make compose-up
```

启动后可访问：

| 服务 | 地址 |
|------|------|
| 监控大屏 | http://localhost:8080 |
| Grafana | http://localhost:3000 |
| Prometheus | http://localhost:9090 |
| Alertmanager | http://localhost:9093 |
| Kibana | http://localhost:5601 |
| Jaeger | http://localhost:16686 |

默认登录信息：

| 服务 | 用户名 | 密码 |
|------|--------|------|
| 监控大屏 | `admin` | `server-monitor-local-admin` |
| Grafana | `admin` | `server-monitor-local-grafana` |

说明：

- `.env.example` 提供完整本地配置参考，复制后按需修改 `.env` 即可。
- 如果需要启用 LLM 或 Kubernetes 集成，请直接编辑 `.env`，具体项以 `.env.example` 为准。

### 本地开发

```bash
cd server-monitor
make frontend-install
make dev-deps
make dev-web
```

前端开发可另开一个终端：

```bash
cd server-monitor
make dev-frontend
```

开发模式约定：

- `make dev-deps` 启动 Redis、MySQL、Kafka、Prometheus、Alertmanager、Grafana、Jaeger 和 `server-probe`。
- `make dev-web` 本地运行 `server-web`，使用适配本机端口的默认开发配置。
- `make dev-alert-service` 可单独在本机启动告警消费服务。
- `make dev-stop` 停止 Compose 依赖服务。

### Helm / Kubernetes 部署

Helm 部署：

```bash
cd server-monitor
make helm-lint
make deploy-helm HELM_RELEASE=server-monitor KUBE_NAMESPACE=server-monitor
```

如果需要覆盖镜像、Secret 或其他 values，可追加 `HELM_SET_ARGS` 或直接替换 `HELM_VALUES`：

```bash
make deploy-helm \
  HELM_RELEASE=server-monitor \
  KUBE_NAMESPACE=server-monitor \
  HELM_SET_ARGS="--set serverWeb.image.repository=<repo>/server-web --set serverWeb.image.digest=sha256:<digest>"
```

生产发布应为三个应用镜像分别提供不可变 `sha256` digest；`image.tag` 仅作为本地兼容回退。Chart 的 `values.schema.json` 会拒绝格式无效的 digest。

原始清单部署：

```bash
cd server-monitor
make deploy-k8s
```

### 本地启用 K8s 集成（可选）

```bash
cd server-monitor
make k8s-setup
```

这会创建/检查 kind 集群、准备测试资源并生成本机 `docker/kubeconfig`。该文件含本地集群凭据、已被 Git 忽略，不得提交。完成后按 `.env.example` 调整 `.env` 中的 K8s 相关开关，再重新启动 `server-web` 或整套 Compose 服务。

## Makefile 常用命令

以下命令都在 `server-monitor/` 目录执行。

### 构建

| 命令 | 说明 |
|------|------|
| `make build` | 构建所有 Go 服务和前端资源 |
| `make build-go` | 构建所有 Go 服务 |
| `make build-server-probe` | 构建 `server-probe` |
| `make build-server-web` | 构建 `server-web` |
| `make build-alert-service` | 构建 `alert-service` |
| `make build-frontend` | 构建前端资源 |

### 测试与校验

| 命令 | 说明 |
|------|------|
| `make test` | 运行 Go 测试和前端类型检查 |
| `make test-go` | 运行所有 Go 模块测试 |
| `make test-k8s` | 运行 `server-web` 中的 K8s 相关测试 |
| `make test-frontend` | 运行前端类型检查 |
| `make vet` | 运行所有 Go vet |
| `make lint` | 运行 Go lint 和前端 ESLint |
| `make check-goimports` | 检查 Go imports 是否规范 |
| `make fmt` | 使用 `go fmt` 格式化 Go 代码 |
| `make fmt-goimports` | 使用 `goimports` 格式化 Go 代码 |
| `make k8s-script-check` | 检查 `docker/setup-k8s.sh` 语法 |
| `make check` | 本地完整校验入口 |
| `make ci` | 对齐 CI 的本地校验入口 |

### 开发运行

| 命令 | 说明 |
|------|------|
| `make frontend-install` | 安装前端依赖 |
| `make dev-deps` | 启动本地开发依赖 |
| `make dev-web` | 本地运行 `server-web` |
| `make dev-probe` | 本地运行 `server-probe` |
| `make dev-alert-service` | 本地运行 `alert-service` |
| `make dev-frontend` | 启动前端开发服务器 |
| `make dev-stop` | 停止本地开发依赖 |

### Compose / 部署

| 命令 | 说明 |
|------|------|
| `make compose-config` | 展开并检查 Compose 配置 |
| `make compose-build` | 构建 Compose 镜像 |
| `make compose-up` | 启动 Compose 服务 |
| `make compose-down` | 停止 Compose 服务 |
| `make compose-ps` | 查看 Compose 服务状态 |
| `make compose-logs LOGS_SERVICE=server-web` | 查看日志 |
| `make compose-clean` | 停止并清理 Compose 数据卷 |
| `make helm-lint` | 校验 Helm Chart |
| `make helm-template` | 渲染 Helm 模板 |
| `make deploy-helm` | 使用 Helm 部署/升级 |
| `make undeploy-helm` | 卸载 Helm Release |
| `make deploy-k8s` | 使用原始清单部署 |
| `make undeploy-k8s` | 删除原始清单 |
| `make k8s-setup` | 准备本地 kind 集成环境 |
| `make k8s-teardown` | 删除本地 kind 集群 |
| `make clean` | 清理本地产物 |

兼容说明：旧的 `make docker-up`、`make docker-down`、`make docker-logs`、`make ci-check` 等命令仍可用，但 README 统一以新的标准命名为准。

## 配置与部署文件

| 路径 | 用途 |
|------|------|
| `server-monitor/.env.example` | 本地配置参考 |
| `server-monitor/docker-compose.yml` | 本地 Compose 编排入口 |
| `server-monitor/docker/` | Prometheus、Alertmanager、Grafana、Jaeger、Fluent Bit、Elasticsearch 初始化等配置 |
| `server-monitor/charts/server-monitor/values.yaml` | Helm 默认 values |
| `server-monitor/k8s/` | 原始 Kubernetes 清单 |
| `server-monitor/docker/setup-k8s.sh` | 本地 kind 集成环境准备脚本 |

## 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| `server-web` | `8080` | API、WebSocket、内置静态资源 |
| `alert-service` | `8081` | 告警事件消费服务 |
| `server-probe` | `8082 -> 9090` | Prometheus 指标出口 |
| `Grafana` | `3000` | 监控大盘 |
| `Redis` | `6379` | 缓存与 Pub/Sub |
| `VictoriaMetrics` | `8428` | 长期指标存储 |
| `Prometheus` | `9090` | 指标查询与规则加载 |
| `Kafka` | `9092` | 事件总线 |
| `Alertmanager` | `9093` | 告警管理 |
| `Jaeger` | `16686` | 链路追踪 UI |
| `Elasticsearch` | `9200` | 日志存储 |
| `Kibana` | `5601` | 日志查询 |

说明：Compose 场景下 `server-probe` 通过宿主机 `8082` 映射到容器内 `9090`；如果直接执行 `make dev-probe`，默认监听 `http://localhost:9090`。

## CI

CI 定义位于 `.github/workflows/ci.yaml`，所有外部 Actions 均固定到经过官方仓库 release 核验的 40 位 commit SHA。当前流程覆盖：

- Go 模块矩阵检查：`goimports`、`golangci-lint`、uncached `go test`、race、`go vet`、`go build`
- `server-web` 的 NLU / RAG / Multi-intent 评估
- 前端 clean install、ESLint、strict typecheck、unit test 与 production build
- Compose、Shell、Workflow YAML/actionlint、Kubernetes YAML/kubeconform 校验
- Prometheus 规则校验
- Helm values schema、lint、template 与渲染清单校验
- Docker 镜像构建、OCI label、漏洞扫描和 SBOM 校验
- DockerHub commit-SHA 镜像、exact-digest Trivy/SBOM、provenance/SBOM attestations、OIDC keyless signing 与严格 Cosign verify（`push-images`，仅 pushed `v*` tag 且通过 `production` environment 后触发）
- 三服务验证通过后生成单一原子 digest manifest；可选 Helm 部署仅消费该 manifest 中的 repository 和 digest（`deploy`，仅 `v*` tag、`vars.DEPLOY_ENABLED == 'true'` 且受保护 `production` environment 通过后触发）

非生产 hosted supply-chain 验证定义于 `.github/workflows/hosted-supply-chain-validation.yaml`。它只有无输入的 `workflow_dispatch` 入口，并在 job 层拒绝非 `refs/heads/main` 的执行。该路径使用 `GITHUB_TOKEN` 写入独立的 `ghcr.io/<owner>/cloudops-copilot-validation-<service>` package，不读取 DockerHub、Kubernetes、Argo CD 或 production secrets，也没有 Helm/Kubernetes deploy job。

每个 validation tag 都包含 exact Git SHA、workflow run ID 和 run attempt。签名与验证只使用 Registry digest；验证同时限制 GitHub Actions issuer、repository、workflow 文件、workflow name、`refs/heads/main`、Git SHA 和 `workflow_dispatch` event，并导出 workflow metadata、OCI labels、SBOM/Trivy hashes、provenance/SBOM bundle、Cosign certificate claims 和 Rekor evidence。临时 GHCR package version 在证据生成后 best-effort 删除；清理结果单独写入 artifact，清理失败不会覆盖主 sign/verify Gate 的结果。GHCR 的未标记 referrer/retention 行为仍应由专用 validation package 的 retention policy 兜底。

该 hosted validation workflow 目前只完成本地静态实现与检查，尚未在 GitHub-hosted runner 上执行。运行前需要确认仓库允许 Actions 写入/删除上述专用 GHCR package，并保留 `packages: write`、`attestations: write` 与 `id-token: write` 的最小 job 权限；不得把 validation package 改为正式 release repository。
