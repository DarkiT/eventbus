# adapter 跨节点桥接

`adapter` 包（`github.com/darkit/eventbus/adapter`）让进程内的 EventBus 具备**跨节点 pub/sub** 能力，同时保持核心库零 broker SDK 依赖。本文件所有 API 经 `adapter/` 源码核验。

## 架构

```
本地 EventBus ──► Bridge ──► Transport ──► Broker / 集群（Redis、NATS、MQTT、Kafka…）
                       │
                       ├── Codec：Go 对象 ↔ []byte
                       ├── 出站队列 + worker：有界、异步、不阻塞发布者
                       ├── 防回环：Message.Origin + context 标记
                       ├── 去重：Message.ID 时间窗口
                       └── 错误上报：ErrorHandler 回调
```

**职责边界**：Bridge **不提供**任何 broker 实现，只做编排。Transport 接口由你（或第三方适配器）实现。

## 语义边界（v1）—— 务必先读

这是最容易踩坑的地方。adapter 明确**只保证 best-effort**：

**支持**：
- best-effort 跨节点 pub/sub（at-least-once 投递，可能重复）
- 通过 `Message.Origin` + context 标记**防回环**（自己发出去的不会再被自己当远端消息处理）
- 通过 `Message.ID` 做**时间窗口去重**（缓解重复投递）
- 有界出站队列（可配超时）
- 错误回调（`ErrorHandler`，内部锁外同步调用，需快速返回）
- Transport 能力声明（通配符 / 持久化 / ACK / 消费组 / 按键保序 / 请求-回复）

**不支持**（别去试，会得到错误结果或静默失效）：
- ❌ 跨节点 `PublishSyncAll` / `PublishSyncAny` —— 响应式语义只在**本地进程内**成立
- ❌ 全局 `SubscribeOnce` —— 跨节点无法保证"恰好一次"
- ❌ 全局顺序 —— 除非 Transport 的 `Capabilities.OrderedPerKey=true` 且配合 `Message.Key`
- ❌ exactly-once 投递 —— **应用层必须幂等**

> 一句话记忆：**跨节点 = 异步广播 + 幂等消费者**，不要把进程内事务语义（SyncAll）外推到集群。

## 核心 API

### Bridge

```go
func NewBridge(bus *eventbus.EventBus, transport Transport, opts ...Option) (*Bridge, error)

func (b *Bridge) Start(ctx context.Context) error   // 启动后才真正订阅本地与远端；不可重复启动（ErrBridgeStarted）
func (b *Bridge) Close() error                      // 停止并释放远端订阅；仅 WithOwnTransport(true) 时才关闭 Transport
func (b *Bridge) NodeID() string                    // 当前节点标识
func (b *Bridge) Stats() BridgeStats                // 运行指标快照
```

### BridgeStats 字段

```go
type BridgeStats struct {
    OutboundQueued     uint64  // 入队数
    OutboundPublished  uint64  // 成功发布到 transport 数
    OutboundDropped    uint64  // 出站丢弃（队列满/超时）
    InboundPublished   uint64  // 远端消息成功发布回本地数
    InboundDropped     uint64  // 入站丢弃
    DuplicateDropped   uint64  // 去重命中丢弃数
    EncodeErrors       uint64
    DecodeErrors       uint64
    TransportErrors    uint64
    LocalPublishErrors uint64
    QueueLength        int     // 当前出站队列长度
}
```

## Option 配置（14 个）

| Option | 默认值 | 说明 |
|---|---|---|
| `WithNodeID(id)` | `NewNodeID()`（hostname+随机） | 生产建议固定为实例 ID / pod name，便于排障 |
| `WithCodec(codec)` | `JSONCodec{}` | payload 编解码 |
| `WithLocalPattern(pattern)` | `"#"` | 本地哪些 topic 转发到 transport（通配符） |
| `WithRemotePatterns(ps...)` | `["#"]` | transport 订阅的远端 patterns |
| `WithOwnTransport(bool)` | `false` | Close 时是否同时关闭 Transport |
| `WithPublishTimeout(d)` | `5s` | worker 调 `Transport.Publish` 的超时 |
| `WithEnqueueTimeout(d)` | `1s` | 本地事件进队最长等待 |
| `WithOutboundBuffer(n)` | `1024` | 出站队列容量 |
| `WithOutboundWorkers(n)` | `1` | 出站发布 worker 数 |
| `WithDedup(ttl, max)` | `10min, 10000` | 去重窗口；`<=0` 关闭去重 |
| `WithErrorHandler(h)` | `nil` | 错误回调（同步调用，recover 保护） |
| `WithKeyFunc(fn)` | `nil` | 生成 `Message.Key`，配合 `OrderedPerKey` broker |
| `WithHeaders(h)` | `nil` | 所有出站消息携带的静态 header |

## Transport / Codec / Message

```go
// Transport：broker 抽象，实现必须并发安全，尊重 ctx 取消/超时
type Transport interface {
    Publish(ctx context.Context, msg Message) error
    Subscribe(ctx context.Context, pattern string, handler TransportHandler) (Subscription, error)
    Close() error
    Capabilities() Capabilities
}

type TransportHandler func(ctx context.Context, msg Message) error
type Subscription interface { Close() error }

// 能力声明——别把 Kafka/MQTT/Redis 强行伪装成同一模型
type Capabilities struct {
    Wildcard      bool  // 支持通配符订阅
    Durable       bool  // 持久化
    Ack           bool  // ACK 语义
    ConsumerGroup bool  // 消费组
    OrderedPerKey bool  // 按 Key 保序（配合 Message.Key）
    RequestReply  bool  // 请求-回复
}

// Codec：payload 与字节互转，必须并发安全
type Codec interface {
    Encode(ctx context.Context, topic string, payload any) ([]byte, map[string]string, error)
    Decode(ctx context.Context, msg Message) (any, error)
}

// 内置实现
type JSONCodec struct{}  // 默认；Decode 用 json.Number 保留数字精度
type BytesCodec struct{} // 仅 []byte / string；其他返回 ErrUnsupportedPayload

// 跨节点事件信封
type Message struct {
    ID        string            // 去重键（NewMessageID 生成）
    Topic     string
    Key       string            // 可选，配合 OrderedPerKey broker
    Payload   []byte            // 已编码字节
    Headers   map[string]string
    Origin    string            // 来源节点，用于防回环
    Timestamp time.Time
}
func (m Message) Clone() Message
func (m Message) EnsureDefaults(origin string) Message  // 补齐 ID/Timestamp/Headers
func NewMessageID() string
func NewNodeID() string

// 从本地 handler 的 ctx 中取出远端信封（判断"这条消息是不是从远端来的"）
func MessageFromContext(ctx context.Context) (Message, bool)
```

**Header 常量**：`HeaderContentType = "content-type"`、`HeaderCodec = "eventbus-codec"`。

## 错误处理

```go
type ErrorEvent struct {
    Stage     ErrorStage  // 见下
    Topic     string
    MessageID string
    Err       error
}
type ErrorHandler func(ErrorEvent)

// 错误阶段
const (
    StageEncode            ErrorStage = "encode"              // 本地 payload 编码失败
    StageEnqueue           ErrorStage = "enqueue"             // 出站队列入队失败
    StageTransportPublish  ErrorStage = "transport_publish"   // transport 发布失败
    StageTransportSubscribe ErrorStage = "transport_subscribe" // transport 订阅失败
    StageDecode            ErrorStage = "decode"              // 远端消息解码失败
    StageLocalPublish      ErrorStage = "local_publish"       // 远端消息发回本地失败
    StageClose             ErrorStage = "close"               // 关闭资源失败
)
```

`NewBridge` / `Start` 同步返回的错误：`ErrNilEventBus`、`ErrNilTransport`、`ErrNilCodec`、`ErrBridgeStarted`、`ErrBridgeClosed`、`ErrWildcardUnsupported`（声明不支持 wildcard 却订了 wildcard pattern）、`ErrInvalidPattern`。运行期出站满返回 `ErrOutboundQueueFull`。

## 完整用法

```go
package main

import (
    "context"
    "log"

    "github.com/darkit/eventbus"
    "github.com/darkit/eventbus/adapter"
)

func main() {
    bus := eventbus.New(-1)
    defer bus.Close()

    transport := NewYourTransport(/* 实现 adapter.Transport */) // 见下方最小示例

    bridge, err := adapter.NewBridge(bus, transport,
        adapter.WithNodeID("order-svc-1"),               // 生产固定 NodeID
        adapter.WithLocalPattern("orders.#"),            // 本地 orders.* 转发出去
        adapter.WithRemotePatterns("payments.#"),        // 订阅远端 payments.*
        adapter.WithOutboundBuffer(2048),
        adapter.WithOutboundWorkers(2),
        adapter.WithDedup(10*time.Minute, 20000),
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

    // 本地发布 orders.created 会被 Bridge 转发到 transport；
    // 远端发来的 payments.paid 会被 Bridge 解码后发布回本地 bus。
    bus.Publish("orders.created", map[string]any{"id": "A-1"})
}
```

## 实现一个最小 Transport（理解接口）

Bridge 不绑定任何 broker。下面是一个进程内内存 Transport，演示接口契约——生产中替换为 Redis/NATS/Kafka 适配器即可。

```go
type memTransport struct {
    mu     sync.Mutex
    subs   []memSub
    closed bool
}

type memSub struct {
    pattern string
    handler adapter.TransportHandler
}

func (m *memTransport) Capabilities() adapter.Capabilities {
    return adapter.Capabilities{Wildcard: true} // 内存实现支持通配符匹配
}

func (m *memTransport) Publish(ctx context.Context, msg adapter.Message) error {
    m.mu.Lock()
    subs := append([]memSub(nil), m.subs...)
    m.mu.Unlock()
    for _, s := range subs {
        if matchTopic(s.pattern, msg.Topic) { // 你自己的通配符匹配
            _ = s.handler(ctx, msg.Clone())
        }
    }
    return nil
}

func (m *memTransport) Subscribe(ctx context.Context, pattern string, handler adapter.TransportHandler) (adapter.Subscription, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.closed {
        return nil, adapter.ErrBridgeClosed
    }
    // 若 transport 不支持通配符却收到通配符 pattern，Bridge 会拦截并返回 ErrWildcardUnsupported
    m.subs = append(m.subs, memSub{pattern, handler})
    return &memSubscription{parent: m}, nil
}

func (m *memTransport) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.closed = true
    m.subs = nil
    return nil
}

type memSubscription struct{ parent *memTransport }
func (s *memSubscription) Close() error { return nil } // 内存实现无需清理
```

**实现要点**：`Publish` 必须并发安全；`Subscribe` 返回的 `Subscription.Close` 要能释放该订阅；`Capabilities` 必须如实声明（Bridge 会据此校验 wildcard pattern）。

## 生产配置清单

- **固定 NodeID**：用实例 ID / pod name / 容器 hostname，重启后仍可排障。`adapter.NewNodeID()` 是兜底（hostname+随机后缀）。
- **必接 ErrorHandler**：Bridge 错误不接就会静默丢消息。至少打日志，最好接指标；handler 要快速返回，耗时逻辑自行投递到有界队列。
- **应用层幂等**：消费者按 `Message.ID` 或业务主键去重，应对 at-least-once 重复投递。
- **监控 Stats**：关注 `OutboundDropped`（队列满）、`DuplicateDropped`（重复多）、`TransportErrors`、`DecodeErrors`。
- **通配符确认**：订阅 wildcard pattern 前，确认 Transport 的 `Capabilities.Wildcard=true`，否则 `Start` 报 `ErrWildcardUnsupported`。
- **保序需求**：需要同 key 顺序时，用 `WithKeyFunc` 生成稳定 key，并确认 broker `OrderedPerKey=true`。
- **关闭归属**：默认 `WithOwnTransport(false)`——Bridge 关闭不会关 Transport（适合共享 Transport）。独占时设 `true`。
