package adapter

import (
	"context"
	"errors"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkit/eventbus"
)

// BridgeStats 是 Bridge 运行指标快照。
type BridgeStats struct {
	OutboundQueued     uint64
	OutboundPublished  uint64
	OutboundDropped    uint64
	InboundPublished   uint64
	InboundDropped     uint64
	DuplicateDropped   uint64
	EncodeErrors       uint64
	DecodeErrors       uint64
	TransportErrors    uint64
	LocalPublishErrors uint64
	QueueLength        int
}

type bridgeCounters struct {
	outboundQueued     atomic.Uint64
	outboundPublished  atomic.Uint64
	outboundDropped    atomic.Uint64
	inboundPublished   atomic.Uint64
	inboundDropped     atomic.Uint64
	duplicateDropped   atomic.Uint64
	encodeErrors       atomic.Uint64
	decodeErrors       atomic.Uint64
	transportErrors    atomic.Uint64
	localPublishErrors atomic.Uint64
}

// Bridge 将本地 EventBus 与远端 Transport 连接成跨节点事件转发器。
// Bridge 自身不提供 broker 实现，只负责编码、去重、防回环、生命周期和错误上报。
type Bridge struct {
	bus       *eventbus.EventBus
	transport Transport
	options   options

	mu            sync.Mutex
	started       bool
	closed        bool
	closedFlag    atomic.Bool
	outbound      chan Message
	stopCh        chan struct{}
	workerWG      sync.WaitGroup
	subscriptions []Subscription
	localHandler  func(context.Context, string, any)
	seen          *seenCache
	counters      bridgeCounters
}

// NewBridge 创建 Bridge；调用 Start 后才会真正订阅本地与远端事件。
func NewBridge(bus *eventbus.EventBus, transport Transport, opts ...Option) (*Bridge, error) {
	if bus == nil {
		return nil, ErrNilEventBus
	}
	if transport == nil {
		return nil, ErrNilTransport
	}
	options := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&options)
		}
	}
	if options.codec == nil {
		return nil, ErrNilCodec
	}
	if options.localPattern == "" {
		options.localPattern = defaultLocalPattern
	}
	if len(options.remotePatterns) == 0 {
		options.remotePatterns = []string{options.localPattern}
	}

	return &Bridge{
		bus:       bus,
		transport: transport,
		options:   options,
		outbound:  make(chan Message, options.outboundBuffer),
		stopCh:    make(chan struct{}),
		seen:      newSeenCache(options.dedupTTL, options.dedupMaxEntries),
	}, nil
}

// Start 启动 Bridge 的 transport 订阅、本地订阅和出站 worker。
func (b *Bridge) Start(ctx context.Context) error {
	if b == nil {
		return ErrBridgeClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrBridgeClosed
	}
	if b.started {
		return ErrBridgeStarted
	}
	if err := b.validatePatterns(); err != nil {
		return err
	}

	for i := 0; i < b.options.outboundWorkers; i++ {
		b.workerWG.Add(1)
		go b.publishWorker()
	}

	for _, pattern := range b.options.remotePatterns {
		sub, err := b.transport.Subscribe(ctx, pattern, b.handleRemote)
		if err != nil {
			b.closeLocked()
			return err
		}
		b.subscriptions = append(b.subscriptions, sub)
	}

	b.localHandler = b.handleLocal
	if err := b.bus.SubscribeWithPriority(b.options.localPattern, b.localHandler, b.options.localPriority); err != nil {
		b.closeLocked()
		return err
	}

	b.started = true
	return nil
}

// Close 停止 Bridge 并释放远端订阅；只有 WithOwnTransport(true) 时才关闭 Transport。
func (b *Bridge) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closeLocked()
}

func (b *Bridge) closeLocked() error {
	if b.closed {
		return nil
	}
	b.closed = true
	b.closedFlag.Store(true)
	close(b.stopCh)

	var closeErr error
	if b.localHandler != nil {
		err := b.bus.Unsubscribe(b.options.localPattern, b.localHandler)
		if err != nil &&
			!errors.Is(err, eventbus.ErrNoSubscriber) &&
			!errors.Is(err, eventbus.ErrEventBusClosed) {
			closeErr = errors.Join(closeErr, err)
		}
	}
	for _, sub := range b.subscriptions {
		if sub == nil {
			continue
		}
		if err := sub.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	if b.options.ownTransport {
		if err := b.transport.Close(); err != nil {
			closeErr = errors.Join(closeErr, err)
		}
	}
	b.workerWG.Wait()
	return closeErr
}

// NodeID 返回 Bridge 当前节点标识。
func (b *Bridge) NodeID() string {
	if b == nil {
		return ""
	}
	return b.options.nodeID
}

// Stats 返回 Bridge 指标快照。
func (b *Bridge) Stats() BridgeStats {
	if b == nil {
		return BridgeStats{}
	}
	return BridgeStats{
		OutboundQueued:     b.counters.outboundQueued.Load(),
		OutboundPublished:  b.counters.outboundPublished.Load(),
		OutboundDropped:    b.counters.outboundDropped.Load(),
		InboundPublished:   b.counters.inboundPublished.Load(),
		InboundDropped:     b.counters.inboundDropped.Load(),
		DuplicateDropped:   b.counters.duplicateDropped.Load(),
		EncodeErrors:       b.counters.encodeErrors.Load(),
		DecodeErrors:       b.counters.decodeErrors.Load(),
		TransportErrors:    b.counters.transportErrors.Load(),
		LocalPublishErrors: b.counters.localPublishErrors.Load(),
		QueueLength:        len(b.outbound),
	}
}

func (b *Bridge) handleLocal(ctx context.Context, topic string, payload any) {
	if _, ok := MessageFromContext(ctx); ok {
		return
	}
	if b.isClosed() {
		return
	}
	data, headers, err := b.options.codec.Encode(ctx, topic, payload)
	if err != nil {
		b.counters.encodeErrors.Add(1)
		b.report(StageEncode, topic, "", err)
		return
	}
	msg := Message{
		ID:        NewMessageID(),
		Topic:     topic,
		Payload:   data,
		Headers:   mergeHeaders(b.options.headers, headers),
		Origin:    b.options.nodeID,
		Timestamp: time.Now().UTC(),
	}
	if b.options.keyFunc != nil {
		msg.Key = b.options.keyFunc(topic, payload)
	}
	b.enqueue(ctx, msg)
}

func (b *Bridge) enqueue(ctx context.Context, msg Message) {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(b.options.enqueueTimeout)
	defer timer.Stop()
	select {
	case b.outbound <- msg:
		b.counters.outboundQueued.Add(1)
	case <-ctx.Done():
		b.counters.outboundDropped.Add(1)
		b.report(StageEnqueue, msg.Topic, msg.ID, ctx.Err())
	case <-b.stopCh:
		b.counters.outboundDropped.Add(1)
		b.report(StageEnqueue, msg.Topic, msg.ID, ErrBridgeClosed)
	case <-timer.C:
		b.counters.outboundDropped.Add(1)
		b.report(StageEnqueue, msg.Topic, msg.ID, ErrOutboundQueueFull)
	}
}

func (b *Bridge) publishWorker() {
	defer b.workerWG.Done()
	for {
		select {
		case msg := <-b.outbound:
			b.publishOutbound(msg)
		case <-b.stopCh:
			return
		}
	}
}

func (b *Bridge) publishOutbound(msg Message) {
	ctx, cancel := context.WithTimeout(context.Background(), b.options.publishTimeout)
	defer cancel()
	if err := b.transport.Publish(ctx, msg.EnsureDefaults(b.options.nodeID)); err != nil {
		b.counters.transportErrors.Add(1)
		b.report(StageTransportPublish, msg.Topic, msg.ID, err)
		return
	}
	b.counters.outboundPublished.Add(1)
}

func (b *Bridge) handleRemote(ctx context.Context, msg Message) error {
	if b.isClosed() {
		b.counters.inboundDropped.Add(1)
		return nil
	}
	if msg.Origin == b.options.nodeID {
		b.counters.inboundDropped.Add(1)
		return nil
	}
	if b.seen != nil && b.seen.seen(msg.ID) {
		b.counters.duplicateDropped.Add(1)
		return nil
	}
	payload, err := b.options.codec.Decode(ctx, msg)
	if err != nil {
		b.counters.decodeErrors.Add(1)
		b.report(StageDecode, msg.Topic, msg.ID, err)
		return err
	}
	remoteCtx := markRemoteContext(ctx, msg)
	if err := b.bus.PublishWithContext(remoteCtx, msg.Topic, payload); err != nil {
		b.counters.localPublishErrors.Add(1)
		b.report(StageLocalPublish, msg.Topic, msg.ID, err)
		return err
	}
	b.counters.inboundPublished.Add(1)
	return nil
}

func (b *Bridge) validatePatterns() error {
	caps := b.transport.Capabilities()
	if caps.Wildcard {
		for _, pattern := range b.options.remotePatterns {
			if strings.TrimSpace(pattern) == "" {
				return ErrInvalidPattern
			}
		}
		return nil
	}
	for _, pattern := range b.options.remotePatterns {
		if strings.TrimSpace(pattern) == "" {
			return ErrInvalidPattern
		}
		if isWildcardPattern(pattern) {
			return ErrWildcardUnsupported
		}
	}
	return nil
}

func (b *Bridge) isClosed() bool {
	return b.closedFlag.Load()
}

// report 将 Bridge 内部错误派发给用户配置的 ErrorHandler。
//
// 调用点均位于内部锁之外，因此这里同步调用 handler 并用 recover 隔离用户回调 panic。
// 生产环境下 handler 仍应快速返回，耗时日志/告警建议自行投递到有界队列，避免拖慢
// publish worker 或 transport 回调链路。
func (b *Bridge) report(stage ErrorStage, topic, messageID string, err error) {
	if err == nil || b == nil || b.options.errorHandler == nil {
		return
	}
	event := ErrorEvent{Stage: stage, Topic: topic, MessageID: messageID, Err: err}
	defer func() { _ = recover() }()
	b.options.errorHandler(event)
}

func mergeHeaders(static map[string]string, dynamic map[string]string) map[string]string {
	if len(static) == 0 && len(dynamic) == 0 {
		return nil
	}
	merged := make(map[string]string, len(static)+len(dynamic))
	maps.Copy(merged, static)
	maps.Copy(merged, dynamic)
	return merged
}

func isWildcardPattern(pattern string) bool {
	for _, part := range strings.FieldsFunc(pattern, func(r rune) bool { return r == '.' || r == '/' }) {
		switch part {
		case "#", "+", "*":
			return true
		}
	}
	return false
}
