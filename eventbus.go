package eventbus

import (
	"fmt"
	"reflect"
	"sort"
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
	// 应用过滤器
	for _, filter := range c.eventBus.filters {
		if !filter.Filter(topic, payload) {
			return
		}
	}

	// 应用中间件 Before 钩子
	for _, middleware := range c.eventBus.middlewares {
		payload = middleware.Before(topic, payload)
	}

	// 获取所有处理器并按优先级排序
	var subs []*Subscription
	c.handlers.Range(func(_, value any) bool {
		if sub, ok := value.(*Subscription); ok {
			subs = append(subs, sub)
		}
		return true
	})
	sort.Slice(subs, func(i, j int) bool {
		return subs[i].Priority > subs[j].Priority
	})

	// 调用处理器
	var payloadValue reflect.Value
	topicValue := reflect.ValueOf(topic)
	for _, sub := range subs {
		handler := sub.Handler.(*reflect.Value)
		typ := handler.Type()

		if payload == nil {
			payloadValue = reflect.New(typ.In(1)).Elem()
		} else {
			payloadValue = reflect.ValueOf(payload)
		}
		handler.Call([]reflect.Value{topicValue, payloadValue})
	}

	// 应用中间件 After 钩子
	for _, middleware := range c.eventBus.middlewares {
		middleware.After(topic, payload)
	}
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

// publishSync triggers the handlers defined for this channel synchronously.
// The payload argument will be passed to the handler.
// It does not use channels and instead directly calls the handler function.
func (c *channel) publishSync(payload any) error {
	c.RLock()
	defer c.RUnlock()
	if c.closed {
		return ErrChannelClosed
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
	c.channel <- payload
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

// Subscribe adds a handler for a topic
func (e *EventBus) Subscribe(topic string, handler any) error {
	// 使用默认优先级 0 调用 SubscribeWithPriority
	return e.SubscribeWithPriority(topic, handler, 0)
}

// Publish sends a message to all subscribers of a topic asynchronously
func (e *EventBus) Publish(topic string, payload any) error {
	ch, ok := e.channels.Load(topic)
	if !ok {
		ch = newChannel(topic, e.bufferSize, e) // 传入 EventBus 实例
		e.channels.Store(topic, ch)
		go ch.(*channel).loop()
	}
	return ch.(*channel).publish(payload)
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

// Unsubscribe removes a handler from a topic
func (e *EventBus) Unsubscribe(topic string, handler any) error {
	ch, ok := e.channels.Load(topic)
	if !ok {
		return ErrNoSubscriber
	}
	return ch.(*channel).unsubscribe(handler)
}
