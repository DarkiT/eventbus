package adapter

import (
	"maps"
	"time"
)

const (
	defaultLocalPattern    = "#"
	defaultOutboundBuffer  = 1024
	defaultOutboundWorkers = 1
	defaultPublishTimeout  = 5 * time.Second
	defaultEnqueueTimeout  = time.Second
	defaultDedupTTL        = 10 * time.Minute
	defaultDedupMaxEntries = 10000
	defaultLocalPriority   = -1 << 30
)

// ErrorStage 标识 Bridge 错误发生阶段。
type ErrorStage string

const (
	// StageEncode 表示本地 payload 编码失败。
	StageEncode ErrorStage = "encode"
	// StageEnqueue 表示出站队列写入失败。
	StageEnqueue ErrorStage = "enqueue"
	// StageTransportPublish 表示 transport 发布失败。
	StageTransportPublish ErrorStage = "transport_publish"
	// StageTransportSubscribe 表示 transport 订阅失败。
	StageTransportSubscribe ErrorStage = "transport_subscribe"
	// StageDecode 表示远端消息解码失败。
	StageDecode ErrorStage = "decode"
	// StageLocalPublish 表示远端消息发布回本地 EventBus 失败。
	StageLocalPublish ErrorStage = "local_publish"
	// StageClose 表示关闭资源失败。
	StageClose ErrorStage = "close"
)

// ErrorEvent 描述 Bridge 内部异步错误，供生产环境接入日志或指标。
type ErrorEvent struct {
	Stage     ErrorStage
	Topic     string
	MessageID string
	Err       error
}

// ErrorHandler 处理 Bridge 内部异步错误。
type ErrorHandler func(ErrorEvent)

// KeyFunc 根据本地事件生成跨节点消息 Key，适配 Kafka 等按 key 保序的 broker。
type KeyFunc func(topic string, payload any) string

// Option 配置 Bridge 行为。
type Option func(*options)

type options struct {
	nodeID          string
	codec           Codec
	localPattern    string
	remotePatterns  []string
	ownTransport    bool
	publishTimeout  time.Duration
	enqueueTimeout  time.Duration
	outboundBuffer  int
	outboundWorkers int
	dedupTTL        time.Duration
	dedupMaxEntries int
	localPriority   int
	errorHandler    ErrorHandler
	keyFunc         KeyFunc
	headers         map[string]string
}

func defaultOptions() options {
	return options{
		nodeID:          NewNodeID(),
		codec:           JSONCodec{},
		localPattern:    defaultLocalPattern,
		remotePatterns:  []string{defaultLocalPattern},
		publishTimeout:  defaultPublishTimeout,
		enqueueTimeout:  defaultEnqueueTimeout,
		outboundBuffer:  defaultOutboundBuffer,
		outboundWorkers: defaultOutboundWorkers,
		dedupTTL:        defaultDedupTTL,
		dedupMaxEntries: defaultDedupMaxEntries,
		localPriority:   defaultLocalPriority,
	}
}

// WithNodeID 设置稳定节点 ID；生产环境建议使用实例 ID 或 pod name。
func WithNodeID(nodeID string) Option {
	return func(o *options) {
		if nodeID != "" {
			o.nodeID = nodeID
		}
	}
}

// WithCodec 设置 payload 编解码器。
func WithCodec(codec Codec) Option {
	return func(o *options) { o.codec = codec }
}

// WithLocalPattern 设置需要从本地 EventBus 转发到 transport 的 topic pattern。
func WithLocalPattern(pattern string) Option {
	return func(o *options) {
		if pattern != "" {
			o.localPattern = pattern
		}
	}
}

// WithRemotePatterns 设置 transport 订阅的远端 topic patterns。
func WithRemotePatterns(patterns ...string) Option {
	return func(o *options) {
		if len(patterns) == 0 {
			return
		}
		o.remotePatterns = append([]string(nil), patterns...)
	}
}

// WithOwnTransport 指定 Bridge.Close 是否同时关闭 Transport。
func WithOwnTransport(own bool) Option {
	return func(o *options) { o.ownTransport = own }
}

// WithPublishTimeout 设置出站 worker 调用 Transport.Publish 的超时。
func WithPublishTimeout(timeout time.Duration) Option {
	return func(o *options) {
		if timeout > 0 {
			o.publishTimeout = timeout
		}
	}
}

// WithEnqueueTimeout 设置本地事件进入出站队列的最长等待时间。
func WithEnqueueTimeout(timeout time.Duration) Option {
	return func(o *options) {
		if timeout > 0 {
			o.enqueueTimeout = timeout
		}
	}
}

// WithOutboundBuffer 设置出站队列容量。
func WithOutboundBuffer(size int) Option {
	return func(o *options) {
		if size > 0 {
			o.outboundBuffer = size
		}
	}
}

// WithOutboundWorkers 设置出站发布 worker 数。
func WithOutboundWorkers(workers int) Option {
	return func(o *options) {
		if workers > 0 {
			o.outboundWorkers = workers
		}
	}
}

// WithDedup 设置远端消息去重窗口；maxEntries 或 ttl 小于等于 0 时关闭去重。
func WithDedup(ttl time.Duration, maxEntries int) Option {
	return func(o *options) {
		o.dedupTTL = ttl
		o.dedupMaxEntries = maxEntries
	}
}

// WithErrorHandler 设置异步错误处理器。
func WithErrorHandler(handler ErrorHandler) Option {
	return func(o *options) { o.errorHandler = handler }
}

// WithKeyFunc 设置本地事件到跨节点 Message.Key 的映射函数。
func WithKeyFunc(fn KeyFunc) Option {
	return func(o *options) { o.keyFunc = fn }
}

// WithHeaders 设置所有出站消息都会携带的静态 header。
func WithHeaders(headers map[string]string) Option {
	return func(o *options) {
		if headers == nil {
			return
		}
		o.headers = make(map[string]string, len(headers))
		maps.Copy(o.headers, headers)
	}
}
