package eventbus

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync/atomic"
)

const maxSubscribeRetry = 16

func (e *EventBus) getOrCreateChannel(normalizedTopic string) *channel {
	for range maxSubscribeRetry {
		candidate := newChannel(normalizedTopic, e.bufferSize, e)
		ch, loaded := e.channels.LoadOrStore(normalizedTopic, candidate)
		if loaded {
			candidate.close()
			actual := ch.(*channel)
			if actual.closed.Load() {
				e.channels.CompareAndDelete(normalizedTopic, actual)
				continue
			}
			return actual
		}

		e.topicIndex.Insert(normalizedTopic)
		return candidate
	}

	return nil
}

func (e *EventBus) subscribeWithRetry(normalizedTopic string, subscribe func(*channel) error) error {
	for range maxSubscribeRetry {
		ch := e.getOrCreateChannel(normalizedTopic)
		if ch == nil {
			return ErrChannelClosed
		}

		err := subscribe(ch)
		if err == nil {
			return nil
		}

		if ch.totalHandlerCount() == 0 {
			e.tryRemoveEmptyChannel(normalizedTopic, ch)
		}

		if errors.Is(err, ErrChannelClosed) {
			e.channels.CompareAndDelete(normalizedTopic, ch)
			continue
		}

		return err
	}

	return ErrChannelClosed
}

// Subscribe 订阅主题
func (e *EventBus) Subscribe(topic string, handler any) error {
	return e.SubscribeWithPriority(topic, handler, 0)
}

// SubscribeOnce 一次性订阅，处理器仅执行一次后自动退订
func (e *EventBus) SubscribeOnce(topic string, handler any) error {
	return e.SubscribeOnceWithPriority(topic, handler, 0)
}

// SubscribeOnceWithPriority 带优先级的一次性订阅
// 使用 atomic.Bool 确保并发场景下只执行一次，并在首次执行后自动退订
func (e *EventBus) SubscribeOnceWithPriority(topic string, handler any, priority int) error {
	if err := e.checkClosed(); err != nil {
		return err
	}

	if err := validateTopic(topic); err != nil {
		return err
	}

	// 先复用签名校验，确认是否需要 context 参数
	info, err := newHandlerInfo(handler, priority, false)
	if err != nil {
		return err
	}

	var done atomic.Bool
	original := reflect.ValueOf(handler)

	if info.expectsContext {
		var wrapped func(context.Context, string, any)
		wrapped = func(ctx context.Context, t string, payload any) {
			if !done.CompareAndSwap(false, true) {
				return
			}
			defer func() { _ = e.Unsubscribe(topic, wrapped) }()
			original.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(t), reflect.ValueOf(payload)})
		}
		return e.SubscribeWithPriority(topic, wrapped, priority)
	}

	var wrapped func(string, any)
	wrapped = func(t string, payload any) {
		if !done.CompareAndSwap(false, true) {
			return
		}
		defer func() { _ = e.Unsubscribe(topic, wrapped) }()
		original.Call([]reflect.Value{reflect.ValueOf(t), reflect.ValueOf(payload)})
	}

	return e.SubscribeWithPriority(topic, wrapped, priority)
}

// SubscribeWithPriority 带优先级订阅
func (e *EventBus) SubscribeWithPriority(topic string, handler any, priority int) error {
	if err := e.checkClosed(); err != nil {
		return err
	}

	if err := validateTopic(topic); err != nil {
		return err
	}

	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		return err
	}

	return e.subscribeWithRetry(normalizedTopic, func(ch *channel) error {
		return ch.subscribeWithPriority(handler, priority)
	})
}

// SubscribeWithFilter 订阅带过滤器的处理器
// 过滤器在处理器执行前调用，返回 false 则跳过该处理器
func (e *EventBus) SubscribeWithFilter(topic string, handler any, filter EventFilter) error {
	return e.SubscribeWithFilterAndPriority(topic, handler, filter, 0)
}

// SubscribeWithFilterAndPriority 订阅带过滤器和优先级的处理器
func (e *EventBus) SubscribeWithFilterAndPriority(topic string, handler any, filter EventFilter, priority int) error {
	if err := e.checkClosed(); err != nil {
		return err
	}
	if handler == nil {
		return ErrHandlerIsNotFunc
	}
	if err := validateTopic(topic); err != nil {
		return err
	}

	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		return err
	}

	info, err := newHandlerInfo(handler, priority, false)
	if err != nil {
		return err
	}
	info.filter = filter

	return e.subscribeWithRetry(normalizedTopic, func(ch *channel) error {
		return ch.subscribeHandlerInfo(info, handler)
	})
}

// SubscribeWithResponse 订阅支持返回值的处理器（默认优先级0）
func (e *EventBus) SubscribeWithResponse(topic string, handler ResponseHandler) error {
	return e.SubscribeWithResponseAndPriority(topic, handler, 0)
}

// SubscribeWithResponseAndPriority 带优先级的响应订阅
func (e *EventBus) SubscribeWithResponseAndPriority(topic string, handler ResponseHandler, priority int) error {
	if err := e.checkClosed(); err != nil {
		return err
	}

	if handler == nil {
		return ErrHandlerIsNotFunc
	}

	if err := validateTopic(topic); err != nil {
		return err
	}

	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		return err
	}

	return e.subscribeWithRetry(normalizedTopic, func(ch *channel) error {
		return ch.subscribeWithResponse(handler, priority)
	})
}

// SubscribeWithResponseContext 订阅支持context的响应处理器（默认优先级0）
func (e *EventBus) SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error {
	return e.SubscribeWithResponseContextAndPriority(topic, handler, 0)
}

// SubscribeWithResponseContextAndPriority 带优先级的context响应订阅
func (e *EventBus) SubscribeWithResponseContextAndPriority(topic string, handler ResponseHandlerWithContext, priority int) error {
	if err := e.checkClosed(); err != nil {
		return err
	}

	if handler == nil {
		return ErrHandlerIsNotFunc
	}

	if err := validateTopic(topic); err != nil {
		return err
	}

	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		return err
	}

	return e.subscribeWithRetry(normalizedTopic, func(ch *channel) error {
		return ch.subscribeWithResponse(handler, priority)
	})
}

// SubscribeReliable 订阅可靠处理器：处理失败（返回 error 或 panic）时按配置自动重试，
// 所有尝试均失败或遇到不可重试错误后触发死信回调（若配置）。
//
// 零配置即采用安全默认：3 次尝试、指数 jitter 退避（100ms 起、封顶 1s）、对所有错误重试。
// 通过 RetryOption 自定义：WithMaxAttempts、WithBackoff、WithRetryIf、WithDeadLetter。
//
// 重试为同步语义——在某条消息重试期间，同 topic 后续消息会排队等待，请据此控制
// handler 耗时与退避时长。异步投递路径下重试使用 channel 生命周期 ctx：Close 时中断、
// drain 时不中断（尽力让已入队消息完成）；同步投递路径下使用调用方 ctx。
func (e *EventBus) SubscribeReliable(topic string, handler ReliableHandler, opts ...RetryOption) error {
	if err := e.checkClosed(); err != nil {
		return err
	}
	if handler == nil {
		return ErrHandlerIsNotFunc
	}
	if err := validateTopic(topic); err != nil {
		return err
	}

	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		return err
	}

	fn := reflect.ValueOf(handler)
	info := &HandlerInfo{
		fn:          fn,
		reliableFn:  handler,
		ID:          fn.Pointer(),
		HandlerID:   "handler_" + strconv.FormatUint(uint64(fn.Pointer()), 10),
		retryConfig: applyRetryOptions(opts),
	}

	return e.subscribeWithRetry(normalizedTopic, func(ch *channel) error {
		return ch.subscribeHandlerInfo(info, handler)
	})
}
