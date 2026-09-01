# Freebuff2API 优化建议

> 创建时间：2026-09-01
> 目标：向正式 API 提供商（OpenRouter / DeepSeek / OpenAI）靠齐

---

## 一、当前项目差距总览

对照 OpenRouter、DeepSeek、OpenAI 等主流 API 提供商的功能，Freebuff2API 当前的差距：

| 功能 | OpenRouter | DeepSeek | OpenAI | Freebuff2API 当前 | 优先级 |
|---|---|---|---|---|---|
| 模型列表端点 | ✅ `/v1/models` | ✅ | ✅ | ✅ 有，但信息极简 | 🔴 高 |
| 模型元数据（上下文长度、定价、能力） | ✅ 丰富 | ✅ | ✅ | ❌ 仅 id+object | 🔴 高 |
| 模型别名/友好名 | ✅ | ✅ | ✅ | ❌ 仅原始 ID | 🔴 高 |
| 模型自动降级/fallback | ✅ `route: fallback` | ❌ | ❌ | ❌ | 🔴 高 |
| 模型暂停感知 | — | — | — | ❌ 不感知上游暂停 | 🔴 高 |
| 速率限制头 | ✅ `X-RateLimit-*` | ✅ | ✅ | ❌ | 🟡 中 |
| 用量统计 | ✅ `GET /api/v1/key` | ✅ | ✅ | ❌ 仅 healthz 有 token 状态 | 🟡 中 |
| 流式响应中的错误处理 | ✅ `finish_reason: error` | ✅ | ✅ | ❌ 直接中断 | 🟡 中 |
| 结构化输出 (`response_format`) | ✅ | ✅ | ✅ | ❌ 不支持 | 🟡 中 |
| 请求/响应日志 | ✅ | ✅ | ✅ | ❌ 仅 log.Printf | 🟡 中 |
| 模型分组/标签 | ✅ | — | — | ❌ | 🟢 低 |
| 上下文压缩 | ✅ plugin | — | — | ❌ | 🟢 低 |
| Embeddings 端点 | ✅ | ✅ | ✅ | ❌ | 🟢 低 |

---

## 二、具体优化方案

### 2.1 🔴 模型列表增强：元数据 + 友好名

**当前问题**：`/v1/models` 返回的信息极度简陋，只有 `id`、`object`、`created`、`owned_by`，客户端无法知道模型的上下文长度、是否支持流式、是否多模态、是否为 Premium 等。

**当前代码** (`server.go:109-133`)：
```go
for _, model := range modelsList {
    models = append(models, map[string]any{
        "id":         model,
        "object":     "model",
        "created":    created,
        "owned_by":   "Freebuff2API",
        "root":       model,
        "permission": []any{},
    })
}
```

**优化方案**：扩展 `ModelRegistry`，为每个模型维护完整元数据：

```go
type ModelInfo struct {
    ID            string `json:"id"`
    Object        string `json:"object"`
    Created       int64  `json:"created"`
    OwnedBy       string `json:"owned_by"`
    
    // 扩展字段（OpenRouter 风格）
    Name          string `json:"name"`              // 友好显示名 "GLM 5.3 Flash"
    Description   string `json:"description"`       // 简短描述
    ContextLength int    `json:"context_length"`    // 上下文长度
    MaxTokens     int    `json:"max_tokens"`        // 最大输出 token
    Multimodal    bool   `json:"multimodal"`        // 是否支持图片输入
    Streaming     bool   `json:"streaming"`         // 是否支持流式
    Reasoning     bool   `json:"reasoning"`          // 是否支持 thinking
    Tier          string `json:"tier"`              // "standard" | "premium" | "limited"
    Premium       bool   `json:"premium"`           // 是否为 Premium（有 session 限制）
    Available     bool   `json:"available"`         // 当前是否可用（非暂停）
    Tags          []string `json:"tags,omitempty"`  // ["free","coding","multimodal"]
}
```

**数据来源**：从 `freebuff-models.ts` 和 `freebuff-model-ids.ts` 提取静态元数据，构建为内置 map。或从 `free-agents.ts` 解析时同步提取 agent→model 的 tier 信息。

**效果**：客户端（如 Cursor、LobeChat）可以显示模型详情，而非裸 ID。

---

### 2.2 🔴 模型别名与自由切换

**当前问题**：用户必须发送完整的模型 ID（如 `z-ai/glm-5.3-flash`），不支持别名、不支持自动选择。

**优化方案**：

#### 2.2.1 模型别名（Alias）

```json
// 用户发送
{"model": "glm", "messages": [...]}

// Freebuff2API 自动映射到
{"model": "z-ai/glm-5.3-flash", ...}
```

内置常用别名表：

| 别名 | 映射到 | 说明 |
|---|---|---|
| `default` / `auto` | `z-ai/glm-5.3-flash` | 当前推荐默认 |
| `glm` | `z-ai/glm-5.3-flash` | GLM 系列最新 |
| `mimo` | `xiaomi/mimo-2.5` | MiMo |
| `luna` / `gpt` | `openai/gpt-5.6-luna` | GPT-5.6 Luna |
| `flash` | `z-ai/glm-5.3-flash` | 快速模型 |
| `deepseek` | `deepseek/deepseek-v4-flash` | DeepSeek（注意已暂停，会 fallback） |

配置文件支持自定义别名：
```json
{
  "MODEL_ALIASES": {
    "my-default": "z-ai/glm-5.3-flash",
    "cheap": "xiaomi/mimo-2.5",
    "smart": "openai/gpt-5.6-luna"
  }
}
```

#### 2.2.2 自动降级（Auto-Fallback）

当请求的模型不可用（已暂停、session 用完、token 冷却）时，自动降级到可用模型：

```
请求: openai/gpt-5.6-luna (Premium)
  → session 额度用完 (429 / waiting_room)
  → 自动降级到 z-ai/glm-5.3-flash (Standard, 不限量)
  → 响应中添加 header: X-Fallback-Model: z-ai/glm-5.3-flash
  → 响应中添加 header: X-Fallback-Reason: premium_session_limit
```

配置：
```json
{
  "MODEL_FALLBACKS": {
    "openai/gpt-5.6-luna": ["z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"],
    "upstage/solar-pro4": ["z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"],
    "deepseek/deepseek-v4-flash": ["z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"]
  },
  "DEFAULT_FALLBACK": "xiaomi/mimo-2.5"
}
```

这参考了 OpenRouter 的 `route: "fallback"` 机制。

#### 2.2.3 智能路由（可选，高阶）

参考 OpenRouter 的 `models` 数组参数，允许客户端指定 fallback 链：

```json
{
  "model": "openai/gpt-5.6-luna",
  "models": ["openai/gpt-5.6-luna", "z-ai/glm-5.3-flash", "xiaomi/mimo-2.5"],
  "messages": [...]
}
```

---

### 2.3 🔴 模型暂停感知

**当前问题**：Freebuff2API 不感知上游模型暂停（`FREEBUFF_PAUSED_FREE_MODEL_IDS`），请求已暂停的模型会成功返回但被上游 coerce 到 fallback 模型，客户端不知道实际用的是什么模型。

**优化方案**：

1. **从 `free-agents.ts` 解析时同步提取暂停列表**：上游源文件中暂停的模型不会出现在 `free-agents.ts` 的 Set 中，因此 `ModelRegistry` 自然不会包含它们。但应额外维护一个"已知但暂停"的列表。

2. **返回模型状态**：`/v1/models` 中标记 `available: false` 的模型。

3. **请求暂停模型时的行为**：
   ```json
   {
     "error": {
       "message": "Model 'minimax/minimax-m3' has been paused. Falling back to 'xiaomi/mimo-2.5'.",
       "type": "model_paused",
       "code": "model_paused",
       "fallback_model": "xiaomi/mimo-2.5"
     }
   }
   ```
   或静默 fallback 并在响应 header 中提示。

---

### 2.4 🟡 更新硬编码 Fallback

**当前 `models.go` 的 hardcodedFallback**（严重过时）：
```go
"base2-free":         {"minimax/minimax-m2.7", "z-ai/glm-5.1"},  // 不存在的模型
"file-picker":        {"google/gemini-2.5-flash-lite"},           // 不存在
```

**应更新为**（从 2026-09-01 源码确认）：
```go
var hardcodedFallback = map[string][]string{
    "base2-free":             {"xiaomi/mimo-2.5", "z-ai/glm-5.3-flash", "z-ai/glm-5.2"},
    "base2-free-mimo":        {"xiaomi/mimo-2.5"},
    "base2-free-glm":         {"z-ai/glm-5.2"},
    "base2-free-glm-5-3-flash": {"z-ai/glm-5.3-flash"},
    "base2-free-luna":        {"openai/gpt-5.6-luna"},
    "base2-free-luna-es":     {"openai/gpt-5.6-luna-es"},
    "base2-free-solar-pro4": {"upstage/solar-pro4"},
    "base2-free-ox-alpha":   {"stealth/ox-alpha"},
    "base2-free-fable":      {"anthropic/claude-fable-5"},
    "base2-free-muse-spark": {"meta/muse-spark-1.2-contributor"},
    "base2-free-kimi-k3-eco": {"crof/kimi-k3-eco"},
    "base2-free-deepseek":   {"deepseek/deepseek-v4-pro"},
    "base2-free-deepseek-flash": {"deepseek/deepseek-v4-flash"},
    "base2-free-deepseek-pro-max":  {"deepseek/deepseek-v4-pro-max"},
    "base2-free-deepseek-flash-max": {"deepseek/deepseek-v4-flash-max"},
    "base2-free-luna-max":   {"openai/gpt-5.6-luna-max"},
    "base2-free-cloud-planner": {"xiaomi/mimo-2.5"},  // FALLBACK_FREEBUFF_MODEL_ID
}
```

---

### 2.5 🟡 速率限制响应头

**当前问题**：Freebuff2API 没有返回任何速率限制头，客户端无法提前感知额度耗尽。

**优化方案**：参考 OpenRouter 的 `X-RateLimit-*` 头：

```
X-RateLimit-Limit: 4
X-RateLimit-Remaining: 2
X-RateLimit-Reset: 2026-09-02T00:00:00-08:00
```

以及 Freebuff 特有的 session 限制信息：

```
X-Freebuff-Session-Status: active
X-Freebuff-Session-Expires: 2026-09-01T17:00:00Z
X-Freebuff-Premium-Used: 2
X-Freebuff-Premium-Limit: 4
X-Freebuff-Premium-Reset: 2026-09-02T00:00:00-08:00
```

数据来源：从 `RunManager.Snapshots()` 和 `free_session.go` 的 session 状态中提取。

---

### 2.6 🟡 用量统计端点

**当前问题**：只有 `/healthz` 返回 token 池状态，没有面向用户的用量查询端点。

**优化方案**：新增 `/v1/usage` 端点（参考 OpenRouter 的 `GET /api/v1/key`）：

```json
{
  "data": {
    "total_requests": 142,
    "total_tokens_in": 456789,
    "total_tokens_out": 234567,
    "premium_sessions_used": 2,
    "premium_sessions_limit": 4,
    "premium_reset_at": "2026-09-02T00:00:00-08:00",
    "tokens": [
      {
        "name": "token-1",
        "session_status": "active",
        "session_expires_at": "2026-09-01T17:00:00Z",
        "cooldown": false,
        "request_count": 87
      }
    ]
  }
}
```

---

### 2.7 🟡 流式响应错误处理

**当前问题**：流式响应中途出错时直接断开连接，客户端不知道原因。

**当前代码** (`server.go:700-720`)：`copyResponseBody` 只是简单 copy，不解析 SSE 内容。

**优化方案**：参考 OpenRouter 的 mid-stream error 机制，在流式响应中注入错误事件：

```
data: {"id":"...","choices":[{"delta":{"content":""},"finish_reason":"error"}],"error":{"code":429,"message":"Premium session limit reached"}}
```

需要在 `copyResponseBody` 中加一个"错误拦截层"：检测上游返回的 SSE 事件中是否包含错误，若则转换为标准错误 SSE 事件再转发。

---

### 2.8 🟡 结构化输出（`response_format`）

**当前问题**：不支持 `response_format: { type: "json_object" }` 和 `json_schema`。

**优化方案**：在 `injectUpstreamMetadata` 中透传 `response_format` 字段到上游。Codebuff 上游是否支持取决于其后端模型，但至少应该透传而非丢弃。

同时支持 `json_schema` 模式时，在响应中验证 JSON 格式，失败时重试或报错。

---

### 2.9 🟢 上下文压缩

**当前问题**：长对话历史会完整发送到上游，消耗大量 token。

**优化方案**：参考 OpenRouter 的 `context-compression` plugin，在发送前压缩中间消息：

1. 保留 system message + 最近 N 轮对话
2. 将更早的消息用模型生成摘要
3. 发送 `system + summary + recent_messages` 而非完整历史

这可以在客户端侧实现，也可以在代理侧实现（更透明）。

```json
{
  "context_compression": {
    "enabled": true,
    "keep_recent_turns": 5,
    "max_context_tokens": 32768
  }
}
```

---

### 2.10 🟢 请求日志与可观测性

**当前问题**：只有 `log.Printf`，无结构化日志、无 request ID。

**优化方案**：

1. 为每个请求生成 `X-Request-ID`，贯穿请求→上游→响应日志
2. 使用 Go `slog`（1.21+）替代 `log.Printf`
3. 新增 `/metrics` 端点（Prometheus 格式）：

```
# TYPE freebuff_requests_total counter
freebuff_requests_total{model="z-ai/glm-5.3-flash",status="success"} 142
freebuff_requests_total{model="openai/gpt-5.6-luna",status="error"} 3

# TYPE freebuff_request_duration_seconds histogram
freebuff_request_duration_seconds_bucket{model="z-ai/glm-5.3-flash",le="1"} 89

# TYPE freebuff_token_pool_status gauge
freebuff_token_pool_status{token="token-1",status="active"} 1
freebuff_premium_sessions_used{token="token-1"} 2
freebuff_premium_sessions_limit 4
```

---

### 2.11 🟢 模型分组与标签

**当前问题**：`/v1/models` 返回扁平列表，客户端无法区分免费/Premium/暂停。

**优化方案**：在模型元数据中添加分组信息：

```json
{
  "id": "openai/gpt-5.6-luna",
  "tier": "premium",
  "tags": ["premium", "reasoning", "gpt"],
  "max_sessions_per_day": 4,
  "premium_pool_remaining": 2
}
```

支持 `/v1/models?tier=standard` 过滤。

---

## 三、实施优先级排序

| 阶段 | 任务 | 预计工作量 | 影响面 |
|---|---|---|---|
| **P0** | 更新 `hardcodedFallback` 模型列表 | 30 分钟 | 远程抓取失败时不再用过时模型 |
| **P0** | 模型别名 + 自动降级 | 2 小时 | 用户体验质变 |
| **P1** | `/v1/models` 元数据增强 | 1 小时 | 客户端可显示模型详情 |
| **P1** | 模型暂停感知 | 1 小时 | 不再静默用错模型 |
| **P1** | 速率限制响应头 | 1 小时 | 客户端可提前感知限额 |
| **P2** | 用量统计端点 `/v1/usage` | 1.5 小时 | 用户可自查用量 |
| **P2** | 结构化日志 + request ID | 1 小时 | 运维可观测性 |
| **P2** | 流式响应错误注入 | 2 小时 | 流式中断有明确原因 |
| **P3** | 结构化输出透传 | 30 分钟 | 支持 JSON mode |
| **P3** | Prometheus metrics | 1 小时 | 生产监控 |
| **P3** | 上下文压缩 | 4 小时 | 省 token |
| **P3** | 模型分组标签 | 1 小时 | 客户端筛选 |

---

## 四、与主流 API 提供商对标

| 功能 | OpenRouter | DeepSeek | 本项目优化后 |
|---|---|---|---|
| `/v1/models` | ✅ 含定价、上下文长度、能力 | ✅ 简洁 | ✅ 含 tier、别名、可用性 |
| 模型别名 | ✅ | ❌ | ✅ |
| 自动 fallback | ✅ `route: fallback` | ❌ | ✅ 配置式 + 自动 |
| 速率限制头 | ✅ `X-RateLimit-*` | ✅ | ✅ `X-RateLimit-*` + session 头 |
| 用量查询 | ✅ `GET /v1/key` | ✅ dashboard | ✅ `GET /v1/usage` |
| 流式错误 | ✅ `finish_reason: error` | ✅ | ✅ SSE 错误注入 |
| 结构化输出 | ✅ `response_format` | ✅ | ✅ 透传 |
| 上下文压缩 | ✅ plugin | ❌ | ✅ 中间消息摘要 |
| 多 Token 池 | ❌ | ❌ | ✅ **独有** |
| Claude 兼容 | ✅ `/v1/messages` | ✅ | ✅ **已有** |
| 健康检查 | ✅ | ✅ | ✅ `GET /healthz` |

---

## 五、代码修改清单

### 需要修改的文件

| 文件 | 修改内容 |
|---|---|
| `models.go` | 1. 更新 hardcodedFallback<br>2. 扩展 ModelInfo 结构体<br>3. 新增别名解析逻辑<br>4. 新增暂停列表 |
| `config.go` | 1. 新增 MODEL_ALIASES 配置<br>2. 新增 MODEL_FALLBACKS 配置<br>3. 新增 DEFAULT_FALLBACK 配置 |
| `server.go` | 1. `/v1/models` 返回完整元数据<br>2. handleChatCompletions 加入别名解析<br>3. proxyChatRequest 加入自动降级逻辑<br>4. 响应头加入速率限制信息<br>5. 新增 `/v1/usage` 端点 |
| `run_manager.go` | 1. Snapshots 增加 session 使用计数<br>2. 暴露 premium session 剩余量 |
| `main.go` | 1. httpClient 超时从 config 读取 |

### 可能需要新增的文件

| 文件 | 内容 |
|---|---|
| `model_meta.go` | 模型元数据静态表（名称、上下文长度、能力等） |
| `aliases.go` | 别名解析逻辑 |
| `fallback.go` | 自动降级逻辑 |
| `usage.go` | 用量统计端点 |
| `metrics.go` | Prometheus 指标 |

---

## 六、总结

Freebuff2API 的核心代理逻辑已经相当成熟（多 Token 池、session 管理、Claude 兼容、流式转换），但在**模型管理**和**可观测性**方面与正式 API 提供商差距较大。

最关键的三个优化是：

1. **模型别名 + 自动降级** — 让用户不关心底层模型 ID，Premium 用完自动切免费模型
2. **模型元数据** — 让客户端知道每个模型的能力和限制
3. **更新过时的硬编码 fallback** — 防止远程抓取失败时用过时的模型列表
