# FreeAiGo

FreeAiGo 是一个基于 Go 的多账号 AI 代理网关，用于统一管理多个上游 AI 账号或 API Token，并对外提供 OpenAI-compatible API。

它适合在本地、内网或自托管环境中搭建一个轻量的 AI Token 池：统一鉴权、模型映射、账号路由、额度记录、请求日志和故障自动切换。

## 特性

- OpenAI-compatible 代理接口：`/v1/models`、`/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`
- 多账号管理：支持账号启停、分组、排序、权重、状态和订阅到期时间
- 平台密钥：为客户端生成独立访问密钥，避免暴露真实上游 Token
- 模型映射：将客户端模型名映射到不同上游模型和账号组
- 自动路由：根据模型、账号状态、额度、权重和优先级选择可用账号
- 失败切换：认证失败、限流、额度不足、网络错误等场景可自动切换账号
- 流式响应：支持 SSE 转发，流式开始后不再切换账号，避免响应拼接错误
- 额度管理：记录 Token、余额、窗口额度、刷新时间和账号健康状态
- 请求日志：记录命中账号、上游模型、错误类型、延迟、Token 用量和切换原因
- 上游适配：通过 [`github.com/wfu-work/proxy-api-lib`](https://github.com/wfu-work/proxy-api-lib) 处理 Responses、Chat Completions、Provider 预设、usage 查询和错误归一

## 适用场景

- 你有多个可合法使用的 AI API Key，希望统一路由和管理
- 你需要给本地工具或团队成员提供一个稳定的 OpenAI-compatible Base URL
- 你想隐藏真实上游 Token，只向客户端分发可控的平台密钥
- 你希望在账号限流、余额不足或上游失败时自动切换备用账号
- 你需要审计请求日志、账号状态、额度和用量

## 安全边界

FreeAiGo 只用于管理用户自己拥有或已被授权使用的账号与 Token。

本项目不提供，也不鼓励以下用途：

- 自动注册账号
- 绕过验证码或平台风控
- 伪造身份
- 刷量或滥用免费额度
- 未授权使用第三方账号或 Token

请遵守上游服务条款和当地法律法规。

## 技术栈

- Go
- Gin
- GORM
- SQLite
- `github.com/wfu-work/nav-common-go-lib`
- `github.com/wfu-work/proxy-api-lib`

## 快速开始

### 1. 获取代码

```bash
git clone https://github.com/wfu-work/free-ai-go.git
cd free-ai-go
```

### 2. 准备配置

项目默认读取 `config.yaml`。发布后的二进制首次运行时，如果程序所在目录没有 `config.yaml`，会自动从内嵌默认配置复制一份到该目录。你也可以通过 `-c /path/to/config.yaml` 或 `NAV_CONFIG` 指定配置文件。

关键配置示例：

```yaml
system:
  addr: 48760
  router-prefix: /api

sqlite:
  db-name: freeai
  path: ./data/

freeai:
  proxy-prefix: /v1
  default-upstream-base-url: "https://api.openai.com/v1"
  request-timeout-seconds: 120
  stream-idle-timeout-seconds: 60
  max-retries: 1
  routing-strategy: weighted_round_robin
  secret-key-file: ./data/master.key
  log-prompt-content: false
```

### 3. 启动服务

```bash
go run .
```

或使用 Makefile：

```bash
make run
```

服务启动后默认提供：

- 管理 API：`http://127.0.0.1:48760/api`
- AI 代理 API：`http://127.0.0.1:48760/v1`

## 客户端接入

将客户端配置为 OpenAI-compatible 格式：

```text
Base URL: http://127.0.0.1:48760/v1
API Key: FreeAiGo 中创建的平台密钥
```

示例请求：

```bash
curl http://127.0.0.1:48760/v1/chat/completions \
  -H "Authorization: Bearer <your-platform-key>" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4.1",
    "messages": [
      {"role": "user", "content": "用一句话介绍 FreeAiGo"}
    ]
  }'
```

## 核心概念

### 账号

账号代表一个上游 AI Provider 的访问凭据，例如 OpenAI-compatible API Key、Bearer Token 或登录回调 Token。账号密钥会加密保存，只展示脱敏提示。

账号支持：

- Provider 和 Base URL
- 认证类型
- 支持模型列表
- 分组、优先级、权重
- 启用状态
- 额度和订阅到期信息
- 最近使用时间和失败计数

### 平台密钥

平台密钥是 FreeAiGo 分发给客户端的访问密钥，不等于上游 Token。

平台密钥支持：

- 启用和禁用
- 请求频率限制
- 绑定模型
- 限制可访问模型
- 设置 reasoning effort 和 service tier 覆盖
- 记录最后使用时间

### 模型映射

模型映射用于将客户端请求的公开模型名映射到真实上游模型。

例如客户端请求 `gpt-4.1`，FreeAiGo 可以根据配置路由到某个 Provider、账号组和具体上游模型。

### 路由与切换

请求进入代理层后，FreeAiGo 会按以下流程处理：

```text
校验平台密钥
  -> 解析模型
  -> 查找模型映射
  -> 选择可用账号
  -> 解密账号 Token
  -> 调用 proxy-api-lib 请求上游
  -> 根据结果更新额度、日志和账号状态
```

当上游返回认证失败、限流、额度不足、超时、网络错误或 5xx 时，系统会在允许的范围内切换到下一个可用账号。

## API 概览

代理接口：

```text
GET  /v1/models
POST /v1/chat/completions
POST /v1/responses
POST /v1/embeddings
```

管理接口按模块挂载在 `system.router-prefix` 下，默认是 `/api`，包括：

- 账号管理
- 账号分组
- 平台密钥
- 模型映射
- 额度管理
- 请求日志
- 运行配置
- 备份与恢复

具体接口以 `routers/` 和 `apis/` 目录中的实现为准。

## 构建

运行测试：

```bash
make test
```

运行完整检查：

```bash
make check
```

构建当前平台二进制：

```bash
make build
```

构建多平台二进制：

```bash
make build-all
```

默认输出到 `bin/` 目录。

## 工程结构

```text
.
├── apis/         # HTTP handler
├── domains/      # GORM 数据模型
├── inits/        # 应用初始化
├── routers/      # 路由注册
├── scheduleds/   # 定时任务
├── services/     # 核心业务逻辑
├── utils/        # 加密、安全和工具函数
├── webs/         # 内置静态资源
├── config.yaml   # 默认配置
├── Makefile
└── main.go
```

## 与 proxy-api-lib 的关系

FreeAiGo 负责业务编排：

- 账号池
- 平台密钥鉴权
- 模型映射
- 路由选择
- 额度状态
- 请求日志
- 自动切换

`proxy-api-lib` 负责协议和 Provider 细节：

- OpenAI-compatible Responses 调用
- Chat Completions 到 Responses 的兼容转换
- SSE 流式事件处理
- Provider 预设
- Usage 查询
- 上游错误归一

这样可以让 FreeAiGo 专注于网关和账号池逻辑，避免在应用层重复实现协议细节。

## 开发

安装依赖并整理模块：

```bash
go mod tidy
```

运行测试：

```bash
go test ./...
```

提交前建议运行：

```bash
make check
```

## 贡献

欢迎提交 Issue 和 Pull Request。

建议在提交 PR 前确认：

- 代码已通过 `gofmt`
- `go test ./...` 通过
- 新增功能有必要的测试或说明
- 不提交真实 Token、密钥、数据库文件或本地日志

## License

本项目基于 MIT License 开源，详见 [LICENSE](LICENSE)。
