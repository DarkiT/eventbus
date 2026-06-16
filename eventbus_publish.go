package eventbus

import (
	"context"
	"fmt"
	"runtime/debug"
	"slices"
	"time"
)

// Publish 异步发布消息。
//
// 注意："异步"指处理器在独立 goroutine 中执行；发布本身在目标通道为无缓冲或队列已满、
// 且消费者较慢时，仍会阻塞调用方直至消息入队、超时（受 SetTimeout 控制）或总线关闭，
// 即背压会传递给生产者。如需严格非阻塞，请用 New(bufferSize) 提供足够缓冲并保证消费吞吐。
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
	if err := e.beginPublish(); err != nil {
		return err
	}
	defer e.endPublish()

	if ctx == nil {
		ctx = context.Background()
	}

	startTime := time.Now()
	metadata := PublishMetadata{
		Timestamp: startTime,
		Async:     async,
		QueueSize: e.bufferSize,
	}

	e.tracePublish(topic, payload, metadata)

	if err := validateTopic(topic); err != nil {
		e.traceError(topic, err)
		return err
	}

	originalTopic := topic // 保持原始主题用于传递给处理器
	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		e.traceError(topic, err)
		return err
	}

	// 获取过滤器和中间件快照（无锁读取，COW 优化）
	filters := e.getFilters()
	middlewares := e.getMiddlewares()

	// 过滤与中间件编排与响应式 / 批量路径共用同一实现，避免多份逻辑漂移。
	// 中间件 After 的失败语义保持不变：仍作为 error 返回，由调用方感知中间件故障。
	payload, allowed, after, fmErr := applyFiltersAndMiddlewares(filters, middlewares, normalizedTopic, payload)
	if fmErr != nil {
		e.traceError(normalizedTopic, fmErr)
		return fmErr
	}
	if !allowed {
		return nil
	}

	deliverErr := e.deliverMessage(ctx, originalTopic, normalizedTopic, payload, async)
	afterErr := after()
	if afterErr != nil {
		e.traceError(normalizedTopic, afterErr)
	}
	if deliverErr != nil {
		return deliverErr
	}
	if afterErr != nil {
		return afterErr
	}

	if err := ctx.Err(); err != nil {
		return err
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
			err = fmt.Errorf("filter panic: %v\nstack: %s", r, debug.Stack())
			allowed = false
		}
	}()
	return filter.Filter(topic, payload), nil
}

func safeMiddlewareBefore(middleware IMiddleware, topic string, payload any) (nextPayload any, err error) {
	if middleware == nil {
		return payload, nil
	}
	nextPayload = payload
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("middleware before panic: %v\nstack: %s", r, debug.Stack())
			nextPayload = payload
		}
	}()
	nextPayload = middleware.Before(topic, payload)
	return nextPayload, nil
}

func safeMiddlewareAfter(middleware IMiddleware, topic string, payload any) (err error) {
	if middleware == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("middleware after panic: %v\nstack: %s", r, debug.Stack())
		}
	}()
	middleware.After(topic, payload)
	return nil
}

func runMiddlewareAfter(middlewares []IMiddleware, topic string, payload any) error {
	var firstErr error
	for i := len(middlewares) - 1; i >= 0; i-- {
		if err := safeMiddlewareAfter(middlewares[i], topic, payload); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// deliverMessage 优化的消息投递
func (e *EventBus) deliverMessage(ctx context.Context, originalTopic, normalizedTopic string, payload any, async bool) error {
	var lastErr error
	deliveredChannels := make(map[*channel]bool) // 防止重复投递

	if async {
		// 异步路径：使用 Trie 树高效匹配
		// 1. 精确匹配
		if ch, ok := e.channels.Load(normalizedTopic); ok {
			channel := ch.(*channel)
			if channel.handlerCount() > 0 {
				deliveredChannels[channel] = true
				if err := channel.publishAsync(ctx, originalTopic, payload); err != nil {
					lastErr = err
					e.traceError(normalizedTopic, err)
				}
			}
		}

		// 2. 使用 Trie 树查找所有匹配的通配符模式
		matchedPatterns := e.topicIndex.Match(normalizedTopic)
		for _, pattern := range matchedPatterns {
			if pattern == normalizedTopic {
				continue // 已经处理过精确匹配
			}
			if ch, ok := e.channels.Load(pattern); ok {
				channel := ch.(*channel)
				if deliveredChannels[channel] || channel.handlerCount() == 0 {
					continue
				}
				deliveredChannels[channel] = true
				if err := channel.publishAsync(ctx, originalTopic, payload); err != nil {
					lastErr = err
					e.traceError(normalizedTopic, err)
				}
			}
		}
		return lastErr
	}

	// 同步路径：收集所有匹配的处理器，按全局优先级执行
	handlers := e.getHandlers(normalizedTopic)
	slices.SortStableFunc(handlers, compareHandlerOrder)

	startTime := time.Now()
	executed := 0
	success := true

	for _, info := range handlers {
		if err := ctx.Err(); err != nil {
			lastErr = err
			success = false
			e.traceError(normalizedTopic, err)
			break
		}
		if info == nil || !info.fn.IsValid() {
			continue
		}
		executed++
		func(h *HandlerInfo) {
			defer func() {
				if r := recover(); r != nil {
					success = false
					e.traceError(normalizedTopic, fmt.Errorf("handler panic: %v\nstack: %s", r, debug.Stack()))
				}
			}()
			var err error
			if h.reliableFn != nil {
				// 同步路径使用调用方 ctx：重试期间调用方阻塞等待，ctx 生命周期覆盖重试。
				err = callReliableWithRetry(h, ctx, originalTopic, payload)
			} else {
				_, err = callHandler(h, ctx, originalTopic, payload)
			}
			if err != nil {
				success = false
				e.traceError(normalizedTopic, err)
			}
		}(info)
	}

	latency := time.Since(startTime)
	if latency > slowConsumerThreshold {
		e.traceSlowConsumer(originalTopic, latency)
	}
	e.traceComplete(originalTopic, CompleteMetadata{
		StartTime:      startTime,
		EndTime:        startTime.Add(latency),
		ProcessingTime: latency,
		HandlerCount:   executed,
		Success:        success,
	})

	if lastErr != nil {
		return lastErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
