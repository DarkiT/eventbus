package eventbus

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultTimeout 默认超时时间
	DefaultTimeout = 5 * time.Second
	// slowConsumerThreshold 慢消费者阈值
	slowConsumerThreshold = 100 * time.Millisecond
)

// defaultBufferSize 默认缓冲区大小，根据CPU核心数动态设置
var defaultBufferSize = runtime.NumCPU() * 64

// HandlerInfo 处理器信息，支持优先级
type HandlerInfo struct {
	Handler  *reflect.Value
	Priority int
	ID       uintptr // 用于唯一标识处理器
}

// channel 优化后的通道实现
type channel struct {
	sync.RWMutex
	topic      string
	channel    chan any
	handlers   []*HandlerInfo // 使用切片替代 map，支持优先级排序
	closed     atomic.Bool    // 使用原子操作
	stopCh     chan struct{}
	eventBus   *EventBus
	handlerMap map[uintptr]*HandlerInfo // 用于快速查找和删除
	bufferSize int
	ctx        context.Context
	cancel     context.CancelFunc
}

// newChannel 创建优化后的通道
func newChannel(topic string, bufferSize int, bus *EventBus) *channel {
	var ch chan any
	if bufferSize <= 0 {
		ch = make(chan any)
	} else {
		ch = make(chan any, bufferSize)
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := &channel{
		topic:      topic,
		channel:    ch,
		handlers:   make([]*HandlerInfo, 0),
		handlerMap: make(map[uintptr]*HandlerInfo),
		stopCh:     make(chan struct{}),
		eventBus:   bus,
		bufferSize: bufferSize,
		ctx:        ctx,
		cancel:     cancel,
	}

	go c.loop()
	return c
}

// loop 优化的消息处理循环
func (c *channel) loop() {
	defer func() {
		if r := recover(); r != nil {
			if c.eventBus.tracer != nil {
				c.eventBus.tracer.OnError(c.topic, fmt.Errorf("panic in handler: %v", r))
			}
		}
	}()

	for {
		select {
		case payload := <-c.channel:
			c.transfer(c.topic, payload)
		case <-c.ctx.Done():
			return
		case <-c.stopCh:
			return
		}
	}
}

// transfer 优化的消息传递，支持优先级
func (c *channel) transfer(topic string, payload any) {
	c.RLock()
	handlers := slices.Clone(c.handlers) // 复制切片避免并发问题
	c.RUnlock()

	startTime := time.Now()

	// 按优先级顺序执行处理器
	for _, handlerInfo := range handlers {
		if handlerInfo.Handler != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						if c.eventBus.tracer != nil {
							c.eventBus.tracer.OnError(topic, fmt.Errorf("handler panic: %v", r))
						}
					}
				}()

				topicVal := reflect.ValueOf(topic)
				payloadVal := reflect.ValueOf(payload)
				if topicVal.IsValid() && payloadVal.IsValid() {
					handlerInfo.Handler.Call([]reflect.Value{topicVal, payloadVal})
				}
			}()
		}
	}

	// 检查处理延迟
	latency := time.Since(startTime)
	if latency > slowConsumerThreshold && c.eventBus.tracer != nil {
		c.eventBus.tracer.OnSlowConsumer(topic, latency)
	}
}

// subscribe 添加处理器
func (c *channel) subscribe(handler any) error {
	return c.subscribeWithPriority(handler, 0)
}

// subscribeWithPriority 添加带优先级的处理器
func (c *channel) subscribeWithPriority(handler any, priority int) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}

	fn := reflect.ValueOf(handler)
	if fn.Kind() != reflect.Func {
		return ErrHandlerIsNotFunc
	}

	// 验证函数签名
	typ := fn.Type()
	if typ.NumIn() != 2 {
		return ErrHandlerParamNum
	}
	if typ.In(0).Kind() != reflect.String {
		return ErrHandlerFirstParam
	}

	handlerInfo := &HandlerInfo{
		Handler:  &fn,
		Priority: priority,
		ID:       fn.Pointer(),
	}

	c.Lock()
	defer c.Unlock()

	// 添加到映射表
	c.handlerMap[handlerInfo.ID] = handlerInfo

	// 添加到切片并按优先级排序（优先级高的在前）
	c.handlers = append(c.handlers, handlerInfo)
	slices.SortFunc(c.handlers, func(a, b *HandlerInfo) int {
		return b.Priority - a.Priority // 降序排列
	})

	if c.eventBus.tracer != nil {
		c.eventBus.tracer.OnSubscribe(c.topic, handler)
	}

	return nil
}

// publishAsync 真正的异步发布
func (c *channel) publishAsync(payload any) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}

	// 检查队列是否满
	if c.bufferSize > 0 && len(c.channel) >= c.bufferSize {
		if c.eventBus.tracer != nil {
			c.eventBus.tracer.OnQueueFull(c.topic, len(c.channel))
		}
	}

	// 对于无缓冲通道，使用超时机制
	if c.bufferSize <= 0 {
		select {
		case c.channel <- payload:
			return nil
		case <-time.After(DefaultTimeout):
			return ErrPublishTimeout
		}
	}

	// 对于有缓冲通道，非阻塞发送
	select {
	case c.channel <- payload:
		return nil
	default:
		return ErrPublishTimeout
	}
}

// publishSync 同步发布
func (c *channel) publishSync(payload any) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}

	if payload == nil {
		return nil
	}

	c.transfer(c.topic, payload)
	return nil
}

// unsubscribe 移除处理器
func (c *channel) unsubscribe(handler any) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}

	fn := reflect.ValueOf(handler)
	handlerID := fn.Pointer()

	c.Lock()
	defer c.Unlock()

	// 从映射表中删除
	if _, exists := c.handlerMap[handlerID]; !exists {
		return ErrNoSubscriber
	}
	delete(c.handlerMap, handlerID)

	// 从切片中删除
	c.handlers = slices.DeleteFunc(c.handlers, func(h *HandlerInfo) bool {
		return h.ID == handlerID
	})

	if c.eventBus.tracer != nil {
		c.eventBus.tracer.OnUnsubscribe(c.topic, handler)
	}

	return nil
}

// close 关闭通道
func (c *channel) close() {
	if !c.closed.CompareAndSwap(false, true) {
		return // 已经关闭
	}

	c.cancel() // 取消上下文
	close(c.stopCh)

	// 安全关闭通道
	go func() {
		time.Sleep(100 * time.Millisecond) // 给处理器一些时间完成
		close(c.channel)
	}()

	c.Lock()
	c.handlers = nil
	c.handlerMap = nil
	c.Unlock()
}

// EventBus 优化后的事件总线
type EventBus struct {
	bufferSize  int
	channels    *_CowMap
	timeout     time.Duration
	filters     []EventFilter
	middlewares []Middleware
	tracer      EventTracer
	lock        sync.RWMutex
	closed      atomic.Bool
}

// New 创建事件总线
// 可选参数：bufferSize - 缓冲区大小，不传或传0表示无缓冲，负数使用默认缓冲大小
func New(bufferSize ...int) *EventBus {
	size := -1 // 默认无缓冲
	if len(bufferSize) > 0 {
		if bufferSize[0] > 0 {
			size = bufferSize[0]
		} else if bufferSize[0] == 0 {
			size = -1 // 显式指定无缓冲
		} else {
			size = defaultBufferSize // 负数使用默认缓冲大小
		}
	}

	return &EventBus{
		bufferSize:  size,
		channels:    newCowMap(),
		timeout:     DefaultTimeout,
		filters:     make([]EventFilter, 0),
		middlewares: make([]Middleware, 0),
	}
}

// AddFilter 添加过滤器
func (e *EventBus) AddFilter(filter EventFilter) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.filters = append(e.filters, filter)
}

// SetTracer 设置追踪器
func (e *EventBus) SetTracer(tracer EventTracer) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.tracer = tracer
}

// Use 添加中间件
func (e *EventBus) Use(middleware Middleware) {
	e.lock.Lock()
	defer e.lock.Unlock()
	e.middlewares = append(e.middlewares, middleware)
}

// Subscribe 订阅主题
func (e *EventBus) Subscribe(topic string, handler any) error {
	return e.SubscribeWithPriority(topic, handler, 0)
}

// SubscribeWithPriority 带优先级订阅
func (e *EventBus) SubscribeWithPriority(topic string, handler any, priority int) error {
	if e.closed.Load() {
		return ErrChannelClosed
	}

	if err := validateHandler(handler); err != nil {
		return err
	}

	topic = normalizeTopic(topic)

	// 创建或获取通道
	ch, ok := e.channels.Load(topic)
	if !ok {
		ch = newChannel(topic, e.bufferSize, e)
		e.channels.Store(topic, ch)
	}

	return ch.(*channel).subscribeWithPriority(handler, priority)
}

// Publish 异步发布消息
func (e *EventBus) Publish(topic string, payload any) error {
	return e.publishInternal(topic, payload, true)
}

// PublishSync 同步发布消息
func (e *EventBus) PublishSync(topic string, payload any) error {
	return e.publishInternal(topic, payload, false)
}

// PublishWithContext 带上下文的发布
func (e *EventBus) PublishWithContext(ctx context.Context, topic string, payload any) error {
	if e.closed.Load() {
		return ErrChannelClosed
	}

	done := make(chan error, 1)
	go func() {
		done <- e.PublishSync(topic, payload)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// publishInternal 内部发布实现
func (e *EventBus) publishInternal(topic string, payload any, async bool) error {
	if e.closed.Load() {
		return ErrChannelClosed
	}

	startTime := time.Now()
	metadata := PublishMetadata{
		Timestamp: startTime,
		Async:     async,
		QueueSize: e.bufferSize,
	}

	if e.tracer != nil {
		e.tracer.OnPublish(topic, payload, metadata)
	}

	topic = normalizeTopic(topic)

	// 应用过滤器
	e.lock.RLock()
	filters := slices.Clone(e.filters)
	middlewares := slices.Clone(e.middlewares)
	e.lock.RUnlock()

	for _, filter := range filters {
		if !filter.Filter(topic, payload) {
			return nil
		}
	}

	// 构建处理链
	handler := func(t string, p any) error {
		return e.deliverMessage(t, p, async)
	}

	// 应用中间件（逆序包装）
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		next := handler
		handler = func(t string, p any) error {
			p = mw.Before(t, p)
			err := next(t, p)
			mw.After(t, p)
			return err
		}
	}

	return handler(topic, payload)
}

// deliverMessage 优化的消息投递
func (e *EventBus) deliverMessage(topic string, payload any, async bool) error {
	var lastErr error
	deliveredChannels := make(map[*channel]bool) // 防止重复投递

	// 直接匹配
	if ch, ok := e.channels.Load(topic); ok {
		channel := ch.(*channel)
		deliveredChannels[channel] = true

		if async {
			if err := channel.publishAsync(payload); err != nil {
				lastErr = err
				if e.tracer != nil {
					e.tracer.OnError(topic, err)
				}
			}
		} else {
			if err := channel.publishSync(payload); err != nil {
				lastErr = err
				if e.tracer != nil {
					e.tracer.OnError(topic, err)
				}
			}
		}
	}

	// 通配符匹配 - 遍历所有订阅的模式
	e.channels.Range(func(key, value any) bool {
		pattern := key.(string)
		channel := value.(*channel)

		// 跳过已经直接匹配的通道
		if deliveredChannels[channel] {
			return true
		}

		// 检查是否匹配通配符模式
		if matchTopic(pattern, topic) {
			deliveredChannels[channel] = true

			if async {
				if err := channel.publishAsync(payload); err != nil {
					lastErr = err
					if e.tracer != nil {
						e.tracer.OnError(topic, err)
					}
				}
			} else {
				if err := channel.publishSync(payload); err != nil {
					lastErr = err
					if e.tracer != nil {
						e.tracer.OnError(topic, err)
					}
				}
			}
		}
		return true
	})

	return lastErr
}

// Unsubscribe 取消订阅
func (e *EventBus) Unsubscribe(topic string, handler any) error {
	if e.tracer != nil {
		e.tracer.OnUnsubscribe(topic, handler)
	}

	ch, ok := e.channels.Load(topic)
	if !ok {
		return ErrNoSubscriber
	}
	return ch.(*channel).unsubscribe(handler)
}

// UnsubscribeAll 取消主题的所有订阅
func (e *EventBus) UnsubscribeAll(topic string) error {
	ch, ok := e.channels.Load(topic)
	if !ok {
		return ErrNoSubscriber
	}

	channel := ch.(*channel)
	channel.Lock()
	channel.handlers = nil
	channel.handlerMap = make(map[uintptr]*HandlerInfo)
	channel.Unlock()

	return nil
}

// Close 关闭事件总线
func (e *EventBus) Close() {
	if !e.closed.CompareAndSwap(false, true) {
		return
	}

	// 关闭所有通道
	e.channels.Range(func(key, value interface{}) bool {
		ch := value.(*channel)
		ch.close()
		return true
	})
}

// HealthCheck 健康检查
func (e *EventBus) HealthCheck() error {
	if e.closed.Load() {
		return ErrChannelClosed
	}
	return nil
}

// GetStats 获取统计信息
func (e *EventBus) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})

	channelCount := e.channels.Len()
	stats["channel_count"] = channelCount
	stats["buffer_size"] = e.bufferSize
	stats["timeout"] = e.timeout
	stats["closed"] = e.closed.Load()

	return stats
}

// 辅助函数
func splitTopic(topic string) []string {
	if topic == "" {
		return []string{}
	}
	return strings.Split(topic, ".")
}

func joinTopic(parts []string) string {
	return strings.Join(parts, ".")
}

func validateHandler(handler any) error {
	if reflect.TypeOf(handler).Kind() != reflect.Func {
		return ErrHandlerIsNotFunc
	}

	typ := reflect.TypeOf(handler)
	if typ.NumIn() != 2 {
		return ErrHandlerParamNum
	}
	if typ.In(0).Kind() != reflect.String {
		return ErrHandlerFirstParam
	}

	return nil
}
