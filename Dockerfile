# ========== Stage 1: Go builder for warp-endpoint-probe ==========
FROM golang:1.24-alpine AS warp-probe-builder
ARG TARGETARCH
COPY warp-endpoint-probe/ /src/
WORKDIR /src
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags="-s -w" -o /warp-endpoint-probe .

# ========== Stage 2: Main image ==========
FROM debian:bookworm-slim

SHELL ["/bin/bash", "-c"]

ARG TARGETARCH
ARG S6_OVERLAY_VERSION=3.2.2.0
ARG GOST_VERSION=3.2.6

# ---------- s6-overlay ----------
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      ca-certificates curl gnupg lsb-release wget xz-utils; \
    case "${TARGETARCH}" in \
      amd64) S6_ARCH="x86_64" ;; \
      arm64) S6_ARCH="aarch64" ;; \
      *)     echo "Unsupported: ${TARGETARCH}"; exit 1 ;; \
    esac; \
    wget -qO /tmp/s6-noarch.tar.xz \
      "https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-noarch.tar.xz"; \
    tar -C / -Jxpf /tmp/s6-noarch.tar.xz; \
    wget -qO /tmp/s6-arch.tar.xz \
      "https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-${S6_ARCH}.tar.xz"; \
    tar -C / -Jxpf /tmp/s6-arch.tar.xz; \
    rm /tmp/s6-*.tar.xz

# ---------- Cloudflare WARP ----------
RUN set -eux; \
    curl -fsSL https://pkg.cloudflareclient.com/pubkey.gpg \
      | gpg --dearmor -o /usr/share/keyrings/cloudflare-warp-archive-keyring.gpg; \
    echo "deb [signed-by=/usr/share/keyrings/cloudflare-warp-archive-keyring.gpg] https://pkg.cloudflareclient.com/ $(lsb_release -cs) main" \
      > /etc/apt/sources.list.d/cloudflare-warp.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends cloudflare-warp dbus iptables iproute2 jq; \
    apt-get clean; rm -rf /var/lib/apt/lists/*

# ---------- gost v3 (SOCKS5 / Shadowsocks) ----------
RUN set -eux; \
    wget -qO /tmp/gost.tar.gz \
      "https://github.com/go-gost/gost/releases/download/v${GOST_VERSION}/gost_${GOST_VERSION}_linux_${TARGETARCH}.tar.gz"; \
    tar -xzf /tmp/gost.tar.gz -C /tmp gost; \
    install -m 755 /tmp/gost /usr/local/bin/gost; \
    rm -f /tmp/gost /tmp/gost.tar.gz

# ---------- warp-endpoint-probe (from Go builder) ----------
COPY --from=warp-probe-builder /warp-endpoint-probe /usr/local/bin/warp-endpoint-probe
COPY warp-speed-test.sh /usr/local/bin/warp-speed-test.sh
RUN chmod +x /usr/local/bin/warp-endpoint-probe /usr/local/bin/warp-speed-test.sh

COPY root /
RUN chmod +x /usr/bin/generate-mdm-xml /usr/bin/restart-gost && \
    find /etc/s6-overlay/s6-rc.d -name "run" -exec chmod +x {} \;

EXPOSE 1080

# === WARP 配置 ===
ENV WARP_MODE=
ENV WARP_LICENSE_KEY=
ENV WARP_PROXY_PORT=40000

# === 代理配置 ===
ENV PROXY_TYPE=
ENV PROXY_PORT=1080
ENV SS_PASSWORD=
ENV SS_METHOD=chacha20-ietf-poly1305
ENV SS_PORT=8388

# === MDM 部署 (企业 Zero Trust) ===
# 主开关：设为 true 启用 MDM 部署，其他 MDM 参数才会生效
ENV WARP_MDM_ENABLED=false
# Zero Trust 组织名 (必填)
ENV WARP_ORG=
# Service Token 凭证 (必填)
ENV WARP_AUTH_CLIENT_ID=
ENV WARP_AUTH_CLIENT_SECRET=
# 服务模式: warp / doh / warp+doh / dot / warp+dot / proxy / tunnel_only / postureonly
ENV WARP_SERVICE_MODE=
# 隧道协议: masque / wireguard
ENV WARP_TUNNEL_PROTOCOL=
# --- 开关控制 ---
ENV WARP_SWITCH_LOCKED=
ENV WARP_AUTO_CONNECT=
# --- 界面配置 ---
ENV WARP_ONBOARDING=
ENV WARP_DISPLAY_NAME=
ENV WARP_SUPPORT_URL=
# --- DNS-only 模式 ---
ENV WARP_GATEWAY_ID=
# --- 高级选项 ---
ENV WARP_ENABLE_PMTUD=
ENV WARP_ENABLE_POST_QUANTUM=
ENV WARP_ENABLE_NETBT=
# --- Endpoint 覆盖 (中国网络等特殊场景) ---
ENV WARP_OVERRIDE_API_ENDPOINT=
ENV WARP_OVERRIDE_DOH_ENDPOINT=
ENV WARP_OVERRIDE_WARP_ENDPOINT=
# --- External Emergency Disconnect ---
ENV WARP_EMERGENCY_SIGNAL_URL=
ENV WARP_EMERGENCY_SIGNAL_FINGERPRINT=
ENV WARP_EMERGENCY_SIGNAL_INTERVAL=

# === IP 优选 ===
ENV WARP_IP_SELECTION_ENABLED=false
ENV WARP_API_SELECTION_ENABLED=false
ENV WARP_IPV6_SELECTION=false
ENV WARP_LOG_LEVEL=info
ENV WARP_PROBE_CONCURRENCY=200

# === 网关模式 ===
ENV GATEWAY_MODE=false
# 需要路由到 WARP 隧道的目标网段，逗号分隔 (例: 10.143.0.0/16,172.16.0.0/12)
ENV GATEWAY_ROUTES=

ENTRYPOINT ["/init"]
