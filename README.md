# Docker Cloudflare WARP

## Changelog

| 日期 | 变更内容 | 原因 |
|------|---------|------|
| 2026-02-12 11:30 | 新增 `.env.example`，compose 改用 `env_file` 引用，支持 Portainer 导入 | 环境变量管理更清晰 |
| 2026-02-12 11:00 | 移除无效 `WARP_ACCEPT_TOS`，容器内 `--accept-tos` 已硬编码 | 该变量未实际生效，清理冗余 |
| 2026-02-12 10:30 | 新增 MDM 部署模式，支持 Service Token headless 部署 | 企业级 Zero Trust 场景支持 |
| 2026-02-11 12:58 | 级回 warp-taskbar，接受 GUI 回调部分错误但保持基本使用 | CLI 一把抓不简洁 |
| 2026-02-11 12:19 | 禁用 GUI 登录回调，改为纯 CLI 方式（License Key / 命令行注册） | GUI 回调注册无法跨越容器边界 (reverted) |
| 2026-02-11 10:55 | 增加 WARP TOS 自动接受开关 `WARP_ACCEPT_TOS` | 修复启动时报错提示接受条款 |
| 2026-02-11 10:40 | 切换为 Cloudflare WARP，补充 D-Bus、s6 服务、WARP 环境变量与数据持久化 | 执行重构计划 |

基于 [dogbutcat/kasmvnc:ubuntunoble](https://github.com/dogbutcat/docker-kasmvnc) 基础镜像，集成 Cloudflare WARP，并保留 KasmVNC Desktop 与 ZeroTier。

> 使用 TUN 模式需要在宿主机启用 `net.ipv4.ip_forward=1`（写入 `/etc/sysctl.conf`）

## 快速开始

```bash
cp .env.example .env
# 编辑 .env，取消注释并填入你的配置
docker compose up -d
```

Portainer 用户可直接在 Stack 配置界面点击 **Load variables from .env file** 导入 `.env.example`。

## 部署方式

支持两种部署方式，根据使用场景选择：

| 方式 | 适用场景 | 主要配置 |
|------|---------|---------|
| **LICENSE_KEY** | 个人 / WARP+ | `WARP_LICENSE_KEY` |
| **MDM** | 企业 / Zero Trust / Service Token | `WARP_MDM_ENABLED=true` + MDM 参数 |

---

## 方式一：LICENSE_KEY 部署 (个人)

### 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `WARP_LICENSE_KEY` | - | WARP+ 许可证密钥 |
| `WARP_MODE` | `proxy` | 工作模式：`warp` / `doh` / `warp+doh` / `dot` / `warp+dot` / `proxy` / `tunnel_only` |
| `WARP_PROXY_PORT` | `1080` | 代理端口（仅 proxy 模式） |
| `ZT` | `false` | 启用 ZeroTier |

> 容器内所有 `warp-cli` 调用已自动附带 `--accept-tos`，无需额外配置。

### 注册说明

**推荐方式**：传入 `WARP_LICENSE_KEY` 环境变量（Cloudflare Zero Trust License），容器启动时自动注册并连接。

**GUI 登录方式**：通过 KasmVNC 桌面启动的 taskbar 可点击登录按钮进行 SSO。此功能中回调注册会出现"Cannot register 'warp_cli' as browser callback"警告，这是容器隔离限制，但不影响 WARP 本身代理功能。

手动 CLI 方式（无 License 时）：

```bash
docker exec warp warp-cli --accept-tos registration new
docker exec warp warp-cli --accept-tos proxy port 1080
docker exec warp warp-cli --accept-tos mode proxy
docker exec warp warp-cli --accept-tos connect
```

---

## 方式二：MDM 部署 (企业 / Zero Trust)

通过环境变量配置 MDM 参数，容器启动时自动生成 `/var/lib/cloudflare-warp/mdm.xml`。

> **注意**：如果 volume 中已存在 `mdm.xml`（手动挂载或上次生成），脚本会跳过生成，直接使用已有文件。如需更新配置，请先删除已有的 `mdm.xml` 再重启容器。

### 核心环境变量

| 变量 | 必填 | 说明 |
|------|:----:|------|
| `WARP_MDM_ENABLED` | ✓ | **主开关**，设为 `true` 启用 MDM 模式 |
| `WARP_ORG` | ✓ | Zero Trust 组织名 (team name) |
| `WARP_AUTH_CLIENT_ID` | ✓ | Service Token Client ID (格式: `xxx.access`) |
| `WARP_AUTH_CLIENT_SECRET` | ✓ | Service Token Client Secret |

### 可选环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `WARP_SERVICE_MODE` | `warp` | 服务模式：`warp` / `doh` / `warp+doh` / `dot` / `warp+dot` / `proxy` / `tunnel_only` / `postureonly` |
| `WARP_PROXY_PORT` | - | 代理端口（仅 `proxy` 模式需要） |
| `WARP_TUNNEL_PROTOCOL` | `masque` | 隧道协议：`masque` / `wireguard` |
| `WARP_SWITCH_LOCKED` | `false` | 锁定开关，用户无法手动断开 |
| `WARP_AUTO_CONNECT` | - | 自动重连间隔 (0-1440 分钟) |
| `WARP_ONBOARDING` | `true` | 显示隐私政策引导页 |
| `WARP_DISPLAY_NAME` | - | GUI 显示名称 |
| `WARP_SUPPORT_URL` | - | 支持链接 (https:// 或 mailto:) |
| `WARP_GATEWAY_ID` | - | DoH subdomain (DNS-only 模式) |
| `WARP_ENABLE_PMTUD` | `false` | Path MTU Discovery |
| `WARP_ENABLE_POST_QUANTUM` | - | 后量子加密 |
| `WARP_ENABLE_NETBT` | `false` | NetBIOS over TCP/IP (Windows 兼容) |

### 高级环境变量 (通常不需要)

| 变量 | 说明 |
|------|------|
| `WARP_OVERRIDE_API_ENDPOINT` | 覆盖 API 端点 IP (中国网络伙伴) |
| `WARP_OVERRIDE_DOH_ENDPOINT` | 覆盖 DoH 端点 IP |
| `WARP_OVERRIDE_WARP_ENDPOINT` | 覆盖 WARP 端点 IP:Port |
| `WARP_EMERGENCY_SIGNAL_URL` | External Emergency Disconnect URL |
| `WARP_EMERGENCY_SIGNAL_FINGERPRINT` | Emergency Signal 证书指纹 (SHA-256) |
| `WARP_EMERGENCY_SIGNAL_INTERVAL` | Emergency Signal 轮询间隔 (秒，最小 30) |

### MDM 部署示例

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

---

## 通用配置

- 以 root 用户运行（支持 TUN 模式）
- WARP 数据目录：`/var/lib/cloudflare-warp/`
- KasmVNC 配置目录：`/config/`
- 可选 ZeroTier：环境变量 `ZT=true` 开启

## Build

```bash
docker buildx build --platform linux/amd64 -t local/warp .
```

## 验证

状态检查：

```bash
docker exec warp warp-cli --accept-tos status
```

代理测试：

```bash
# proxy 模式
curl -x socks5h://127.0.0.1:1080 https://www.cloudflare.com/cdn-cgi/trace
```

返回内容包含 `warp=on` 即表示代理模式可用。
