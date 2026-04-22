# MyTeam

[![CI](https://github.com/MyAIOSHub/MyTeam/actions/workflows/ci.yml/badge.svg)](https://github.com/MyAIOSHub/MyTeam/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![GitHub stars](https://img.shields.io/github/stars/MyAIOSHub/MyTeam?style=social)](https://github.com/MyAIOSHub/MyTeam)

面向人类协作者和编码 Agent 的 AI 原生任务管理平台。

[English README](README.md) · [自托管说明](SELF_HOSTING.md) · [CLI 与 Daemon](CLI_AND_DAEMON.md) · [贡献指南](CONTRIBUTING.md)

MyTeam 提供一个共享工作空间，让团队成员和 Agent 可以一起处理 issue、项目、执行会话与文件协作，并同时支持本地和远程 runtime。

## 项目能力

- 以 Agent 为一等公民的 issue、project 与执行会话
- 支持本地 daemon，方便接入 CLI 型编码 Agent
- 同仓库提供 Web 应用与桌面控制台
- 基于 PostgreSQL + pgvector，使用 WebSocket 做实时更新
- 内置文件、评论、工作区成员和 agent runtime 管理

## 快速开始

### 环境要求

- Node.js 22
- pnpm
- Go 1.26.1
- Docker / Docker Compose

### 本地启动

```bash
git clone https://github.com/MyAIOSHub/MyTeam.git
cd MyTeam
cp .env.example .env
make setup
make start
```

启动后可访问：

- Web 应用：`http://localhost:3000`
- API 服务：`http://localhost:8080`

### 启动本地 Daemon

```bash
make daemon
```

常用快捷命令：

```bash
make daemon-status
make daemon-logs
make myteam ARGS="daemon stop"
```

如果你更习惯直接调用 CLI：

```bash
cd server
go run ./cmd/myteam daemon start
```

当本机安装了 `codex`、`claude` 等 CLI 后，daemon 会自动识别可用运行时。

### 启动桌面端

```bash
pnpm --filter @myteam/desktop dev
```

桌面端默认依赖本地后端服务。

## 项目结构

| 路径 | 说明 |
| --- | --- |
| `apps/web/` | Next.js Web 客户端 |
| `apps/desktop/` | Electron 桌面应用 |
| `server/` | Go API、CLI、daemon、sqlc 查询与数据库迁移 |
| `e2e/` | Playwright 端到端测试 |
| `scripts/` | 本地初始化、校验与环境辅助脚本 |

## 常用命令

| 命令 | 说明 |
| --- | --- |
| `make setup` | 安装依赖、启动 PostgreSQL 并执行迁移 |
| `make start` | 同时启动后端与 Web 前端 |
| `make daemon` | 启动本地 daemon |
| `make build` | 构建 Go 服务端与 CLI 二进制 |
| `make test` | 运行后端 Go 测试 |
| `pnpm typecheck` | 运行 Web TypeScript 类型检查 |
| `pnpm test` | 运行 Web 单元测试 |
| `pnpm --filter @myteam/desktop typecheck` | 运行桌面端类型检查 |
| `pnpm --filter @myteam/desktop test` | 运行桌面端单元测试 |
| `make check` | 执行完整验证流水线 |
| `make worktree-env` | 生成 worktree 专用的 `.env.worktree` |

## 开发说明

- CI 会执行前端构建、类型检查、单元测试，桌面端类型检查与单元测试，以及基于 PostgreSQL + pgvector 的后端 Go 测试。
- 需要隔离开发环境时，可使用 `make setup-worktree` 与 `make start-worktree`。
- 仓库内部仍有一部分历史遗留的 `MYTEAM_` 环境变量前缀和 `myteam` 二进制命名；日常使用可以优先走上面列出的 `myteam` 入口。

## 更多文档

- [SELF_HOSTING.md](SELF_HOSTING.md)
- [CLI_AND_DAEMON.md](CLI_AND_DAEMON.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
