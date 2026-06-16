package adapter

import (
	"context"
	"errors"
)

var (
	// ErrNilEventBus 表示创建 Bridge 时未提供本地 EventBus。
	ErrNilEventBus = errors.New("adapter: nil event bus")
	// ErrNilTransport 表示创建 Bridge 时未提供 Transport。
	ErrNilTransport = errors.New("adapter: nil transport")
	// ErrNilCodec 表示配置了空 Codec。
	ErrNilCodec = errors.New("adapter: nil codec")
	// ErrBridgeStarted 表示 Bridge 已启动，不能重复启动。
	ErrBridgeStarted = errors.New("adapter: bridge already started")
	// ErrBridgeClosed 表示 Bridge 已关闭。
	ErrBridgeClosed = errors.New("adapter: bridge closed")
	// ErrWildcardUnsupported 表示 Transport 声明不支持 wildcard，却订阅了 wildcard pattern。
	ErrWildcardUnsupported = errors.New("adapter: wildcard pattern unsupported by transport")
	// ErrOutboundQueueFull 表示 Bridge 出站队列在限定时间内无法写入。
	ErrOutboundQueueFull = errors.New("adapter: outbound queue full")
	// ErrUnsupportedPayload 表示当前 Codec 不支持该 payload 类型。
	ErrUnsupportedPayload = errors.New("adapter: unsupported payload type")
	// ErrInvalidPattern 表示远端订阅 pattern 为空或格式不被 adapter 接受。
	ErrInvalidPattern = errors.New("adapter: invalid pattern")
)

// TransportHandler 是 transport 收到远端消息后的回调。
// 返回 error 时，具体 adapter 可映射为 nack、重试或错误日志。
type TransportHandler func(ctx context.Context, msg Message) error

// Transport 抽象不同 broker 的最小发布/订阅能力。
// 实现必须并发安全，并尊重传入 ctx 的取消与超时。
type Transport interface {
	Publish(ctx context.Context, msg Message) error
	Subscribe(ctx context.Context, pattern string, handler TransportHandler) (Subscription, error)
	Close() error
	Capabilities() Capabilities
}

// Subscription 表示远端订阅句柄，Bridge.Close 会调用 Close 释放资源。
type Subscription interface {
	Close() error
}

// Capabilities 描述 transport 的 broker 语义能力，避免把 Kafka、MQTT、Redis 等强行伪装成同一种模型。
type Capabilities struct {
	Wildcard      bool
	Durable       bool
	Ack           bool
	ConsumerGroup bool
	OrderedPerKey bool
	RequestReply  bool
}
