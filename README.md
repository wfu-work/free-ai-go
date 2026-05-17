# FreeAiGo

FreeAiGo 是一个基于 Go 的多账号 AI Token 池与本地聚合代理服务。项目以 `nav-common-go-lib` 为基础框架，使用 Gin 提供 HTTP API，使用 SQLite + GORM 持久化账号、密钥、额度、模型映射、请求日志和路由状态。

AI Provider 请求封装、OpenAI / Responses 数据格式、工具调用、流式解析、上游错误映射等请求链路逻辑统一下沉到本地库 `proxy-api-lib`：

```text
/Users/wfu/Documents/works/xiaoxi/code/free-model/proxy-api-lib
```

FreeModelGo 不重复实现这些协议细节，只负责账号池、路由决策、密钥鉴权、额度状态、日志审计和 HTTP 管理 API。

本阶段只实现后端服务和 HTTP API，不实现前端页面。

> 账号来源边界：FreeModelGo 只管理用户手动添加、自己拥有或已被授权使用的账号与 Token。不实现自动注册账号、验证码绕过、风控绕过、伪造身份或刷量能力。

## 参考工程

基础框架用法参考本地项目：

```text
/Users/wfu/Documents/works/xiaoxi/code/recodex/relay-go
```

采用同类工程组织方式：

- `main.go` 保持极薄，只调用 `inits.Init()`。
- `inits/` 对接 `nav-common-go-lib/inits.SysInit`。
- `domains/` 存放 GORM 数据模型。
- `apis/` 存放 Gin handler。
- `routers/` 按模块注册路由。
- `services/` 存放业务逻辑。
- `scheduleds/` 存放定时任务。
- `utils/` 存放安全、脱敏、加密等工具。

代码要求：

- 不把所有结构体、服务和 API 写到一个文件中。
- 每个业务模块独立拆分 domain、api、router、service。
- handler 只做参数绑定、调用 service、返回 response。
- service 负责事务、状态流转、路由选择和上游调用。
- GORM 模型单独放在 `domains/`，并由 `inits/registerTables()` 统一 AutoMigrate。

## 核心目标

- 手动添加和维护多个 AI 平台账号或 Token。
- 对账号进行启停、排序、权重、标签和分组管理。
- 记录账号额度、限流状态、订阅状态、恢复时间和健康度。
- 对外提供本地 OpenAI-compatible API。
- 根据模型、额度、状态和权重自动选择可用账号。
- 调用 `proxy-api-lib` 完成上游请求、流式转发、Responses 兼容和工具调用处理。
- 上游账号限流或失效时，根据 `proxy-api-lib` 返回的错误分类自动切换到下一个可用账号。
- 记录请求日志、失败原因、Token 估算、命中账号和切换原因。
- 使用平台密钥保护本地代理 API，避免直接暴露上游 Token。

## 技术选型

- 基础框架：`github.com/wfu-work/nav-common-go-lib`
- HTTP 框架：Gin
- 数据库：SQLite
- ORM：GORM
- 配置：沿用 `nav-common-go-lib` 配置结构，使用 `config.yaml`
- 日志：沿用 `nav-common-go-lib/global.NAV_LOG`
- 数据库实例：沿用 `nav-common-go-lib/global.NAV_DB`
- 定时任务：沿用 `nav-common-go-lib/scheduleds`
- API 返回：沿用 `nav-common-go-lib/response`
- 鉴权中间件：优先复用 `nav-common-go-lib/middlewares`
- 请求封装：依赖本地 `proxy-api-lib`

## 三方库规划

### 基础框架与 HTTP

| 用途 | 推荐库 | 说明 |
| --- | --- | --- |
| 基础框架 | `github.com/wfu-work/nav-common-go-lib` | 复用配置、日志、数据库、JWT、路由分组、定时任务和通用响应结构。 |
| HTTP API | `github.com/gin-gonic/gin` | 管理 API 和 `/v1` 代理入口。 |
| CORS | `github.com/gin-contrib/cors` | 后续前端或本地客户端跨域访问时使用。 |
| Gzip | `github.com/gin-contrib/gzip` | 管理 API 响应压缩；流式代理不强制启用。 |

### 数据库与持久化

| 用途 | 推荐库 | 说明 |
| --- | --- | --- |
| ORM | `gorm.io/gorm` | 统一使用 GORM 模型和事务。 |
| SQLite 驱动 | `gorm.io/driver/sqlite` | 默认 SQLite 驱动，配合 `nav-common-go-lib` 初始化。 |
| 纯 Go SQLite 备选 | `github.com/glebarez/sqlite` | 如需避免 CGO，可作为 SQLite 驱动备选。 |

### AI 请求封装

| 用途 | 推荐库 | 说明 |
| --- | --- | --- |
| 统一请求库 | 本地 `proxy-api-lib` | 封装 OpenAI / Responses / OpenAI-compatible 上游请求、工具调用、SSE 流式解析、错误映射和 Provider 适配。 |
| 请求链路参考 | `router-for-me/CLIProxyAPI` | 只由 `proxy-api-lib` 参考，不作为 FreeModelGo 运行时依赖。 |
| 官方 OpenAI 类型参考 | `github.com/openai/openai-go/v3` | 如需使用，也放在 `proxy-api-lib` 内部；FreeModelGo 不直接绑定 SDK 类型。 |
| JSON 处理 | Go 标准库 `encoding/json` | FreeModelGo 仅处理管理 API 和日志字段；上游协议 JSON 处理由 `proxy-api-lib` 负责。 |
| Token 估算 | `github.com/pkoukk/tiktoken-go` | 优先放在 `proxy-api-lib`；FreeModelGo 只消费返回的 usage / estimate 结果。 |

### 代理、限流与任务

| 用途 | 推荐库 | 说明 |
| --- | --- | --- |
| 上游请求 | 本地 `proxy-api-lib` | 上游 HTTP 请求、Responses wire API、SSE 流式解析和 provider 错误映射统一由库处理。 |
| HTTP 基础能力 | Go 标准库 `net/http` | FreeModelGo 主要用于接收客户端请求、传递 request context 和写回响应。 |
| 限流 | `golang.org/x/time/rate` | 平台密钥、账号、全局请求限流。 |
| 定时任务 | `github.com/robfig/cron/v3` | 通过 `nav-common-go-lib/scheduleds` 接入。 |

### 安全与工具

| 用途 | 推荐库 | 说明 |
| --- | --- | --- |
| 密码哈希 | `golang.org/x/crypto/bcrypt` | 管理员密码或本地访问密码。 |
| Token 加密 | Go 标准库 `crypto/aes` + `crypto/cipher` | 使用 AES-GCM 加密上游 Token。 |
| Key 派生 | `golang.org/x/crypto/argon2` | 从本地主密码派生加密 key。 |
| UUID | `github.com/google/uuid` | 如 `nav-common-go-lib` 已提供 GUID 能力，则优先复用框架。 |
| 测试断言 | `github.com/stretchr/testify` | service、router、proxy 单元测试。 |

## proxy-api-lib 依赖边界

FreeModelGo 直接依赖本地 `proxy-api-lib`，但只把它当作请求执行库，不把它变成应用框架。

本地路径：

```text
/Users/wfu/Documents/works/xiaoxi/code/free-model/proxy-api-lib
```

建议 `go.mod` 依赖方式：

```go
require github.com/wfu-work/proxy-api-lib v0.0.0

replace github.com/wfu-work/proxy-api-lib => ../proxy-api-lib
```

如果 `proxy-api-lib` 后续使用不同 module path，以它的 `go.mod` 为准，FreeModelGo 只调整 `require/replace`。

### FreeModelGo 负责

- Gin HTTP API。
- 平台密钥鉴权。
- 账号、Token、额度、模型映射、请求日志持久化。
- 根据模型、账号状态、额度和权重选择账号。
- 从 SQLite 读取账号密钥并解密。
- 构造 `proxy-api-lib` 所需的 provider config、credential 和请求上下文。
- 根据 `proxy-api-lib` 返回的 usage、错误类型、流式状态更新数据库。
- 自动切换账号和记录切换原因。

### proxy-api-lib 负责

- OpenAI Responses 请求构造与发送。
- OpenAI-compatible / Codex-compatible 上游适配。
- API Key / Bearer Token 鉴权注入。
- Base URL、wire API、默认 header、HTTP client、超时和重试策略。
- Responses / Chat Completions 兼容转换。
- 工具调用与工具结果字段约定。
- SSE 流式响应解析。
- OpenAI 风格错误解析与统一错误包装。
- Token usage / estimate 的基础返回。

### 调用链路

```text
客户端请求
  -> FreeModelGo /v1 API
  -> PlatformKeyService 校验平台密钥
  -> ModelService 查询模型映射
  -> RouterService 选择账号
  -> AccountService 解密账号 Token
  -> ProxyService 调用 proxy-api-lib
  -> proxy-api-lib 请求上游并处理协议细节
  -> FreeModelGo 记录日志、额度和账号状态
  -> 返回客户端
```

### 期望的库接口

FreeModelGo 侧只需要依赖 `proxy-api-lib` 的稳定高层接口，避免接触 OpenAI SDK 内部类型。

期望能力：

```go
type ProviderConfig struct {
	Name    string
	BaseURL string
	WireAPI string
}

type Credential struct {
	Type  string
	Value string
}

type ProxyRequest struct {
	Endpoint string
	Model    string
	Body     []byte
	Stream   bool
}

type ProxyResult struct {
	StatusCode   int
	Header       http.Header
	Body         []byte
	Usage        Usage
	ErrorType    string
	FirstTokenMs int64
	LatencyMs    int64
}

type Client interface {
	Do(ctx context.Context, provider ProviderConfig, credential Credential, req ProxyRequest) (*ProxyResult, error)
	Stream(ctx context.Context, provider ProviderConfig, credential Credential, req ProxyRequest, w io.Writer) (*ProxyResult, error)
}
```

具体类型以后以 `proxy-api-lib` 实现为准。FreeModelGo README 只约束依赖方向：协议细节在库中，业务编排在 FreeModelGo 中。

## 服务形态

FreeModelGo 启动后提供两类 API。

管理 API 默认挂载在 `system.router-prefix` 下，例如：

```text
http://127.0.0.1:48760/api
```

OpenAI-compatible 代理 API 独立挂载在：

```text
http://127.0.0.1:48760/v1
```

客户端配置示例：

```text
Base URL: http://127.0.0.1:48760/v1
API Key: FreeModelGo 生成的平台密钥
```

## 功能模块

### 1. 账号管理

账号必须由用户手动添加。每个账号保存：

- 账号名称。
- 登录邮箱或备注名。
- 平台类型，例如 `freemodel`、`openai`、`anthropic`、`custom`。
- 账号类型，例如 `free`、`plus`、`team`、`custom`。
- 认证类型，例如 `api_key`、`bearer_token`、`oauth_token`、`cookie_token`。
- 加密后的上游密钥。
- 密钥脱敏提示。
- 支持模型列表。
- 分组、排序、权重。
- 启用状态。
- 最近刷新时间。
- 最近使用时间。
- 订阅到期时间。
- 备注。

账号状态：

- `available`：可用。
- `limited`：限流中。
- `cooldown`：错误后冷却中。
- `exhausted`：额度耗尽。
- `disabled`：手动禁用。
- `expired`：订阅或 Token 过期。
- `invalid`：认证失败。
- `unknown`：状态未知，等待刷新。

### 2. 平台密钥

平台密钥用于保护 FreeModelGo 本地代理 API，不等于上游账号 Token。

能力：

- 创建平台密钥。
- 删除平台密钥。
- 启用和禁用平台密钥。
- 设置密钥名称和备注。
- 设置每分钟请求限制。
- 限制可访问模型或模型分组。
- 记录最后使用时间。

安全要求：

- 平台密钥只保存哈希。
- 创建时只返回一次明文。
- 日志中不打印完整密钥。

### 3. 额度管理

每个账号可维护多个额度窗口：

- 5 小时窗口。
- 7 天窗口。
- 每日窗口。
- 月度窗口。
- 自定义窗口。

额度字段：

- 窗口类型。
- 已用百分比。
- 剩余 Token。
- 总 Token。
- 重置时间。
- 下次刷新时间。
- 额度状态。

额度刷新策略：

- 手动刷新。
- 定时刷新。
- 请求成功后更新估算用量。
- 请求失败后根据错误码刷新状态。

没有官方额度接口时，先用响应结果反推：

- `401` 标记账号为 `invalid`。
- `429` 标记账号为 `limited`。
- 上游额度不足错误标记为 `exhausted`。
- `5xx` 增加失败计数，必要时进入 `cooldown`。
- 请求成功更新最近使用时间和 Token 估算。

### 4. 模型映射

客户端请求的模型名可以映射到真实上游模型。

示例：

```yaml
models:
  gpt-4.1:
    upstream: gpt-4.1
    provider: freemodel
    account_group: default
  gpt-4o:
    upstream: gpt-4o
    provider: freemodel
    account_group: fast
  claude-sonnet:
    upstream: claude-3-7-sonnet
    provider: anthropic
    account_group: anthropic
```

模型路由需要支持：

- 模型别名。
- 上游模型名。
- 平台类型。
- 账号分组。
- 是否启用。
- 默认超时时间。
- 是否支持流式响应。

### 5. 账号路由与自动切换

请求进入代理层后，路由器按以下顺序选择账号：

```text
校验平台密钥
  -> 解析模型映射
  -> 匹配平台和账号分组
  -> 过滤禁用、限流、过期、无效账号
  -> 过滤不支持该模型的账号
  -> 按路由策略选择账号
  -> 注入上游 Token
  -> 转发请求
  -> 根据响应更新账号状态
```

路由策略：

- `round_robin`：按顺序轮询。
- `weighted_round_robin`：按权重轮询。
- `least_recently_used`：优先使用最近最少使用账号。
- `most_quota_remaining`：优先使用剩余额度高的账号。
- `priority_first`：优先使用排序靠前账号。

自动切换规则：

- 认证失败、限流、额度不足、连接失败时可切换下一个账号。
- 流式响应在尚未输出任何数据前可以切换。
- 流式响应一旦开始输出，不再切换，避免客户端收到拼接错误。
- 默认最多重试 1 次，可配置。
- 每次切换都必须记录原因。

### 6. 聚合代理 API

第一阶段实现 OpenAI-compatible API：

```text
GET  /v1/models
POST /v1/chat/completions
POST /v1/responses
POST /v1/embeddings
```

代理层职责：

- 校验 `Authorization: Bearer <platform_key>`。
- 读取请求模型。
- 调用 router 选择账号。
- 修改上游请求地址、模型名和认证头。
- 支持普通 JSON 响应。
- 支持 SSE 流式响应。
- 记录请求日志。
- 处理错误分类和自动切换。

### 7. 请求日志

每次代理请求记录：

- 请求 ID。
- 平台密钥 ID。
- 命中账号 ID。
- 请求模型。
- 上游模型。
- 上游平台。
- HTTP 状态码。
- 错误类型。
- 是否触发账号切换。
- 切换次数。
- 切换原因。
- 首 Token 延迟。
- 总耗时。
- 输入 Token 估算。
- 输出 Token 估算。
- 创建时间。

默认不保存完整 Prompt 和响应正文。调试模式下如需采样保存，必须提供脱敏处理。

### 8. 定时任务

使用 `nav-common-go-lib/scheduleds` 注册定时任务：

- 刷新账号额度。
- 恢复到期的限流账号。
- 清理过期请求日志。
- 统计账号健康度。
- 轮换或检查主密钥状态。

## 工程结构

项目采用多文件拆分，不使用单文件大杂烩。

```text
.
├── main.go
├── config.yaml
├── go.mod
├── apis/
│   ├── index.go
│   ├── account_api.go
│   ├── platform_key_api.go
│   ├── model_api.go
│   ├── quota_api.go
│   ├── proxy_api.go
│   ├── request_log_api.go
│   └── ops_api.go
├── domains/
│   ├── account.go
│   ├── account_quota.go
│   ├── model_mapping.go
│   ├── platform_key.go
│   ├── request_log.go
│   ├── route_state.go
│   ├── audit_log.go
│   └── constants.go
├── inits/
│   └── inits.go
├── routers/
│   ├── index.go
│   ├── account_router.go
│   ├── platform_key_router.go
│   ├── model_router.go
│   ├── quota_router.go
│   ├── proxy_router.go
│   ├── request_log_router.go
│   ├── ops_router.go
│   └── health_router.go
├── services/
│   ├── account_service.go
│   ├── platform_key_service.go
│   ├── model_service.go
│   ├── quota_service.go
│   ├── router_service.go
│   ├── proxy_service.go
│   ├── proxyapi_client.go
│   ├── request_log_service.go
│   ├── audit_service.go
│   └── config.go
├── scheduleds/
│   ├── index.go
│   ├── bootstrap.go
│   ├── quota_sched.go
│   ├── cooldown_sched.go
│   └── cleanup_sched.go
└── utils/
    ├── crypto.go
    ├── mask.go
    ├── token.go
    └── http.go
```

## nav-common-go-lib 初始化方式

`main.go`：

```go
package main

import "freemodel-go/inits"

func main() {
	inits.Init()
}
```

`inits.Init()` 负责：

- `OnWebInit`：注册 `/healthz` 和 `/v1` 代理入口。
- `OnTableInit`：执行 GORM AutoMigrate。
- `OnRouterInit`：注册 `/api` 管理接口。
- `OnOtherInit`：启动业务初始化逻辑。
- `OnScheInit`：注册定时任务。
- `OnShutInit`：关闭上游连接和后台任务。

表注册示例：

```go
func registerTables() {
	db := global.NAV_DB
	if err := db.AutoMigrate(
		domains.Account{},
		domains.AccountQuota{},
		domains.ModelMapping{},
		domains.PlatformKey{},
		domains.RequestLog{},
		domains.RouteState{},
		domains.AuditLog{},
	); err != nil {
		global.NAV_LOG.Error("register FreeModelGo tables failed", zap.Error(err))
		os.Exit(1)
	}
	global.NAV_LOG.Info("register FreeModelGo tables success")
}
```

## 配置文件规划

默认使用根目录 `config.yaml`。

```yaml
system:
  app-name: "FreeModelGo"
  addr: 48760
  db-type: sqlite
  router-prefix: /api

jwt:
  issuer: FreeModelGo
  signing-key: "change-me"
  expires-time: 7d
  buffer-time: 1d

local:
  oss-path: ./data/oss
  proxy-oss-path: /oss

sqlite:
  db-name: freemodel-go
  path: ./data/
  log-mode: silent

zap:
  director: logback
  level: info
  prefix: '[FreeModelGo]'
  retention-day: 7

freemodel:
  proxy-prefix: /v1
  default-upstream-base-url: "https://api.openai.com/v1"
  request-timeout-seconds: 120
  stream-idle-timeout-seconds: 60
  max-retries: 1
  routing-strategy: weighted_round_robin
  quota-refresh-seconds: 300
  cooldown-seconds: 300
  cleanup-log-retention-days: 30
  secret-key-file: ./data/master.key
  log-prompt-content: false
```

`services/config.go` 负责读取业务配置，并提供默认值。

## 数据模型草案

### accounts

```go
type Account struct {
	common.NAV_MODEL
	Name                  string `json:"name" gorm:"size:100;comment:账号名称"`
	Email                 string `json:"email" gorm:"size:255;index;comment:邮箱或备注"`
	Provider              string `json:"provider" gorm:"size:40;index;comment:平台"`
	AccountType           string `json:"accountType" gorm:"size:40;index;comment:账号类型"`
	AuthType              string `json:"authType" gorm:"size:40;comment:认证类型"`
	EncryptedSecret       string `json:"-" gorm:"comment:加密密钥"`
	SecretHint            string `json:"secretHint" gorm:"size:120;comment:密钥提示"`
	SupportedModels       string `json:"supportedModels" gorm:"comment:支持模型JSON"`
	AccountGroup          string `json:"accountGroup" gorm:"size:80;index;comment:账号分组"`
	Status                string `json:"status" gorm:"size:40;index;comment:状态"`
	Priority              int    `json:"priority" gorm:"index;comment:顺序"`
	Weight                int    `json:"weight" gorm:"comment:权重"`
	Enabled               bool   `json:"enabled" gorm:"index;comment:是否启用"`
	LastUsedAt            int64  `json:"lastUsedAt" gorm:"index;comment:最后使用时间"`
	LastRefreshedAt       int64  `json:"lastRefreshedAt" gorm:"comment:最后刷新时间"`
	SubscriptionExpiredAt int64  `json:"subscriptionExpiredAt" gorm:"index;comment:订阅过期时间"`
	FailureCount          int    `json:"failureCount" gorm:"comment:连续失败次数"`
	CooldownUntil         int64  `json:"cooldownUntil" gorm:"index;comment:冷却结束时间"`
	Remark                string `json:"remark" gorm:"comment:备注"`
}
```

### account_quotas

```go
type AccountQuota struct {
	common.NAV_MODEL
	AccountGuid     string  `json:"accountGuid" gorm:"size:50;index;comment:账号guid"`
	WindowType      string  `json:"windowType" gorm:"size:40;index;comment:窗口类型"`
	UsedPercent     float64 `json:"usedPercent" gorm:"comment:已用百分比"`
	RemainingTokens int64   `json:"remainingTokens" gorm:"comment:剩余Token"`
	TotalTokens     int64   `json:"totalTokens" gorm:"comment:总Token"`
	ResetAt         int64   `json:"resetAt" gorm:"index;comment:重置时间"`
	NextRefreshAt   int64   `json:"nextRefreshAt" gorm:"index;comment:下次刷新时间"`
	Status          string  `json:"status" gorm:"size:40;index;comment:状态"`
}
```

### model_mappings

```go
type ModelMapping struct {
	common.NAV_MODEL
	PublicModel   string `json:"publicModel" gorm:"size:100;uniqueIndex;comment:对外模型"`
	UpstreamModel string `json:"upstreamModel" gorm:"size:100;comment:上游模型"`
	Provider      string `json:"provider" gorm:"size:40;index;comment:平台"`
	AccountGroup  string `json:"accountGroup" gorm:"size:80;index;comment:账号分组"`
	Stream        bool   `json:"stream" gorm:"comment:是否支持流式"`
	TimeoutSec    int    `json:"timeoutSec" gorm:"comment:超时秒数"`
	Enabled       bool   `json:"enabled" gorm:"index;comment:是否启用"`
}
```

### platform_keys

```go
type PlatformKey struct {
	common.NAV_MODEL
	Name               string `json:"name" gorm:"size:100;comment:密钥名称"`
	KeyHash            string `json:"-" gorm:"size:128;uniqueIndex;comment:密钥哈希"`
	KeyPrefix          string `json:"keyPrefix" gorm:"size:20;index;comment:密钥前缀"`
	AllowedModels      string `json:"allowedModels" gorm:"comment:允许模型JSON"`
	RateLimitPerMinute int    `json:"rateLimitPerMinute" gorm:"comment:每分钟限制"`
	Enabled            bool   `json:"enabled" gorm:"index;comment:是否启用"`
	LastUsedAt         int64  `json:"lastUsedAt" gorm:"index;comment:最后使用时间"`
	Remark             string `json:"remark" gorm:"comment:备注"`
}
```

### request_logs

```go
type RequestLog struct {
	common.NAV_MODEL
	RequestID      string `json:"requestId" gorm:"size:80;uniqueIndex;comment:请求ID"`
	PlatformKeyID  string `json:"platformKeyId" gorm:"size:50;index;comment:平台密钥"`
	AccountGuid    string `json:"accountGuid" gorm:"size:50;index;comment:命中账号"`
	Model          string `json:"model" gorm:"size:100;index;comment:请求模型"`
	UpstreamModel  string `json:"upstreamModel" gorm:"size:100;comment:上游模型"`
	Provider       string `json:"provider" gorm:"size:40;index;comment:平台"`
	StatusCode     int    `json:"statusCode" gorm:"index;comment:状态码"`
	ErrorType      string `json:"errorType" gorm:"size:80;index;comment:错误类型"`
	Switched       bool   `json:"switched" gorm:"index;comment:是否切换"`
	SwitchCount    int    `json:"switchCount" gorm:"comment:切换次数"`
	SwitchReason   string `json:"switchReason" gorm:"comment:切换原因"`
	LatencyMs      int64  `json:"latencyMs" gorm:"comment:总耗时"`
	FirstTokenMs   int64  `json:"firstTokenMs" gorm:"comment:首Token耗时"`
	InputTokens    int64  `json:"inputTokens" gorm:"comment:输入Token"`
	OutputTokens   int64  `json:"outputTokens" gorm:"comment:输出Token"`
	CreatedAtUnix  int64  `json:"createdAtUnix" gorm:"index;comment:创建时间"`
}
```

## HTTP API 规划

### 健康检查

```text
GET /healthz
```

### 账号管理

```text
GET    /api/accounts
POST   /api/accounts
GET    /api/accounts/:guid
PUT    /api/accounts/:guid
DELETE /api/accounts/:guid
POST   /api/accounts/:guid/enable
POST   /api/accounts/:guid/disable
POST   /api/accounts/:guid/refresh
POST   /api/accounts/:guid/test
POST   /api/accounts/reorder
```

### 平台密钥

```text
GET    /api/platform-keys
POST   /api/platform-keys
DELETE /api/platform-keys/:guid
POST   /api/platform-keys/:guid/enable
POST   /api/platform-keys/:guid/disable
```

### 模型映射

```text
GET    /api/models
POST   /api/models
PUT    /api/models/:guid
DELETE /api/models/:guid
POST   /api/models/:guid/enable
POST   /api/models/:guid/disable
```

### 额度

```text
GET  /api/quotas
GET  /api/accounts/:guid/quotas
POST /api/accounts/:guid/quotas
```

### 请求日志

```text
GET    /api/request-logs
GET    /api/request-logs/:guid
DELETE /api/request-logs
```

### 运维状态

```text
GET /api/ops/metrics
GET /api/ops/stats
GET /api/ops/routes
```

### OpenAI-compatible API

```text
GET  /v1/models
POST /v1/chat/completions
POST /v1/responses
POST /v1/embeddings
```

## 服务拆分

### AccountService

- 创建账号。
- 更新账号。
- 删除账号。
- 启用和禁用账号。
- 调整顺序。
- 更新状态。
- 维护失败次数和冷却时间。

### PlatformKeyService

- 生成平台密钥。
- 哈希保存密钥。
- 校验请求密钥。
- 检查模型权限。
- 更新最后使用时间。

### QuotaService

- 查询额度。
- 写入额度。
- 根据请求结果反推额度状态。
- 计算下次刷新时间。
- 恢复限流到期账号。

### ModelService

- 管理模型映射。
- 根据 public model 查找上游模型。
- 判断模型是否支持流式响应。

### RouterService

- 根据模型和账号状态选择账号。
- 实现轮询和权重轮询。
- 跳过不可用账号。
- 记录路由状态。

### ProxyService

- 接收 OpenAI-compatible 请求。
- 根据路由结果组装 `proxy-api-lib` 的 provider config、credential 和请求参数。
- 调用 `proxy-api-lib` 完成上游请求。
- 将 `proxy-api-lib` 返回的普通响应或 SSE 流式响应写回客户端。
- 将 `proxy-api-lib` 返回的 usage、错误类型、首 Token 延迟和总耗时交给日志与额度服务。
- 根据错误分类触发账号切换，不在 FreeModelGo 内部实现 provider 协议转换。

### RequestLogService

- 写入请求日志。
- 查询请求日志。
- 清理过期日志。
- 统计成功率、错误率和平均延迟。

## 路由注册方式

`routers/index.go` 统一聚合各模块路由：

```go
type RouterGroup struct {
	HealthRouter
	AccountRouter
	PlatformKeyRouter
	ModelRouter
	QuotaRouter
	ProxyRouter
	RequestLogRouter
	OpsRouter
}

func (r *RouterGroup) InitFreeModelRouters(publicGroup *gin.RouterGroup, privateGroup *gin.RouterGroup) {
	r.InitHealthRouter(publicGroup)
	r.InitAccountRouter(privateGroup)
	r.InitPlatformKeyRouter(privateGroup)
	r.InitModelRouter(privateGroup)
	r.InitQuotaRouter(privateGroup)
	r.InitRequestLogRouter(privateGroup)
	r.InitOpsRouter(privateGroup)
}
```

`/v1` 代理路由可在 `OnWebInit` 直接挂载到 Gin engine，避免被 `/api` 前缀包裹。

## 请求处理流程

```text
客户端
  -> /v1/chat/completions
  -> ProxyApi
  -> PlatformKeyService 校验平台密钥
  -> ModelService 查询模型映射
  -> RouterService 选择账号
  -> ProxyService 调用 proxy-api-lib
  -> proxy-api-lib 处理上游请求、协议转换和流式响应
  -> QuotaService 更新额度和账号状态
  -> RequestLogService 写入日志
  -> 返回客户端
```

## 错误分类

错误类型建议统一枚举：

- `auth_failed`
- `rate_limited`
- `quota_exhausted`
- `upstream_timeout`
- `upstream_5xx`
- `network_error`
- `model_not_supported`
- `no_available_account`
- `platform_key_invalid`
- `platform_key_limited`

## 安全要求

- 上游 Token 使用本地主密钥加密保存。
- 平台密钥只保存哈希。
- 所有 API 返回值默认脱敏。
- 日志不输出完整 Token、Cookie、Authorization Header。
- 默认不保存 Prompt 和响应正文。
- 删除账号时保留请求日志中的脱敏账号引用。
- 管理 API 默认走 `privateGroup`，复用框架鉴权能力。
- `/v1` 代理 API 使用平台密钥单独鉴权。

## 开发里程碑

### M1：基础工程

- 初始化 Go module，项目名 `FreeModelGo`。
- 接入 `nav-common-go-lib`。
- 配置 SQLite。
- 实现 `inits.Init()`。
- 注册 GORM 表。
- 实现 `/healthz`。

### M2：账号和平台密钥

- 实现账号 CRUD。
- 实现账号启用、禁用、排序。
- 实现 Token 加密和脱敏。
- 实现平台密钥创建和校验。
- 实现基础请求日志。

### M3：模型映射和路由

- 实现模型映射 CRUD。
- 实现账号模型匹配。
- 实现 `round_robin`。
- 实现 `weighted_round_robin`。
- 实现不可用账号过滤。

### M4：聚合代理

- 实现 `/v1/models`。
- 实现 `/v1/chat/completions`。
- 接入本地 `proxy-api-lib`。
- 通过 `proxy-api-lib` 支持普通 JSON 转发。
- 通过 `proxy-api-lib` 支持 SSE 流式转发。
- 消费 `proxy-api-lib` 返回的上游错误分类。
- 支持失败后自动切换账号。

### M5：额度和定时任务

- 实现额度表。
- 根据 `proxy-api-lib` 返回的 usage / estimate 更新额度。
- 根据 `proxy-api-lib` 返回的 401/429/5xx 分类反推账号状态。
- 实现定时恢复限流账号。
- 实现定时清理请求日志。

### M6：运维 API

- 实现统计接口。
- 实现路由状态接口。
- 实现账号健康度接口。
- 为后续前端页面预留 API。

## 非目标

- 第一阶段不实现前端页面。
- 不实现自动注册账号。
- 不实现验证码、人机验证、风控绕过。
- 不实现代理池刷量。
- 不默认保存 Prompt 和响应正文。
- 不默认暴露到公网。
