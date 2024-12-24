package eventbus

import (
	"fmt"
	"reflect"
	"sync"
	"time"
)

const (
	// DefaultBufferSize 是默认的通道缓冲区大小
	DefaultBufferSize = 512
	// DefaultTimeout 是发布操作的默认超时时间
	DefaultTimeout = 5 * time.Second
	// slowConsumerThreshold 是慢消费者的阈值
	slowConsumerThreshold = 100 * time.Millisecond
)

type channel struct {
	sync.RWMutex
	bufferSize int
	topic      string
	channel    chan any
	handlers   *_CowMap
	closed     bool
	stopCh     chan struct{}
	timeout    time.Duration
	eventBus   *EventBus
}

// newChannel 创建一个新的 channel，指定主题和缓冲区大小
func newChannel(topic string, bufferSize int, bus *EventBus) *channel {
	var ch chan any
	if bufferSize <= 0 {
		ch = make(chan any)
	} else {
		ch = make(chan any, bufferSize)
	}

	return &channel{
		bufferSize: bufferSize,
		topic:      topic,
		channel:    ch,
		handlers:   newCowMap(),
		stopCh:     make(chan struct{}),
		eventBus:   bus, // 存储 EventBus 引用
	}
}

// transfer 调用所有处理器并传递给定的负载
func (c *channel) transfer(topic string, payload any) {
	c.RLock()
	defer c.RUnlock()

	// 获取所有处理器
	c.handlers.Range(func(_, value any) bool {
		if fn, ok := value.(*reflect.Value); ok && fn != nil {
			// 确保参数不为空
			topicVal := reflect.ValueOf(topic)
			payloadVal := reflect.ValueOf(payload)
			if !topicVal.IsValid() || !payloadVal.IsValid() {
				return true
			}
			fn.Call([]reflect.Value{topicVal, payloadVal})
		}
		return true
	})
}

// loop 监听通道并用负载调用处理器。 它从通道接收消息，然后遍历处理器映射并调用它们。
func (c *channel) loop() {
	for {
		select {
		case payload := <-c.channel:
			c.transfer(c.topic, payload)
		case <-c.stopCh:
			return
		}
	}
}

// subscribe 添加处理器到通道，如果通道已关闭则返回错误
func (c *channel) subscribe(handler any) error {
	c.RLock()
	defer c.RUnlock()
	if c.closed {
		return ErrChannelClosed
	}
	fn := reflect.ValueOf(handler)
	c.handlers.Store(fn.Pointer(), &fn)
	return nil
}

// publishSync 同步触发该通道定义的处理器
func (c *channel) publishSync(payload any) error {
	c.RLock()
	defer c.RUnlock()
	if c.closed {
		return ErrChannelClosed
	}

	if payload == nil {
		return nil
	}

	c.transfer(c.topic, payload)
	return nil
}

// publish 异步触发该通道定义的处理器。
// `payload` 参数将被传递给处理器。
// 它使用通道异步调用处理器。
func (c *channel) publish(payload any) error {
	c.RLock()
	defer c.RUnlock()
	if c.closed {
		return ErrChannelClosed
	}

	// 检查队列是否满
	if c.bufferSize > 0 && len(c.channel) >= c.bufferSize {
		if c.eventBus.tracer != nil {
			c.eventBus.tracer.OnQueueFull(c.topic, len(c.channel))
		}
	}

	startTime := time.Now()
	c.transfer(c.topic, payload)

	// 检查处理延迟
	latency := time.Since(startTime)
	if latency > slowConsumerThreshold && c.eventBus.tracer != nil {
		c.eventBus.tracer.OnSlowConsumer(c.topic, latency)
	}

	return nil
}

// unsubscribe 移除该通道的处理器
func (c *channel) unsubscribe(handler any) error {
	c.RLock()
	defer c.RUnlock()
	if c.closed {
		return ErrChannelClosed
	}
	fn := reflect.ValueOf(handler)
	c.handlers.Delete(fn.Pointer())
	return nil
}

// close 关闭通道
func (c *channel) close() {
	c.Lock()
	defer c.Unlock()

	if c.closed {
		return
	}

	c.closed = true
	close(c.channel)
	close(c.stopCh)
	c.handlers.Clear()
}

// EventBus 是事件主题的容器。
// 每个主题对应一个通道。`eventbus.Publish()` 将消息推送到通道，
// 然后 `eventbus.Subscribe()` 中的处理器会处理通道中的消息。
type EventBus struct {
	bufferSize  int
	channels    *_CowMap
	timeout     time.Duration
	once        sync.Once
	filters     []EventFilter
	middlewares []Middleware
	tracer      EventTracer
}

// New 创建一个无缓冲的 EventBus
func New() *EventBus {
	return &EventBus{
		bufferSize:  -1,
		channels:    newCowMap(),
		timeout:     DefaultTimeout,
		filters:     make([]EventFilter, 0),
		middlewares: make([]Middleware, 0),
	}
}

// NewBuffered 创建一个有缓冲区大小的 EventBus
func NewBuffered(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	return &EventBus{
		bufferSize:  bufferSize,
		channels:    newCowMap(),
		timeout:     DefaultTimeout,
		filters:     make([]EventFilter, 0),
		middlewares: make([]Middleware, 0),
	}
}

// AddFilter 添加事件过滤器
func (e *EventBus) AddFilter(filter EventFilter) {
	e.filters = append(e.filters, filter)
}

// SetTracer 添加事件追踪器
func (e *EventBus) SetTracer(tracer EventTracer) {
	e.tracer = tracer
}

// Use 添加中间件
func (e *EventBus) Use(middleware Middleware) {
	e.middlewares = append(e.middlewares, middleware)
}

// SubscribeWithPriority 为某个主题添加优先级处理器
func (e *EventBus) SubscribeWithPriority(topic string, handler any, priority int) error {
	if reflect.TypeOf(handler).Kind() != reflect.Func {
		return ErrHandlerIsNotFunc
	}

	ch, ok := e.channels.Load(topic)
	if !ok {
		ch = newChannel(topic, e.bufferSize, e)
		e.channels.Store(topic, ch)
		go ch.(*channel).loop()
	}
	return ch.(*channel).subscribeWithPriority(handler, priority)
}

// HealthCheck 健康检查
func (e *EventBus) HealthCheck() error {
	var errors []error
	e.channels.Range(func(key, value interface{}) bool {
		ch := value.(*channel)
		if ch.closed {
			errors = append(errors, fmt.Errorf("channel %v is closed", key))
		}
		return true
	})
	if len(errors) > 0 {
		return fmt.Errorf("health check failed: %v", errors)
	}
	return nil
}

// Close 关闭 eventbus
func (e *EventBus) Close() {
	e.once.Do(func() {
		e.channels.Range(func(key, value interface{}) bool {
			ch := value.(*channel)
			// 先停止接收新的消息
			ch.Lock()
			ch.closed = true
			ch.Unlock()

			// 清空现有消息
			for len(ch.channel) > 0 {
				<-ch.channel
			}

			// 关闭channel
			close(ch.channel)
			close(ch.stopCh)
			return true
		})
	})
}

// subscribeWithPriority 添加优先级处理器
func (c *channel) subscribeWithPriority(handler any, priority int) error {
	c.RLock()
	if c.closed {
		c.RUnlock()
		return ErrChannelClosed
	}
	c.RUnlock()

	fn := reflect.ValueOf(handler)
	if fn.Kind() != reflect.Func {
		return ErrHandlerIsNotFunc
	}

	// 检查函数参数
	typ := fn.Type()
	if typ.NumIn() != 2 {
		return ErrHandlerParamNum
	}
	if typ.In(0).Kind() != reflect.String {
		return ErrHandlerFirstParam
	}

	sub := &Subscription{
		Handler:  &fn,
		Priority: priority,
	}
	c.handlers.Store(fn.Pointer(), sub)
	return nil
}

// Subscribe 支持通配符的订阅
func (e *EventBus) Subscribe(topic string, handler any) error {
	if err := validateHandler(handler); err != nil {
		return err
	}

	if e.tracer != nil {
		e.tracer.OnSubscribe(topic, handler)
	}

	// 标准化主题
	topic = normalizeTopic(topic)

	// 创建或获取 channel
	ch, ok := e.channels.Load(topic)
	if !ok {
		ch = newChannel(topic, e.bufferSize, e)
		e.channels.Store(topic, ch)
		go ch.(*channel).loop()
	}

	return ch.(*channel).subscribe(handler)
}

// Publish 支持通配符的发布
func (e *EventBus) Publish(topic string, payload any) error {
	startTime := time.Now()
	metadata := PublishMetadata{
		Timestamp: startTime,
		Async:     true,
		QueueSize: e.bufferSize,
	}

	if e.tracer != nil {
		e.tracer.OnPublish(topic, payload, metadata)
	}

	// 标准化主题
	topic = normalizeTopic(topic)

	// 应用过滤器
	for _, filter := range e.filters {
		if !filter.Filter(topic, payload) {
			return nil
		}
	}

	// 应用中间件
	handler := func(topic string, payload any) error {
		// 查找所有匹配的订阅者
		e.channels.Range(func(key, value any) bool {
			pattern := key.(string)
			if matchTopic(pattern, topic) {
				if err := value.(*channel).publish(payload); err != nil {
					if e.tracer != nil {
						e.tracer.OnError(topic, err)
					}
				}
			}
			return true
		})
		return nil
	}

	// 包装中间件
	for i := len(e.middlewares) - 1; i >= 0; i-- {
		mw := e.middlewares[i]
		next := handler
		handler = func(t string, p any) error {
			// 执行前置处理
			p = mw.Before(t, p)

			// 执行处理
			err := next(t, p)

			// 执行后置处理
			mw.After(t, p)

			return err
		}
	}

	// 执行处理链
	return handler(topic, payload)
}

// PublishSync 同步发送消息到所有订阅者
func (e *EventBus) PublishSync(topic string, payload any) error {
	startTime := time.Now()
	metadata := PublishMetadata{
		Timestamp: startTime,
		Async:     false,
		QueueSize: e.bufferSize,
	}

	if e.tracer != nil {
		e.tracer.OnPublish(topic, payload, metadata)
	}

	// 标准化主题
	topic = normalizeTopic(topic)

	// 应用过滤器
	for _, filter := range e.filters {
		if !filter.Filter(topic, payload) {
			return nil
		}
	}

	// 应用中间件
	handler := func(topic string, payload any) error {
		// 查找所有匹配的订阅者
		var lastErr error
		e.channels.Range(func(key, value any) bool {
			pattern := key.(string)
			if matchTopic(pattern, topic) {
				if err := value.(*channel).publishSync(payload); err != nil {
					lastErr = err
					if e.tracer != nil {
						e.tracer.OnError(topic, err)
					}
				}
			}
			return true
		})
		return lastErr
	}

	// 包装中间件
	for i := len(e.middlewares) - 1; i >= 0; i-- {
		mw := e.middlewares[i]
		next := handler
		handler = func(t string, p any) error {
			// 执行前置处理
			p = mw.Before(t, p)

			// 执行处理
			err := next(t, p)

			// 执行后置处理
			mw.After(t, p)

			return err
		}
	}

	// 执行处理链
	return handler(topic, payload)
}

// PublishWithTimeout 添加带超时的发布方法
func (e *EventBus) PublishWithTimeout(topic string, payload interface{}, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- e.PublishSync(topic, payload)
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("publish timeout after %v", timeout)
	}
}

// Unsubscribe 移除某个主题的处理器
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

// UnsubscribeAll 移除某个主题的所有处理器
func (e *EventBus) UnsubscribeAll(topic string) error {
	ch, ok := e.channels.Load(topic)
	if !ok {
		return ErrNoSubscriber
	}
	ch.(*channel).handlers.Clear()
	return nil
}

// validateHandler 验证处理器函数的合法性
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
