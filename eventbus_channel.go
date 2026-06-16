package eventbus

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

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
	pendingMessages  atomic.Int64 // 已成功进入异步投递路径、尚未完成处理的消息数
	activeDeliveries atomic.Int64 // 正在执行 transfer 的消息数，供 Shutdown 判断同步投递是否完成
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
			c.eventBus.traceError(c.topic, fmt.Errorf("panic in handler: %v\nstack: %s", r, debug.Stack()))
		}
	}()

	for {
		select {
		case message, ok := <-c.channel:
			if !ok {
				return
			}

			func() {
				defer c.pendingMessages.Add(-1)
				if msg, ok := message.(messageEnvelope); ok {
					c.transfer(msg.ctx, msg.topic, msg.payload)
				} else {
					c.transfer(context.Background(), c.topic, message)
				}
			}()
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
	c.activeDeliveries.Add(1)
	defer c.activeDeliveries.Add(-1)

	c.RLock()
	handlers := slices.Clone(c.handlers) // 复制切片避免并发问题
	c.RUnlock()

	startTime := time.Now()
	executed := 0
	success := true

	defer func() {
		latency := time.Since(startTime)
		if latency > slowConsumerThreshold {
			c.eventBus.traceSlowConsumer(topic, latency)
		}
		c.eventBus.traceComplete(topic, CompleteMetadata{
			StartTime:      startTime,
			EndTime:        startTime.Add(latency),
			ProcessingTime: latency,
			HandlerCount:   executed,
			Success:        success,
		})
	}()

	// 按优先级顺序执行处理器
	for _, handlerInfo := range handlers {
		if err := ctx.Err(); err != nil {
			success = false
			c.eventBus.traceError(topic, err)
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
					c.eventBus.traceError(topic, fmt.Errorf("handler panic: %v\nstack: %s", r, debug.Stack()))
				}
			}()
			var err error
			if info.reliableFn != nil {
				// 异步路径下用 channel 生命周期 ctx，避免发布者短生命周期 ctx 短路重试；
				// Close 时取消以终止重试，drain（Shutdown）时不取消以尽力完成已入队消息。
				err = callReliableWithRetry(info, c.ctx, topic, payload)
			} else {
				_, err = callHandler(info, ctx, topic, payload)
			}
			if err != nil {
				success = false
				c.eventBus.traceError(topic, err)
			}
		}(handlerInfo)
	}
}

// handlerCount 返回普通处理器数量（加锁读取，避免并发读写竞态）。
func (c *channel) handlerCount() int {
	c.RLock()
	count := len(c.handlers)
	c.RUnlock()
	return count
}

// isIdle 返回通道是否既无待消费队列、也无正在执行的消息。
func (c *channel) isIdle() bool {
	if c == nil {
		return true
	}
	return c.pendingMessages.Load() == 0 && c.activeDeliveries.Load() == 0
}

// totalHandlerCount 返回普通处理器与响应处理器总数。
func (c *channel) totalHandlerCount() int {
	c.RLock()
	count := len(c.handlers) + len(c.responseHandlers)
	c.RUnlock()
	return count
}

func (c *channel) subscribeHandlerInfo(handlerInfo *HandlerInfo, originalHandler any) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}
	if handlerInfo == nil {
		return ErrHandlerIsNotFunc
	}

	c.Lock()
	defer c.Unlock()
	if c.closed.Load() {
		return ErrChannelClosed
	}

	if handlerInfo.IsResponse {
		handlerInfo.Sequence = c.eventBus.nextSequence()
		c.responseMap[handlerInfo.ID] = handlerInfo
		idx := sort.Search(len(c.responseHandlers), func(i int) bool {
			return c.responseHandlers[i].Priority < handlerInfo.Priority
		})
		c.responseHandlers = slices.Insert(c.responseHandlers, idx, handlerInfo)
	} else {
		handlerInfo.Sequence = c.eventBus.nextSequence()
		c.handlerMap[handlerInfo.ID] = handlerInfo
		idx := sort.Search(len(c.handlers), func(i int) bool {
			return c.handlers[i].Priority < handlerInfo.Priority
		})
		c.handlers = slices.Insert(c.handlers, idx, handlerInfo)
	}

	c.eventBus.traceSubscribe(c.topic, originalHandler)
	return nil
}

// subscribe 添加处理器
func (c *channel) subscribe(handler any) error {
	return c.subscribeWithPriority(handler, 0)
}

// subscribeWithPriority 添加带优先级的处理器
func (c *channel) subscribeWithPriority(handler any, priority int) error {
	handlerInfo, err := newHandlerInfo(handler, priority, false)
	if err != nil {
		return err
	}
	return c.subscribeHandlerInfo(handlerInfo, handler)
}

// subscribeWithResponse 添加响应处理器
func (c *channel) subscribeWithResponse(handler any, priority int) error {
	handlerInfo, err := newHandlerInfo(handler, priority, true)
	if err != nil {
		return err
	}
	return c.subscribeHandlerInfo(handlerInfo, handler)
}

// publishAsync 真正的异步发布
func (c *channel) publishAsync(ctx context.Context, topic string, payload any) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}

	// 检查队列是否满
	if c.bufferSize > 0 && len(c.channel) >= c.bufferSize {
		c.eventBus.traceQueueFull(c.topic, len(c.channel))
	}

	// 包装消息以包含实际主题和上下文
	message := messageEnvelope{
		ctx:     ctx,
		topic:   topic,
		payload: payload,
	}
	sent := false
	c.pendingMessages.Add(1)
	defer func() {
		if !sent {
			c.pendingMessages.Add(-1)
		}
	}()

	// 对于无缓冲通道，使用阻塞等待并配合超时
	if c.bufferSize <= 0 {
		// 快速路径：接收方已就绪时立即发送，避免每次都分配 timer
		select {
		case c.channel <- message:
			sent = true
			return nil
		default:
		}

		timer := time.NewTimer(c.eventBus.getTimeout())
		defer timer.Stop()
		select {
		case c.channel <- message:
			sent = true
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
		sent = true
		return nil
	default:
	}

	timer := time.NewTimer(c.eventBus.getTimeout())
	defer timer.Stop()

	select {
	case c.channel <- message:
		sent = true
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.stopCh:
		return ErrChannelClosed
	case <-timer.C:
		return ErrPublishTimeout
	}
}

// publishBatchAsync 批量异步发布，减少重复的通道检测与定时器创建
func (c *channel) publishBatchAsync(envelopes []messageEnvelope) error {
	if c.closed.Load() {
		return ErrChannelClosed
	}
	if len(envelopes) == 0 {
		return nil
	}

	timeout := c.eventBus.getTimeout()

	// 缓冲通道路径
	if c.bufferSize > 0 {
		// 若初始队列已满，先记录一次告警
		if len(c.channel) >= c.bufferSize {
			c.eventBus.traceQueueFull(c.topic, len(c.channel))
		}

		for _, env := range envelopes {
			sent := false
			c.pendingMessages.Add(1)
			// 快速路径，尽量无阻塞写入
			select {
			case c.channel <- env:
				sent = true
				continue
			default:
			}

			// 阻塞等待或超时/取消
			timer := time.NewTimer(timeout)
			select {
			case c.channel <- env:
				sent = true
			case <-env.ctx.Done():
				timer.Stop()
				if !sent {
					c.pendingMessages.Add(-1)
				}
				return env.ctx.Err()
			case <-c.stopCh:
				timer.Stop()
				if !sent {
					c.pendingMessages.Add(-1)
				}
				return ErrChannelClosed
			case <-timer.C:
				if !sent {
					c.pendingMessages.Add(-1)
				}
				return ErrPublishTimeout
			}
			timer.Stop()
		}
		return nil
	}

	// 无缓冲通道路径
	for _, env := range envelopes {
		sent := false
		c.pendingMessages.Add(1)
		// 快速路径：接收方已就绪时立即发送，避免分配 timer
		select {
		case c.channel <- env:
			sent = true
			continue
		default:
		}

		timer := time.NewTimer(timeout)
		select {
		case c.channel <- env:
			sent = true
		case <-env.ctx.Done():
			timer.Stop()
			if !sent {
				c.pendingMessages.Add(-1)
			}
			return env.ctx.Err()
		case <-c.stopCh:
			timer.Stop()
			if !sent {
				c.pendingMessages.Add(-1)
			}
			return ErrChannelClosed
		case <-timer.C:
			if !sent {
				c.pendingMessages.Add(-1)
			}
			return ErrPublishTimeout
		}
		timer.Stop()
	}
	return nil
}

// publishSync 同步发布
func (c *channel) publishSync(ctx context.Context, topic string, payload any) error {
	if c.closed.Load() {
		return ErrChannelClosed
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

// closeIfEmpty 仅在通道仍为空时关闭，避免并发订阅被空通道回收误杀。
func (c *channel) closeIfEmpty() bool {
	if c == nil {
		return false
	}

	c.Lock()
	if len(c.handlers)+len(c.responseHandlers) != 0 {
		c.Unlock()
		return false
	}
	if !c.closed.CompareAndSwap(false, true) {
		c.Unlock()
		return true
	}
	c.Unlock()

	c.cancel()
	close(c.stopCh)
	c.loopWG.Wait()

	c.Lock()
	c.handlers = nil
	c.handlerMap = nil
	c.responseHandlers = nil
	c.responseMap = nil
	c.Unlock()
	return true
}
