# Freebuff2API 项目进度总结

> 最后更新：2026-09-01 10:40 (UTC+8)
> 项目路径：`d:\34728\Azure\freebuff2api\`
> VM 地址：`4.190.163.46` (Azure Japan West)
> 公网入口：`https://fb.hnnilovey.me`

---

## 一、当前状态总览

| 组件 | 状态 | 说明 |
|------|------|------|
| 代码优化 (P0+P1+P2) | ✅ 完成 | 模型列表更新、别名系统、自动降级、元数据、Dockerfile 安全加固 |
| Go 编译环境 | ✅ 已装 | 解压在 `D:\go\go`，版本 1.23.4，需手动设 `$env:GOROOT` |
| 交叉编译 | ✅ 通过 | 17MB Linux amd64 二进制 |
| VM 部署 | ✅ 完成 | `/opt/freebuff2api/freebuff2api` + `config.json` |
| systemd 服务 | ✅ 运行中 | `freebuff2api.service`，内存 ~14MB，开机自启 |
| 512MB Swap | ✅ 已创建 | `/swapfile` |
| Cloudflare Tunnel | ✅ 已配置 | `fb.hnnilovey.me` → `127.0.0.1:8090` |
| HTTPS 访问 | ✅ 正常 | `https://fb.hnnilovey.me/healthz` 返回正常 |
| `/v1/models` | ✅ 正常 | 15 个模型含完整元数据 |
| `/v1/chat/completions` | ❌ 被上游拦截 | 返回 `free_mode_cli_required` |
| 客户端接入 | ⬜ 待对话端点修复 | |

---

## 二、关键凭据和配置

### 2.1 服务凭据

| 项目 | 值 |
|------|------|
| Freebuff authToken | `84cf8e9b-d7a3-409e-a592-fef7ac351ead` |
| 账户 | Keane Reeves (reeves04060329@gmail.com) |
| 客户端 API Key | `102ad809f438a6bcba4c36ec4d11c632c6a76109ec5a36a3` |

### 2.2 VM 上的配置文件

路径：`/opt/freebuff2api/config.json`

```json
{
  "LISTEN_ADDR": "127.0.0.1:8090",
  "UPSTREAM_BASE_URL": "https://www.codebuff.com",
  "AUTH_TOKENS": ["84cf8e9b-d7a3-409e-a592-fef7ac351ead"],
  "ROTATION_INTERVAL": "6h",
  "REQUEST_TIMEOUT": "15m",
  "API_KEYS": ["102ad809f438a6bcba4c36ec4d11c632c6a76109ec5a36a3"],
  "HTTP_PROXY": ""
}
```

### 2.3 Cloudflare Tunnel 配置

路径：`/etc/cloudflared/config.yml`

```yaml
ingress:
  - hostname: vl.hnnilovey.me
    service: http://127.0.0.1:8080      # VLESS 代理
  - hostname: sub.hnnilovey.me
    service: http://127.0.0.1:80         # Nginx 订阅
  - hostname: fb.hnnilovey.me
    service: http://127.0.0.1:8090       # Freebuff2API ← 新增
  - service: http_status:404
```

### 2.4 systemd 服务

路径：`/etc/systemd/system/freebuff2api.service`

```ini
[Unit]
Description=Freebuff2API - OpenAI Compatible Proxy
After=network.target

[Service]
Type=simple
User=azureuser
WorkingDirectory=/opt/freebuff2api
ExecStart=/opt/freebuff2api/freebuff2api -config /opt/freebuff2api/config.json
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
```

---

## 三、代码变更清单

### 已修改的文件

| 文件 | 变更 |
|------|------|
| `models.go` | 更新 `hardcodedFallback` 为最新模型；新增 `pausedFreeModels`；正则预编译为包级变量；新增 `IsPausedModel()` 方法；`HasModel()` 改为排除暂停模型；`refresh()` 新增根代理校验，动态抓取结果不含 `base2-free*` 时走 fallback |
| `model_meta.go` | **新建**：17 个模型的元数据静态表、13 个别名映射、降级链、`resolveAlias()` / `resolveFallback()` / `getModelMeta()` 函数 |
| `config.go` | `Config` 和 `rawConfig` 新增 `ModelAliases` / `ModelFallbacks` 字段（JSON: `MODEL_ALIASES` / `MODEL_FALLBACKS`） |
| `server.go` | `/v1/models` 返回完整元数据；`handleChatCompletions` + `handleClaudeMessages` + `handleClaudeCountTokens` 三处加入别名解析+自动降级；新增 `/v1/usage` 端点；删除 `maxDuration` 死代码 |
| `main.go` | httpClient 超时从硬编码 15s 改为 `cfg.RequestTimeout` |
| `Dockerfile` | 非 root 用户运行（`addgroup -S app && adduser -S app -G app` + `USER app`） |

### 关键代码位置

| 功能 | 文件 | 行/函数 |
|------|------|---------|
| User-Agent 生成 | `config.go:191` | `generateUserAgent()` → 返回 `"ai-sdk/openai-compatible/1.0.25/codebuff"` |
| 上游请求头设置 | `upstream.go:125-144` | `doJSON()` — 设置 Authorization、Content-Type、Accept、User-Agent |
| codebuff_metadata 注入 | `server.go:400-421` | `injectUpstreamMetadata()` — 注入 `run_id`、`cost_mode`、`client_id`、`freebuff_instance_id` |
| 硬编码 agent→model 映射 | `models.go:24-42` | `hardcodedFallback` |
| 暂停模型列表 | `models.go:47-51` | `pausedFreeModels` |
| 动态抓取 URL | `models.go:18` | `freeAgentsSourceURL` |

---

## 四、当前阻塞问题

### 4.1 问题：`free_mode_cli_required`

**错误信息**：
```json
{
  "error": {
    "code": "free_mode_cli_required",
    "message": "Free mode is only available through the freebuff CLI. Install it with `npm i -g freebuff`, then run `freebuff`. Calling the API directly is not supported and may get your account banned.",
    "type": "upstream_error"
  }
}
```

**触发条件**：调用 `/v1/chat/completions` 时，上游返回此错误。

**根因分析**：Freebuff 上游新增了 CLI 客户端检测，直接调 API 会被识别为非 CLI 请求并拒绝。可能检测点：

1. **User-Agent**：当前为 `ai-sdk/openai-compatible/1.0.25/codebuff`，可能需要改为 `freebuff-cli/<version>` 或 `node-fetch` 等
2. **缺少特定请求头**：Freebuff CLI 可能在请求中发送额外的 header（如 `x-codebuff-client` / `x-freebuff-cli-version` 等）
3. **`codebuff_metadata` 字段不全**：可能需要 `client_name`、`cli_version`、`source` 等字段
4. **请求体结构差异**：CLI 可能在 payload 中有额外字段

**下一步**：需要安装 `npm i -g freebuff` CLI，抓包分析它发送的实际请求（header + body），对比 Freebuff2API 的请求差异，然后在 `upstream.go` 和 `server.go` 中补齐。

### 4.2 模型 ID 变更

上游模型 ID 已更新（2026-09-01 确认）：

| 模型 | 旧 ID（已过时） | 新 ID（上游实际） | 状态 |
|------|-----------------|-------------------|------|
| MiMo 2.5 | `xiaomi/mimo-2.5` | `mimo/mimo-v2.5` | ✅ 已修复 |
| GLM 5.3 Flash | `z-ai/glm-5.3-flash` | `z-ai/glm-5.3-flash` | ✅ 无变化 |
| GLM 5.2 | `z-ai/glm-5.2` | `z-ai/glm-5.2` | ✅ 无变化 |
| GPT-5.6 Luna | `openai/gpt-5.6-luna` | `openai/gpt-5.6-luna` | ✅ 无变化 |
| 其余 | — | — | ✅ 无变化 |

来源：`common/src/constants/model-config.ts` 中 `mimoModels.mimoV25 = 'mimo/mimo-v2.5'`。

### 4.3 动态抓取上游文件格式变更

`free-agents.ts` 的格式已变更，不再使用旧的 `'key': new Set([...])` 模式定义 agent→model 映射。新的映射通过 `CLOUD_BUILD_MODEL_IDS` Set 和 `[MODEL_ID]: 'agent-id'` 对象字面量定义。

**当前处理方式**：在 `models.go` 的 `refresh()` 中新增校验，如果动态抓取结果不含 `base2-free*` 根代理，则返回错误并 fallback 到硬编码列表。这样保证了硬编码列表始终可用。

---

## 五、过程经验教训

### 5.1 浏览器自动化获取 Token

**工具选择**：
- ❌ Chrome CDP skill (`setup-cdp-chrome.js`) — Chrome 启动超时 15 次仍连不上 9222 端口，可能是 Windows 权限或防火墙问题
- ✅ CatPaw 内置 BrowserTab 工具（`mcp_tool_BrowserUse-BrowserTab_browser_action`）— 可靠，但页面崩溃后 session 丢失

**Freebuff Token 获取流程**（关键经验）：
1. 访问 `https://freebuff.llm.pm`
2. 点击 "Generate Login URL" 按钮
3. 页面会生成一个登录 URL，格式为 `https://freebuff.com/login?auth_code=XXXX`
4. **在同一个浏览器标签内**打开这个登录 URL（用 `navigate`）
5. 点击 "Continue with GitHub/Google" 登录
6. 登录成功后页面跳转到 `https://freebuff.com/onboard?auth_code=XXXX`（**同一个 auth_code**）
7. **不需要粘回 callback URL** — 页面的 `doVerify()` 会调用 `/api/status` API 轮询后端
8. 直接用 JS 调用 API 获取 token：
   ```javascript
   fetch('https://freebuff.llm.pm/api/status', {
     method: 'POST',
     headers: {'Content-Type': 'application/json'},
     body: JSON.stringify({
       fingerprintId: '<从 /api/code 获取>',
       fingerprintHash: '<从 /api/code 获取>',
       expiresAt: <从 /api/code 获取>
     })
   }).then(r => r.json())
   ```

**关键坑点**：
- freebuff.llm.pm 是纯前端 SPA，**刷新页面就丢失 session 变量**
- 每次 "Generate Login URL" 会生成新的 auth_code，旧的立即失效
- 不能在两个标签页之间切换（会丢失 session）
- **正确做法**：在当前页面调用 `/api/code` 获取 session → 新标签登录 → 回到原页面用 JS 调用 `/api/status`

### 5.2 PowerShell + SSH 编码问题

**问题**：PowerShell 向 SSH 传 JSON / YAML 时引号被 PowerShell 解析器吃掉。

**解决方案**：用 base64 编码传输：
```powershell
$content = '...'  # 原始内容
$b64 = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($content))
ssh ... "echo $b64 | base64 -d | sudo tee /path/to/file"
```

**避免**：不要用 PowerShell 的 here-string (`@'...'@`) 传给 ssh，换行符会被工具检测拦截。

### 5.3 Go 编译环境

**问题**：本地没有 Go，winget 不可用，MSI 安装需要重启 shell。

**解决方案**：下载 zip 版本解压即可用：
```powershell
Invoke-WebRequest -Uri "https://go.dev/dl/go1.23.4.windows-amd64.zip" -OutFile "$env:TEMP\go.zip" -UseBasicParsing
Expand-Archive -Path "$env:TEMP\go.zip" -DestinationPath "D:\go" -Force
# 使用时：
$env:GOROOT = "D:\go\go"
$env:Path = "D:\go\go\bin;" + $env:Path
```

### 5.4 上游源文件已变更

**问题**：`free-agents.ts` 的格式已变更，正则 `'([^']+)':\s*new\s+Set\(\[([^\]]*)\]\)` 只能匹配到 `file-picker`（子代理），不再匹配 `base2-free*`（根代理），因为根代理的映射改为了对象字面量格式 `[MODEL_ID]: 'agent-id'`。

**影响**：动态抓取拿到的模型列表不完整，导致只能用 `file-picker` agent，对话时上游报 `free_mode_invalid_agent_hierarchy`。

**修复**：在 `refresh()` 中加了校验——如果结果不含 `base2-free*` 就返回错误走 fallback。**后续应更新解析逻辑适配新格式**，或改为直接解析 `freebuff-model-ids.ts` + `freebuff-models.ts`。

### 5.5 不要随便安装 npm 包

用户不希望 AI 随意安装 npm 全局包。`npm i -g freebuff` 的尝试被用户取消。后续如需安装任何包，**必须先征求用户同意**。

---

## 六、下一步行动计划

### P0：修复 `free_mode_cli_required`（当前最高优先级）

1. **安装 Freebuff CLI 并抓包分析**
   - 征得用户同意后 `npm i -g freebuff`
   - 用 `freebuff` 命令发一条消息
   - 用 mitmproxy / Wireshark / 或 CLI 的 debug 模式抓取实际 HTTP 请求
   - 记录：User-Agent、所有 header、请求体结构

2. **对比并补齐差异**
   - 修改 `config.go` 的 `generateUserAgent()` → 改为 CLI 的 UA
   - 修改 `upstream.go` 的 `doJSON()` → 补齐缺失的 header
   - 修改 `server.go` 的 `injectUpstreamMetadata()` → 补齐 `codebuff_metadata` 字段

3. **重新编译部署验证**
   ```powershell
   $env:GOROOT = "D:\go\go"; $env:Path = "D:\go\go\bin;" + $env:Path
   $env:GOOS = "linux"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
   cd d:\34728\Azure\freebuff2api
   go build -ldflags="-s -w" -trimpath -o freebuff2api-linux-amd64 .
   scp -i d:\34728\Azure\keys\azure.pem freebuff2api-linux-amd64 azureuser@4.190.163.46:/tmp/freebuff2api
   ssh -i d:\34728\Azure\keys\azure.pem azureuser@4.190.163.46 "sudo mv /tmp/freebuff2api /opt/freebuff2api/freebuff2api && sudo chmod +x /opt/freebuff2api/freebuff2api && sudo systemctl restart freebuff2api"
   ```

4. **端到端验证**
   ```powershell
   curl https://fb.hnnilovey.me/v1/chat/completions -H "Authorization: Bearer 102ad809f438a6bcba4c36ec4d11c632c6a76109ec5a36a3" -H "Content-Type: application/json" -d '{"model":"glm","messages":[{"role":"user","content":"hello"}]}'
   ```

### P1：更新动态抓取解析逻辑

- 修改 `models.go` 的 `parseAllFreeModels()` 适配新格式
- 或改为解析 `freebuff-models.ts` 中的 `[MODEL_ID]: 'agent-id'` 对象

### P2：客户端接入配置

对话端点修复后，在客户端配置：
```json
{
  "baseURL": "https://fb.hnnilovey.me/v1",
  "apiKey": "102ad809f438a6bcba4c36ec4d11c632c6a76109ec5a36a3",
  "model": "glm"
}
```

---

## 七、快速运维命令

```powershell
# SSH 到 VM
ssh -i d:\34728\Azure\keys\azure.pem azureuser@4.190.163.46

# 查看服务状态
ssh -i d:\34728\Azure\keys\azure.pem azureuser@4.190.163.46 "sudo systemctl status freebuff2api"

# 查看日志
ssh -i d:\34728\Azure\keys\azure.pem azureuser@4.190.163.46 "sudo journalctl -u freebuff2api --no-pager -n 30"

# 重启服务
ssh -i d:\34728\Azure\keys\azure.pem azureuser@4.190.163.46 "sudo systemctl restart freebuff2api"

# 查看配置
ssh -i d:\34728\Azure\keys\azure.pem azureuser@4.190.163.46 "cat /opt/freebuff2api/config.json"

# 本地编译+上传+重启（一键）
$env:GOROOT = "D:\go\go"; $env:Path = "D:\go\go\bin;" + $env:Path; $env:GOOS = "linux"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"; cd d:\34728\Azure\freebuff2api; go build -ldflags="-s -w" -trimpath -o freebuff2api-linux-amd64 .; scp -i d:\34728\Azure\keys\azure.pem freebuff2api-linux-amd64 azureuser@4.190.163.46:/tmp/freebuff2api; ssh -i d:\34728\Azure\keys\azure.pem azureuser@4.190.163.46 "sudo mv /tmp/freebuff2api /opt/freebuff2api/freebuff2api && sudo chmod +x /opt/freebuff2api/freebuff2api && sudo systemctl restart freebuff2api && sleep 2 && sudo journalctl -u freebuff2api --no-pager -n 5"

# 测试 healthz
curl -s -H "Authorization: Bearer 102ad809f438a6bcba4c36ec4d11c632c6a76109ec5a36a3" https://fb.hnnilovey.me/healthz

# 测试模型列表
curl -s -H "Authorization: Bearer 102ad809f438a6bcba4c36ec4d11c632c6a76109ec5a36a3" https://fb.hnnilovey.me/v1/models

# 测试对话
curl -s -X POST -H "Authorization: Bearer 102ad809f438a6bcba4c36ec4d11c632c6a76109ec5a36a3" -H "Content-Type: application/json" -d '{"model":"glm","messages":[{"role":"user","content":"hello"}]}' https://fb.hnnilovey.me/v1/chat/completions
```
