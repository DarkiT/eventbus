package eventbus

import (
	"errors"
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
	// OnQueueFull 在队列满时调用
	OnQueueFull(topic string, size int)
	// OnSlowConsumer 在慢消费者时调用
	OnSlowConsumer(topic string, latency time.Duration)
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

// QueueStats 队列统计
type QueueStats struct {
	maxSize     atomic.Int64 // 最大队列大小
	fullCount   atomic.Int64 // 队列满次数
	avgSize     atomic.Int64 // 平均队列大小
	sampleCount atomic.Int64 // 采样次数
}

// LatencyStats 延迟统计
type LatencyStats struct {
	maxLatency   atomic.Int64 // 最大延迟
	totalLatency atomic.Int64 // 总延迟
	slowCount    atomic.Int64 // 慢消费次数
	sampleCount  atomic.Int64 // 采样次数
}

// MetricsTracer 实现基础的指标收集
type MetricsTracer struct {
	messageCount    atomic.Int64             // 消息总数
	errorCount      atomic.Int64             // 错误总数
	processingTime  atomic.Int64             // 处理总时间(纳秒)
	subscriberCount map[string]*atomic.Int32 // 每个主题的订阅者数量
	queueStats      map[string]*QueueStats   // 每个主题的队列统计
	latencyStats    map[string]*LatencyStats // 每个主题的延迟统计
	slowThreshold   time.Duration            // 慢消费阈值
}

// NewMetricsTracer 创建新的指标追踪器
func NewMetricsTracer() *MetricsTracer {
	return &MetricsTracer{
		subscriberCount: make(map[string]*atomic.Int32),
	}
}

// 实现 EventTracer 接口

// OnPublish 在消息发布时调用
func (m *MetricsTracer) OnPublish(topic string, payload any, metadata PublishMetadata) {
	m.messageCount.Add(1)
}

// OnSubscribe 在订阅时调用
func (m *MetricsTracer) OnSubscribe(topic string, handler any) {
	if _, ok := m.subscriberCount[topic]; !ok {
		counter := &atomic.Int32{}
		m.subscriberCount[topic] = counter
	}
	m.subscriberCount[topic].Add(1)
}

// OnUnsubscribe 在取消订阅时调用
func (m *MetricsTracer) OnUnsubscribe(topic string, handler any) {
	if counter, ok := m.subscriberCount[topic]; ok {
		counter.Add(-1)
	}
}

// OnError 在发生错误时调用
func (m *MetricsTracer) OnError(topic string, err error) {
	if errors.Is(err, ErrNoSubscriber) {
		m.errorCount.Add(1)
		m.messageCount.Add(1)
	}
}

// OnComplete 在消息处理完成时调用
func (m *MetricsTracer) OnComplete(topic string, metadata CompleteMetadata) {
	if metadata.ProcessingTime < time.Millisecond {
		metadata.ProcessingTime = time.Millisecond
	}
	m.processingTime.Add(metadata.ProcessingTime.Nanoseconds())
}

// OnQueueFull 在队列满时调用
func (m *MetricsTracer) OnQueueFull(topic string, size int) {
	stats, ok := m.queueStats[topic]
	if !ok {
		stats = &QueueStats{}
		m.queueStats[topic] = stats
	}

	// 更新统计信息
	stats.fullCount.Add(1)
	stats.sampleCount.Add(1)

	// 更新最大队列大小
	for {
		current := stats.maxSize.Load()
		if int64(size) <= current {
			break
		}
		if stats.maxSize.CompareAndSwap(current, int64(size)) {
			break
		}
	}

	// 更新平均队列大小
	avgSize := stats.avgSize.Load()
	sampleCount := stats.sampleCount.Load()
	newAvg := (avgSize*sampleCount + int64(size)) / (sampleCount + 1)
	stats.avgSize.Store(newAvg)
}

// OnSlowConsumer 在慢消费者时调用
func (m *MetricsTracer) OnSlowConsumer(topic string, latency time.Duration) {
	stats, ok := m.latencyStats[topic]
	if !ok {
		stats = &LatencyStats{}
		m.latencyStats[topic] = stats
	}

	// 更新统计信息
	stats.slowCount.Add(1)
	stats.sampleCount.Add(1)
	stats.totalLatency.Add(latency.Nanoseconds())

	// 更新最大延迟
	for {
		current := stats.maxLatency.Load()
		if latency.Nanoseconds() <= current {
			break
		}
		if stats.maxLatency.CompareAndSwap(current, latency.Nanoseconds()) {
			break
		}
	}
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

// GetQueueMetrics 获取队列指标
func (m *MetricsTracer) GetQueueMetrics() map[string]map[string]int64 {
	metrics := make(map[string]map[string]int64)
	for topic, stats := range m.queueStats {
		metrics[topic] = map[string]int64{
			"max_size":     stats.maxSize.Load(),
			"full_count":   stats.fullCount.Load(),
			"avg_size":     stats.avgSize.Load(),
			"sample_count": stats.sampleCount.Load(),
		}
	}
	return metrics
}

// GetLatencyMetrics 获取延迟指标
func (m *MetricsTracer) GetLatencyMetrics() map[string]map[string]interface{} {
	metrics := make(map[string]map[string]interface{})
	for topic, stats := range m.latencyStats {
		sampleCount := stats.sampleCount.Load()
		if sampleCount == 0 {
			continue
		}

		avgLatency := time.Duration(stats.totalLatency.Load() / sampleCount)
		metrics[topic] = map[string]interface{}{
			"max_latency":  time.Duration(stats.maxLatency.Load()),
			"avg_latency":  avgLatency,
			"slow_count":   stats.slowCount.Load(),
			"sample_count": sampleCount,
		}
	}
	return metrics
}
