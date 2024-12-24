package eventbus

import (
	"fmt"
	"reflect"
	"sync"
	"time"
)

const (
	// DefaultBufferSize is the default channel buffer size
	DefaultBufferSize = 1024
	// DefaultTimeout is the default timeout for publish operations
	DefaultTimeout = 5 * time.Second
)

// channel represents a topic and its associated handlers
type channel struct {
	sync.RWMutex
	bufferSize int
	topic      string
	channel    chan any
	handlers   *CowMap // 存储 Subscription
	closed     bool
	stopCh     chan struct{}
	timeout    time.Duration
	eventBus   *EventBus // 新增：引用 EventBus 实例以访问过滤器和中间件
}

// newChannel creates a new channel with specified topic and buffer size
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
		handlers:   NewCowMap(),
		stopCh:     make(chan struct{}),
		eventBus:   bus, // 存储 EventBus 引用
	}
}

// transfer calls all handlers with the given payload
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

// loop listens to the channel and calls handlers with payload.
// It receives messages from the channel and then iterates over the handlers
// in the handlers map to call them with the payload.
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

// subscribe add a handler to a channel, return error if the channel is closed.
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

// publishSync triggers the handlers defined for this channel synchronously
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

// publish triggers the handlers defined for this channel asynchronously.
// The `payload` argument will be passed to the handler.
// It uses the channel to asynchronously call the handler.
func (c *channel) publish(payload any) error {
	c.RLock()
	defer c.RUnlock()
	if c.closed {
		return ErrChannelClosed
	}

	// 直接调用 transfer，不使用 channel
	c.transfer(c.topic, payload)
	return nil
}

// unsubscribe removes handler defined for this channel.
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

// close closes a channel
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

// EventBus is a container for event topics.
// Each topic corresponds to a channel. `eventbus.Publish()` pushes a message to the channel,
// and the handler in `eventbus.Subscribe()` will process the message coming out of the channel.
type EventBus struct {
	bufferSize  int
	channels    *CowMap
	timeout     time.Duration
	once        sync.Once
	filters     []EventFilter
	middlewares []Middleware
	tracer      EventTracer
}

// New creates an unbuffered EventBus
func New() *EventBus {
	return &EventBus{
		bufferSize:  -1,
		channels:    NewCowMap(),
		timeout:     DefaultTimeout,
		filters:     make([]EventFilter, 0),
		middlewares: make([]Middleware, 0),
	}
}

// NewBuffered creates a buffered EventBus with the specified buffer size
func NewBuffered(bufferSize int) *EventBus {
	if bufferSize <= 0 {
		bufferSize = DefaultBufferSize
	}
	return &EventBus{
		bufferSize:  bufferSize,
		channels:    NewCowMap(),
		timeout:     DefaultTimeout,
		filters:     make([]EventFilter, 0),
		middlewares: make([]Middleware, 0),
	}
}

// AddFilter adds an event filter
func (e *EventBus) AddFilter(filter EventFilter) {
	e.filters = append(e.filters, filter)
}

// Use adds a middleware
func (e *EventBus) Use(middleware Middleware) {
	e.middlewares = append(e.middlewares, middleware)
}

// SubscribeWithPriority adds a handler with priority for a topic
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

// Close closes the eventbus
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

// PublishSync sends a message to all subscribers of a topic synchronously
func (e *EventBus) PublishSync(topic string, payload any) error {
	ch, ok := e.channels.Load(topic)
	if !ok {
		ch = newChannel(topic, e.bufferSize, e) // 传入 EventBus 实例
		e.channels.Store(topic, ch)
		go ch.(*channel).loop()
	}
	return ch.(*channel).publishSync(payload)
}

// PublishWithTimeout 添加带超时的发布方法
func (e *EventBus) PublishWithTimeout(topic string, payload interface{}, timeout time.Duration) error {
	ch, ok := e.channels.Load(topic)
	if !ok {
		ch = newChannel(topic, e.bufferSize, e)
		e.channels.Store(topic, ch)
	}

	select {
	case ch.(*channel).channel <- payload:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("publish timeout after %v", timeout)
	}
}

// Unsubscribe removes a handler from a topic
func (e *EventBus) Unsubscribe(topic string, handler any) error {
	ch, ok := e.channels.Load(topic)
	if !ok {
		return ErrNoSubscriber
	}
	return ch.(*channel).unsubscribe(handler)
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
