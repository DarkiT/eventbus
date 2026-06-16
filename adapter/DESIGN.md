# EventBus Adapter 设计说明

## 设计目标

`adapter` 包用于把进程内 `github.com/darkit/eventbus` 扩展为可选的跨节点 pub/sub 能力，同时保持核心库轻量、无 broker SDK 依赖。

目标：

- 提供稳定的 `Transport` 接口，后续可独立实现 Redis、NATS、MQTT、Kafka 等 adapter。
- 提供生产可用的 `Bridge`：编码、出站队列、发布超时、防回环、远端消息去重、错误上报、生命周期管理。
- 明确语义边界，避免把本地 EventBus 伪装成完整分布式消息系统。

## 方案选择

### 方案 A：直接在核心 EventBus 中加入集群能力

优点：调用路径短，用户无感。

缺点：核心包会引入 broker 语义、生命周期、重试、ACK、序列化等复杂度；不同 broker 的能力差异会污染本地 API。

结论：拒绝。

### 方案 B：独立 `adapter` 子包 + Transport 接口 + Bridge

优点：核心包保持进程内事件总线职责；集群能力按需引入；具体 broker adapter 可独立演进。

缺点：调用方需要显式创建 Bridge，并理解跨节点语义不是本地语义的等价复制。

结论：采用。

## 关键决策

### 1. Transport 只定义最小发布/订阅接口

`Transport` 只包含：

- `Publish(ctx, Message)`
- `Subscribe(ctx, pattern, handler)`
- `Close()`
- `Capabilities()`

ACK、持久化、ConsumerGroup、按 Key 保序等差异通过 `Capabilities` 声明，由具体 adapter 决定如何映射。

### 2. Bridge 默认 best-effort pub/sub

Bridge 只承诺跨节点 best-effort 转发，不承诺 exactly-once。`Message.ID` 仅做本节点时间窗口去重，不能替代 broker 级幂等或业务幂等。

### 3. 防回环使用 Origin + Context 标记

- `Message.Origin == NodeID` 的远端消息会被丢弃。
- 远端消息发布回本地 EventBus 时，Bridge 会写入 context 标记；本地通配符转发器看到标记后不会再次发布到 Transport。

### 4. Payload 编解码通过 Codec 扩展

默认 `JSONCodec` 满足最小使用；高性能、schema 化、跨语言兼容等场景可替换为自定义 Codec。

### 5. 出站发布异步化

Bridge 使用 bounded queue + worker 调用 `Transport.Publish`，避免本地 `PublishSync` 被 broker IO 长时间拖住。队列满、编码失败、transport 失败通过 `ErrorHandler` 与 `Stats` 暴露。

## 安全边界

- 本包不读取环境变量、凭证文件或网络配置，不直接连接任何 broker。
- `Message.Payload` 被视为不可信数据；Decode 错误会返回给 Transport handler，用于 adapter 映射 nack/retry。
- `NodeID` 应由生产环境显式配置，避免重启后 origin 变化造成排障困难。
- `Headers` 不应存放密钥或敏感凭证；如需认证，应由具体 Transport 使用 broker 原生认证机制。

## 已知限制

- 不支持跨节点 `PublishSyncAll` / `PublishSyncAny`。
- 不支持全局 `SubscribeOnce`。
- 不保证全局顺序；如需按实体有序，应由具体 Transport 使用 `Message.Key` 映射 broker partition/subject。
- 不保证 exactly-once；业务侧仍需幂等。
- wildcard 能力取决于具体 Transport 的 `Capabilities.Wildcard`。

## 变更历史

- 2026-04-28：新增 adapter 包，包含 Message、Codec、Transport、Bridge、README、DESIGN 与单元测试。
