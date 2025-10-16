package eventbus

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
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

// ResponseHandler 响应式处理器（不带context参数）
// 签名: func(topic string, payload any) (any, error)
type ResponseHandler func(topic string, payload any) (any, error)

// ResponseHandlerWithContext 响应式处理器（带context参数）
// 签名: func(ctx context.Context, topic string, payload any) (any, error)
type ResponseHandlerWithContext func(ctx context.Context, topic string, payload any) (any, error)

// HandlerResult 处理器执行结果
type HandlerResult struct {
	HandlerID string        `json:"handler_id"` // 处理器唯一标识
	Success   bool          `json:"success"`    // 执行是否成功
	Result    any           `json:"result"`     // 返回值
	Error     error         `json:"error"`      // 错误信息
	Duration  time.Duration `json:"duration"`   // 执行耗时
}

// SyncResult 同步发布结果
type SyncResult struct {
	Success      bool            `json:"success"`       // 整体是否成功
	HandlerCount int             `json:"handler_count"` // 处理器总数
	SuccessCount int             `json:"success_count"` // 成功数量
	FailureCount int             `json:"failure_count"` // 失败数量
	Results      []HandlerResult `json:"results"`       // 详细结果
	TotalTime    time.Duration   `json:"total_time"`    // 总耗时
}

// HandlerInfo 处理器信息，支持优先级
type HandlerInfo struct {
	fn             reflect.Value
	Priority       int
	ID             uintptr // 用于唯一标识处理器
	IsResponse     bool    // 标识是否为响应处理器
	expectsContext bool    // 标识处理器是否期望 context
}

// messageEnvelope 封装通道中的消息，附带上下文信息
type messageEnvelope struct {
	ctx     context.Context
	topic   string
	payload any
}

// channel 优化后的通道实现
type channel struct {
	sync.RWMutex
	topic            string
	channel          chan any
	handlers         []*HandlerInfo // 使用切片替代 map，支持优先级排序
	closed           atomic.Bool    // 使用原子操作
	stopCh           chan struct{}
	eventBus         *EventBus
	handlerMap       map[uintptr]*HandlerInfo // 用于快速查找和删除
	responseHandlers []*HandlerInfo           // 新增：响应处理器列表
	responseMap      map[uintptr]*HandlerInfo // 新增：响应处理器映射
	bufferSize       int
	ctx              context.Context
	cancel           context.CancelFunc
	loopWG           sync.WaitGroup
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
		topic:            topic,
		channel:          ch,
		handlers:         make([]*HandlerInfo, 0),
		handlerMap:       make(map[uintptr]*HandlerInfo),
		responseHandlers: make([]*HandlerInfo, 0),        // 新增：初始化响应处理器列表
		responseMap:      make(map[uintptr]*HandlerInfo), // 新增：初始化响应处理器映射
		stopCh:           make(chan struct{}),
		eventBus:         bus,
		bufferSize:       bufferSize,
		ctx:              ctx,
		cancel:           cancel,
	}

	c.loopWG.Add(1)
	go c.loop()
	return c
}

// loop 优化的消息处理循环
func (c *channel) loop() {
	defer c.loopWG.Done()
	defer func() {
		if r := recover(); r != nil {
			if c.eventBus.tracer != nil {
				c.eventBus.tracer.OnError(c.topic, fmt.Errorf("panic in handler: %v", r))
			}
		}
	}()

	for {
		select {
		case message, ok := <-c.channel:
			if !ok {
				return
			}

			switch msg := message.(type) {
			case messageEnvelope:
				c.transfer(msg.ctx, msg.topic, msg.payload)
			case *messageEnvelope:
				c.transfer(msg.ctx, msg.topic, msg.payload)
			case map[string]any:
				// 兼容旧格式，保证向后兼容
				ctx := context.Background()
				topic, _ := msg["topic"].(string)
				if topic == "" {
					topic = c.topic
				}
				c.transfer(ctx, topic, msg["payload"])
			default:
				c.transfer(context.Background(), c.topic, message)
			}
		case <-c.ctx.Done():
			return
		case <-c.stopCh:
			return
		}
	}
}

// transfer 优化的消息传递，支持优先级
func (c *channel) transfer(ctx context.Context, topic string, payload any) {
	if ctx == nil {
		ctx = context.Background()
	}

	c.RLock()
	handlers := slices.Clone(c.handlers) // 复制切片避免并发问题
	c.RUnlock()

	startTime := time.Now()
	executed := 0
	success := true

	defer func() {
		latency := time.Since(startTime)
		if latency > slowConsumerThreshold && c.eventBus.tracer != nil {
			c.eventBus.tracer.OnSlowConsumer(topic, latency)
		}
		if c.eventBus.tracer != nil {
			c.eventBus.tracer.OnComplete(topic, CompleteMetadata{
				StartTime:      startTime,
				EndTime:        startTime.Add(latency),
				ProcessingTime: latency,
				HandlerCount:   executed,
				Success:        success,
			})
		}
	}()

	// 按优先级顺序执行处理器
	for _, handlerInfo := range handlers {
		if err := ctx.Err(); err != nil {
			success = false
			if c.eventBus.tracer != nil {
				c.eventBus.tracer.OnError(topic, err)
			}
			return
		}
		func(info *HandlerInfo) {
			if !info.fn.IsValid() {
				return
			}
			executed++
			defer func() {
				if r := recover(); r != nil {
					success = false
					if c.eventBus.tracer != nil {
						c.eventBus.tracer.OnError(topic, fmt.Errorf("handler panic: %v", r))
					}
				}
			}()

			args := make([]reflect.Value, 0, 3)
			if info.expectsContext {
				args = append(args, reflect.ValueOf(ctx))
			}
			args = append(args, reflect.ValueOf(topic), reflect.ValueOf(payload))
			info.fn.Call(args)
		}(handlerInfo)
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

	handlerInfo, err := newHandlerInfo(handler, priority, false)
	if err != nil {
		return err
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

// subscribeWithResponse 添加响应处理器
func (c *channel) subscribeWithResponse(handler any, priority int) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}

	handlerInfo, err := newHandlerInfo(handler, priority, true)
	if err != nil {
		return err
	}

	c.Lock()
	defer c.Unlock()

	// 添加到响应处理器映射表
	c.responseMap[handlerInfo.ID] = handlerInfo

	// 添加到响应处理器切片并按优先级排序（优先级高的在前）
	c.responseHandlers = append(c.responseHandlers, handlerInfo)
	slices.SortFunc(c.responseHandlers, func(a, b *HandlerInfo) int {
		return b.Priority - a.Priority // 降序排列
	})

	if c.eventBus.tracer != nil {
		c.eventBus.tracer.OnSubscribe(c.topic, handler)
	}

	return nil
}

// publishAsync 真正的异步发布
func (c *channel) publishAsync(ctx context.Context, topic string, payload any) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}

	// 检查队列是否满
	if c.bufferSize > 0 && len(c.channel) >= c.bufferSize {
		if c.eventBus.tracer != nil {
			c.eventBus.tracer.OnQueueFull(c.topic, len(c.channel))
		}
	}

	// 包装消息以包含实际主题和上下文
	message := messageEnvelope{
		ctx:     ctx,
		topic:   topic,
		payload: payload,
	}

	// 对于无缓冲通道，使用阻塞等待并配合超时
	if c.bufferSize <= 0 {
		timer := time.NewTimer(c.eventBus.timeout)
		defer timer.Stop()
		select {
		case c.channel <- message:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopCh:
			return ErrChannelClosed
		case <-timer.C:
			return ErrPublishTimeout
		}
	}

	// 对于有缓冲通道，先尝试一次快速写入；若队列已满则阻塞等待直至释放或超时
	select {
	case c.channel <- message:
		return nil
	default:
	}

	timer := time.NewTimer(c.eventBus.timeout)
	defer timer.Stop()

	select {
	case c.channel <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.stopCh:
		return ErrChannelClosed
	case <-timer.C:
		return ErrPublishTimeout
	}
}

// publishSync 同步发布
func (c *channel) publishSync(ctx context.Context, topic string, payload any) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}

	if payload == nil {
		return nil
	}

	c.transfer(ctx, topic, payload)
	return nil
}

// unsubscribe 移除处理器
func (c *channel) unsubscribe(handler any) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}

	fn := reflect.ValueOf(handler)
	if !fn.IsValid() || fn.Kind() != reflect.Func {
		return ErrHandlerIsNotFunc
	}
	handlerID := fn.Pointer()

	c.Lock()
	defer c.Unlock()

	removed := false
	if _, exists := c.handlerMap[handlerID]; exists {
		delete(c.handlerMap, handlerID)
		c.handlers = slices.DeleteFunc(c.handlers, func(h *HandlerInfo) bool {
			return h.ID == handlerID
		})
		removed = true
	}

	if _, exists := c.responseMap[handlerID]; exists {
		delete(c.responseMap, handlerID)
		c.responseHandlers = slices.DeleteFunc(c.responseHandlers, func(h *HandlerInfo) bool {
			return h.ID == handlerID
		})
		removed = true
	}

	if !removed {
		return ErrNoSubscriber
	}

	return nil
}

// close 关闭通道
func (c *channel) close() {
	if !c.closed.CompareAndSwap(false, true) {
		return
	}

	c.cancel()      // 取消context
	close(c.stopCh) // 关闭停止信号
	c.loopWG.Wait() // 等待loop退出

	// 保留底层通道，避免并发写协程发生 send on closed channel 的崩溃

	c.Lock()
	c.handlers = nil
	c.handlerMap = nil
	c.responseHandlers = nil
	c.responseMap = nil
	c.Unlock()
}

// EventBus 优化后的事件总线
type EventBus struct {
	bufferSize  int
	channels    *cowMap
	timeout     time.Duration
	filters     []EventFilter
	middlewares []IMiddleware
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
		middlewares: make([]IMiddleware, 0),
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
func (e *EventBus) Use(middleware IMiddleware) {
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

	topic = normalizeTopic(topic)

	// 创建或获取通道
	ch, ok := e.channels.Load(topic)
	if !ok {
		ch = newChannel(topic, e.bufferSize, e)
		e.channels.Store(topic, ch)
	}

	return ch.(*channel).subscribeWithPriority(handler, priority)
}

// SubscribeWithResponse 订阅支持返回值的处理器（默认优先级0）
func (e *EventBus) SubscribeWithResponse(topic string, handler ResponseHandler) error {
	return e.SubscribeWithResponseAndPriority(topic, handler, 0)
}

// SubscribeWithResponseAndPriority 带优先级的响应订阅
func (e *EventBus) SubscribeWithResponseAndPriority(topic string, handler ResponseHandler, priority int) error {
	if e.closed.Load() {
		return ErrChannelClosed
	}

	if handler == nil {
		return ErrHandlerIsNotFunc
	}

	topic = normalizeTopic(topic)

	// 创建或获取通道
	ch, ok := e.channels.Load(topic)
	if !ok {
		ch = newChannel(topic, e.bufferSize, e)
		e.channels.Store(topic, ch)
	}

	return ch.(*channel).subscribeWithResponse(handler, priority)
}

// SubscribeWithResponseContext 订阅支持context的响应处理器（默认优先级0）
func (e *EventBus) SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error {
	return e.SubscribeWithResponseContextAndPriority(topic, handler, 0)
}

// SubscribeWithResponseContextAndPriority 带优先级的context响应订阅
func (e *EventBus) SubscribeWithResponseContextAndPriority(topic string, handler ResponseHandlerWithContext, priority int) error {
	if e.closed.Load() {
		return ErrChannelClosed
	}

	if handler == nil {
		return ErrHandlerIsNotFunc
	}

	topic = normalizeTopic(topic)

	// 创建或获取通道
	ch, ok := e.channels.Load(topic)
	if !ok {
		ch = newChannel(topic, e.bufferSize, e)
		e.channels.Store(topic, ch)
	}

	return ch.(*channel).subscribeWithResponse(handler, priority)
}

// Publish 异步发布消息
func (e *EventBus) Publish(topic string, payload any) error {
	return e.publishInternal(context.Background(), topic, payload, true)
}

// PublishSync 同步发布消息
func (e *EventBus) PublishSync(topic string, payload any) error {
	return e.publishInternal(context.Background(), topic, payload, false)
}

// PublishWithContext 带上下文的异步发布
func (e *EventBus) PublishWithContext(ctx context.Context, topic string, payload any) error {
	return e.publishInternal(ctx, topic, payload, true)
}

// PublishAsyncWithContext 显式的带上下文异步发布
func (e *EventBus) PublishAsyncWithContext(ctx context.Context, topic string, payload any) error {
	return e.publishInternal(ctx, topic, payload, true)
}

// PublishSyncWithContext 带上下文的同步发布
func (e *EventBus) PublishSyncWithContext(ctx context.Context, topic string, payload any) error {
	return e.publishInternal(ctx, topic, payload, false)
}

// publishInternal 内部发布实现
func (e *EventBus) publishInternal(ctx context.Context, topic string, payload any, async bool) error {
	if e.closed.Load() {
		return ErrChannelClosed
	}

	if ctx == nil {
		ctx = context.Background()
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

	originalTopic := topic                   // 保持原始主题用于传递给处理器
	normalizedTopic := normalizeTopic(topic) // 用于匹配

	// 应用过滤器
	e.lock.RLock()
	filters := slices.Clone(e.filters)
	middlewares := slices.Clone(e.middlewares)
	e.lock.RUnlock()

	for _, filter := range filters {
		allowed, filterErr := safeFilterInvoke(filter, normalizedTopic, payload)
		if filterErr != nil {
			if e.tracer != nil {
				e.tracer.OnError(normalizedTopic, filterErr)
			}
			return filterErr
		}
		if !allowed {
			return nil
		}
	}

	// 构建处理链
	handler := func(t string, p any) error {
		return e.deliverMessage(ctx, originalTopic, normalizedTopic, p, async)
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

	err := handler(normalizedTopic, payload)
	if err != nil {
		return err
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	return nil
}

// safeFilterInvoke 安全执行过滤器，避免 panic 影响主流程
func safeFilterInvoke(filter EventFilter, topic string, payload any) (allowed bool, err error) {
	if filter == nil {
		return true, nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("filter panic: %v", r)
			allowed = false
		}
	}()
	return filter.Filter(topic, payload), nil
}

// deliverMessage 优化的消息投递
func (e *EventBus) deliverMessage(ctx context.Context, originalTopic, normalizedTopic string, payload any, async bool) error {
	var lastErr error
	deliveredChannels := make(map[*channel]bool) // 防止重复投递

	// 直接匹配
	if ch, ok := e.channels.Load(normalizedTopic); ok {
		channel := ch.(*channel)
		deliveredChannels[channel] = true

		if async {
			if err := channel.publishAsync(ctx, originalTopic, payload); err != nil {
				lastErr = err
				if e.tracer != nil {
					e.tracer.OnError(normalizedTopic, err)
				}
			}
		} else {
			if err := channel.publishSync(ctx, originalTopic, payload); err != nil {
				lastErr = err
				if e.tracer != nil {
					e.tracer.OnError(normalizedTopic, err)
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
		if matchTopic(pattern, normalizedTopic) {
			deliveredChannels[channel] = true

			if async {
				if err := channel.publishAsync(ctx, originalTopic, payload); err != nil {
					lastErr = err
					if e.tracer != nil {
						e.tracer.OnError(normalizedTopic, err)
					}
				}
			} else {
				if err := channel.publishSync(ctx, originalTopic, payload); err != nil {
					lastErr = err
					if e.tracer != nil {
						e.tracer.OnError(normalizedTopic, err)
					}
				}
			}
		}
		return true
	})

	return lastErr
}

// getResponseHandlers 获取主题的所有响应处理器
func (e *EventBus) getResponseHandlers(normalizedTopic string) []*HandlerInfo {
	var responseHandlers []*HandlerInfo
	processedChannels := make(map[*channel]bool) // 防止重复处理

	// 直接匹配
	if ch, ok := e.channels.Load(normalizedTopic); ok {
		channel := ch.(*channel)
		processedChannels[channel] = true
		channel.RLock()
		responseHandlers = append(responseHandlers, slices.Clone(channel.responseHandlers)...)
		channel.RUnlock()
	}

	// 通配符匹配 - 遍历所有订阅的模式
	e.channels.Range(func(key, value any) bool {
		pattern := key.(string)
		channel := value.(*channel)

		// 跳过已经处理过的通道
		if processedChannels[channel] {
			return true
		}

		// 检查是否匹配通配符模式
		if matchTopic(pattern, normalizedTopic) {
			processedChannels[channel] = true
			channel.RLock()
			responseHandlers = append(responseHandlers, slices.Clone(channel.responseHandlers)...)
			channel.RUnlock()
		}
		return true
	})

	return responseHandlers
}

// publishSyncWithStrategy 核心同步发布实现
func (e *EventBus) publishSyncWithStrategy(ctx context.Context, topic string, payload any, strategy string, timeout time.Duration) (*SyncResult, error) {
	if e.closed.Load() {
		return nil, ErrChannelClosed
	}

	if ctx == nil {
		ctx = context.Background()
	}

	var cancel context.CancelFunc
	if timeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			ctx, cancel = context.WithTimeout(ctx, timeout)
		}
	}

	if cancel == nil {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	startTime := time.Now()
	originalTopic := topic
	normalizedTopic := normalizeTopic(topic)

	// 获取所有响应处理器
	responseHandlers := e.getResponseHandlers(normalizedTopic)
	if len(responseHandlers) == 0 {
		totalTime := time.Since(startTime)
		result := &SyncResult{
			Success:      true,
			HandlerCount: 0,
			TotalTime:    totalTime,
		}
		if e.tracer != nil {
			e.tracer.OnComplete(originalTopic, CompleteMetadata{
				StartTime:      startTime,
				EndTime:        startTime.Add(totalTime),
				ProcessingTime: totalTime,
				HandlerCount:   0,
				Success:        true,
			})
		}
		return result, nil
	}

	// 执行处理器并收集结果
	results := make([]HandlerResult, len(responseHandlers))
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := int32(0)
	var executedCount atomic.Int32
	var canceledBySuccess atomic.Bool

	for i, handlerInfo := range responseHandlers {
		wg.Add(1)
		go func(idx int, info *HandlerInfo) {
			defer wg.Done()
			if !info.fn.IsValid() {
				return
			}

			if err := ctx.Err(); err != nil {
				mu.Lock()
				results[idx] = HandlerResult{
					HandlerID: fmt.Sprintf("handler_%d", info.ID),
					Success:   false,
					Error:     err,
					Duration:  0,
				}
				mu.Unlock()
				return
			}
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					results[idx] = HandlerResult{
						HandlerID: fmt.Sprintf("handler_%d", info.ID),
						Success:   false,
						Error:     fmt.Errorf("handler panic: %v", r),
						Duration:  0,
					}
					mu.Unlock()
				}
			}()

			execStart := time.Now()
			executedCount.Add(1)
			result, err := callResponseHandler(info, ctx, originalTopic, payload)
			duration := time.Since(execStart)

			success := err == nil
			if success {
				atomic.AddInt32(&successCount, 1)
				if strategy == "any" {
					canceledBySuccess.Store(true)
					cancel()
				}
			}

			mu.Lock()
			results[idx] = HandlerResult{
				HandlerID: fmt.Sprintf("handler_%d", info.ID),
				Success:   success,
				Result:    result,
				Error:     err,
				Duration:  duration,
			}
			mu.Unlock()
		}(i, handlerInfo)
	}

	// 等待所有处理器完成或超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 所有处理器完成
	case <-ctx.Done():
		<-done
		if ctxErr := ctx.Err(); errors.Is(ctxErr, context.DeadlineExceeded) {
			return nil, fmt.Errorf("publish timeout after %v", timeout)
		}
		if ctxErr := ctx.Err(); ctxErr != nil && !canceledBySuccess.Load() {
			return nil, ctxErr
		}
	}

	totalTime := time.Since(startTime)
	finalSuccessCount := int(atomic.LoadInt32(&successCount))
	finalExecuted := int(executedCount.Load())

	// 根据策略判断成功
	var overallSuccess bool
	switch strategy {
	case "all":
		overallSuccess = finalSuccessCount == len(responseHandlers)
	case "any":
		overallSuccess = finalSuccessCount > 0
	default:
		return nil, fmt.Errorf("unknown strategy: %s", strategy)
	}

	result := &SyncResult{
		Success:      overallSuccess,
		HandlerCount: len(responseHandlers),
		SuccessCount: finalSuccessCount,
		FailureCount: len(responseHandlers) - finalSuccessCount,
		Results:      results,
		TotalTime:    totalTime,
	}

	if e.tracer != nil {
		e.tracer.OnComplete(originalTopic, CompleteMetadata{
			StartTime:      startTime,
			EndTime:        startTime.Add(totalTime),
			ProcessingTime: totalTime,
			HandlerCount:   finalExecuted,
			Success:        overallSuccess,
		})
	}

	return result, nil
}

// PublishSyncAll 所有处理器成功才算成功
func (e *EventBus) PublishSyncAll(topic string, payload any) (*SyncResult, error) {
	return e.publishSyncWithStrategy(context.Background(), topic, payload, "all", e.timeout)
}

// PublishSyncAllWithContext 所有处理器成功才算成功，且透传调用方上下文
func (e *EventBus) PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	return e.publishSyncWithStrategy(ctx, topic, payload, "all", e.timeout)
}

// PublishSyncAny 任一处理器成功即算成功
func (e *EventBus) PublishSyncAny(topic string, payload any) (*SyncResult, error) {
	return e.publishSyncWithStrategy(context.Background(), topic, payload, "any", e.timeout)
}

// PublishSyncAnyWithContext 任一处理器成功即算成功，且透传调用方上下文
func (e *EventBus) PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	return e.publishSyncWithStrategy(ctx, topic, payload, "any", e.timeout)
}

// Unsubscribe 取消订阅
func (e *EventBus) Unsubscribe(topic string, handler any) error {
	if e.closed.Load() {
		return ErrChannelClosed
	}

	normalizedTopic := normalizeTopic(topic)

	ch, ok := e.channels.Load(normalizedTopic)
	if !ok {
		return ErrNoSubscriber
	}

	err := ch.(*channel).unsubscribe(handler)
	if err != nil {
		return err
	}

	if e.tracer != nil {
		e.tracer.OnUnsubscribe(normalizedTopic, handler)
	}

	return nil
}

// UnsubscribeAll 取消主题的所有订阅
func (e *EventBus) UnsubscribeAll(topic string) error {
	if e.closed.Load() {
		return ErrChannelClosed
	}

	normalizedTopic := normalizeTopic(topic)

	ch, ok := e.channels.Load(normalizedTopic)
	if !ok {
		return ErrNoSubscriber
	}

	channel := ch.(*channel)
	channel.Lock()
	channel.handlers = nil
	channel.handlerMap = make(map[uintptr]*HandlerInfo)
	channel.responseHandlers = nil
	channel.responseMap = make(map[uintptr]*HandlerInfo)
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

var (
	contextType    = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorInterface = reflect.TypeOf((*error)(nil)).Elem()
)

// newHandlerInfo 构建处理器信息，并校验函数签名
func newHandlerInfo(handler any, priority int, isResponse bool) (*HandlerInfo, error) {
	if handler == nil {
		return nil, ErrHandlerIsNotFunc
	}

	// 快速路径：检查是否为具体的ResponseHandler类型
	if isResponse {
		switch h := handler.(type) {
		case ResponseHandler:
			fn := reflect.ValueOf(h)
			return &HandlerInfo{
				fn:             fn,
				Priority:       priority,
				ID:             fn.Pointer(),
				IsResponse:     true,
				expectsContext: false,
			}, nil
		case ResponseHandlerWithContext:
			fn := reflect.ValueOf(h)
			return &HandlerInfo{
				fn:             fn,
				Priority:       priority,
				ID:             fn.Pointer(),
				IsResponse:     true,
				expectsContext: true,
			}, nil
		}
	}

	// 通用路径：使用反射验证
	fn := reflect.ValueOf(handler)
	if !fn.IsValid() || fn.Kind() != reflect.Func {
		return nil, ErrHandlerIsNotFunc
	}

	typ := fn.Type()
	paramIndex := 0
	expectsContext := false
	if typ.NumIn() > 0 && typ.In(0) == contextType {
		expectsContext = true
		paramIndex = 1
	}

	// 需要 topic 和 payload 两个参数
	if typ.NumIn() != paramIndex+2 {
		return nil, ErrHandlerParamNum
	}
	if typ.In(paramIndex).Kind() != reflect.String {
		return nil, ErrHandlerFirstParam
	}

	if isResponse {
		if typ.NumOut() != 2 {
			return nil, ErrResponseReturnNum
		}
		if !typ.Out(1).Implements(errorInterface) {
			return nil, ErrResponseReturnType
		}
	} else if typ.NumOut() != 0 {
		return nil, ErrHandlerReturnNum
	}

	return &HandlerInfo{
		fn:             fn,
		Priority:       priority,
		ID:             fn.Pointer(),
		IsResponse:     isResponse,
		expectsContext: expectsContext,
	}, nil
}

// callResponseHandler 调用响应式处理器，并解析返回值
func callResponseHandler(info *HandlerInfo, ctx context.Context, topic string, payload any) (any, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	args := make([]reflect.Value, 0, 3)
	if info.expectsContext {
		args = append(args, reflect.ValueOf(ctx))
	}
	args = append(args, reflect.ValueOf(topic), reflect.ValueOf(payload))

	outputs := info.fn.Call(args)
	var result any
	if len(outputs) > 0 {
		result = outputs[0].Interface()
	}

	var err error
	if len(outputs) > 1 {
		errVal := outputs[1]
		if errVal.IsValid() && !errVal.IsNil() {
			err = errVal.Interface().(error)
		}
	}

	return result, err
}
