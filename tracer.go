package eventbus

import (
	"sync"
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

// ResponseAwareTracer 可选接口，区分响应式处理的慢消费者
type ResponseAwareTracer interface {
	OnSlowConsumerResponse(topic string, latency time.Duration)
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
	maxSize   atomic.Int64 // 最大队列大小
	fullCount atomic.Int64 // 队列满次数
	avgSize   atomic.Int64 // 平均队列大小
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
	mu              sync.RWMutex             // 添加互斥锁保护 map
	messageCount    atomic.Int64             // 消息总数
	errorCount      atomic.Int64             // 错误总数
	processingTime  atomic.Int64             // 处理总时间(纳秒)
	subscriberCount map[string]*atomic.Int32 // 每个主题的订阅者数量
	queueStats      map[string]*QueueStats   // 每个主题的队列统计
	latencyStats    map[string]*LatencyStats // 普通处理延迟统计
	respLatency     map[string]*LatencyStats // 响应式处理延迟统计
	slowThreshold   time.Duration            // 慢消费阈值
}

// NewMetricsTracer 创建新的指标追踪器
func NewMetricsTracer() *MetricsTracer {
	return &MetricsTracer{
		subscriberCount: make(map[string]*atomic.Int32),
		queueStats:      make(map[string]*QueueStats),   // 初始化队列统计 map
		latencyStats:    make(map[string]*LatencyStats), // 初始化延迟统计 map
		respLatency:     make(map[string]*LatencyStats), // 初始化响应式延迟统计 map
		slowThreshold:   time.Second,                    // 设置默认的慢消费阈值
	}
}

// 实现 EventTracer 接口

// OnPublish 在消息发布时调用
func (m *MetricsTracer) OnPublish(topic string, payload any, metadata PublishMetadata) {
	m.messageCount.Add(1)
}

// OnSubscribe 在订阅时调用
func (m *MetricsTracer) OnSubscribe(topic string, handler any) {
	m.mu.Lock() // 获取写锁
	if _, ok := m.subscriberCount[topic]; !ok {
		counter := &atomic.Int32{}
		m.subscriberCount[topic] = counter
	}
	counter := m.subscriberCount[topic]
	m.mu.Unlock() // 释放写锁

	counter.Add(1)
}

// OnUnsubscribe 在取消订阅时调用
func (m *MetricsTracer) OnUnsubscribe(topic string, handler any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if counter, ok := m.subscriberCount[topic]; ok {
		newVal := counter.Add(-1)
		if newVal < 0 {
			counter.Store(0)
		}
		if newVal <= 0 {
			delete(m.subscriberCount, topic)
		}
	}
}

// OnError 在发生错误时调用
func (m *MetricsTracer) OnError(topic string, err error) {
	m.errorCount.Add(1)
	// ErrNoSubscriber 仍然算一条错误，但消息数已在 OnPublish 计入，无需重复加一
}

// OnComplete 在消息处理完成时调用。
//
// 处理耗时以纳秒累加；小于 1ms 的样本会被记为 1ms 下限，避免极快处理在累加中
// 失真。代价是高频小消息的平均处理时间会偏高，若需精确计时请在处理器内部自行埋点。
func (m *MetricsTracer) OnComplete(topic string, metadata CompleteMetadata) {
	if metadata.ProcessingTime < time.Millisecond {
		metadata.ProcessingTime = time.Millisecond
	}
	m.processingTime.Add(metadata.ProcessingTime.Nanoseconds())
}

// OnQueueFull 在队列满时调用
func (m *MetricsTracer) OnQueueFull(topic string, size int) {
	m.mu.Lock() // 获取写锁
	stats, ok := m.queueStats[topic]
	if !ok {
		stats = &QueueStats{}
		m.queueStats[topic] = stats
	}
	m.mu.Unlock() // 释放写锁

	// 更新统计信息
	stats.fullCount.Add(1)

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

	// 使用无锁 EMA 更新平均队列大小，α = 0.1
	for {
		oldAvg := stats.avgSize.Load()
		newAvg := int64(float64(oldAvg)*0.9 + float64(size)*0.1)
		if stats.avgSize.CompareAndSwap(oldAvg, newAvg) {
			break
		}
	}
}

// OnSlowConsumer 在慢消费者时调用
func (m *MetricsTracer) OnSlowConsumer(topic string, latency time.Duration) {
	m.mu.Lock() // 获取写锁
	stats, ok := m.latencyStats[topic]
	if !ok {
		stats = &LatencyStats{}
		m.latencyStats[topic] = stats
	}
	m.mu.Unlock() // 释放写锁

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

// OnSlowConsumerResponse 记录响应式处理路径的慢消费
func (m *MetricsTracer) OnSlowConsumerResponse(topic string, latency time.Duration) {
	m.mu.Lock()
	stats, ok := m.respLatency[topic]
	if !ok {
		stats = &LatencyStats{}
		m.respLatency[topic] = stats
	}
	m.mu.Unlock()

	stats.slowCount.Add(1)
	stats.sampleCount.Add(1)
	stats.totalLatency.Add(latency.Nanoseconds())

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
func (m *MetricsTracer) GetMetrics() map[string]any {
	metrics := map[string]any{
		"message_count":   m.messageCount.Load(),
		"error_count":     m.errorCount.Load(),
		"processing_time": time.Duration(m.processingTime.Load()),
	}

	m.mu.RLock()
	subscriberSnapshot := make(map[string]int32, len(m.subscriberCount))
	for topic, counter := range m.subscriberCount {
		subscriberSnapshot[topic] = counter.Load()
	}
	m.mu.RUnlock()
	metrics["subscriber_count"] = subscriberSnapshot

	return metrics
}

// GetQueueMetrics 获取队列指标
func (m *MetricsTracer) GetQueueMetrics() map[string]map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	metrics := make(map[string]map[string]int64, len(m.queueStats))
	for topic, stats := range m.queueStats {
		metrics[topic] = map[string]int64{
			"max_size":   stats.maxSize.Load(),
			"full_count": stats.fullCount.Load(),
			"avg_size":   stats.avgSize.Load(),
		}
	}
	return metrics
}

// GetLatencyMetrics 获取延迟指标
func (m *MetricsTracer) GetLatencyMetrics() map[string]map[string]map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	build := func(src map[string]*LatencyStats) map[string]map[string]any {
		res := make(map[string]map[string]any, len(src))
		for topic, stats := range src {
			sampleCount := stats.sampleCount.Load()
			if sampleCount == 0 {
				continue
			}
			avgLatency := time.Duration(stats.totalLatency.Load() / sampleCount)
			res[topic] = map[string]any{
				"max_latency":  time.Duration(stats.maxLatency.Load()),
				"avg_latency":  avgLatency,
				"slow_count":   stats.slowCount.Load(),
				"sample_count": sampleCount,
			}
		}
		return res
	}

	return map[string]map[string]map[string]any{
		"normal":   build(m.latencyStats),
		"response": build(m.respLatency),
	}
}
