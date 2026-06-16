# EventBus Adapter

`adapter` 包为 `github.com/darkit/eventbus` 提供跨节点集群桥接能力，使核心库保持轻量且无 broker SDK 依赖。

## 安装

```bash
go get github.com/darkit/eventbus/adapter
```

## 快速开始

```go
package main

import (
    "context"
    "log"
    "github.com/darkit/eventbus"
    "github.com/darkit/eventbus/adapter"
)

func main() {
    // 创建本地事件总线
    bus := eventbus.New(-1)

    // 创建传输层（实现 adapter.Transport 接口）
    transport := NewYourTransport(...)

    // 创建并启动桥接器
    bridge, err := adapter.NewBridge(
        bus,
        transport,
        adapter.WithNodeID("service-a-1"),
        adapter.WithRemotePatterns("orders.#"),
        adapter.WithErrorHandler(func(e adapter.ErrorEvent) {
            log.Printf("bridge error: stage=%s topic=%s id=%s err=%v",
                e.Stage, e.Topic, e.MessageID, e.Err)
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    if err := bridge.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer bridge.Close()

    // 现在本地发布的匹配事件将被桥接到远端
    // 远端事件也将被发布回本地 EventBus
}
```

## 架构

```
本地 EventBus → Bridge → Transport → Broker/集群
```

- **EventBus**: 负责进程内发布、订阅、通配符匹配、优先级和本地处理器执行
- **Bridge**: 管理编码、出站队列、防回环、去重、生命周期和错误上报
- **Transport**: 由具体 adapter 实现的抽象接口（Redis、NATS、MQTT、Kafka 等）
- **Codec**: 负责 payload 序列化（默认：JSONCodec）

## 核心概念

### Message

[Message](https://pkg.go.dev/github.com/darkit/eventbus/adapter#Message) 是跨节点传输的标准事件信封：

```go
type Message struct {
    ID        string            // 消息唯一 ID，用于去重
    Topic     string            // 事件主题
    Key       string            // 可选，用于按键保序
    Payload   []byte            // 已编码的负载字节
    Headers   map[string]string // 可选，消息头
    Origin    string            // 生产者节点 ID
    Timestamp time.Time         // 消息时间戳
}
```

### Transport 接口

[Transport](https://pkg.go.dev/github.com/darkit/eventbus/adapter#Transport) 定义 broker 适配层的最小接口：

```go
type Transport interface {
    Publish(ctx context.Context, msg Message) error
    Subscribe(ctx context.Context, pattern string, handler TransportHandler) (Subscription, error)
    Close() error
    Capabilities() Capabilities
}
```

### Bridge 配置项

使用函数式选项配置 Bridge：

- `WithNodeID(nodeID string)` - 设置稳定节点标识
- `WithCodec(codec Codec)` - 设置 payload 编解码器（默认：JSONCodec）
- `WithLocalPattern(pattern string)` - 设置本地转发到 Transport 的 topic 模式
- `WithRemotePatterns(patterns ...string)` - 设置 Transport 订阅的远端 topic 模式
- `WithOutboundBuffer(size int)` - 设置出站队列容量（默认：1024）
- `WithOutboundWorkers(workers int)` - 设置发布 worker 数量（默认：1）
- `WithPublishTimeout(timeout time.Duration)` - 设置发布超时（默认：5s）
- `WithEnqueueTimeout(timeout time.Duration)` - 设置入队超时（默认：1s）
- `WithDedup(ttl time.Duration, maxEntries int)` - 启用去重窗口
- `WithErrorHandler(handler ErrorHandler)` - 设置错误回调（内部锁外同步调用，需快速返回）
- `WithKeyFunc(fn KeyFunc)` - 设置 Key 生成函数
- `WithHeaders(headers map[string]string)` - 设置静态消息头

## 语义边界

### 支持的能力

- best-effort 跨节点 pub/sub
- 通过 Message.Origin 防回环
- 通过 Message.ID 时间窗口去重
- 出站队列与可配置超时
- 错误回调
- Transport 能力声明（通配符、持久化、ACK、消费组、按键保序、请求-回复）

### 不保证的能力

- 跨节点 PublishSyncAll / PublishSyncAny
- 全局 SubscribeOnce
- 全局顺序
- exactly-once 投递
- Broker 持久化与 ACK 语义一致性

如需消费组、有序投递或 exactly-once 语义，请使用在其 broker 上提供这些特性的 Transport 实现。

## Codec 实现

### JSONCodec（默认）

使用 `encoding/json` 进行 payload 序列化。Decode 使用 `json.Number` 保留数字精度。

### BytesCodec

直接传输 `[]byte` 或 `string` 负载，适合调用方自行序列化或对性能要求高的场景。

### 自定义 Codec

实现 [Codec](https://pkg.go.dev/github.com/darkit/eventbus/adapter#Codec) 接口以自定义序列化：

```go
type Codec interface {
    Encode(ctx context.Context, topic string, payload any) ([]byte, map[string]string, error)
    Decode(ctx context.Context, msg Message) (any, error)
}
```

## 设计决策

详见 [DESIGN.md](DESIGN.md) 了解详细设计原理：

- 为什么使用独立的 adapter 包而不是嵌入核心 EventBus
- Transport 接口设计的极简主义
- 默认 best-effort 语义
- 使用 Origin + context 标记防回环
- 异步出站发布模式

## 测试

```bash
go test ./...
```
