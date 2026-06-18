Socks5 Proxy

这是一个用 Go 编写的轻量级 SOCKS5 代理实现，包含 TCP/UDP 中继、简单的用户名/密码骨架、阻断缓存与按客户端聚合的流量指标上报功能。

**主要特性**

- 支持 SOCKS5 的 CONNECT（TCP）和 UDP ASSOCIATE（UDP）命令
- 简单的用户名/密码认证骨架（示例验证）
- 目标阻断（blocked cache），支持持久化到文件并按 TTL 自动解除
- 流量聚合器，按客户端统计周期性上传/下载流量并发送到 Kafka
- 命令行交互（stdin），可以查询当前用户流量

**要求**

- Go 1.25.x 或更高（项目 go.mod 指定 go 1.25.3）

**构建与运行**
在项目根目录运行：

```bash
go build -o socks5
./socks5 -listen :1080 -http-addr :8080
```

或直接运行：

```bash
go run . -listen :1080 -http-addr :8080
```

命令行参数（可选）

- -listen : 本地 SOCKS5 监听地址，默认 :1080
- -http-addr : HTTP 静态文件服务监听地址，默认 :8080
- -blocked-ttl : 封禁目标的默认持续时间，默认 10m
- -blocked-file : 持久化阻断地址的文件，默认 blocked.txt

示例：

```bash
./socks5 -listen :1080 -http-addr :8080 -blocked-ttl 5m -blocked-file blocked.txt
```

**HTTP 静态服务 / 指标上报**

- 静态文件托管在项目根（程序会把根目录作为 HTTP 根），可直接在浏览器打开 http://<http-addr>/index.html。
- metrics 包不再暴露旧版浏览器实时监控接口，流量数据会按聚合周期发送到 Kafka。

**交互式命令**
程序会在后台启动一个命令行处理器（stdin），支持命令：

- show <username>: 显示指定用户的上传/下载字节数
- exit : 退出程序

（命令处理在 cmd/handleCommandLine.go）

**关键源码文件说明**

- main.go — 启动参数、HTTP 静态文件服务、metrics 聚合与主服务器入口
- server.go — TCP/UDP 接受、连接处理、TCP 双向 relay 与 UDP relay 实现
- socks5.go — SOCKS5 握手、请求解析、响应构造、简单用户认证骨架
- utils.go — 辅助函数（错误判断、缓冲池）
- metrics/ — 客户端流量计数、周期性聚合与 Kafka 上报
- cmd/ — 命令行交互实现
- stess-test/ — 压测相关代码（样例）

**阻断逻辑（blocked）**
程序维护一个 blocked 缓存（通过 -blocked-ttl 控制）。当对某个目标的连接频繁失败时，会短期封禁该目标并可写入 blocked-file 持久化保存。

**指标与聚合**
流量在转发时会调用 metrics.AddTraffic() 按客户端 IP 累积上传/下载字节数，metrics.StartAggregator() 周期性（默认 main 中设置为 1s）收集并重置计数器，然后将 TrafficMetric JSON 消息发送到 Kafka。

**开发与调试**

- 在开发环境，可用 go run . 快速启动
- 日志使用标准库 log，便于在容器中重定向

**贡献**
欢迎提交 issue 与 PR。改进点建议：完善用户认证、添加配置文件支持、增加单元测试覆盖、优化 UDP relay 性能。
