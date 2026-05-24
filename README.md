<div align="center">

# UBAA.Server

面向 [BUAASubnet/UBAA](https://github.com/BUAASubnet/UBAA) 的高性能 Go 后端重写。

[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Fiber](https://img.shields.io/badge/Fiber-v2-00ACD7)](https://gofiber.io/)
[![SQLite](https://img.shields.io/badge/SQLite-backed-003B57?logo=sqlite&logoColor=white)](https://www.sqlite.org/)
[![FreeCache](https://img.shields.io/badge/FreeCache-enabled-4B8BBE)](https://github.com/coocood/freecache)
[![License](https://img.shields.io/badge/license-see%20upstream-lightgrey)](https://github.com/BUAASubnet/UBAA)

[原项目](https://github.com/BUAASubnet/UBAA) · [本仓库](https://github.com/BUAASubnet/UBAA.Server) · [快速开始](#快速开始) · [功能覆盖](#功能覆盖) · [配置项](#配置项) · [兼容性](#兼容性)

</div>

---

## 简介

[`UBAA.Server`](https://github.com/BUAASubnet/UBAA.Server) 是 [`BUAASubnet/UBAA`](https://github.com/BUAASubnet/UBAA) 后端的 Go 重写版本，目标是在**不重写前端**的前提下，用 [Go](https://go.dev/)、[Fiber](https://gofiber.io/)、[FreeCache](https://github.com/coocood/freecache) 和 [SQLite](https://www.sqlite.org/) 提供与原 [Ktor](https://ktor.io/) 后端兼容的 API 行为。

它保留了现有 Compose Multiplatform 前端依赖的路由、DTO 字段名、鉴权方式和错误结构，同时把服务端会话、Cookie、缓存和上游请求逻辑迁移到 Go 实现中。

## 亮点

- **前端零改动**：继续使用 `/api/v1/...` 路由和 `Authorization: Bearer <token>` 鉴权。
- **单文件数据库**：使用 [SQLite](https://www.sqlite.org/) 保存会话、刷新令牌、登录统计和上游 Cookie。
- **内存缓存**：使用 [FreeCache](https://github.com/coocood/freecache) 缓存活跃会话，减少持久化读取。
- **兼容原行为**：错误响应、JWT claims、默认端口和主要 JSON 字段保持兼容。
- **覆盖真实查询链路**：课表、成绩、考试、图书馆、SPOC、博雅等接口均按前端契约暴露。

## 目录

```text
.
├── cmd/server/                 # 服务入口
├── internal/app/               # Fiber app、指标和中间件装配
├── internal/auth/              # 登录、JWT、会话与鉴权
├── internal/features/          # 课表、成绩、图书馆等业务功能
├── internal/storage/           # SQLite 持久化
├── internal/upstream/          # 上游 HTTP 客户端和 WebVPN 适配
├── internal/dto/               # 前端兼容 DTO
└── docs/compatibility.md       # 兼容性说明
```

## 快速开始

### 直接运行

```bash
go run ./cmd/server
```

默认监听地址与原后端一致：

```bash
SERVER_BIND_HOST=0.0.0.0
SERVER_PORT=5432
SQLITE_PATH=data/ubaa-server.db
```

启动后检查服务状态：

```bash
curl http://127.0.0.1:5432/health/ready
```

### 构建二进制

```bash
go build -trimpath -ldflags "-s -w" -o bin/ubaa-server ./cmd/server
./bin/ubaa-server
```

## 功能覆盖

| 模块 | 路由前缀 | 状态 |
| --- | --- | --- |
| 鉴权与会话 | [`/api/v1/auth`](internal/auth/routes.go) | 已实现 |
| 用户信息 | [`/api/v1/user`](internal/features/routes.go) | 已实现 |
| 课表查询 | [`/api/v1/schedule`](internal/features/routes.go) | 已实现 |
| 考试查询 | [`/api/v1/exam`](internal/features/routes.go) | 已实现 |
| 成绩查询 | [`/api/v1/grade`](internal/features/routes.go) | 已实现 |
| 空闲教室 | [`/api/v1/classroom`](internal/features/routes.go) | 已实现 |
| 博雅课程 | [`/api/v1/bykc`](internal/features/routes.go) | 已实现 |
| SPOC 作业 | [`/api/v1/spoc`](internal/features/routes.go) | 已实现 |
| Judge 作业 | [`/api/v1/judge`](internal/features/routes.go) | 已实现 |
| 评教 | [`/api/v1/evaluation`](internal/features/routes.go) | 已实现 |
| 图书馆座位 | [`/api/v1/libbook`](internal/features/routes.go) | 已实现 |
| 场馆预约 | [`/api/v1/cgyy`](internal/features/routes.go) | 已实现 |
| 阳光打卡 | [`/api/v1/ygdk`](internal/features/routes.go) | 已实现 |
| 签到 | [`/api/v1/signin`](internal/features/routes.go) | 已实现 |
| 健康检查 | [`/health/live`, `/health/ready`](internal/health/routes.go) | 已实现 |
| Prometheus 指标 | [`/metrics`](internal/app/metrics.go) | 已实现 |

更多兼容性细节见 [docs/compatibility.md](docs/compatibility.md)。

## 配置项

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| [`SERVER_BIND_HOST`](internal/config/config.go) | `0.0.0.0` | 服务监听地址 |
| [`SERVER_PORT`](internal/config/config.go) | `5432` | 服务端口 |
| [`SQLITE_PATH`](internal/config/config.go) | `data/ubaa-server.db` | SQLite 数据库路径 |
| [`JWT_SECRET`](internal/config/config.go) | `ubaa-dev-secret-unsafe` | JWT 签名密钥，生产环境必须修改 |
| [`ACCESS_TOKEN_TTL_MINUTES`](internal/config/config.go) | `30` | Access Token 有效期 |
| [`REFRESH_TOKEN_TTL_DAYS`](internal/config/config.go) | `7` | Refresh Token 有效期 |
| [`SESSION_TTL_DAYS`](internal/config/config.go) | `7` | 服务端会话有效期 |
| [`FREECACHE_SIZE_MB`](internal/config/config.go) | `64` | FreeCache 内存大小 |
| [`CORS_ALLOWED_ORIGINS`](internal/config/config.go) | 空 | 允许的跨域来源，逗号分隔 |
| [`UPDATE_DOWNLOAD_URL`](internal/config/config.go) | GitHub Releases | 应用更新下载地址 |

本地开发可以使用环境变量，也可以放在 `.env` 中。`.env`、数据库、日志和 token 不应提交到仓库。

## 开发

安装依赖并运行测试：

```bash
go mod tidy
go test ./...
```

常用开发命令：

```bash
go run ./cmd/server
go test ./...
go test ./internal/features -run Test
```

## 兼容性

为了让 [`BUAASubnet/UBAA`](https://github.com/BUAASubnet/UBAA) 中的桌面端和多平台前端无需重写，本服务遵循以下约定：

- 路由路径保持 `/api/v1/...`。
- JSON 字段名对齐原 shared Kotlin DTO。
- 鉴权方式保持 `Authorization: Bearer <token>`。
- JWT issuer/audience 与原后端保持一致。
- 常见错误码和错误响应结构保持一致。
- 默认端口保持 `5432`。

注意：课表、成绩、考试、图书馆、SPOC 等接口会访问 BUAA 上游系统。端到端耗时会受到学校网络、上游响应、登录态、限流和风控策略影响。

## 安全说明

- 不要提交 `.env`、SQLite 数据库、日志、token 或 Cookie。
- 生产环境必须设置强 `JWT_SECRET`。
- 不要用错误账号密码反复尝试 SSO 登录，可能触发风控。
- 预约、签到、评教提交等写入接口不适合做压测。
- 压测只应覆盖查询接口，并设置合理的并发和 RPS 上限。

## 相关项目

- [`BUAASubnet/UBAA`](https://github.com/BUAASubnet/UBAA)：原项目、前端和 Kotlin 后端。
- [`BUAASubnet/UBAA.Server`](https://github.com/BUAASubnet/UBAA.Server)：本仓库，Go 后端重写。
- [Go](https://go.dev/)：本仓库主要开发语言。
- [Fiber](https://gofiber.io/)：HTTP Web 框架。
- [FreeCache](https://github.com/coocood/freecache)：内存缓存。
- [SQLite](https://www.sqlite.org/)：本地持久化数据库。
- [Ktor](https://ktor.io/)：原后端使用的 Kotlin Web 框架。

## 许可证

本项目与上游 [`BUAASubnet/UBAA`](https://github.com/BUAASubnet/UBAA) 保持同一项目体系。使用和分发前请确认上游仓库的许可证与合规要求。
