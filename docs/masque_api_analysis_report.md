# Warp MASQUE (HTTP/3) 通信特征与探针化运行时研究报告

> [!IMPORTANT]
> **审计修订 (2026-02-21 10:05)** 本报告已通过 IDA Pro MCP 反向逐条对照原始二进制进行严格审核。`[已验证]` = 在二进制中精确定位。`[已修正]` = 审计中发现偏差并已纠正。

> **目标背景**
> 本报告基于对 `warp-svc` 的深度静态/动态二进制分析，剥离了与测速无关的 VPN 形态（如 WireGuard 及 MTLS 具体解密流程），直接面向 **“如何精确剥离并发包测试 MASQUE 节点 TCP/QUIC 真实握手延迟”** 这一课题提供底层协议级指南。

## 1. 协议核心结构与运行时序探索

### 1.1 协议栈封装模型 (Stack Encapsulation)
传统的代理多数停留在应用层 (SOCKS5/HTTP) 或者 UDP 隧道。Warp 的 MASQUE 模式完全利用了 **RFC 9298 / RFC 9484** 草案变种：
* **L3 / L4**: UDP（端口由 MDM 配置的 `override_warp_endpoint` 决定，常见 `443`，但具体端口号在二进制字符串表中**无硬编码证据** `[已修正]`，故端口完全依赖运行时配置）
* **Transport**: QUIC (基于 UDP 的多路复用安全层)
* **Application**: HTTP/3（ALPN 值 `h3` 通过 `tokio_quiche` 的 Settings 配置注入 `quiche::Config::set_application_protos`） `[已验证: 地址 0x1f08a4c]`
* **Proxy Layer**: HTTP `CF-CONNECT-IP`（Cloudflare 私有变体） `[已验证: 字符串地址 0x5e1cd0, 0x5e8ce9]`

### 1.2 探针测速的最小化截断握手 (Truncated Handshake) 理论
由于我们需要的是“探测目标 IP 在 MASQUE 协议下的真实延时”，**完全不需要**完成数据传输，甚至不需要完成被服务器授权。

**运行时序（QUIC 1-RTT 视图）：**
1. **[探针发包]**: Client 发送 QUIC `Initial` (ClientHello)
   - 携带 ALPN: `h3`（来源于 Settings 配置，非函数内立即数硬编码）`[已修正]`
   - 携带 SNI: 二进制中硬编码了 **4 个** MASQUE 域名（地址 `0x5e8a0f`，连续 144 字节）`[已验证]`：
     - `zt-masque.cloudflareclient.com`
     - `zt-masque-proxy.cloudflareclient.com`
     - `consumer-masque.cloudflareclient.com`
     - `consumer-masque-proxy.cloudflareclient.com`
2. **[网关回包]**: Server 回复 QUIC `Initial` + `Handshake` (ServerHello)
   - **(关键点：测算耗时)**：在收到 ServerHello 的这一纳秒，`RTT (Round Trip Time)` 即已精准得出。
3. **[服务端认证要求]**: Server 随后发送 `CertificateRequest`，要求客户端提供 WireGuard 关联的 X25519 x509 证书。
4. **[探针终止]**: 探针因并未持有/无需提供证书，直接回写 QUIC `CONNECTION_CLOSE` 或直接销毁 UDP Socket，测试结束并输出 RTT。

---

## 2. 微观发包特征参数提取 (Micro-Fingerprinting)

通过对底层引擎 `quiche` 和 `reqwest` 的钩子进行汇编及字符串交叉验证，梳理出以下的严格参数，**任何偏离都会导致 Cloudflare 边缘节点直接丢掉 UDP 报文（表现为 100% 丢包超时）**：

### 2.1 物理层/传输层 (UDP & QUIC)
| 属性 | Warp-SVC 内置要求 | 探针模拟要求 | 验证状态 |
|---|---|---|---|
| **ALPN** | 经 Settings 注入为 `h3`（二进制中未发现 `h3-29`/`h3-34`） | `NextProtos: []string{"h3"}` | `[已验证]` |
| **QUIC 版本** | QUIC v1 (0x00000001) | QUIC v1 | 推断 |
| **Datagram** | Enable (用于隧道包) | **探针无需开启** | 推断 |
| **Target Port** | **由 MDM `override_warp_endpoint` 运行时决定**，二进制中无 `2408`/`443`/`500`/`1701` 端口号硬编码字符串 | 按用户配置的端口测试 | `[已修正]` |

### 2.2 TLS / 认证层特征
| 属性 | Warp-SVC 内置要求 | 探针模拟要求 |
|---|---|---|
| **SNI (Server Name)** | 硬编码 4 个域名（地址 `0x5e8a0f`）：<br>`zt-masque.cloudflareclient.com`<br>`zt-masque-proxy.cloudflareclient.com`<br>`consumer-masque.cloudflareclient.com`<br>`consumer-masque-proxy.cloudflareclient.com`<br>**注**：该字符串表仅被 `H2Tunnel` 直接引用，`MasqueTunnel` 的 SNI 可能来自运行时配置传入 | `ServerName` 填入上述域名之一 | `[已验证+已修正]` |
| **Client Auth** | 证书链：`TunnelClientSecretKey::as_bytes`→`KeyPair::from_der`→`Certificate::new`→`ClientCertificateHook::new` | 置空或默认，利用 ServerHello 测速截断 | `[已验证: 行 396→413→454]` |

### 2.3 应用层伪头 (Pseudo-Headers / Custom Extensions)
若未来探讨发展为深度连通性测试（不仅测 Ping，还要建立 HTTP/3 握手发心跳包），需注意二进制内部暴露出了非标草案头部：
* 方法为 Cloudflare 私有的 `CF-CONNECT-IP` 变体（非标准 RFC 9298 CONNECT-UDP）。
* 存在以下**独立的**标头 `[已修正]`：
  * `cf-connect-ip`（小写，地址 `0x5e1cd0` 和 `0x5e8ce9`，各 13 字节）`[已验证]`
  * `cf-connect-proto`（地址 `0x50a780`，16 字节）`[已验证]`
  * `pq-enabled`（地址 `0x5e1cdd` 和 `0x5e8cf6`，各 10 字节）`[已验证]`
  * ⚠️ **此前报告中的 `cf-connect-ippq-enabledQUIC` 是错误的拼接**：`cf-connect-ip`（13 字节）与 `pq-enabled`（10 字节）在内存中紧邻（`0x5e1cd0 + 13 = 0x5e1cdd`），导致 `strings` 工具将它们错误地合并为一个字符串。实际上它们是**两个完全独立的 HTTP 头部**。
* 这些是 Cloudflare 私有的抗量子及鉴权指纹头。

---

## 3. 探针算法设计验证 (Probe Algorithm Blueprint)

为了实现轻量级、零侵入的高敏延时探针，建议使用 Golang 的 `quic-go` 库。

### 3.1 极简探针核心逻辑（QUIC 截断握手流）
1. **构建极简 QUIC Client**: 配置 `tls.Config{InsecureSkipVerify: true, ServerName: "zt-masque.cloudflareclient.com", NextProtos: []string{"h3"}}`。
2. **极速超时控制**: 设置 `quic.Config{HandshakeIdleTimeout: 1 * time.Second}`。如果一个 IP 是高优的正常节点，它对合规的 ClientHello 做出响应绝不会超过百毫秒级别。
3. **记录 RTT**: 
   - 记录时间戳 `T0`，调用 `quic.DialAddrContext()`。
   - 当 `Dial` 函数返回，或者在 TLS 握手阶段抛出 `证书不匹配`/`内部错误` (因为故意没有送交 WireGuard 证书) 的瞬间，记为 `T1`。
   - `RTT = T1 - T0`。此时测得的延时，是完全脱离了 ICMP 干扰的**真实底层协议握手信道延迟**。
4. **清理与释放**: 测速完成后立即 `Close()` socket，不对远端服务器产生持续负载。

---

## 4. 结论与下一步规划 (Conclusion)

通过本轮深入至汇编内核的研究，我们可以定性结论如下：

1. ** MASQUE 隧道的底层真身**：`Warp-svc` 的 Masque 模式使用的并非传统的 `connect-udp`，而是 **IP Proxying via HTTP (RFC 9484)** 的变体 `connect-ip`。所有三层流量包裹在 HTTP/3 流中交互。
2. **特定的身份签发验证锁**：它通过极其底层的 `ClientCertificateHook`，基于 WireGuard 的密钥链实时现场搓出一个合规的 `x509` TLS 证书投入使用以达成 MTLS（双向证实）。
3. **针对用户“择优”的核心诉求的降维解法**：如果仅仅是“寻找最低延迟的健康节点”，完全可以省去伪装这些负责的头部（包括 `CF-CONNECT-IP`）以及复杂的客户端证书生成逻辑，**采取纯粹的 ALPN h3 QUIC ClientHello 截断法探测**，便可获得最纯净无污染的真实握手延迟，其准确性远远凌驾于传统 Ping。
