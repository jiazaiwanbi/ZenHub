# ZenHub

ZenHub 是一个面向 AI CLI / SDK 的本地代理与控制面同步实验项目。

当前仓库已经具备这些核心能力：

- 本地 `localhost` OpenAI 风格代理入口
- `direct` / `relay` 两种数据面路径
- provider group 负载均衡
- 社区服务端单用户同步
- 启动前拉取、退出后上传、冲突检测与手动解决
- 客户端托盘模式入口

它目前更适合作为：

- 本地试用项目
- 架构演示和简历项目
- 后续继续产品化的基础版本

还不算“已经打包完成、可直接对外长期交付”的成熟产品。

## 仓库结构

- [cmd/client](/Users/psp/project/ZenHub/cmd/client): 纯命令行客户端，本地启动代理并在退出时同步
- [cmd/gui](/Users/psp/project/ZenHub/cmd/gui): 桌面托盘客户端
- [cmd/server-community](/Users/psp/project/ZenHub/cmd/server-community): 开源基础社区服务端
- [sample-config.json](/Users/psp/project/ZenHub/sample-config.json): 客户端配置示例
- [requirements_CN.md](/Users/psp/project/ZenHub/requirements_CN.md): 需求文档
- [DELIVERY_TODO.md](/Users/psp/project/ZenHub/DELIVERY_TODO.md): 交付级待办清单

## 环境要求

- Go `1.26.1` 或兼容版本
- macOS 或 Windows 更适合客户端托盘模式
- MySQL 用于 `server-community`

## 快速开始

### 1. 启动命令行客户端

```bash
go run ./cmd/client
```

如果不传 `-config`，客户端会自动使用默认配置目录，并在第一次启动时创建一份 starter config。

也可以显式指定配置文件：

```bash
go run ./cmd/client -config /path/to/config.json
```

### 2. 启动托盘客户端

```bash
go run ./cmd/gui
```

托盘客户端和命令行客户端共用同一份客户端配置文件。

### 3. 启动社区服务端

先准备环境变量：

```bash
export ZENHUB_SERVER_DATABASE_DSN='user:pass@tcp(127.0.0.1:3306)/zenhub?parseTime=true'
export ZENHUB_SERVER_ADMIN_USERNAME='admin'
export ZENHUB_SERVER_ADMIN_PASSWORD='secret-pass'
export ZENHUB_SERVER_TOKEN_SECRET='0123456789abcdef'
```

然后启动：

```bash
go run ./cmd/server-community
```

可选参数：

```bash
go run ./cmd/server-community \
  -listen 127.0.0.1:8081 \
  -bootstrap-config /path/to/config.json
```

## 默认客户端目录

当 `cmd/client` 或 `cmd/gui` 没有传 `-config` 时，会使用默认客户端目录。

目录规则基于 `os.UserConfigDir()`：

- macOS: `~/Library/Application Support/ZenHub`
- Linux: `~/.config/ZenHub`
- Windows: `%AppData%\\ZenHub`

当前约定的关键文件：

- `config.json`: 客户端主配置
- `config.json.sync-state.json`: 同步状态文件
- `logs/client.log`: 预留日志目录位置

## starter config 行为

第一次运行客户端且默认配置不存在时，会自动生成一份 starter config。

这份配置默认包含两条演示路由：

- `gpt-4o-mini` 走 `direct`
- `relay-model` 走 `relay`

默认环境变量约定：

- `OPENAI_API_KEY`
- `ZENHUB_COMMUNITY_PASSWORD`
- `ZENHUB_COMMUNITY_TOKEN`

如果你不需要 relay，同步也可以保持关闭。

## 配置文件说明

完整示例见 [sample-config.json](/Users/psp/project/ZenHub/sample-config.json)。

主要字段：

- `listen`: 本地代理监听地址
- `observability.max_records`: 最近请求观测缓冲区大小
- `sync`: 同步配置
- `codex`: 本地 Codex live 配置目录和当前已切换的 provider 记录
- `routes`: 模型到路径/分组的映射
- `provider_groups`: provider 分组、节点、超时、重试和被动健康检查配置

### `sync` 字段

- `enabled`: 是否启用同步
- `server_url`: 社区服务端地址
- `username` / `username_env`: 同步用户名
- `password` / `password_env`: 同步密码

建议：

- 优先使用 `*_env` 字段，不要把密码和 token 直接写进配置文件

### `provider_groups[].nodes[]` 里的认证字段

- `api_key`: 直接写入的节点密钥
- `api_key_env`: 从环境变量读取节点密钥

优先级规则：

- 如果 `api_key` 非空，运行时优先使用 `api_key`
- 只有当 `api_key` 为空时，才会读取 `api_key_env`

建议：

- 交付别人长期使用时，优先使用 `api_key_env`
- 仅在本地临时调试时再考虑把 `api_key` 直接写进配置

### `provider_groups[].codex` 字段

如果某个 provider group 还需要被写入本机 Codex 的 live 配置，可以给它补一段 `codex`：

- `auth`: 要写入 `auth.json` 的 JSON 对象
- `config`: 要写入 `config.toml` 的文本

客户端托盘里的 `Switch Codex Provider` 会：

- 首次切换前备份当前 live 的 `auth.json` 和 `config.toml`
- 切走前把当前 live 配置回填到上一个 provider group 的本地模板
- 原子覆盖本机 Codex live 配置目录

## 社区服务端环境变量

- `ZENHUB_SERVER_LISTEN`: 服务监听地址，默认 `127.0.0.1:8081`
- `ZENHUB_SERVER_DATABASE_DSN`: MySQL DSN
- `ZENHUB_SERVER_ADMIN_USERNAME`: 管理员用户名
- `ZENHUB_SERVER_ADMIN_PASSWORD`: 管理员密码
- `ZENHUB_SERVER_TOKEN_SECRET`: token 签名密钥，至少 16 个字符
- `ZENHUB_SERVER_TOKEN_TTL`: token 生命周期，默认 `24h`
- `ZENHUB_SERVER_BOOTSTRAP_CONFIG`: 启动时导入的初始快照配置

## 已支持的主要接口

客户端本地接口：

- `POST /v1/chat/completions`
- `GET /v1/models`

社区服务端接口：

- `POST /api/v1/auth/login`
- `GET /api/v1/sync/status`
- `POST /api/v1/sync/pull`
- `POST /api/v1/sync/push`
- `GET /api/v1/catalog/providers`
- `GET /api/v1/models`
- `POST /api/v1/relay/chat/completions`

## 同步与冲突

当前同步模型：

- 启动前先拉取
- 退出后再上传
- 支持手动同步
- 支持本地/云端冲突检测

冲突出现后，当前托盘模式支持：

- `Sync Providers`
- `Keep Local Snapshot`
- `Keep Cloud Snapshot`

## 测试与构建

运行所有测试：

```bash
go test ./...
```

构建主要入口：

```bash
go build ./cmd/client ./cmd/gui ./cmd/server-community
```

## 当前边界

这个版本仍然有这些边界：

- 还没有正式安装包和自动更新
- 托盘模式已经实现，但仍建议做真实桌面环境手工验收
- 协议扩展目前以 OpenAI 风格主链路为主
- 更完整的多用户中心服务能力还不在这个仓库里

## 下一步

如果目标是“可以交给别人长期使用”，下一步建议直接照着 [DELIVERY_TODO.md](/Users/psp/project/ZenHub/DELIVERY_TODO.md) 的 `P0` 往下做。
