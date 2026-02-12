FROM dogbutcat/kasmvnc:ubuntunoble

# ZeroTier
RUN curl -s https://install.zerotier.com | bash
RUN cp -r /var/lib/zerotier-one/ /var/lib/zerotier-one.bak/

# Cloudflare WARP
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      curl \
      dbus \
      firefox \
      gnupg \
      lsb-release \
      xdg-desktop-portal \
      xdg-desktop-portal-gtk \
      xdg-utils && \
    mkdir -p /usr/share/keyrings && \
    curl -fsSL https://pkg.cloudflareclient.com/pubkey.gpg | gpg --dearmor -o /usr/share/keyrings/cloudflare-warp-archive-keyring.gpg && \
    echo "deb [signed-by=/usr/share/keyrings/cloudflare-warp-archive-keyring.gpg] https://pkg.cloudflareclient.com/ $(lsb_release -cs) main" \
      > /etc/apt/sources.list.d/cloudflare-warp.list && \
    apt-get update && \
    apt-get install -y --no-install-recommends cloudflare-warp && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

COPY root /
RUN chmod +x /usr/bin/generate-mdm-xml

VOLUME "/var/lib/cloudflare-warp"
VOLUME "/var/lib/zerotier-one"

EXPOSE 3000

# === 基础配置 ===
ENV ZT=false
ENV WARP_MODE=proxy
ENV WARP_PROXY_PORT=1080

# === MDM 部署模式 (Service Token) ===
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