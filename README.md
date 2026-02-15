# Docker Cloudflare WARP

## Changelog

| 日期 | 变更内容 | 原因 |
|------|---------|------|
| 2026-02-13 16:00 | v2.0 重构: 移除 GUI/VNC/ZeroTier，基于 debian:bookworm-slim + s6-overlay；新增 gost 代理层，支持 SOCKS5/Shadowsocks 外部代理 | 精简为纯 CLI 容器，减小镜像体积，增加标准代理协议支持 |
| 2026-02-12 11:30 | 新增 `.env.example`，compose 改用 `env_file` 引用 | 环境变量管理更清晰 |
| 2026-02-12 10:30 | 新增 MDM 部署模式，支持 Service Token headless 部署 | 企业级 Zero Trust 场景支持 |

纯 CLI 容器，基于 `debian:bookworm-slim` + s6-overlay，集成 Cloudflare WARP + gost 代理。

支持三种工作模式：
- **纯 WARP 模式**：不设 `PROXY_TYPE`，只运行 WARP 客户端，不启动 gost 代理
- **裸代理模式**：设 `PROXY_TYPE` 但不配置 WARP，gost 作为独立 SOCKS5/SS 代理，流量从服务器 IP 直出
- **WARP + 代理模式**：同时配置 WARP 和 `PROXY_TYPE`，流量经 Cloudflare 网络出去

**WARP + 代理架构**: 外部客户端 → gost (SOCKS5/SS) → warp-svc → Cloudflare 网络

## 快速开始

### 纯 WARP (不启动代理)

```bash
cp .env.example .env
# 编辑 .env：
#   WARP_LICENSE_KEY=xxxxxxxx-xxxxxxxx-xxxxxxxx
#   WARP_MODE=warp
# 不设 PROXY_TYPE，gost 不启动
docker compose up -d
```

### 裸代理 (不走 WARP)

```bash
cp .env.example .env
# 编辑 .env：
#   PROXY_TYPE=socks5
#   PROXY_PORT=1080
docker compose up -d
```

### WARP + 代理 (LICENSE_KEY)

```bash
cp .env.example .env
# 编辑 .env：
#   WARP_LICENSE_KEY=xxxxxxxx-xxxxxxxx-xxxxxxxx
#   WARP_MODE=proxy
docker compose up -d
```

默认暴露 SOCKS5 代理在 `1080` 端口。

## 代理模式

通过 `PROXY_TYPE` 环境变量切换，所有配置均通过 `.env` 文件管理。

### SOCKS5 (默认)

```bash
# .env
WARP_LICENSE_KEY=xxxxxxxx-xxxxxxxx-xxxxxxxx
PROXY_TYPE=socks5
PROXY_PORT=1080
```

验证:
```bash
curl -x socks5h://127.0.0.1:1080 https://www.cloudflare.com/cdn-cgi/trace
```

### Shadowsocks

```bash
# .env
WARP_LICENSE_KEY=xxxxxxxx-xxxxxxxx-xxxxxxxx
PROXY_TYPE=ss
PROXY_PORT=8388
SS_PASSWORD=your-strong-password
SS_METHOD=chacha20-ietf-poly1305
```

客户端连接: `ss://chacha20-ietf-poly1305:your-strong-password@<host>:8388`

### SOCKS5 + Shadowsocks 同时开启

```bash
# .env
WARP_LICENSE_KEY=xxxxxxxx-xxxxxxxx-xxxxxxxx
PROXY_TYPE=socks5+ss
PROXY_PORT=1080       # SOCKS5 端口
SS_PORT=8388          # Shadowsocks 端口
SS_PASSWORD=your-strong-password
SS_METHOD=chacha20-ietf-poly1305
```

需要在 `docker-compose.yml` 中同时映射两个端口:
```yaml
ports:
  - "1080:1080"
  - "8388:8388"
```

## 环境变量

### 代理配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PROXY_TYPE` | - | 代理类型: `socks5` / `ss` / `socks5+ss`。不设或 `none` 则不启动 gost |
| `PROXY_PORT` | `1080` | 外部代理端口 |
| `SS_PASSWORD` | - | Shadowsocks 密码 (ss/socks5+ss 必填) |
| `SS_METHOD` | `chacha20-ietf-poly1305` | SS 加密方式 |
| `SS_PORT` | `8388` | SS 端口 (仅 socks5+ss 模式) |

### WARP 配置

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `WARP_LICENSE_KEY` | - | WARP+ 许可证密钥 |
| `WARP_MODE` | - | LICENSE_KEY 模式的工作模式：`warp` / `doh` / `warp+doh` / `dot` / `warp+dot` / `proxy` / `tunnel_only`。不设置则不走 WARP |
| `WARP_PROXY_PORT` | `40000` | WARP proxy 模式内部监听端口 (通常不需要改) |

### MDM 部署 (企业 / Zero Trust)

通过环境变量配置 MDM 参数，容器启动时自动生成 `/var/lib/cloudflare-warp/mdm.xml`。

> **注意**：如果 volume 中已存在 `mdm.xml`（手动挂载或上次生成），脚本会跳过生成，直接使用已有文件。如需更新配置，请先删除已有的 `mdm.xml` 再重启容器。

#### 核心环境变量

| 变量 | 必填 | 说明 |
|------|:----:|------|
| `WARP_MDM_ENABLED` | ✓ | **主开关**，设为 `true` 启用 MDM 模式 |
| `WARP_ORG` | ✓ | Zero Trust 组织名 (team name) |
| `WARP_AUTH_CLIENT_ID` | ✓ | Service Token Client ID (格式: `xxx.access`) |
| `WARP_AUTH_CLIENT_SECRET` | ✓ | Service Token Client Secret |

#### 可选环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `WARP_SERVICE_MODE` | `warp` | 服务模式：`warp` / `doh` / `warp+doh` / `dot` / `warp+dot` / `proxy` / `tunnel_only` / `postureonly` |
| `WARP_TUNNEL_PROTOCOL` | `masque` | 隧道协议：`masque` / `wireguard` |
| `WARP_SWITCH_LOCKED` | `false` | 锁定开关，用户无法手动断开 |
| `WARP_AUTO_CONNECT` | - | 自动重连间隔 (0-1440 分钟) |
| `WARP_ONBOARDING` | `true` | 显示隐私政策引导页 |
| `WARP_DISPLAY_NAME` | - | 显示名称 |
| `WARP_SUPPORT_URL` | - | 支持链接 (https:// 或 mailto:) |
| `WARP_GATEWAY_ID` | - | DoH subdomain (DNS-only 模式) |
| `WARP_ENABLE_PMTUD` | `false` | Path MTU Discovery |
| `WARP_ENABLE_POST_QUANTUM` | - | 后量子加密 |
| `WARP_ENABLE_NETBT` | `false` | NetBIOS over TCP/IP (Windows 兼容) |

#### 高级环境变量 (通常不需要)

| 变量 | 说明 |
|------|------|
| `WARP_OVERRIDE_API_ENDPOINT` | 覆盖 API 端点 IP (中国网络伙伴) |
| `WARP_OVERRIDE_DOH_ENDPOINT` | 覆盖 DoH 端点 IP |
| `WARP_OVERRIDE_WARP_ENDPOINT` | 覆盖 WARP 端点 IP:Port |
| `WARP_EMERGENCY_SIGNAL_URL` | External Emergency Disconnect URL |
| `WARP_EMERGENCY_SIGNAL_FINGERPRINT` | Emergency Signal 证书指纹 (SHA-256) |
| `WARP_EMERGENCY_SIGNAL_INTERVAL` | Emergency Signal 轮询间隔 (秒，最小 30) |

#### MDM 部署示例

```yaml
services:
  warp:
    image: dogbutcat/warp
    environment:
      - WARP_MDM_ENABLED=true
      - WARP_ORG=your-team-name
      - WARP_AUTH_CLIENT_ID=88bf3b6d86161464f6509f7219099e57.access
      - WARP_AUTH_CLIENT_SECRET=bdd31cbc4dec990953e39163fbbb194c93313ca9f0a6e420346af9d326b1d2a5
      - WARP_SERVICE_MODE=warp
      - WARP_TUNNEL_PROTOCOL=masque
      - WARP_SWITCH_LOCKED=true
      - WARP_AUTO_CONNECT=0
      - WARP_ONBOARDING=false
    # ... 其他配置同上
```

> **参考文档**: [Cloudflare MDM Parameters](https://developers.cloudflare.com/cloudflare-one/connections/connect-devices/warp/deployment/mdm-deployment/parameters/)

## Build

```bash
docker buildx build --platform linux/amd64 -t local/warp .
```

## 手动注册 (无 License Key)

```bash
docker exec warp warp-cli --accept-tos registration new
docker exec warp warp-cli --accept-tos connect
```

## 状态检查

```bash
docker exec warp warp-cli --accept-tos status
```
