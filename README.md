# FreeAiGo

FreeAiGo 是一个基于 ChatGPT/Codex OAuth 登录账号的本地账号池和 OpenAI-compatible 网关。上游账号不使用 OpenAI Platform API Key，也不接受裸 Bearer Token、登录回调地址或自定义中转站配置。

下游客户端仍通过 FreeAiGo 自己签发的“平台密钥”访问 `/v1`。平台密钥只用于保护本地网关，不会被发送给 ChatGPT 上游。

## 核心链路

```text
Codex OAuth 账号 JSON
  └─ AES-GCM 加密存储
      ├─ /backend-api/wham/usage             额度窗口
      ├─ /backend-api/accounts/check/...     账号与权益
      ├─ /backend-api/subscriptions          订阅补充信息
      ├─ /backend-api/codex/models           官方 Codex 模型
      └─ /backend-api/codex/responses        模型请求与额度响应头
             ├─ 正常响应头采样
             ├─ 主动最小请求探测
             └─ 本地请求日志、故障切换与账号调度
```

ChatGPT 内部接口不是公开稳定的 OpenAI Platform API。具体协议集中封装在相邻的 `proxy-api-lib/chatgpt` 与 `proxy-api-lib/codexauth` 包中，业务层不直接解析其原始 JSON。

## 能力

- 导入、更新和敏感导出规范 OAuth 账号文件。
- 使用主密钥对完整账号文件进行 AES-GCM 加密。
- Access Token 到期前自动刷新；Refresh Token 轮换后原子持久化。
- 上游返回 401 时强制刷新并只重试一次。
- 同步常见 5 小时、7 天以及 Code Review、Spark 等附加额度窗口。
- 从正常请求响应头和主动探测响应头补充额度快照。
- 同时查询 `accounts/check` 与 `subscriptions`，合并套餐、到期、续费和订阅状态。
- 按模型、账号组、状态、额度、优先级、权重或最低已用比例调度账号。
- 认证失败、限流、额度耗尽、网络和上游错误时切换备用账号。
- 记录本地平台密钥、命中账号、模型、延迟、Token 用量和切换原因。
- 平台密钥明文只在创建成功时返回一次，列表和详情只显示不可用于鉴权的前缀。
- 请求进入上游前事务预占平台 Token 额度，完成后按真实用量原子结算。
- 按官方模型、服务等级、普通输入、缓存输入和输出 Token 估算参考成本。
- 限制单请求大小和全局并发数；启用 Redis 后跨实例共享 RPM 限流和轮询游标。
- 对下游提供 `/v1/models`、`/v1/chat/completions` 和 `/v1/responses`。

不支持 `/v1/embeddings`，也没有手工写入账号额度的管理接口。账号额度只来自上游真实信号。

## OAuth 账号文件

导入接口只接受下列规范结构。示例值均为占位符：

```json
{
  "tokens": {
    "access_token": "<oauth-access-token>",
    "id_token": "<oauth-id-token>",
    "refresh_token": "<oauth-refresh-token>",
    "account_id": "<chatgpt-account-id>"
  },
  "meta": {
    "label": "name@example.com",
    "issuer": "https://auth.openai.com",
    "status": "active",
    "workspaceId": "<workspace-id>",
    "chatgptAccountId": "<chatgpt-account-id>",
    "exportedAt": 0
  }
}
```

相同 `account_id` 再次导入会更新原账号的加密凭据，不会创建重复账号。旧 API Key、裸 Token 或旧认证字段不会被猜测转换；迁移时没有规范 `encrypted_account_file` 的旧账号会被禁用。

## 额度语义

额度记录保存窗口的真实语义，而不是虚构余额：

| 字段 | 含义 |
| --- | --- |
| `usedPercent` | 当前窗口已使用百分比 |
| `limitWindowSeconds` | 上游给出的窗口长度 |
| `resetAt` | 窗口重置时间 |
| `allowed` | 上游是否允许继续请求 |
| `limitReached` | 上游是否明确达到限制 |
| `source` | `wham`、`response_header` 或 `active_probe` |
| `nextRefreshAt` | 本地下次计划同步时间 |

`usedPercent` 不是剩余 Token 数，也不是金额。窗口已经重置后，旧快照会被删除，等待下一次 wham 或响应头采样重建。

## 账号状态与调度

- `available`：可以参与调度。
- `limited` / `cooldown`：限流或连续上游错误，等待冷却。
- `exhausted`：有效额度窗口明确耗尽。
- `invalid`：OAuth 刷新或认证失败。
- `expired`：订阅明确不续费且已经到期。
- `disabled`：管理员手动禁用。

订阅 `willRenew=true` 的账号不会仅因为当前账期结束时间已到就被错误移出账号池。路由会过滤阻断额度，并可在 `most_quota_remaining` 策略下比较每个账号最紧张窗口的 `usedPercent`。

## 管理接口

管理接口默认位于 `/api`：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/api/accounts/import` | 导入或更新 OAuth 账号文件 |
| `GET` | `/api/accounts/list` | 分页查询账号及额度窗口 |
| `GET` | `/api/accounts/list/all` | 查询全部账号 |
| `GET` | `/api/accounts/:guid` | 查询账号详情 |
| `PUT` | `/api/accounts/:guid` | 更新账号组、模型与调度参数 |
| `DELETE` | `/api/accounts/:guid` | 删除账号及其额度快照 |
| `GET` | `/api/accounts/:guid/export` | 导出含令牌的规范账号文件 |
| `POST` | `/api/accounts/:guid/refresh-usage` | 同步 wham、订阅与权益 |
| `POST` | `/api/accounts/:guid/probe` | 最小 Codex 请求并采样响应头 |
| `POST` | `/api/accounts/fetch-models` | 获取官方 Codex 模型清单 |
| `POST` | `/api/accounts/:guid/enable` | 启用账号 |
| `POST` | `/api/accounts/:guid/disable` | 禁用账号 |
| `POST` | `/api/accounts/reorder` | 更新优先级和权重 |
| `GET` | `/api/quotas/list` | 分页查询真实额度快照 |
| `GET` | `/api/accounts/:guid/quotas` | 查询单账号额度快照 |
| `GET` | `/api/ops/account-health` | 查询账号、订阅和额度健康状态 |

敏感导出响应使用 `Cache-Control: no-store`。前端会在下载前进行二次确认。

## 下游代理接口

```text
GET  /v1/models
POST /v1/chat/completions
POST /v1/responses
```

客户端使用本地平台密钥：

```bash
curl http://127.0.0.1:8787/v1/responses \
  -H "Authorization: Bearer sk-your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"your-public-model","input":"Hello"}'
```

这里的 `sk-...` 是 FreeAiGo 创建的下游访问凭据，不是上游 OpenAI API Key。

进程存活检查为 `GET /healthz`。部署就绪检查为 `GET /readyz`；只有数据库、可用官方账号、启用模型、启用平台密钥以及至少一组真实可路由组合都满足时才返回 HTTP 200，否则返回 HTTP 503 和未就绪原因。

## 配置与运行

关键配置位于 `config.yaml`：

```yaml
system:
  addr: 8787
  router-prefix: /api

freeai:
  proxy-prefix: /v1
  request-timeout-seconds: 120
  stream-idle-timeout-seconds: 60
  max-retries: 1
  routing-strategy: weighted_round_robin
  quota-refresh-seconds: 180
  cooldown-seconds: 300
  cleanup-log-retention-days: 30
  secret-key-file: ./data/master.key
  log-prompt-content: false
  max-request-body-bytes: 8388608
  max-concurrent-requests: 128
  quota-default-reserve-tokens: 8192
  quota-reservation-ttl-seconds: 1800
```

单实例可以保持 `system.use-redis: false`。多实例部署应启用 Redis，使平台密钥 RPM 限流和账号轮询游标在实例之间保持一致。

启动：

```bash
go run .
```

测试：

```bash
go test ./...
```

前端构建产物位于 `webs/freeai-web.zip`，由 `webs` 包嵌入发布后的后端程序。

## 安全边界

- 只导入本人拥有或获授权使用的 OAuth 账号。
- 不要把导出的账号 JSON、数据库、主密钥或日志发送给第三方。
- `master.key` 与数据库必须一起备份；缺少其中任一项都无法恢复账号文件。
- JWT 未验证解析只用于展示元信息和账号路由，不用于授权判断。
- 默认不记录提示词正文；不要在错误日志中输出令牌或完整账号文件。
- 公网部署必须放在 HTTPS 反向代理之后，并在反向代理层配置连接数、请求超时和可信来源限制；不要直接暴露数据库、主密钥或管理接口。
- 本项目不实现自动注册、验证码绕过、身份伪造或滥用免费额度。
