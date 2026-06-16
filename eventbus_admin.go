package eventbus

import (
	"context"
	"time"
)

// Unsubscribe 取消订阅
func (e *EventBus) Unsubscribe(topic string, handler any) error {
	if err := e.checkClosed(); err != nil {
		return err
	}

	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		return err
	}

	ch, ok := e.channels.Load(normalizedTopic)
	if !ok {
		return ErrNoSubscriber
	}

	err = ch.(*channel).unsubscribe(handler)
	if err != nil {
		if err == ErrChannelClosed {
			e.channels.CompareAndDelete(normalizedTopic, ch.(*channel))
			e.topicIndex.Remove(normalizedTopic)
			return ErrNoSubscriber
		}
		return err
	}

	e.traceUnsubscribe(normalizedTopic, handler)
	e.tryRemoveEmptyChannel(normalizedTopic, ch.(*channel))

	return nil
}

// UnsubscribeAll 取消主题的所有订阅
func (e *EventBus) UnsubscribeAll(topic string) error {
	if err := e.checkClosed(); err != nil {
		return err
	}

	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		return err
	}

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
	e.tryRemoveEmptyChannel(normalizedTopic, channel)

	return nil
}

func (e *EventBus) tryRemoveEmptyChannel(normalizedTopic string, ch *channel) {
	if e == nil || ch == nil {
		return
	}
	if !ch.closeIfEmpty() {
		return
	}
	if e.channels.CompareAndDelete(normalizedTopic, ch) {
		e.topicIndex.Remove(normalizedTopic)
	}
}

// Close 关闭事件总线
func (e *EventBus) Close() {
	if !e.closed.CompareAndSwap(false, true) {
		return
	}

	// 关闭所有通道
	e.channels.Range(func(key, value any) bool {
		ch := value.(*channel)
		ch.close()
		return true
	})
}

// Shutdown 优雅关闭：先标记排空（拒绝新发布），尽力等待已通过接收检查的发布调用
// 完成入队/分发，并等待所有通道队列中已入队的消息被消费完毕，或 ctx 超时后，
// 再执行与 Close 等价的关闭。
//
// 与 Close 的区别：Close 立即取消并丢弃队列内未消费消息；Shutdown 尽力排空，适用于
// 平滑发版/重启。返回 ctx.Err()（nil 表示正常排空完毕，context.DeadlineExceeded 表示
// 因超时提前关闭）。注意：无缓冲通道为同步交付语义、几乎无积压，drain 主要对有缓冲
// 通道生效；超时或关闭后仍在投递路径上的发送方会收到 ErrShuttingDown / ErrChannelClosed。
func (e *EventBus) Shutdown(ctx context.Context) error {
	if e == nil {
		return ErrEventBusClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if e.closed.Load() {
		return ErrEventBusClosed
	}
	// 标记排空，阻止新发布；channel.loop 仍在运行，持续消费既有队列。
	e.draining.Store(true)

	ticker := time.NewTicker(shutdownPollInterval)
	defer ticker.Stop()
	for !e.allChannelsIdle() {
		select {
		case <-ctx.Done():
			goto drainDone
		case <-ticker.C:
		}
	}

drainDone:
	e.Close()
	return ctx.Err()
}

// allChannelsIdle 检查所有通道的待消费队列与当前处理中的消息是否均已排空。
func (e *EventBus) allChannelsIdle() bool {
	if e.activePublishes.Load() > 0 {
		return false
	}
	empty := true
	e.channels.Range(func(key, value any) bool {
		if !value.(*channel).isIdle() {
			empty = false
			return false
		}
		return true
	})
	return empty
}

// HealthCheck 健康检查
func (e *EventBus) HealthCheck() error {
	return e.checkClosed()
}

// GetStats 获取统计信息
func (e *EventBus) GetStats() map[string]any {
	stats := make(map[string]any)

	channelCount := e.channels.Len()
	stats["channel_count"] = channelCount
	stats["buffer_size"] = e.bufferSize
	stats["timeout"] = e.getTimeout()
	stats["closed"] = e.closed.Load()

	return stats
}

// GetTopics 返回所有已注册的主题列表
func (e *EventBus) GetTopics() []string {
	if e == nil || e.closed.Load() {
		return nil
	}

	topics := make([]string, 0, e.channels.Len())
	e.channels.Range(func(key, value any) bool {
		topic, ok := key.(string)
		if !ok {
			return true
		}
		ch, ok := value.(*channel)
		if !ok || ch.totalHandlerCount() == 0 {
			return true
		}
		topics = append(topics, topic)
		return true
	})
	return topics
}

// GetSubscriberCount 返回指定主题的订阅者数量（包括普通处理器和响应处理器）
func (e *EventBus) GetSubscriberCount(topic string) (int, error) {
	if err := e.checkClosed(); err != nil {
		return 0, err
	}

	normalizedTopic, err := normalizeTopic(topic)
	if err != nil {
		return 0, err
	}

	ch, ok := e.channels.Load(normalizedTopic)
	if !ok {
		return 0, nil
	}

	channel := ch.(*channel)
	channel.RLock()
	count := len(channel.handlers) + len(channel.responseHandlers)
	channel.RUnlock()

	return count, nil
}

// HasSubscribers 检查指定主题是否有订阅者
func (e *EventBus) HasSubscribers(topic string) bool {
	count, err := e.GetSubscriberCount(topic)
	return err == nil && count > 0
}
