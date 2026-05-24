# UBAA.Server

UBAA 后端的 Go 重写版本，目标是在不改动现有前端的前提下，提供与原 Ktor 后端兼容的 API 行为。

## 技术栈

- Go
- Fiber
- FreeCache
- SQLite
- git

## 当前状态

本仓库基于 `BUAASubnet/UBAA` 的 `dev` 分支重写后端。前端、共享 DTO 命名和接口路径保持兼容，桌面端可以直接指向本服务。

已覆盖的主要能力：

- Fiber 服务启动与路由注册
- 与原后端兼容的 JSON 错误结构
- JWT 签发与校验
- SQLite 持久化会话、刷新令牌、登录统计和上游 Cookie
- FreeCache 会话缓存
- BUAA SSO/CAS 登录流程、验证码检测、跳转跟随和 UC 会话校验
- WebVPN URL 转换
- `/health/live`、`/health/ready`、`/metrics`
- `/api/v1/app/version`、`/api/v1/app/announcement`
- 前端使用的 `/api/v1` 查询接口：
  - 课表
  - 考试
  - 成绩
  - 空闲教室
  - 博雅课程
  - SPOC 作业
  - Judge 作业
  - 评教
  - 图书馆座位
  - 场馆预约
  - 研究生打卡
  - 签到

## 快速启动

```bash
go run ./cmd/server
```

默认监听地址与原 Ktor 后端保持一致：

```bash
SERVER_BIND_HOST=0.0.0.0
SERVER_PORT=5432
SQLITE_PATH=data/ubaa-server.db
```

启动后可检查：

```bash
curl http://127.0.0.1:5432/health/ready
```

## 构建二进制

```bash
go build -trimpath -ldflags "-s -w" -o bin/ubaa-server ./cmd/server
./bin/ubaa-server
```

## 常用配置

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVER_BIND_HOST` | `0.0.0.0` | 服务监听地址 |
| `SERVER_PORT` | `5432` | 服务端口 |
| `SQLITE_PATH` | `data/ubaa-server.db` | SQLite 数据库路径 |
| `JWT_SECRET` | `ubaa-dev-secret-unsafe` | JWT 签名密钥，生产环境必须修改 |
| `ACCESS_TOKEN_TTL_MINUTES` | `30` | Access Token 有效期 |
| `REFRESH_TOKEN_TTL_DAYS` | `7` | Refresh Token 有效期 |
| `SESSION_TTL_DAYS` | `7` | 服务端会话有效期 |
| `FREECACHE_SIZE_MB` | `64` | FreeCache 内存大小 |
| `CORS_ALLOWED_ORIGINS` | 空 | 允许的跨域来源，逗号分隔 |
| `UPDATE_DOWNLOAD_URL` | GitHub Releases | 应用更新下载地址 |

本地开发可以直接使用环境变量，也可以放在 `.env` 中。不要提交 `.env`。

## 测试

```bash
go test ./...
```

当前仓库包含路由、鉴权、存储、WebVPN 转换以及主要功能模块的单元测试。

## 与原后端的兼容约定

为了让现有 Compose Multiplatform 前端无需重写，本服务保持以下兼容性：

- 路由路径保持 `/api/v1/...`
- DTO 字段名保持 Kotlin shared 模块使用的 JSON 命名
- 鉴权方式保持 `Authorization: Bearer <token>`
- 常见错误码与错误响应结构保持一致
- 默认端口保持 `5432`

注意：部分接口会访问 BUAA 上游系统，端到端耗时会受到学校网络、上游系统响应、登录态和限流影响。

## 安全注意事项

- 不要提交 `.env`、SQLite 数据库、日志、token 或 Cookie。
- 生产环境必须设置强 `JWT_SECRET`。
- 不要用错误账号密码反复尝试 SSO 登录，可能触发风控。
- 图书馆预约、场馆预约、签到、评教提交等写入接口需要谨慎调用；压测时应只测查询接口。

## 仓库关系

- 原项目与前端仓库：`BUAASubnet/UBAA`
- 本仓库：`BUAASubnet/UBAA.Server`

本仓库只包含 Go 后端重写，不包含前端重写。
