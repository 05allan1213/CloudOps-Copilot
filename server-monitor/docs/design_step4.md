# 第四阶段：业务管理增强 — 详细实现方案

## 一、阶段目标

引入 MySQL 存储业务配置和管理数据，实现 **用户认证 / 主机分组 / 告警规则管理 / 通知渠道配置 / 告警历史归档** 的完整业务闭环。

### 阶段边界

第四阶段在第三阶段链路追踪 + 事件驱动基础上，补齐业务管理能力。

**第四阶段必须完成**：

- MySQL 引入，存储用户/权限/配置/告警历史等业务数据
- 用户注册 / 登录 / JWT 认证
- RBAC 角色权限控制（admin / viewer）
- 主机分组管理
- 告警规则配置管理（界面化 CRUD）
- 通知渠道配置（Webhook URL 管理）
- 告警历史归档（从 Redis 迁移到 MySQL 持久化）
- 前端增加登录页、设置页、告警规则管理页

**第四阶段只预留，不实现**：

- ChatOps Agent / AI 分析
- SSO / OAuth2 第三方登录
- 操作审计日志
- 告警静默 / 抑制规则界面化
- 多租户隔离

**第四阶段不做**：

- 原始监控指标存入 MySQL（指标链路仍走 Prometheus → VictoriaMetrics）
- 替换 Redis 缓存/Pub/Sub 为 MySQL
- 替换 Kafka 事件链路为 MySQL
- 告警通知发送（仅配置通知渠道，不实现实际发送逻辑）

### 认证兼容性策略

引入认证后，现有 API 行为将发生变化。具体策略如下：

**公开接口（无需认证）**：

| 接口                                  | 说明                        |
| ----------------------------------- | ------------------------- |
| `GET /healthz`                      | 存活检查                      |
| `GET /readyz`                       | 就绪检查                      |
| `GET /metrics`                      | Prometheus 采集             |
| `POST /api/v1/auth/login`           | 登录                        |
| `POST /api/v1/webhook/alertmanager` | AlertManager 回调（通过其他机制鉴权） |

**需认证的业务接口**：

| 接口                                    | 说明           |
| ------------------------------------- | ------------ |
| `GET /api/v1/hosts`                   | 主机列表         |
| `GET /api/v1/hosts/:instance/metrics` | 主机指标         |
| `GET /api/v1/dashboard/overview`      | 大盘概览         |
| `GET /api/v1/alerts/active`           | 活跃告警         |
| `GET /api/v1/alerts/events`           | 告警事件         |
| `WS /ws/alerts`                       | WebSocket 推送 |
| 所有管理 API                              | CRUD 操作      |

**AUTH\_ENABLED 开关**：

- 环境变量 `AUTH_ENABLED`：默认 `true`
- 设置为 `false` 时，所有业务接口无需认证即可访问（仅限本地开发/演示环境）
- 生产环境**必须**开启认证，`AUTH_ENABLED=false` 在生产环境启动时打印警告日志
- Docker Compose 本地开发默认 `AUTH_ENABLED=false`，Helm 生产部署默认 `AUTH_ENABLED=true`

### 阶段内实施拆分

| 模块             | 内容                                                | 完成标准                         |
| -------------- | ------------------------------------------------- | ---------------------------- |
| 4.1 MySQL 基础设施 | Docker Compose 和 Helm 部署 MySQL，server-web 集成 GORM | MySQL 可连接，GORM 自动迁移建表        |
| 4.2 用户认证       | 用户模型、注册/登录 API、JWT 中间件、初始管理员创建                    | 登录返回 JWT，受保护接口需携带 Token      |
| 4.3 RBAC 权限    | 角色（admin/viewer）和权限中间件、AUTH\_ENABLED 开关           | admin 可管理，viewer 只读          |
| 4.4 主机分组       | 主机分组模型、CRUD API、主机-分组关联                           | 主机可按分组筛选                     |
| 4.5 告警历史归档     | 告警历史模型、双写归档、分页查询 API                              | 告警事件持久化到 MySQL，可按时间/状态查询     |
| 4.6 告警规则管理     | 告警规则模型、CRUD API（仅保存到 MySQL）                       | 前端可增删改告警规则                   |
| 4.7 告警规则同步     | 规则渲染为 YAML、promtool 校验、写入 Prometheus、reload       | Prometheus 自动加载新规则，同步失败保留上一版 |
| 4.8 通知渠道配置     | 通知渠道模型、CRUD API、SSRF 防护的连通性测试                     | 可配置 Webhook 通知地址             |
| 4.9 前端改造       | 登录页、设置页、告警规则管理页、分组筛选                              | 前端完整业务闭环                     |

每个模块完成后单独格式化、测试、验证和提交，不把代码改造、部署改造、前端改造混在同一个提交里。

### 完成标志

- [ ] MySQL 部署成功，server-web 通过 GORM 连接并自动迁移
- [ ] 用户注册 / 登录 API 可用，返回 JWT Token
- [ ] 受保护 API 需携带有效 JWT Token 才能访问（AUTH\_ENABLED=true 时）
- [ ] AUTH\_ENABLED=false 时所有业务接口无需认证
- [ ] RBAC 权限中间件生效，admin 可管理，viewer 只读
- [ ] 主机分组 CRUD API 可用，主机列表可按分组筛选
- [ ] 告警规则 CRUD API 可用（仅保存到 MySQL）
- [ ] 告警规则同步到 Prometheus 后生效，同步失败保留上一版规则
- [ ] 通知渠道 CRUD API 可用，连通性测试有 SSRF 防护
- [ ] 告警历史双写归档到 MySQL，可按时间/状态/分组查询
- [ ] 重复 AlertManager webhook 投递不产生重复历史记录
- [ ] 前端登录页可用，未登录自动跳转
- [ ] 前端设置页可管理告警规则和通知渠道
- [ ] Docker Compose 包含 MySQL
- [ ] Helm Chart 包含 MySQL 部署
- [ ] MySQL 职责边界明确：只存业务数据，不存原始监控指标
- [ ] `/healthz`、`/readyz`、`/metrics` 无需认证即可访问
- [ ] `/readyz` 不检查 MySQL 连通性
- [ ] `JWT_SECRET` 为空或过短时 server-web 启动失败

***

## 二、技术栈

```
数据库：   MySQL 8.0 + GORM
认证：     JWT (golang-jwt/jwt/v5)
权限：     RBAC（admin / viewer 两角色）
密码：     bcrypt (golang.org/x/crypto/bcrypt)
部署：     Docker Compose（本地开发）+ Helm Chart（K8s 生产）
```

### 新增中间件

| 组件    | 版本  | 职责                 | 部署形态            |
| ----- | --- | ------------------ | --------------- |
| MySQL | 8.0 | 用户/权限/配置/告警历史等业务数据 | 单节点 StatefulSet |

### 新增 Go 依赖

| 依赖                           | 用途          | 引入服务       |
| ---------------------------- | ----------- | ---------- |
| gorm.io/gorm                 | Go ORM      | server-web |
| gorm.io/driver/mysql         | MySQL 驱动    | server-web |
| github.com/golang-jwt/jwt/v5 | JWT 生成与验证   | server-web |
| golang.org/x/crypto          | bcrypt 密码哈希 | server-web |

### 不引入的组件

| 组件            | 原因                                     |
| ------------- | -------------------------------------- |
| Casbin        | RBAC 场景简单（仅两角色），GORM + 中间件足够           |
| OAuth2 / OIDC | 第四阶段只做本地认证，SSO 预留但不实现                  |
| Redis 替换      | Redis 仍负责缓存/Pub/Sub/限流，MySQL 不替代 Redis |
| ChatOps Agent | 用户明确要求暂不引入                             |

***

## 三、架构设计

```
┌──────────────────────────────────────────────────────────────────────────┐
│                        Kubernetes Cluster                                 │
│                                                                           │
│  ┌──────────────────────────────────────────────────────────────────┐    │
│  │ server-web (Deployment)                                          │    │
│  │                                                                   │    │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌───────────┐ │    │
│  │  │ Prometheus │  │   Redis    │  │   MySQL    │  │   Kafka   │ │    │
│  │  │  Client    │  │  Client    │  │  (GORM)    │  │  Producer │ │    │
│  │  └────────────┘  └────────────┘  └────────────┘  └───────────┘ │    │
│  │                                                                   │    │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌───────────┐ │    │
│  │  │  JWT Auth  │  │   RBAC    │  │  WebSocket │  │  Webhook  │ │    │
│  │  │ Middleware │  │ Middleware│  │    Hub     │  │  Handler  │ │    │
│  │  └────────────┘  └────────────┘  └────────────┘  └───────────┘ │    │
│  └──────────────────────────────────────────────────────────────────┘    │
│                                                                           │
│  ┌──────────────────┐                                                    │
│  │ MySQL             │  用户表 / 角色表 / 主机分组表 / 告警规则表        │
│  │ (StatefulSet)     │  通知渠道表 / 告警历史表                          │
│  └──────────────────┘                                                    │
│                                                                           │
│  ┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐     │
│  │ Redis            │   │ Prometheus       │   │ VictoriaMetrics  │     │
│  │ 缓存/限流/PubSub │   │ 指标采集+规则计算 │   │ 长期存储         │     │
│  │ 告警实时状态     │   │                  │   │                  │     │
│  └──────────────────┘   └──────────────────┘   └──────────────────┘     │
│                                                                           │
│  ┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐     │
│  │ Kafka            │   │ alert-service    │   │ Jaeger           │     │
│  │ 事件总线         │   │ 告警去重/聚合    │   │ 链路追踪         │     │
│  └──────────────────┘   └──────────────────┘   └──────────────────┘     │
│                                                                           │
│  ┌──────────────────┐   ┌──────────────────┐   ┌──────────────────┐     │
│  │ Elasticsearch    │   │ Grafana          │   │ frontend         │     │
│  │ 日志存储         │   │ 可视化大盘       │   │ Vue3+ECharts     │     │
│  └──────────────────┘   └──────────────────┘   └──────────────────┘     │
└──────────────────────────────────────────────────────────────────────────┘
```

### 关键架构决策

#### MySQL 职责边界

| 数据类型       | 存储位置                         | 说明              |
| ---------- | ---------------------------- | --------------- |
| 用户/角色/权限   | **MySQL**                    | 业务数据，需要事务和关系查询  |
| 主机分组/标签    | **MySQL**                    | 业务配置，需要持久化      |
| 告警规则配置     | **MySQL**                    | 业务配置，CRUD 管理    |
| 通知渠道配置     | **MySQL**                    | 业务配置，CRUD 管理    |
| 告警历史记录     | **MySQL**                    | 业务数据，需要持久化和复杂查询 |
| 原始监控指标     | Prometheus / VictoriaMetrics | **不变**，不存 MySQL |
| 缓存/限流/实时状态 | Redis                        | **不变**，不存 MySQL |
| 事件流        | Kafka                        | **不变**，不存 MySQL |
| 日志         | Elasticsearch                | **不变**，不存 MySQL |
| Trace      | Jaeger                       | **不变**，不存 MySQL |

#### 为什么选择 JWT 而不是 Session

| 维度     | JWT                        | Session               |
| ------ | -------------------------- | --------------------- |
| 无状态    | **服务端不存储会话，适合微服务**         | 需要服务端存储会话             |
| 水平扩展   | **多副本无需共享会话存储**            | 需要共享 Session 存储       |
| K8s 友好 | **Pod 无状态，HPA 无障碍**        | 需要额外 Redis 存储 Session |
| 跨域     | **天然支持跨域**                 | 需要额外配置                |
| 安全性    | Token 有效期短 + Refresh Token | 依赖 Cookie 安全配置        |

#### 为什么选择 RBAC 而不是 ABAC

| 维度   | RBAC             | ABAC              |
| ---- | ---------------- | ----------------- |
| 复杂度  | **简单，两角色即可满足需求** | 复杂，需要策略引擎         |
| 面试价值 | **经典权限模型，面试高频**  | 较少被问              |
| 实现成本 | **低，GORM + 中间件** | 高，需要 Casbin 或 OPA |

#### 为什么告警规则管理不直接修改 Prometheus ConfigMap

Prometheus 的 `rule_files` 支持从文件加载规则，但修改 ConfigMap 后需要 Prometheus 重新加载。第四阶段采用以下方案：

1. 告警规则存储在 MySQL 中（CRUD 管理）
2. server-web 提供规则同步 API，将 MySQL 中的规则渲染为 Prometheus rules YAML
3. 通过 Kubernetes API 更新 Prometheus rules ConfigMap
4. Prometheus 通过 `--web.enable-lifecycle` 支持热加载（`POST /-/reload`）

在 Docker Compose 环境下，直接写入本地 rules 文件并调用 Prometheus reload API。

***

## 四、MySQL 集成

### 4.1 数据库设计

#### 用户表 (users)

```sql
CREATE TABLE users (
    id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    username   VARCHAR(64)  NOT NULL UNIQUE,
    password   VARCHAR(255) NOT NULL,
    role       VARCHAR(32)  NOT NULL DEFAULT 'viewer',
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_username (username)
);
```

#### 主机分组表 (host\_groups)

```sql
CREATE TABLE host_groups (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(128) NOT NULL UNIQUE,
    description VARCHAR(512) NOT NULL DEFAULT '',
    created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_name (name)
);
```

#### 主机-分组关联表 (host\_group\_members)

```sql
CREATE TABLE host_group_members (
    id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    group_id   BIGINT UNSIGNED NOT NULL,
    instance   VARCHAR(256)    NOT NULL,
    created_at DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_group_instance (group_id, instance),
    INDEX idx_instance (instance),
    FOREIGN KEY (group_id) REFERENCES host_groups(id) ON DELETE CASCADE
);
```

#### 告警规则表 (alert\_rules)

```sql
CREATE TABLE alert_rules (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name        VARCHAR(128) NOT NULL UNIQUE,
    expr        TEXT         NOT NULL,
    duration    VARCHAR(32)  NOT NULL DEFAULT '2m',
    severity    VARCHAR(32)  NOT NULL DEFAULT 'warning',
    summary     VARCHAR(512) NOT NULL DEFAULT '',
    description TEXT         NOT NULL DEFAULT '',
    enabled     TINYINT(1)   NOT NULL DEFAULT 1,
    created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_name (name),
    INDEX idx_enabled (enabled)
);
```

#### 通知渠道表 (notification\_channels)

```sql
CREATE TABLE notification_channels (
    id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(128) NOT NULL UNIQUE,
    type       VARCHAR(32)  NOT NULL DEFAULT 'webhook',
    url        VARCHAR(512) NOT NULL DEFAULT '',
    enabled    TINYINT(1)   NOT NULL DEFAULT 1,
    created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_name (name),
    INDEX idx_type (type)
);
```

#### 告警历史表 (alert\_histories)

```sql
CREATE TABLE alert_histories (
    id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    fingerprint  VARCHAR(64)  NOT NULL DEFAULT '',
    alert_name   VARCHAR(128) NOT NULL DEFAULT '',
    instance     VARCHAR(256) NOT NULL DEFAULT '',
    severity     VARCHAR(32)  NOT NULL DEFAULT 'warning',
    status       VARCHAR(32)  NOT NULL DEFAULT 'firing',
    summary      VARCHAR(512) NOT NULL DEFAULT '',
    labels_json  TEXT         NOT NULL DEFAULT '{}',
    fired_at     DATETIME     NOT NULL,
    resolved_at  DATETIME     NULL,
    created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_fingerprint (fingerprint),
    INDEX idx_alert_name (alert_name),
    INDEX idx_status (status),
    INDEX idx_fired_at (fired_at),
    INDEX idx_severity (severity)
);
```

### 4.2 GORM 模型设计

```go
// model/user.go
type User struct {
    ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
    Username  string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"username"`
    Password  string    `gorm:"type:varchar(255);not null" json:"-"`
    Role      string    `gorm:"type:varchar(32);not null;default:viewer" json:"role"`
    CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// model/host_group.go
type HostGroup struct {
    ID          uint64            `gorm:"primaryKey;autoIncrement" json:"id"`
    Name        string            `gorm:"type:varchar(128);uniqueIndex;not null" json:"name"`
    Description string            `gorm:"type:varchar(512);not null;default:''" json:"description"`
    Members     []HostGroupMember `gorm:"foreignKey:GroupID" json:"members,omitempty"`
    CreatedAt   time.Time         `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt   time.Time         `gorm:"autoUpdateTime" json:"updated_at"`
}

type HostGroupMember struct {
    ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
    GroupID   uint64    `gorm:"uniqueIndex:uk_group_instance;not null" json:"group_id"`
    Instance  string    `gorm:"type:varchar(256);uniqueIndex:uk_group_instance;not null" json:"instance"`
    CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// model/alert_rule.go
type AlertRule struct {
    ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
    Name        string    `gorm:"type:varchar(128);uniqueIndex;not null" json:"name"`
    Expr        string    `gorm:"type:text;not null" json:"expr"`
    Duration    string    `gorm:"type:varchar(32);not null;default:2m" json:"duration"`
    Severity    string    `gorm:"type:varchar(32);not null;default:warning" json:"severity"`
    Summary     string    `gorm:"type:varchar(512);not null;default:''" json:"summary"`
    Description string    `gorm:"type:text;not null;default:''" json:"description"`
    Enabled     bool      `gorm:"not null;default:true" json:"enabled"`
    CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// model/notification_channel.go
type NotificationChannel struct {
    ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
    Name      string    `gorm:"type:varchar(128);uniqueIndex;not null" json:"name"`
    Type      string    `gorm:"type:varchar(32);not null;default:webhook" json:"type"`
    URL       string    `gorm:"type:varchar(512);not null;default:''" json:"url"`
    Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
    CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// model/alert_history.go
type AlertHistory struct {
    ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
    Fingerprint string    `gorm:"type:varchar(64);index;not null;default:''" json:"fingerprint"`
    AlertName   string    `gorm:"type:varchar(128);index;not null;default:''" json:"alert_name"`
    Instance    string    `gorm:"type:varchar(256);not null;default:''" json:"instance"`
    Severity    string    `gorm:"type:varchar(32);index;not null;default:warning" json:"severity"`
    Status      string    `gorm:"type:varchar(32);index;not null;default:firing" json:"status"`
    Summary     string    `gorm:"type:varchar(512);not null;default:''" json:"summary"`
    LabelsJSON  string    `gorm:"type:text;not null;default:{}" json:"labels_json"`
    FiredAt     time.Time `gorm:"not null;index" json:"fired_at"`
    ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
    CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
}
```

### 4.3 server-web 目录结构新增

```
server-web/
├── main.go                 入口 + graceful shutdown（新增 MySQL 初始化）
├── api/
│   ├── router.go           路由注册（新增认证/管理路由）
│   ├── handlers/
│   │   ├── handlers.go     现有 handler（不变）
│   │   ├── auth.go         登录/注册 API
│   │   ├── users.go        用户管理 API
│   │   ├── host_groups.go  主机分组 CRUD API
│   │   ├── alert_rules.go  告警规则 CRUD API
│   │   ├── channels.go     通知渠道 CRUD API
│   │   └── alert_history.go 告警历史查询 API
│   └── middleware/
│       ├── auth.go         JWT 认证中间件
│       ├── rbac.go         RBAC 权限中间件
│       └── ... (现有中间件不变)
├── model/
│   ├── user.go             User GORM 模型
│   ├── host_group.go       HostGroup / HostGroupMember 模型
│   ├── alert_rule.go       AlertRule 模型
│   ├── notification_channel.go  NotificationChannel 模型
│   └── alert_history.go    AlertHistory 模型
├── database/
│   └── mysql.go            MySQL 连接管理 + 自动迁移
├── config/
│   └── config.go           配置（新增 MySQL 相关配置）
├── ... (现有目录不变)
```

### 4.4 配置新增 (config/config.go)

```go
type Config struct {
    // ... 现有字段 ...

    MySQLHost      string
    MySQLPort      int
    MySQLUser      string
    MySQLPassword  string
    MySQLDatabase  string
    JWTSecret      string
    JWTExpireHours int
    AuthEnabled    bool
    AdminPassword  string
}

func Load() *Config {
    return &Config{
        // ... 现有配置 ...
        MySQLHost:      getEnv("MYSQL_HOST", "mysql"),
        MySQLPort:      getEnvInt("MYSQL_PORT", 3306),
        MySQLUser:      getEnv("MYSQL_USER", "server_monitor"),
        MySQLPassword:  getEnv("MYSQL_PASSWORD", ""),
        MySQLDatabase:  getEnv("MYSQL_DATABASE", "server_monitor"),
        JWTSecret:      getEnv("JWT_SECRET", ""),
        JWTExpireHours: getEnvInt("JWT_EXPIRE_HOURS", 24),
        AuthEnabled:    getEnvBool("AUTH_ENABLED", true),
        AdminPassword:  getEnv("ADMIN_PASSWORD", ""),
    }
}

func (c *Config) Validate() error {
    if c.AuthEnabled && len(c.JWTSecret) < 32 {
        return fmt.Errorf("JWT_SECRET must be at least 32 bytes when auth is enabled, got %d", len(c.JWTSecret))
    }
    return nil
}
```

### 4.5 MySQL 连接管理 (database/mysql.go)

```go
package database

import (
    "fmt"
    "time"

    "gorm.io/driver/mysql"
    "gorm.io/gorm"
    "gorm.io/gorm/logger"
    "go.uber.org/zap"

    "server-web/model"
)

func Init(host string, port int, user, password, database string) (*gorm.DB, error) {
    dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=UTC",
        user, password, host, port, database,
    )

    db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
        Logger: logger.Default.LogMode(logger.Info),
    })
    if err != nil {
        return nil, fmt.Errorf("connect mysql: %w", err)
    }

    sqlDB, err := db.DB()
    if err != nil {
        return nil, fmt.Errorf("get sql.DB: %w", err)
    }

    sqlDB.SetMaxOpenConns(25)
    sqlDB.SetMaxIdleConns(10)
    sqlDB.SetConnMaxLifetime(5 * time.Minute)

    if err := autoMigrate(db); err != nil {
        return nil, fmt.Errorf("auto migrate: %w", err)
    }

    return db, nil
}

func autoMigrate(db *gorm.DB) error {
    return db.AutoMigrate(
        &model.User{},
        &model.HostGroup{},
        &model.HostGroupMember{},
        &model.AlertRule{},
        &model.NotificationChannel{},
        &model.AlertHistory{},
    )
}
```

### 4.6 自动迁移策略

GORM AutoMigrate 用于本地开发和早期学习项目，生产环境需谨慎：

**开发环境（Docker Compose）**：

- 使用 GORM AutoMigrate 自动建表和加字段
- 启动时自动执行，无需手动操作

**生产环境（K8s / Helm）**：

- AutoMigrate 只做兼容性迁移（加字段、加索引），不自动删除字段或索引
- 每次表结构变更应有可审查的 SQL（在代码注释或 migration 文件中记录）
- 后续如需更严格的迁移管理，可引入 golang-migrate 等工具

***

## 五、用户认证设计

### 5.1 JWT 认证流程

```
┌──────────┐     POST /api/v1/auth/login     ┌──────────┐
│  前端     │ ──────────────────────────────▶ │server-web│
│          │     {username, password}          │          │
│          │ ◀────────────────────────────── │          │
│          │     {token, user}                │          │
└──────────┘                                   └──────────┘

┌──────────┐     GET /api/v1/hosts            ┌──────────┐
│  前端     │ ──────────────────────────────▶ │server-web│
│          │     Authorization: Bearer <JWT>   │          │
│          │ ◀────────────────────────────── │          │
│          │     {hosts}                       │          │
└──────────┘                                   └──────────┘
```

### 5.2 JWT Token 结构

```json
{
  "sub": "1",
  "username": "admin",
  "role": "admin",
  "iat": 1714032000,
  "exp": 1714118400
}
```

### 5.3 认证 API 设计

#### 用户注册

```
POST /api/v1/auth/register

Request:
{
    "username": "admin",
    "password": "password123",
    "role": "admin"
}

Response (201):
{
    "status": "success",
    "data": {
        "id": 1,
        "username": "admin",
        "role": "admin"
    }
}
```

**约束**：

- 用户名 3-64 字符，只允许字母数字下划线
- 密码至少 8 字符
- 注册接口仅 admin 可调用
- 首个用户可通过环境变量 `ADMIN_PASSWORD` 自动创建（启动时检测）

#### 用户登录

```
POST /api/v1/auth/login

Request:
{
    "username": "admin",
    "password": "password123"
}

Response (200):
{
    "status": "success",
    "data": {
        "token": "eyJhbGciOiJIUzI1NiIs...",
        "expires_at": "2024-04-26T10:00:00Z",
        "user": {
            "id": 1,
            "username": "admin",
            "role": "admin"
        }
    }
}
```

#### 获取当前用户信息

```
GET /api/v1/auth/me
Authorization: Bearer <JWT>

Response (200):
{
    "status": "success",
    "data": {
        "id": 1,
        "username": "admin",
        "role": "admin"
    }
}
```

### 5.4 JWT 中间件设计 (api/middleware/auth.go)

```go
func JWTAuth(jwtSecret string) gin.HandlerFunc {
    return func(c *gin.Context) {
        authHeader := c.GetHeader("Authorization")
        if authHeader == "" {
            c.AbortWithStatusJSON(http.StatusUnauthorized, response{
                Status: "error",
                Error:  "authorization header required",
            })
            return
        }

        tokenString := strings.TrimPrefix(authHeader, "Bearer ")
        if tokenString == authHeader {
            c.AbortWithStatusJSON(http.StatusUnauthorized, response{
                Status: "error",
                Error:  "invalid authorization format, expected Bearer token",
            })
            return
        }

        token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
            if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
                return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
            }
            return []byte(jwtSecret), nil
        })

        if err != nil || !token.Valid {
            c.AbortWithStatusJSON(http.StatusUnauthorized, response{
                Status: "error",
                Error:  "invalid or expired token",
            })
            return
        }

        claims, ok := token.Claims.(jwt.MapClaims)
        if !ok {
            c.AbortWithStatusJSON(http.StatusUnauthorized, response{
                Status: "error",
                Error:  "invalid token claims",
            })
            return
        }

        sub, ok := claims["sub"].(float64)
        if !ok {
            c.AbortWithStatusJSON(http.StatusUnauthorized, response{
                Status: "error",
                Error:  "invalid token subject",
            })
            return
        }

        username, ok := claims["username"].(string)
        if !ok {
            c.AbortWithStatusJSON(http.StatusUnauthorized, response{
                Status: "error",
                Error:  "invalid token username",
            })
            return
        }

        role, ok := claims["role"].(string)
        if !ok {
            c.AbortWithStatusJSON(http.StatusUnauthorized, response{
                Status: "error",
                Error:  "invalid token role",
            })
            return
        }

        c.Set("user_id", uint64(sub))
        c.Set("username", username)
        c.Set("role", role)

        c.Next()
    }
}
```

### 5.5 RBAC 中间件设计 (api/middleware/rbac.go)

```go
func RequireRole(roles ...string) gin.HandlerFunc {
    roleSet := make(map[string]struct{}, len(roles))
    for _, r := range roles {
        roleSet[r] = struct{}{}
    }

    return func(c *gin.Context) {
        role, exists := c.Get("role")
        if !exists {
            c.AbortWithStatusJSON(http.StatusForbidden, response{
                Status: "error",
                Error:  "role not found in context",
            })
            return
        }

        if _, ok := roleSet[role.(string)]; !ok {
            c.AbortWithStatusJSON(http.StatusForbidden, response{
                Status: "error",
                Error:  "insufficient permissions",
            })
            return
        }

        c.Next()
    }
}
```

### 5.6 角色权限矩阵

| API                                 | admin | viewer |
| ----------------------------------- | ----- | ------ |
| GET /api/v1/hosts                   | ✅     | ✅      |
| GET /api/v1/hosts/:instance/metrics | ✅     | ✅      |
| GET /api/v1/dashboard/overview      | ✅     | ✅      |
| GET /api/v1/alerts/active           | ✅     | ✅      |
| GET /api/v1/alerts/events           | ✅     | ✅      |
| WS /ws/alerts                       | ✅     | ✅      |
| POST /api/v1/auth/login             | ✅     | ✅      |
| GET /api/v1/auth/me                 | ✅     | ✅      |
| POST /api/v1/auth/register          | ✅     | ❌      |
| GET /api/v1/users                   | ✅     | ❌      |
| DELETE /api/v1/users/:id            | ✅     | ❌      |
| POST/PUT/DELETE /api/v1/host-groups | ✅     | ❌      |
| GET /api/v1/host-groups             | ✅     | ✅      |
| POST/PUT/DELETE /api/v1/alert-rules | ✅     | ❌      |
| GET /api/v1/alert-rules             | ✅     | ✅      |
| POST/PUT/DELETE /api/v1/channels    | ✅     | ❌      |
| GET /api/v1/channels                | ✅     | ✅      |
| GET /api/v1/alert-histories         | ✅     | ✅      |

### 5.7 初始管理员创建

server-web 启动时检测 `users` 表是否为空，如果为空且环境变量 `ADMIN_PASSWORD` 已设置，则自动创建 admin 用户：

```go
func ensureAdminUser(db *gorm.DB, adminPassword string) {
    if adminPassword == "" {
        return
    }

    var count int64
    db.Model(&model.User{}).Count(&count)
    if count > 0 {
        return
    }

    hashedPassword, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
    if err != nil {
        zap.L().Error("hash admin password failed", zap.Error(err))
        return
    }

    user := model.User{
        Username: "admin",
        Password: string(hashedPassword),
        Role:     "admin",
    }

    if err := db.Create(&user).Error; err != nil {
        zap.L().Error("create admin user failed", zap.Error(err))
        return
    }

    zap.L().Info("admin user created")
}
```

***

## 六、主机分组管理

### 6.1 API 设计

```
GET    /api/v1/host-groups           列出所有分组（含成员数量）
POST   /api/v1/host-groups           创建分组
GET    /api/v1/host-groups/:id       获取分组详情（含成员列表）
PUT    /api/v1/host-groups/:id       更新分组
DELETE /api/v1/host-groups/:id       删除分组
POST   /api/v1/host-groups/:id/members   添加主机到分组
DELETE /api/v1/host-groups/:id/members   从分组移除主机
```

#### 创建分组

```
POST /api/v1/host-groups

Request:
{
    "name": "生产环境",
    "description": "生产环境服务器",
    "instances": ["server-1", "server-2"]
}

Response (201):
{
    "status": "success",
    "data": {
        "id": 1,
        "name": "生产环境",
        "description": "生产环境服务器",
        "members": [
            {"instance": "server-1"},
            {"instance": "server-2"}
        ]
    }
}
```

#### 主机列表按分组筛选

在现有 `GET /api/v1/hosts` API 上新增 `group` 查询参数：

```
GET /api/v1/hosts?group=1

当 group 参数存在时，只返回属于指定分组的主机。
当 group 参数不存在时，返回所有主机（现有行为不变）。
```

### 6.2 主机分组与 Prometheus 的关系

主机分组是 **业务层面的逻辑分组**，不影响 Prometheus 的指标采集。分组信息存在 MySQL 中，前端查询主机列表时可以按分组筛选。

分组筛选逻辑：

1. 前端传入 `group` 参数
2. server-web 从 MySQL 查询该分组下的 instance 列表
3. 用 instance 列表过滤从 Prometheus 获取的主机数据

***

## 七、告警规则管理

### 7.1 API 设计

```
GET    /api/v1/alert-rules           列出所有告警规则
POST   /api/v1/alert-rules           创建告警规则
GET    /api/v1/alert-rules/:id       获取告警规则详情
PUT    /api/v1/alert-rules/:id       更新告警规则
DELETE /api/v1/alert-rules/:id       删除告警规则
POST   /api/v1/alert-rules/sync      同步规则到 Prometheus
```

#### 创建告警规则

```
POST /api/v1/alert-rules

Request:
{
    "name": "HighCPU",
    "expr": "server_monitor_cpu_usage_percent > 80",
    "duration": "2m",
    "severity": "warning",
    "summary": "High CPU usage on {{ $labels.instance }}",
    "description": "CPU usage is {{ $value }}% (threshold: 80%)",
    "enabled": true
}

Response (201):
{
    "status": "success",
    "data": {
        "id": 1,
        "name": "HighCPU",
        "expr": "server_monitor_cpu_usage_percent > 80",
        "duration": "2m",
        "severity": "warning",
        "summary": "High CPU usage on {{ $labels.instance }}",
        "description": "CPU usage is {{ $value }}% (threshold: 80%)",
        "enabled": true
    }
}
```

### 7.2 规则同步到 Prometheus

```
MySQL 告警规则                    Prometheus rules YAML
┌──────────────────┐            ┌──────────────────┐
│ id: 1            │            │ groups:          │
│ name: HighCPU    │  ────────▶ │ - name: custom   │
│ expr: cpu > 80   │  渲染为    │   rules:         │
│ duration: 2m     │  YAML     │   - alert: HighCPU│
│ severity: warning│            │     expr: cpu>80 │
│ enabled: true    │            │     for: 2m      │
└──────────────────┘            └──────────────────┘
```

**同步流程**：

1. 从 MySQL 查询所有 `enabled=true` 的告警规则
2. 渲染为 Prometheus rules YAML 格式
3. **原子性保护**：
   a. 渲染到临时文件（如 `alerts.yml.tmp`）
   b. 调用 `promtool check rules` 校验临时文件
   c. 校验通过后，替换正式 rules 文件或 ConfigMap
   d. 调用 Prometheus reload API（`POST /-/reload`）
   e. reload 失败时保留上一版可用规则，记录错误
4. 记录同步状态、错误信息和最后成功同步时间
5. 前端展示"已保存但同步失败"的状态

**Docker Compose 环境**：写入本地 `docker/alerts.yml` 临时文件 → promtool 校验 → 替换正式文件 → reload

**Kubernetes 环境**：渲染到临时 ConfigMap → promtool 校验 → 更新正式 ConfigMap → reload

**Prometheus 热加载**：Prometheus 启动时需添加 `--web.enable-lifecycle` 参数，支持通过 `POST /-/reload` 热加载配置。

### 7.3 规则渲染逻辑

```go
func RenderRulesYAML(rules []model.AlertRule) (string, error) {
    type Rule struct {
        Alert       string `yaml:"alert"`
        Expr        string `yaml:"expr"`
        For         string `yaml:"for"`
        Labels      map[string]string `yaml:"labels"`
        Annotations map[string]string `yaml:"annotations"`
    }

    type Group struct {
        Name  string `yaml:"name"`
        Rules []Rule `yaml:"rules"`
    }

    var enabledRules []Rule
    for _, r := range rules {
        if !r.Enabled {
            continue
        }
        enabledRules = append(enabledRules, Rule{
            Alert: r.Name,
            Expr:  r.Expr,
            For:   r.Duration,
            Labels: map[string]string{
                "severity": r.Severity,
            },
            Annotations: map[string]string{
                "summary":     r.Summary,
                "description": r.Description,
            },
        })
    }

    groups := []Group{{Name: "custom_alerts", Rules: enabledRules}}

    data, err := yaml.Marshal(groups)
    if err != nil {
        return "", fmt.Errorf("marshal rules yaml: %w", err)
    }

    return string(data), nil
}
```

### 7.4 PromQL 安全约束

告警规则的 `expr` 字段需要多层安全校验：

**第一层：基础约束**

1. **限制表达式长度**：最大 2048 字符
2. **限制指标前缀**：只允许引用本系统暴露的指标前缀（如 `server_monitor_`、`up`、`scrape_samples_scrape` 等），或维护指标白名单
3. **禁止使用危险函数**：`admin_api_urls`、`scrape_interval`、`scrape_duration` 等内部指标
4. **禁止使用子查询**：防止资源消耗过大

```go
func validateAlertExpr(expr string) error {
    if len(expr) > 2048 {
        return fmt.Errorf("expr too long (max 2048 chars)")
    }

    dangerous := []string{"admin_api", "scrape_interval", "scrape_duration"}
    lower := strings.ToLower(expr)
    for _, d := range dangerous {
        if strings.Contains(lower, d) {
            return fmt.Errorf("expr contains forbidden reference: %s", d)
        }
    }
    return nil
}
```

**第二层：promtool 校验**

创建/更新规则时，将规则渲染为完整 rules YAML 文件，调用 `promtool check rules` 校验。这是最核心的校验手段，字符串黑名单仅作为补充。

**第三层：运行时保护**

- Prometheus 自身有查询超时和资源限制
- 告警规则 `for` 字段限制了评估频率
- 建议在 Prometheus 配置中设置 `evaluation_interval` 不低于 15s

***

## 八、通知渠道配置

### 8.1 API 设计

```
GET    /api/v1/channels              列出所有通知渠道
POST   /api/v1/channels              创建通知渠道
GET    /api/v1/channels/:id          获取通知渠道详情
PUT    /api/v1/channels/:id          更新通知渠道
DELETE /api/v1/channels/:id          删除通知渠道
POST   /api/v1/channels/:id/test     测试通知渠道连通性
```

#### 创建通知渠道

```
POST /api/v1/channels

Request:
{
    "name": "运维 Webhook",
    "type": "webhook",
    "url": "https://hooks.example.com/alert",
    "enabled": true
}

Response (201):
{
    "status": "success",
    "data": {
        "id": 1,
        "name": "运维 Webhook",
        "type": "webhook",
        "url": "https://hooks.example.com/alert",
        "enabled": true
    }
}
```

#### 测试通知渠道

```
POST /api/v1/channels/:id/test

Response (200):
{
    "status": "success",
    "data": {
        "success": true,
        "latency_ms": 150,
        "status_code": 200
    }
}
```

### 8.2 通知渠道与告警的关系

第四阶段只管理通知渠道配置，**不实现实际告警通知发送逻辑**。通知渠道配置存储在 MySQL 中，为后续阶段（告警通知发送）提供配置基础。

### 8.3 通知渠道测试接口 SSRF 防护

`POST /api/v1/channels/:id/test` 会主动访问用户配置的 URL，必须防止 SSRF 攻击：

**必须实现的防护措施**：

1. **请求超时**：HTTP 请求超时不超过 10 秒
2. **禁止访问内网地址**：
   - 禁止 `127.0.0.0/8`、`10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`
   - 禁止 `localhost`、`0.0.0.0`
   - 禁止云厂商 metadata 地址（`169.254.169.254`）
3. **限制协议**：只允许 `http://` 和 `https://`
4. **禁止重定向到受限地址**：跟随重定向时重新校验目标地址
5. **限制响应体读取大小**：最多读取 1KB
6. **日志不得记录完整 URL**：只记录域名和状态码，不记录 path 和 query

***

## 九、告警历史归档

### 9.1 归档策略

```
告警事件流转：

AlertManager Webhook → server-web
    ├── Redis alert:active（实时状态，不变）
    ├── Redis alert:events（最近 N 条事件，不变）
    ├── Redis Pub/Sub → WebSocket（实时推送，不变）
    ├── Kafka → alert-service（事件处理，不变）
    └── MySQL alert_histories（新增：持久化归档）
```

**双写策略**：

- 告警事件同时写入 Redis（实时）和 MySQL（持久化）
- Redis 仍负责实时推送和短期查询
- MySQL 负责长期存储和复杂查询
- Redis alert:events 保留最近 100 条，MySQL 保留全部历史

**幂等与去重**：

- 以 `fingerprint + status + fired_at` 组合做幂等，重复 webhook 投递不产生重复历史记录
- resolved 事件优先更新已有 firing 记录的 `resolved_at` 字段，而非新增一条记录
- MySQL 写入失败不阻断 Redis / Kafka 主链路

### 9.2 告警历史 API 设计

```
GET /api/v1/alert-histories

查询参数：
  - status: firing / resolved
  - severity: critical / warning / info
  - alert_name: 告警名称
  - instance: 主机实例
  - start: 开始时间 (RFC3339)
  - end: 结束时间 (RFC3339)
  - page: 页码（默认 1）
  - page_size: 每页数量（默认 20，最大 100）

Response (200):
{
    "status": "success",
    "data": {
        "items": [...],
        "total": 150,
        "page": 1,
        "page_size": 20
    }
}
```

### 9.3 告警 Webhook 改造

在现有 `AlertmanagerWebhook` handler 中，增加 MySQL 写入：

```go
// 在现有 Redis 写入和 Kafka 生产之后，增加 MySQL 归档
for _, alert := range payload.Alerts {
    history := model.AlertHistory{
        Fingerprint: alert.Fingerprint,
        AlertName:   alert.Labels["alertname"],
        Instance:    alert.Labels["instance"],
        Severity:    alert.Labels["severity"],
        Status:      alert.Status,
        Summary:     alert.Annotations["summary"],
        LabelsJSON:  labelsToJSON(alert.Labels),
        FiredAt:     alert.StartsAt,
    }
    if alert.Status == "resolved" {
        history.ResolvedAt = &alert.EndsAt
    }

    if err := h.db.Create(&history).Error; err != nil {
        logger.FromContext(c.Request.Context()).Error("archive alert history failed",
            zap.String("fingerprint", alert.Fingerprint),
            zap.Error(err),
        )
    }
}
```

**降级策略**：MySQL 写入失败不影响 Redis 实时链路和 Kafka 事件链路。

***

## 十、前端改造

### 10.1 新增页面

| 页面     | 路由                    | 功能         | 权限    |
| ------ | --------------------- | ---------- | ----- |
| 登录页    | /login                | 用户登录       | 公开    |
| 告警规则管理 | /settings/alert-rules | 告警规则 CRUD  | admin |
| 通知渠道管理 | /settings/channels    | 通知渠道 CRUD  | admin |
| 用户管理   | /settings/users       | 用户列表/创建/删除 | admin |
| 告警历史   | /alerts/history       | 告警历史查询     | 所有    |

### 10.2 现有页面改造

| 页面    | 改造内容                 |
| ----- | -------------------- |
| 所有页面  | 未登录时自动跳转到 /login     |
| 主机列表页 | 新增分组筛选下拉框            |
| 告警面板页 | 新增"历史"标签页            |
| 导航栏   | 新增"设置"入口（仅 admin 可见） |

### 10.3 前端目录结构新增

```
frontend/src/
├── api/
│   ├── auth.ts             登录/注册/用户信息 API
│   ├── host-groups.ts      主机分组 API
│   ├── alert-rules.ts      告警规则 API
│   ├── channels.ts         通知渠道 API
│   └── alert-histories.ts  告警历史 API
├── composables/
│   └── useAuth.ts          认证状态管理（Token 存储/刷新/登出）
├── pages/
│   ├── LoginPage.vue       登录页
│   ├── AlertRulesPage.vue  告警规则管理页
│   ├── ChannelsPage.vue    通知渠道管理页
│   ├── UsersPage.vue       用户管理页
│   └── AlertHistoryPage.vue 告警历史页
├── stores/
│   └── auth.ts             Pinia 认证状态 store
├── router/
│   └── index.ts            路由守卫（未登录跳转）
└── components/
    ├── GroupFilter.vue     分组筛选下拉框
    └── RuleForm.vue        告警规则表单
```

### 10.4 前端认证流程

```typescript
// composables/useAuth.ts
export function useAuth() {
    const token = ref(localStorage.getItem('token') || '')
    const user = ref<User | null>(null)

    function setAuth(newToken: string, newUser: User) {
        token.value = newToken
        user.value = newUser
        localStorage.setItem('token', newToken)
    }

    function logout() {
        token.value = ''
        user.value = null
        localStorage.removeItem('token')
        router.push('/login')
    }

    const isAuthenticated = computed(() => !!token.value)
    const isAdmin = computed(() => user.value?.role === 'admin')

    return { token, user, setAuth, logout, isAuthenticated, isAdmin }
}
```

### 10.5 Axios 请求拦截器

```typescript
// api/client.ts
const client = axios.create({ baseURL: '/api/v1' })

client.interceptors.request.use((config) => {
    const token = localStorage.getItem('token')
    if (token) {
        config.headers.Authorization = `Bearer ${token}`
    }
    return config
})

client.interceptors.response.use(
    (response) => response,
    (error) => {
        if (error.response?.status === 401) {
            localStorage.removeItem('token')
            window.location.href = '/login'
        }
        return Promise.reject(error)
    }
)
```

***

## 十一、Docker Compose 改造

### 11.1 新增服务

```yaml
services:
  # ... 现有服务不变 ...

  # ------------------------------------------
  # MySQL 业务数据库
  # ------------------------------------------
  mysql:
    image: mysql:8.0
    restart: unless-stopped
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD:-server-monitor-root-local}
      MYSQL_DATABASE: server_monitor
      MYSQL_USER: server_monitor
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:-server-monitor-mysql-local}
    ports:
      - "127.0.0.1:3306:3306"
    volumes:
      - mysql-data:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "127.0.0.1"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s
    deploy:
      resources:
        limits:
          cpus: "1.00"
          memory: 1G

volumes:
  mysql-data:
```

### 11.2 现有服务环境变量新增

```yaml
services:
  server-web:
    environment:
      MYSQL_HOST: mysql
      MYSQL_PORT: "3306"
      MYSQL_USER: server_monitor
      MYSQL_PASSWORD: ${MYSQL_PASSWORD:-server-monitor-mysql-local}
      MYSQL_DATABASE: server_monitor
      JWT_SECRET: ${JWT_SECRET:-server-monitor-jwt-secret-local-dev-only}
      JWT_EXPIRE_HOURS: "24"
      AUTH_ENABLED: ${AUTH_ENABLED:-false}
      ADMIN_PASSWORD: ${ADMIN_PASSWORD:-admin}
    depends_on:
      mysql:
        condition: service_healthy
```

**注意**：Docker Compose 本地开发默认 `AUTH_ENABLED=false`，生产环境（Helm）默认 `AUTH_ENABLED=true`。

***

## 十二、Helm Chart 改造

### 12.1 新增模板文件

```
charts/server-monitor/templates/
├── ... 现有文件不变 ...
└── mysql.yaml              MySQL StatefulSet + Service + PVC
```

### 12.2 MySQL Helm 模板

```yaml
# charts/server-monitor/templates/mysql.yaml

apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: mysql
spec:
  serviceName: mysql
  replicas: 1
  selector:
    matchLabels:
      app: mysql
  template:
    metadata:
      labels:
        app: mysql
    spec:
      containers:
        - name: mysql
          image: mysql:8.0
          env:
            - name: MYSQL_ROOT_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: monitor-secret
                  key: MYSQL_ROOT_PASSWORD
            - name: MYSQL_DATABASE
              value: server_monitor
            - name: MYSQL_USER
              value: server_monitor
            - name: MYSQL_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: monitor-secret
                  key: MYSQL_PASSWORD
          ports:
            - containerPort: 3306
          readinessProbe:
            exec:
              command: ["mysqladmin", "ping", "-h", "127.0.0.1"]
            initialDelaySeconds: 30
            periodSeconds: 10
          livenessProbe:
            exec:
              command: ["mysqladmin", "ping", "-h", "127.0.0.1"]
            initialDelaySeconds: 60
            periodSeconds: 20
          resources:
            requests:
              cpu: 250m
              memory: 512Mi
            limits:
              cpu: "1"
              memory: 1Gi
          volumeMounts:
            - name: data
              mountPath: /var/lib/mysql
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: mysql-data
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mysql-data
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 10Gi
---
apiVersion: v1
kind: Service
metadata:
  name: mysql
spec:
  selector:
    app: mysql
  ports:
    - port: 3306
      targetPort: 3306
  type: ClusterIP
```

### 12.3 values.yaml 新增配置

```yaml
mysql:
  enabled: true
  image: mysql:8.0
  database: server_monitor
  user: server_monitor
  persistence:
    enabled: true
    accessModes:
      - ReadWriteOnce
    size: 10Gi
    storageClassName: ""
  resources:
    requests:
      cpu: 250m
      memory: 512Mi
    limits:
      cpu: "1"
      memory: 1Gi

auth:
  enabled: true
  jwtExpireHours: 24

jwt:
  secret: ""
  expireHours: 24

adminPassword: ""
```

**注意**：`jwt.secret` 和 `adminPassword` 在 Helm values 中默认为空，部署时必须显式填写。`auth.enabled` 默认为 `true`（生产环境必须开启）。

### 12.4 现有模板修改

| 文件              | 修改内容                                                                                    |
| --------------- | --------------------------------------------------------------------------------------- |
| configmap.yaml  | 新增 MYSQL\_HOST、MYSQL\_PORT、MYSQL\_USER、MYSQL\_DATABASE、JWT\_EXPIRE\_HOURS、AUTH\_ENABLED |
| secret.yaml     | 新增 MYSQL\_PASSWORD、MYSQL\_ROOT\_PASSWORD、JWT\_SECRET、ADMIN\_PASSWORD                    |
| server-web.yaml | 新增 MySQL/JWT 环境变量，新增 initContainer 等待 MySQL 可连接                                         |
| prometheus.yaml | 新增 `--web.enable-lifecycle` 参数（注意：属于 Prometheus，不属于 AlertManager）                       |

### 12.5 k8s/ 静态清单新增

| 文件             | 内容                                |
| -------------- | --------------------------------- |
| k8s/mysql.yaml | MySQL StatefulSet + PVC + Service |

***

## 十三、CI/CD 改造

### 13.1 GitHub Actions 新增检查

| 步骤         | 内容                             | 说明             |
| ---------- | ------------------------------ | -------------- |
| MySQL 迁移检查 | `go test ./model/...`          | 确保 GORM 模型定义正确 |
| JWT 认证测试   | `go test ./api/middleware/...` | 确保 JWT 中间件逻辑正确 |
| API 测试     | `go test ./api/handlers/...`   | 确保新增 API 逻辑正确  |

### 13.2 Docker 构建影响

server-web 因新增 GORM/JWT 依赖，需要重新构建镜像。其他服务不受影响。

### 13.3 go.mod 变更

server-web 的 go.mod 新增依赖：

```
gorm.io/gorm v1.25.x
gorm.io/driver/mysql v1.5.x
github.com/golang-jwt/jwt/v5 v5.2.x
golang.org/x/crypto v0.x
```

***

## 十四、实施步骤

| 步骤 | 内容                                                    | 验证标准                                    |
| -- | ----------------------------------------------------- | --------------------------------------- |
| 1  | Docker Compose 新增 MySQL                               | `docker compose config` 通过，MySQL 健康检查通过 |
| 2  | server-web 新增 database 包（MySQL 连接 + 自动迁移）             | GORM 连接成功，表自动创建                         |
| 3  | server-web 新增 model 包（6 个 GORM 模型）                    | `go test ./model/...` 通过                |
| 4  | server-web 新增 JWT 认证中间件                               | `go test ./api/middleware/...` 通过       |
| 5  | server-web 新增 RBAC 权限中间件                              | `go test ./api/middleware/...` 通过       |
| 6  | server-web 新增 auth handler（登录/注册/me）                  | `go test ./api/handlers/...` 通过         |
| 7  | server-web 新增初始管理员创建逻辑                                | 启动后 admin 用户自动创建                        |
| 8  | server-web 路由改造：公开路由 + 认证路由 + 管理路由 + AUTH\_ENABLED 开关 | 未认证请求返回 401（AUTH\_ENABLED=true）         |
| 9  | server-web 新增 host\_groups handler（CRUD + 成员管理）       | `go test ./api/handlers/...` 通过         |
| 10 | server-web Hosts API 新增 group 筛选参数                    | 按分组筛选返回正确结果                             |
| 11 | server-web 新增 alert\_history handler（分页查询）            | `go test ./api/handlers/...` 通过         |
| 12 | server-web Webhook handler 增加 MySQL 双写归档              | 告警事件写入 MySQL，重复投递不产生重复记录                |
| 13 | server-web 新增 alert\_rules handler（仅 CRUD，保存到 MySQL）  | `go test ./api/handlers/...` 通过         |
| 14 | 告警规则同步到 Prometheus（渲染 → promtool 校验 → 替换 → reload）    | Prometheus 加载新规则，同步失败保留上一版              |
| 15 | server-web 新增 channels handler（CRUD + SSRF 防护的连通性测试）  | `go test ./api/handlers/...` 通过         |
| 16 | Helm Chart 新增 MySQL                                   | `helm lint` 通过                          |
| 17 | Helm Chart 现有模板新增 MySQL/JWT 环境变量 + initContainer      | `helm template` 通过                      |
| 18 | k8s/ 新增 mysql.yaml                                    | YAML 语法校验通过                             |
| 19 | 前端新增登录页 + 认证状态管理                                      | 登录成功跳转主页                                |
| 20 | 前端路由守卫 + Axios 拦截器                                    | 未登录自动跳转登录页                              |
| 21 | 前端主机列表新增分组筛选                                          | 按分组筛选主机                                 |
| 22 | 前端新增告警规则管理页                                           | CRUD 操作正常                               |
| 23 | 前端新增通知渠道管理页                                           | CRUD 操作正常                               |
| 24 | 前端新增用户管理页                                             | 用户列表/创建/删除正常                            |
| 25 | 前端新增告警历史页                                             | 分页查询正常                                  |
| 26 | 端到端验证                                                 | 完整业务闭环                                  |

***

## 十五、验收标准

### 功能验收

- [ ] MySQL 部署成功，server-web 通过 GORM 连接
- [ ] GORM 自动迁移创建 6 张表
- [ ] 启动时自动创建 admin 用户（ADMIN\_PASSWORD 环境变量）
- [ ] 用户登录返回 JWT Token
- [ ] JWT Token 过期后返回 401
- [ ] 受保护 API 未携带 Token 返回 401
- [ ] admin 角色可访问管理 API
- [ ] viewer 角色访问管理 API 返回 403
- [ ] 主机分组 CRUD 正常
- [ ] 主机列表按分组筛选正常
- [ ] 告警规则 CRUD 正常
- [ ] 告警规则同步到 Prometheus 后生效
- [ ] 通知渠道 CRUD 正常
- [ ] 通知渠道测试连通性正常
- [ ] 告警历史归档到 MySQL
- [ ] 告警历史分页查询正常
- [ ] 前端登录页可用
- [ ] 前端未登录自动跳转登录页
- [ ] 前端告警规则管理页可用
- [ ] 前端通知渠道管理页可用
- [ ] 前端用户管理页可用
- [ ] 前端告警历史页可用
- [ ] Docker Compose 包含 MySQL
- [ ] Helm Chart 包含 MySQL

### 端到端验收用例

- [ ] 注册新用户 → 登录 → 获取用户信息 → 成功
- [ ] viewer 登录 → 尝试创建告警规则 → 返回 403
- [ ] viewer 登录 → 尝试访问所有写接口 → 全部返回 403
- [ ] admin 登录 → 创建告警规则 → 同步到 Prometheus → 触发告警 → 收到 WebSocket 通知
- [ ] admin 登录 → 创建告警规则 → 同步失败 → 前端展示"已保存但同步失败" → 上一版规则仍可用
- [ ] admin 登录 → 创建主机分组 → 添加主机 → 按分组筛选主机列表 → 返回正确结果
- [ ] admin 登录 → 创建通知渠道 → 测试连通性 → 返回成功
- [ ] admin 登录 → 创建通知渠道指向 localhost → 测试连通性 → 返回 SSRF 拦截错误
- [ ] 触发告警 → MySQL alert\_histories 表有记录 → 前端告警历史页可查询
- [ ] 重复 AlertManager webhook 投递 → MySQL 不产生重复历史记录
- [ ] MySQL 不可用 → 主机列表、实时告警、WebSocket 推送仍符合预期
- [ ] `JWT_SECRET` 为空 → server-web 启动失败
- [ ] `AUTH_ENABLED=false` → 所有业务接口无需认证即可访问
- [ ] Helm `helm lint`、`helm template` 通过
- [ ] Prometheus rules `promtool check rules` 通过

### 非功能验收

- [ ] JWT Secret 通过 Secret 管理，不硬编码
- [ ] `JWT_SECRET` 为空或长度不足 32 字节时，server-web 启动失败
- [ ] MySQL 密码通过 Secret 管理，不硬编码
- [ ] 密码使用 bcrypt 哈希存储
- [ ] MySQL 写入失败不影响 Redis 实时链路
- [ ] 告警规则同步失败不影响现有规则（上一版规则仍可用）
- [ ] 前端 Token 存储在 localStorage，过期自动跳转登录页
- [ ] `/healthz`、`/readyz`、`/metrics` 无需认证即可访问
- [ ] `/readyz` 不检查 MySQL 连通性（MySQL 不可用不影响核心链路）
- [ ] `/readyz/full` 检查 MySQL 连通性并反映管理功能降级状态

### 兼容性验收

- [ ] 第一、二、三阶段核心功能正常
- [ ] Docker Compose `docker compose up` 全栈启动正常
- [ ] Helm Chart `helm upgrade` 升级不影响现有服务
- [ ] 现有 API 接口行为兼容（新增认证后，业务接口需携带 Token；`AUTH_ENABLED=false` 时行为与之前一致）
- [ ] 现有 Prometheus 告警规则不受影响
- [ ] 现有 WebSocket 推送不受影响

***

## 十六、风险与注意事项

### 16.1 MySQL 引入的运维复杂度

MySQL 是有状态服务，引入后需关注：

- 数据备份策略
- 主从复制（生产环境）
- 连接池配置
- 磁盘空间监控
- 慢查询监控

### 16.2 JWT 安全注意事项

- JWT Secret 必须足够长（至少 32 字节），通过 Secret 管理
- **`JWT_SECRET`** **为空或长度不足 32 字节时，server-web 启动失败**（生产环境强制要求）
- Docker Compose 本地开发可使用默认值，但启动时打印警告
- Token 有效期不宜过长（默认 24 小时）
- 不在 JWT 中存储敏感信息
- Token 过期后需要重新登录，暂不实现 Refresh Token
- 生产环境应使用 HTTPS，防止 Token 被窃取

### 16.3 生产环境 Secret 强制配置

Helm / K8s 生产环境必须要求显式配置以下 Secret，不允许使用默认值：

| Secret                | 要求                              |
| --------------------- | ------------------------------- |
| `JWT_SECRET`          | 至少 32 字节，为空或过短时 server-web 启动失败 |
| `MYSQL_PASSWORD`      | 不允许为空                           |
| `MYSQL_ROOT_PASSWORD` | 不允许为空                           |
| `ADMIN_PASSWORD`      | 不允许为空                           |

Docker Compose 本地开发可保留默认值（如 `server-monitor-jwt-secret-local`），但 Helm values 中这些字段默认为空，部署时必须显式填写。

### 16.4 告警规则同步的原子性

规则同步流程需要保证：

- MySQL 写入成功后才同步到 Prometheus
- 同步失败时记录错误，但不回滚 MySQL（规则配置已保存）
- Prometheus reload 失败时提供重试机制
- 规则 YAML 渲染需要通过 promtool 校验

### 16.5 MySQL 与 Redis 的职责边界

| 维度   | MySQL          | Redis        |
| ---- | -------------- | ------------ |
| 数据类型 | 业务配置、用户、告警历史   | 缓存、限流、实时状态   |
| 持久化  | **持久化，事务支持**   | 内存为主，可丢失     |
| 查询   | **复杂查询、分页、聚合** | 简单 KV 查询     |
| 实时性  | 写入后立即可查        | **毫秒级响应**    |
| 写入频率 | 低频（配置变更、告警归档）  | **高频（每次请求）** |

**结论**：MySQL 负责业务配置和持久化数据，Redis 负责缓存和实时状态。两者互补，不替代。

### 16.6 密码安全

- 使用 bcrypt 哈希，cost factor 默认 10
- 禁止明文存储密码
- 禁止在日志中记录密码
- 禁止在 API 响应中返回密码
- GORM 模型中 Password 字段使用 `json:"-"` 标签

### 16.7 告警历史归档降级

MySQL 写入失败时的降级策略：

- MySQL 不可用时，告警事件仍写入 Redis（实时链路不受影响）
- MySQL 不可用时，告警历史查询 API 返回错误
- MySQL 恢复后，不自动补录丢失的历史（可接受，实时数据在 Redis 中）

### 16.8 /readyz 与降级策略

MySQL 只影响管理能力和归档能力，不影响核心监控/告警/推送链路。因此 `/readyz` 不应因 MySQL 不可用而将整个 server-web 标记为 Not Ready，否则会导致 Pod 被摘流，核心 API 和 WebSocket 也不可用。

**健康检查拆分策略**：

| 端点                 | 检查内容                               | MySQL 不可用时                           |
| ------------------ | ---------------------------------- | ------------------------------------ |
| `GET /healthz`     | 进程存活                               | 正常返回 200                             |
| `GET /readyz`      | 核心查询链路（Prometheus + Redis）         | 正常返回 200（不检查 MySQL）                  |
| `GET /readyz/full` | 全部依赖状态（Prometheus + Redis + MySQL） | 返回 503，body 中标注 `mysql: unreachable` |

这样当 MySQL 不可用时：

- 核心监控/告警/推送功能仍可服务
- 管理能力（用户/规则/渠道/历史）降级
- 前端可调用 `/readyz/full` 展示管理功能降级状态

### 16.9 前端认证兼容

- WebSocket 连接不支持自定义 Header，需通过 URL 参数传递 Token
- WebSocket 认证：`ws://host/ws/alerts?token=<JWT>`
- server-web WebSocket handler 需验证 Token 有效性
- Token 无效时拒绝 WebSocket 升级

### 16.10 前端 Token 存储安全取舍

当前阶段使用 localStorage 存储 JWT Token，简单直接，但存在以下安全风险：

**localStorage 的风险**：

- 容易受 XSS 攻击窃取 Token
- 一旦被窃取，攻击者在 Token 有效期内可冒充用户

**当前阶段的缓解措施**：

- 所有前端输入和渲染必须避免 XSS（Vue3 默认转义，避免使用 `v-html`）
- Token 不写日志、不拼接到普通 HTTP URL
- WebSocket URL 中的 Token 可能进入代理访问日志，考虑缩短 WebSocket 专用 Token 有效期，或改用握手后首帧认证（后续优化）

**后续可升级方案**：

- HttpOnly Cookie + CSRF Token（更安全，但跨域配置复杂）
- 握手后首帧认证（WebSocket 连接建立后通过首条消息发送 Token，避免 URL 泄露）

***

## 十七、Codex 复查建议

### 17.1 总体结论

在去除 ChatOps / AI Agent 的前提下，第四阶段方向整体合理：MySQL 只承载用户、权限、主机分组、告警规则、通知渠道、告警历史等业务管理数据，不替代 Prometheus / VictoriaMetrics / Redis / Kafka / Elasticsearch 的现有职责；JWT + 简单 RBAC 也符合当前阶段的管理需求。

但当前方案仍然偏“大而全”，不能作为一次性实施任务直接执行。后续实现时必须按小模块拆分，每个模块单独验证、单独提交，避免把数据库、认证、规则同步、前端改造、Helm 改造混在一次变更里。

### 17.2 建议调整的实施边界

建议将第四阶段实施顺序调整为以下闭环：

1. MySQL 基础设施 + server-web 连接 + 健康检查
2. GORM 模型 + 最小迁移策略
3. 用户认证 + 初始管理员创建
4. JWT 中间件 + RBAC 中间件
5. 主机分组 CRUD + hosts 按分组筛选
6. 告警历史双写归档 + 分页查询
7. 告警规则 CRUD，仅保存到 MySQL
8. 告警规则同步到 Prometheus
9. 通知渠道 CRUD + 安全受限的连通性测试
10. 前端登录、鉴权、管理页逐步接入
11. Docker Compose / Helm / k8s 清单分别补齐并独立验证

其中第 7 步和第 8 步建议拆开：先完成规则配置的增删改查，再单独实现 Prometheus 规则渲染、校验、写入和 reload。这样可以降低 Prometheus 配置写坏后影响现有告警链路的风险。

### 17.3 需要修正或补充的关键点

1. **认证兼容性需要明确迁移策略**

   文档中“现有 API 接口行为不变”和“新增认证后需携带 Token”存在冲突。建议明确：
   - `/healthz`、`/readyz`、`/metrics` 保持公开
   - 登录接口公开
   - 业务查询接口是否立刻强制认证需要单独确认
   - 如需兼容演示环境，可提供 `AUTH_ENABLED=false` 的本地开发开关，但生产环境必须开启
2. **不要在 K8s 模板中使用 Compose 语义**

   Helm 章节提到 `server-web.yaml` 增加 `depends_on mysql`，这只适用于 Docker Compose，不适用于 Kubernetes。K8s 中应通过：
   - initContainer 等待 MySQL 可连接
   - readinessProbe 反映 server-web 依赖状态
   - 应用侧连接重试和超时
3. **Prometheus 配置修改位置需要校正**

   文档中写到 `alertmanager.yaml` 新增 `--web.enable-lifecycle` 参数，但该参数属于 Prometheus，不属于 Alertmanager。应修改 Prometheus 的启动参数。
4. **告警规则同步必须有原子性保护**

   规则同步到 Prometheus 前必须先完成：
   - 渲染到临时文件
   - `promtool check rules` 校验
   - 校验通过后再替换正式 rules 文件或 ConfigMap
   - reload 失败时保留上一版可用规则
   - 记录同步状态、错误信息和最后成功同步时间
   MySQL 中保存成功不代表 Prometheus 已生效，前端需要能展示“已保存但同步失败”的状态。
5. **PromQL 校验不能只靠字符串黑名单**

   当前 `validateAlertExpr` 只做字符串包含判断，安全性不足。建议将黑名单校验作为补充，核心校验应依赖：
   - Prometheus promql parser 解析表达式
   - `promtool check rules` 校验完整 rules 文件
   - 限制表达式长度
   - 限制子查询、超大 range selector、高成本函数
   - 只允许引用本系统暴露的指标前缀或维护指标白名单
6. **通知渠道 test 接口需要 SSRF 防护**

   虽然第四阶段不实现实际告警通知发送，但 `POST /api/v1/channels/:id/test` 会主动访问用户配置的 URL，必须补充：
   - 请求超时
   - 禁止访问内网地址、localhost、metadata 地址
   - 限制协议为 HTTPS/HTTP
   - 禁止重定向到受限地址
   - 限制响应体读取大小
   - 日志不得记录完整敏感 URL
7. **告警历史应是双写归档，不是 Redis 迁移**

   文档中“Redis -> MySQL 迁移”容易误导。更准确的表述是：Redis 继续保存实时状态和最近事件，MySQL 新增长期归档。实现时还需要补充：
   - 以 `fingerprint + status + startsAt` 或事件 ID 做幂等
   - resolved 事件优先更新已有 firing 记录的 `resolved_at`
   - 重复 webhook 投递不能产生大量重复历史
   - MySQL 写入失败不阻断 Redis / Kafka 主链路
8. **`/readyz`** **与降级策略需要拆清**

   文档写到 MySQL 不可用时现有监控/告警/推送不受影响，但又要求 `/readyz` 因 MySQL 不可用返回 Not Ready。这样会导致 Pod 被摘流，现有 API 和 WebSocket 也不可用。建议拆分：
   - `/healthz`：进程存活
   - `/readyz`：核心查询链路是否可服务
   - `/readyz/full` 或管理接口状态：展示 MySQL 等管理能力依赖状态
   如果 MySQL 只影响管理能力和归档能力，默认不应让整个 server-web Not Ready，除非认证全面依赖 MySQL 且没有降级策略。
9. **生产 Secret 必须强制配置**

   Compose 中可以保留本地默认值，但 Helm / K8s 生产环境必须要求显式配置：
   - `JWT_SECRET`
   - `MYSQL_PASSWORD`
   - `MYSQL_ROOT_PASSWORD`
   - `ADMIN_PASSWORD`
   `JWT_SECRET` 为空或过短时 server-web 应启动失败。生产环境不应使用文档中的示例默认密码。
10. **前端 Token 存储需要说明安全取舍**

    localStorage 简单，但容易受 XSS 影响。当前阶段可以接受，但文档需要明确：
    - 所有前端输入和渲染必须避免 XSS
    - Token 不写日志、不拼接到普通 HTTP URL
    - WebSocket URL token 可能进入代理访问日志，应缩短有效期或改用握手后首帧认证
11. **自动迁移适合开发环境，生产环境需谨慎**

    GORM AutoMigrate 可用于本地开发和早期学习项目，但生产环境建议改为显式 migration。至少需要：
    - 启动时只做兼容性迁移
    - 不自动删除字段或索引
    - 每次表结构变更有可审查 SQL
12. **文档中的 Go 代码示例需要避免 panic 风险**

    JWT claims 中的 `claims["sub"].(float64)`、`claims["username"].(string)`、`claims["role"].(string)` 直接断言可能 panic。正式实现应使用结构化 claims 或逐项检查类型，错误时返回 401。

### 17.4 建议保留但暂不实现的内容

- SSO / OAuth2
- 操作审计日志
- 告警静默 / 抑制规则界面化
- 多租户隔离
- ChatOps Agent / AI 分析
- 真实告警通知发送链路

这些能力可以继续作为后续阶段预留，不应混入第四阶段的首轮实现。

### 17.5 建议补充的验收项

- MySQL 不可用时，主机列表、实时告警、WebSocket 推送是否仍符合预期
- Prometheus rules 同步失败时，上一版规则是否仍可用
- 重复 Alertmanager webhook 投递不会产生重复历史记录
- viewer 无法访问任何写接口
- `/healthz`、`/readyz`、`/metrics` 的认证策略符合部署预期
- `JWT_SECRET` 为空或过短时服务拒绝启动
- 通知渠道 test 不能访问 localhost、内网地址和云厂商 metadata 地址
- Helm `helm lint`、`helm template`、Prometheus rules `promtool check rules` 均通过

***

