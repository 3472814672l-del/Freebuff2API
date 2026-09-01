# Freebuff2API 项目进度

> 最后更新：2026-09-01 (UTC+8)
> 状态：**已归档** — 账号被封禁，服务已下线

---

## 当前状态

Freebuff 账号 (`84cf8e9b-...`) 已被 Codebuff 封禁 (`account_suspended`)。
所有模型（包括 base2-free root、base2 子 agent、base3 agent、辅助子代理）均返回 403 `account_suspended`。

服务器部署已于 2026-09-01 全部清理完毕。

---

## 清理记录 (2026-09-01)

### 服务器端 (Azure VM 4.190.163.46)

| 操作 | 状态 |
|:---|:---:|
| 停止并禁用 `freebuff2api.service` | ✅ |
| 删除 `/etc/systemd/system/freebuff2api.service` | ✅ |
| 删除 `/opt/freebuff2api/` 目录 | ✅ |
| 删除所有 `/tmp/test_*` 和 `/tmp/freebuff*` 临时文件 | ✅ |
| 从 cloudflared 配置中移除 `fb.hnnilovey.me` 路由 | ✅ |
| 重启 cloudflared 服务（其他隧道不受影响） | ✅ |

### 本地 (d:\34728\Azure\freebuff2api\)

删除的文件（30+ 个）：
- 测试脚本：`test_*.js`, `test_*.sh`, `test_*.mjs`, `test_*.py`, `test_*.json`
- 编译产物：`freebuff2api.exe`, `freebuff2api-test.exe`, `freebuff2api-linux-amd64`, `freebuff2api.zip`
- 调试工具：`sniff-proxy.js`, `mitm_sniff.py`, `test_bun_h2.exe`
- 多余文档：`OPTIMIZATION.md`, `Dockerfile`

保留的文件（19 个）：
- 核心源码：`main.go`, `config.go`, `models.go`, `model_meta.go`, `server.go`, `run_manager.go`, `upstream.go`, `token_count.go`, `anthropic.go`, `free_session.go`, `bun_h2.go`
- 配置：`config.example.json`, `go.mod`, `go.sum`
- 文档：`README.md`, `README_zh.md`, `PROGRESS.md`, `DEPLOY_PLAN.md`, `LICENSE`

---

## 历史技术总结

### 架构

Go 单文件代理服务，将 OpenAI/Claude 协议请求转发到 Codebuff Freebuff 免费层 API。

核心设计：
- **Run Tree**：`base2-free` 根 Run 自动保活 + 子 Agent Run (`file-picker`, `code-reviewer-mimo`) 挂载在根 Run 之下
- **模型注册表**：从 Codebuff GitHub 源码动态拉取 agent→model 映射，6 小时刷新
- **双协议**：OpenAI `/v1/chat/completions` + Claude `/v1/messages`，含流式 SSE 支持

### 关键技术发现

1. `free_mode_cli_required` 并非 TLS/HTTP2 指纹检测，而是业务协议参数不合法时的通用防刷错误
2. Agent Run 必须构成调用树：子 Run 的 `ancestorRunIds` 必须包含根 Run ID
3. Go 切片 `nil` 序列化为 JSON `null`，上游要求 `[]`
4. 模型与 Agent 必须精确绑定（如 `google/gemini-2.5-flash-lite` ↔ `file-picker`）

### Freebuff 可用模型清单（源码提取）

| 模型 ID | Agent | 状态 |
|:---|:---|:---:|
| `deepseek/deepseek-v4-pro` | `base2-free-deepseek` | ⚠️ 已暂停 |
| `deepseek/deepseek-v4-flash` | `base2-free-deepseek-flash` | ✅ 可用* |
| `openai/gpt-5.6-luna` | `base2-free-luna` | ✅ 可用* |
| `z-ai/glm-5.2` | `base2-free-glm` | ✅ 可用* |
| `z-ai/glm-5.3-flash` | `base2-free-glm-5-3-flash` | ✅ 可用* |
| `mimo/mimo-v2.5` | `base2-free-mimo` | ✅ 可用* |
| `upstage/solar-pro4` | `base2-free-solar-pro4` | ✅ 可用* |
| `crof/kimi-k3-eco` | `base2-free-kimi-k3-eco` | ✅ 可用* |
| `anthropic/claude-fable-5` | `base2-free-fable` | 限时体验* |
| `meta/muse-spark-1.2-contributor` | `base2-free-muse-spark` | ✅ 可用* |
| `minimax/minimax-m3` | `base2-free-minimax-m3` | ⚠️ 已暂停 |
| `stealth/ox-alpha` | `base2-free-ox-alpha` | ⚠️ 已暂停 |
| `google/gemini-2.5-flash-lite` | `file-picker` | 辅助子代理 |
| `google/gemini-3.5-flash-lite` | `file-picker-max` | 辅助子代理 |

*标注"可用"的模型需要有效账号，当前账号已封禁不可用。

### 凭据（已失效）

- AuthToken: `84cf8e9b-d7a3-409e-a592-fef7ac351ead` (已封禁)
- API Key: `102ad809f438a6bcba4c36ec4d11c632c6a76109ec5a36a3`
- 公网入口: `fb.hnnilovey.me` (已从 cloudflared 移除)
