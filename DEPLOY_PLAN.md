# Freebuff2API 部署计划

> 创建时间：2026-09-01
> 目标服务器：Azure VM (Japan West, 4.190.163.46)

---

## 一、项目背景

### 1.1 什么是 Freebuff

[Freebuff](https://freebuff.com)（前身为 Codebuff）是一个**广告资助的免费 AI 编程助手**，提供 CLI、桌面端、Web 构建器、云端 IDE 和 Chat 等产品。用户无需 API Key、无需信用卡即可使用多种 AI 模型。

#### 当前免费模型列表（2026-09-01 从 Codebuff 源码确认）

以下模型 ID 来源于 `freebuff-model-ids.ts` 和 `freebuff-models.ts` 中的常量定义，是 Freebuff2API 实际向上游发送的模型标识符：

| 模型 ID | 显示名称 | 说明 | 状态 |
|---|---|---|---|
| `xiaomi/mimo-2.5` | MiMo 2.5 | 小米 MiMo 2.5，多模态，当前**默认 fallback 模型** | ✅ 免费可用 |
| `z-ai/glm-5.3-flash` | GLM 5.3 Flash | 智谱 GLM 5.3 Flash，当前**推荐默认模型** (2026-08-30 起) | ✅ 免费可用 |
| `z-ai/glm-5.2` | GLM 5.2 | 智谱 GLM 5.2，需邀请解锁 | ✅ 免费可用 |
| `openai/gpt-5.6-luna` | GPT-5.6 Luna | OpenAI GPT-5.6 Luna，**Premium 模型**（有每日 session 限制） | ⚠️ Premium |
| `upstage/solar-pro4` | Solar Pro 4 | Upstage Solar Pro 4，实验性，**Premium** | ⚠️ Premium |
| `anthropic/claude-fable-5` | Claude Fable 5 | Anthropic Claude Fable 5，限时试用 | ⚠️ 限时 |
| `meta/muse-spark-1.2-contributor` | Muse Spark 1.2 | Meta Muse Spark 1.2 Contributor，仅 Web 端，60 RPM 限制 | ⚠️ Web only |
| `deepseek/deepseek-v4-flash` | DeepSeek V4 Flash | DeepSeek V4 Flash，**已于 2026-08-18 暂停免费** | ❌ 已暂停 |
| `deepseek/deepseek-v4-pro` | DeepSeek V4 Pro | DeepSeek V4 Pro，**已于 2026-08-26 暂停免费** | ❌ 已暂停 |
| `stealth/ox-alpha` | Ox Alpha | 匿名前沿模型，**已于 2026-08-27 暂停免费** | ❌ 已暂停 |
| `minimax/minimax-m3` | MiniMax M3 | MiniMax M3，**已于 2026-08-20 暂停免费**（成本过高 $213/hr） | ❌ 已暂停 |
| `crof/kimi-k3-eco` | Kimi K3 Eco | Kimi K3 Eco，仅 Web 端 | ⚠️ Web only |
| `google/gemini-3.1-pro-preview` | Gemini 3.1 Pro | Google Gemini 3.1 Pro Preview | ⚠️ Premium |

#### Freebuff2API `models.go` 中的硬编码 fallback 已过时

Freebuff2API 项目 `models.go` 文件中硬编码的 fallback 模型列表已**严重过时**：

```go
// models.go 中的 hardcodedFallback（过时）
"base2-free":         {"minimax/minimax-m2.7", "z-ai/glm-5.1"},      // ← M3 已暂停，GLM 5.1 不存在
"file-picker":        {"google/gemini-2.5-flash-lite"},               // ← 已不存在
"file-picker-max":    {"google/gemini-3.1-flash-lite-preview"},        // ← 已不存在
"editor-lite":        {"minimax/minimax-m2.7", "z-ai/glm-5.1"},      // ← 已过时
```

**实际可用模型应为**：
- `base2-free` → `xiaomi/mimo-2.5`、`z-ai/glm-5.3-flash`、`z-ai/glm-5.2`
- `base2-free-deepseek-flash` → `deepseek/deepseek-v4-flash`（已暂停，会被 coerce 到 fallback）
- `base2-free-luna` → `openai/gpt-5.6-luna`（Premium，有 session 限制）
- `base2-free-glm` → `z-ai/glm-5.2`
- `base2-free-glm-5-3-flash` → `z-ai/glm-5.3-flash`
- `base2-free-mimo` → `xiaomi/mimo-2.5`

> **注意**：Freebuff2API 的 `ModelRegistry` 会每 6 小时从 GitHub 源文件 `free-agents.ts` 动态抓取最新模型列表，运行时会自动覆盖硬编码 fallback。硬编码仅作为远程抓取失败时的降级方案。**建议更新 `models.go` 中的 `hardcodedFallback` 以匹配最新模型列表。**

#### 暂停/恢复机制

Freebuff 使用 "PAUSE" 而非 "DELETE" 策略管理模型：
- **已暂停的模型**仍被服务器识别，但请求会被自动**强制转换为回退模型**（coerce）
- 这保证了旧客户端不会因模型下线而报错，而是静默降级
- 恢复模型只需将其从 `FREEBUFF_PAUSED_FREE_MODEL_IDS` 中移除

当前暂停列表（`FREEBUFF_PAUSED_FREE_MODEL_IDS`）：
1. `minimax/minimax-m3` — 2026-08-20 暂停（成本 $213/hr）
2. `deepseek/deepseek-v4-pro` — 2026-08-26 暂停（成本过高）
3. `stealth/ox-alpha` — 2026-08-27 暂停（匿名主机结束免费促销）

### 1.2 什么是 Freebuff2API

[Freebuff2API](https://github.com/Quorinex/Freebuff2API) 是一个 Go 编写的**反向代理网关**，将 Freebuff 的内部 API 转换为标准 OpenAI 兼容接口，核心能力包括：

| 功能 | 说明 |
|---|---|
| OpenAI 兼容 API | `/v1/chat/completions`、`/v1/models` 标准端点 |
| Claude 兼容 API | `/v1/messages`、`/v1/messages/count_tokens`（Anthropic 格式互转） |
| 多 Token 轮换 | 支持配置多个 Freebuff authToken，自动轮换、冷却、负载均衡 |
| Free Session 管理 | 自动创建/刷新/轮询上游免费 session，含排队室（waiting room）逻辑 |
| 请求伪装 | 动态注入 `run_id`、`client_id`、`freebuff_instance_id` 等元数据，模拟官方 SDK 行为 |
| API Key 鉴权 | 可选的客户端鉴权，防止中转站被他人滥用 |
| HTTP 代理 | 支持配置上游 HTTP 代理（本场景不需要，VM 在日本直连即可） |

### 1.3 为什么需要部署

| 需求 | 说明 |
|---|---|
| **绕开地区限制** | Freebuff 部分区域有限制（限制为 1 个活跃项目等），国内直接访问体验不佳。Azure VM 在日本西部，属于 Freebuff 正常服务区域 |
| **统一 API 入口** | 本地工具（Cursor、CatPaw、curl 等）只需配置一个 OpenAI 兼容的 Base URL + API Key，即可使用 Freebuff 的免费模型 |
| **私人号池** | 注册多个 Freebuff 账号，Token 池轮换，提升并发吞吐量和稳定性 |
| **零成本** | Azure 学生免费额度 + Freebuff 免费层 + Cloudflare Tunnel 免费 = $0/月 |

---

## 二、当前环境

### 2.1 Azure VM 状态

| 项目 | 值 |
|---|---|
| VM 名称 | `linux` |
| 区域 | Japan West（日本西部） |
| 规格 | B2ats v2（2 vCPU, 1GB RAM） |
| 公网 IP | `4.190.163.46` |
| OS | Ubuntu (x86_64) |
| 运行时间 | 96+ 天 |
| 月费 | $0（学生免费额度 750h/月覆盖） |
| SSH | `ssh -i keys/azure.pem azureuser@4.190.163.46` |

### 2.2 当前资源使用

| 指标 | 当前值 | 部署后预估 | 余量 |
|---|---|---|---|
| 内存 | 848MB 总量 / 379MB 使用 / 321MB 可用 | 额外 ~15-25MB（二进制） | ✅ 够用 |
| CPU | 2 vCPU，负载 0.03（空闲） | IO 密集型，CPU 占用极低 | ✅ 够用 |
| 磁盘 | 29GB / 6.4GB 使用 (22%) | 额外 ~10MB（二进制） | ✅ 够用 |
| Swap | 无 | 建议加 512MB 保险 | ⚠️ 待添加 |

### 2.3 已运行服务

| 服务 | 端口 | 用途 |
|---|---|---|
| Xray | 127.0.0.1:8080 | VLESS+WS 代理（经 CF Tunnel 对外） |
| Nginx | 0.0.0.0:80 | 订阅配置分发（Basic Auth） |
| Cloudflared | 443 | Cloudflare Named Tunnel |
| SSH | 22 | 远程管理 |
| Docker | — | ❌ 未安装（不建议安装，省内存） |
| Go | — | ❌ 未安装（本地交叉编译后上传） |

### 2.4 Cloudflare Tunnel 配置

当前 `vl.hnnilovey.me` → `127.0.0.1:8080`（VLESS），可新增路由暴露 Freebuff2API。

---

## 三、部署方案对比

### 方案 A：本地交叉编译 + 二进制上传 + systemd 服务（⭐ 推荐）

```
Windows 本地 (Go 交叉编译)
  → freebuff2api-linux-amd64 二进制
  → SCP 上传到 VM
  → systemd 服务管理
  → Nginx 反代 / CF Tunnel 暴露
```

| 优点 | 缺点 |
|---|---|
| 内存占用最小（~15-25MB） | 需要本地安装 Go 编译环境 |
| 无 Docker 引擎开销 | 更新需重新编译上传 |
| systemd 管理稳定（自启、重启） | |
| 启动快、磁盘占用小 | |

### 方案 B：Docker 部署

```
VM 安装 Docker → docker run ghcr.io/quorinex/freebuff2api:latest
```

| 优点 | 缺点 |
|---|---|
| 部署简单，一行命令 | Docker 引擎占 100-150MB 内存 |
| 更新方便（docker pull） | 1GB 内存 VM 上内存压力较大 |
| 隔离性好 | 需要安装 Docker（当前未装） |

### 方案 C：VM 上直接 Go 编译

```
VM 安装 Go → git clone → go build → systemd
```

| 优点 | 缺点 |
|---|---|
| 无需本地环境 | Go 编译消耗大量内存（可能 OOM） |
| 后续更新方便 | 1GB 内存编译可能失败 |

### 方案选择：**方案 A**

理由：VM 仅 1GB 内存，避免 Docker 引擎开销；本地交叉编译零成本，二进制仅 ~10MB。

---

## 四、实施计划

### 阶段一：准备 Token（用户操作）

1. 访问 `https://freebuff.llm.pm`
2. 用 GitHub/Google 登录 Freebuff 账号
3. 获取 authToken
4. （可选）注册多个账号，获取多个 token 组成号池

### 阶段二：本地交叉编译

1. 本地安装 Go 1.23+（如未安装）
2. 在 `d:\34728\Azure\freebuff2api\` 目录下执行交叉编译：
   ```powershell
   $env:GOOS = "linux"
   $env:GOARCH = "amd64"
   $env:CGO_ENABLED = "0"
   go build -ldflags="-s -w" -trimpath -o freebuff2api-linux-amd64 .
   ```
3. 验证二进制大小（预计 ~10-15MB）

### 阶段三：VM 环境准备

1. **添加 Swap 文件**（512MB 保险）
   ```bash
   sudo fallocate -l 512M /swapfile
   sudo chmod 600 /swapfile
   sudo mkswap /swapfile
   sudo swapon /swapfile
   echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
   ```

2. **创建运行目录**
   ```bash
   sudo mkdir -p /opt/freebuff2api
   ```

### 阶段四：部署二进制

1. **SCP 上传二进制**
   ```powershell
   scp -i keys/azure.pem freebuff2api-linux-amd64 azureuser@4.190.163.46:/tmp/
   ```

2. **移动到安装目录**
   ```bash
   sudo mv /tmp/freebuff2api-linux-amd64 /opt/freebuff2api/freebuff2api
   sudo chmod +x /opt/freebuff2api/freebuff2api
   ```

3. **创建配置文件** `/opt/freebuff2api/config.json`
   ```json
   {
     "LISTEN_ADDR": "127.0.0.1:8090",
     "UPSTREAM_BASE_URL": "https://www.codebuff.com",
     "AUTH_TOKENS": ["token1", "token2"],
     "ROTATION_INTERVAL": "6h",
     "REQUEST_TIMEOUT": "15m",
     "API_KEYS": ["你的自定义API密钥"],
     "HTTP_PROXY": ""
   }
   ```
   - `LISTEN_ADDR` 设为 `127.0.0.1:8090`，仅本地监听，通过 Nginx/CF Tunnel 对外
   - `API_KEYS` 设置一个强随机密钥，防止未授权访问

### 阶段五：systemd 服务

创建 `/etc/systemd/system/freebuff2api.service`：

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

```bash
sudo systemctl daemon-reload
sudo systemctl enable freebuff2api
sudo systemctl start freebuff2api
sudo systemctl status freebuff2api
```

### 阶段六：对外暴露（二选一）

#### 路线 6A：Nginx 反代（简单，走 80 端口已有 Basic Auth）

在 `/etc/nginx/sites-available/freebuff2api` 新增：

```nginx
server {
    listen 80;
    server_name 4.190.163.46;

    # 复用已有的 Basic Auth 或单独配置
    auth_basic "Freebuff2API";
    auth_basic_user_file /etc/nginx/.htpasswd;

    location /fb/ {
        proxy_pass http://127.0.0.1:8090/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_buffering off;              # SSE 流式响应必须关闭缓冲
        proxy_read_timeout 900s;          # 15min 超时
        proxy_connect_timeout 10s;
    }
}
```

访问方式：`http://4.190.163.46/fb/v1/chat/completions`

#### 路线 6B：Cloudflare Tunnel（⭐ 推荐，HTTPS + 隐藏 IP）

修改 `/etc/cloudflared/config.yml`：

```yaml
ingress:
  - hostname: "vl.hnnilovey.me"
    service: http://127.0.0.1:8080
  - hostname: "fb.hnnilovey.me"           # 新增
    service: http://127.0.0.1:8090
  - service: http_status:404
```

在 Cloudflare DNS 添加 `fb.hnnilovey.me` CNAME → Tunnel。

访问方式：`https://fb.hnnilovey.me/v1/chat/completions`

### 阶段七：本地验证

```bash
# 查看可用模型
curl https://fb.hnnilovey.me/v1/models \
  -H "Authorization: Bearer 你的API密钥"

# 测试对话
curl https://fb.hnnilovey.me/v1/chat/completions \
  -H "Authorization: Bearer 你的API密钥" \
  -H "Content-Type: application/json" \
  -d '{"model":"minimax/minimax-m2.7","messages":[{"role":"user","content":"你好"}]}'

# 健康检查
curl https://fb.hnnilovey.me/healthz
```

### 阶段八：本地客户端接入

在任意 OpenAI 兼容客户端中配置：

```json
{
  "baseURL": "https://fb.hnnilovey.me/v1",
  "apiKey": "你的API密钥",
  "model": "minimax/minimax-m2.7"
}
```

---

## 五、费用总结

| 项目 | 费用 |
|---|---|
| Azure VM (B2ats v2) | $0/月（学生免费额度） |
| Freebuff2API | $0（开源 MIT） |
| Codebuff 上游 API | $0（广告资助免费层） |
| Cloudflare Tunnel | $0（免费） |
| 域名 hnnilovey.me | $0（学生包免费） |
| 额外带宽 | $0（CF Tunnel 中转不消耗 VM 公网带宽） |
| **合计** | **$0/月** |

---

## 六、风险评估与应对

| 风险 | 严重度 | 应对 |
|---|---|---|
| Freebuff 修改 API 或关停 | 🟡 中 | Freebuff2API 项目持续维护；多 Token 池降低单一账号风险 |
| Freebuff ToS 禁止代理使用 | 🟡 中 | 仅个人使用，不对外公开；设置 API_KEYS 鉴权 |
| VM 内存不足 OOM | 🟢 低 | 添加 512MB swap；二进制部署内存占用仅 ~25MB |
| Freebuff 检测异常并封号 | 🟡 中 | 多账号轮换；项目已内置伪装机制（User-Agent、session ID 等） |
| CF Tunnel 单点故障 | 🟢 低 | 已稳定运行 96+ 天；可随时切换为 Nginx 直连方案 |

---

## 七、后续可选优化

| 优化项 | 优先级 | 说明 |
|---|---|---|
| Prometheus metrics | 🟢 低 | 添加 `/metrics` 端点，监控请求数、延迟、token 池状态 |
| 结构化日志 | 🟢 低 | 替换 `log.Printf` 为 slog，加 request ID |
| Dockerfile 非 root 用户 | 🟢 低 | 安全加固 |
| 正则预编译 | 🟢 低 | `models.go` 中正则表达式每次刷新重新编译 |
| 自动更新模型列表 | — | 已内置（6h 刷新） |
| 多地域容灾 | 🟢 低 | 如需更高可用性，可在 DO 或 Oracle Cloud 部署第二节点 |

---

## 八、执行检查清单

- [ ] 用户获取 Freebuff authToken(s)
- [ ] 本地安装 Go 1.23+
- [ ] 交叉编译 Linux amd64 二进制
- [ ] VM 添加 512MB swap
- [ ] SCP 上传二进制到 VM
- [ ] 创建配置文件 `config.json`（填入 token 和 API key）
- [ ] 创建 systemd service 并启动
- [ ] 验证 `curl http://127.0.0.1:8090/healthz` 正常
- [ ] 配置 Nginx 反代 或 CF Tunnel 新增路由
- [ ] 本地 `curl` 验证端到端可用
- [ ] 配置客户端接入
