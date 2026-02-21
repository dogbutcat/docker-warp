# Warp Endpoint Probe

一个用于探测 Cloudflare WARP/Zero Trust 边缘节点延迟的轻量级工具集。本项目最初设计用于辅助 `docker-warp` 容器化部署在获取高优节点时的痛点，随后也扩展出了针对 MASQUE (HTTP/3) 协议的定制化测流工具。

## 目录结构
- `main.go` / `probe.go` / `targets.go` / `quic.go` / `https.go` / `noise.go`:
  **常规选优探针 (`warp-endpoint-probe`)**
  主探针负责从指定模式的 IP 段池中拉取目标，并发测试其连通性及延时。它支持针对 WireGuard (基于 noise 握手)、HTTP/3 端点 (通过 `h3` TLS 试探) 和 HTTPS 的全量探测。常用于被包装成 Docker S6 init 脚本的一部分进行日常优选。

- `cmd/masque-probe/main.go`:
  **MASQUE 极简握手测时探针 (`masque-probe`)**
  专门针对 Cloudflare MASQUE 协议开发的极简高精测速工具。
  通过逆向 `warp-svc` 发现，MASQUE 的底层为基于 QUIC 的 `connect-ip` (RFC 9484)。该探针不再尝试进行完整的 MTLS 或 WireGuard 证书认证，而是通过截取 QUIC `ClientHello` 收到 `ServerHello` (或 Reject) 的那一瞬间，精准测量最纯粹的底层网络握手延迟（剔除业务层干扰）。支持多轮探测求平均值以抵消偶发性网络抖动。

## 编译方法

确保环境中已安装 Go 1.24+。

### 编译常规探针
```bash
go build -o warp-endpoint-probe .
```

### 编译 MASQUE 专属测速探针
```bash
# 推荐交叉编译为 Linux 静态二进制，以便在服务器/路由器上直接执行
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o masque-probe-linux-amd64 ./cmd/masque-probe/
```

## 使用方法 (以 `masque-probe` 为例)

### 常用参数
- `-mode`: 测速模式，默认为 `masque`。可选 `api`。不同的模式会加载代码内置的不同 Cloudflare IP 段。
- `-cidr`: 覆盖内建模式，允许传入逗号分隔的特定网段 (如 `1.2.3.0/24, 2.3.4.0/27`)。当指定时 `-mode` 会被忽略。
- `-port`: 目标 UDP 端口，MASQUE 默认且常用为 `443`。
- `-sni`: 伪装/探测域名。`masque` 模式默认为 `zero-trust-client.cloudflareclient.com`。
- `-sample`: 每个网段随机采集的样本数量（由于 CIDR 太大，全量探测过于缓慢）。默认为 `3`。如果要针对单网段全量普查，可调大此值。
- `-rounds`: 多轮探测的次数。默认 `3` 次。会对收到的延时结果取均值。
- `-timeout`: 单轮测试的最长等待时间。默认 `2s`。
- `-top`: 输出选优排名中的前 N 个 IP。默认 `20`。
- `-n`: 并发协程数量。默认 `20`。

### 示例

**1. 普查 API 接口，每个网段抽 3 个 IP 测试，测试 3 轮:**
```bash
./masque-probe-linux-amd64 -mode api -sample 3 -rounds 3
```

**2. 针对已知优质网段进行地毯式并发优选 (提干):**
```bash
./masque-probe-linux-amd64 -cidr "183.131.87.224/27" -sample 30 -rounds 5 -top 30
```

## 贡献与参考

核心协议的逆向依据与底层设计请见文档：
[`docs/masque_api_analysis_report.md`](../../docs/masque_api_analysis_report.md)
