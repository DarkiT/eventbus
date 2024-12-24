package eventbus

import (
	"sync/atomic"
	"time"
)

// EventTracer 定义事件追踪接口
type EventTracer interface {
	// OnPublish 在消息发布时调用
	OnPublish(topic string, payload any, metadata PublishMetadata)
	// OnSubscribe 在订阅时调用
	OnSubscribe(topic string, handler any)
	// OnUnsubscribe 在取消订阅时调用
	OnUnsubscribe(topic string, handler any)
	// OnError 在发生错误时调用
	OnError(topic string, err error)
	// OnComplete 在消息处理完成时调用
	OnComplete(topic string, metadata CompleteMetadata)
}

// PublishMetadata 发布事件的元数据
type PublishMetadata struct {
	Timestamp   time.Time // 发布时间
	Async       bool      // 是否异步发布
	QueueSize   int       // 队列大小
	PublisherID string    // 发布者ID（可选）
}

// CompleteMetadata 事件处理完成的元数据
type CompleteMetadata struct {
	StartTime      time.Time     // 开始处理时间
	EndTime        time.Time     // 结束处理时间
	ProcessingTime time.Duration // 处理耗时
	HandlerCount   int           // 处理器数量
	Success        bool          // 是否成功
}

// MetricsTracer 实现基础的指标收集
type MetricsTracer struct {
	messageCount    atomic.Int64             // 消息总数
	errorCount      atomic.Int64             // 错误总数
	processingTime  atomic.Int64             // 处理总时间(纳秒)
	subscriberCount map[string]*atomic.Int32 // 每个主题的订阅者数量
}

// NewMetricsTracer 创建新的指标追踪器
func NewMetricsTracer() *MetricsTracer {
	return &MetricsTracer{
		subscriberCount: make(map[string]*atomic.Int32),
	}
}

// 实现 EventTracer 接口

func (m *MetricsTracer) OnPublish(topic string, payload any, metadata PublishMetadata) {
	m.messageCount.Add(1)
}

func (m *MetricsTracer) OnSubscribe(topic string, handler any) {
	if _, ok := m.subscriberCount[topic]; !ok {
		counter := &atomic.Int32{}
		m.subscriberCount[topic] = counter
	}
	m.subscriberCount[topic].Add(1)
}

func (m *MetricsTracer) OnUnsubscribe(topic string, handler any) {
	if counter, ok := m.subscriberCount[topic]; ok {
		counter.Add(-1)
	}
}

func (m *MetricsTracer) OnError(topic string, err error) {
	if err == ErrNoSubscriber {
		m.errorCount.Add(1)
		m.messageCount.Add(1)
	}
}

func (m *MetricsTracer) OnComplete(topic string, metadata CompleteMetadata) {
	if metadata.ProcessingTime < time.Millisecond {
		metadata.ProcessingTime = time.Millisecond
	}
	m.processingTime.Add(metadata.ProcessingTime.Nanoseconds())
}

// GetMetrics 获取当前指标
func (m *MetricsTracer) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"message_count":    m.messageCount.Load(),
		"error_count":      m.errorCount.Load(),
		"processing_time":  time.Duration(m.processingTime.Load()),
		"subscriber_count": m.subscriberCount,
	}
}
