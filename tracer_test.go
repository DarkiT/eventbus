package eventbus

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewMetricsTracer(t *testing.T) {
	tracer := NewMetricsTracer()
	assert.NotNil(t, tracer)
	assert.NotNil(t, tracer.subscriberCount)
	assert.NotNil(t, tracer.queueStats)
	assert.NotNil(t, tracer.latencyStats)
	assert.Equal(t, time.Second, tracer.slowThreshold)
}

func TestMetricsTracer_OnPublish(t *testing.T) {
	tracer := NewMetricsTracer()

	metadata := PublishMetadata{
		Timestamp:   time.Now(),
		Async:       true,
		QueueSize:   100,
		PublisherID: "test-publisher",
	}

	// 测试消息计数
	tracer.OnPublish("test.topic", "payload", metadata)
	metrics := tracer.GetMetrics()
	assert.Equal(t, int64(1), metrics["message_count"])
}

func TestMetricsTracer_OnSubscribe(t *testing.T) {
	tracer := NewMetricsTracer()
	handler := func(topic string, payload interface{}) {}

	// 测试订阅计数
	tracer.OnSubscribe("test.topic", handler)
	tracer.OnSubscribe("test.topic", handler)

	metrics := tracer.GetMetrics()
	countMap := metrics["subscriber_count"].(map[string]int32)
	assert.Equal(t, int32(2), countMap["test.topic"])
}

func TestMetricsTracer_OnUnsubscribe(t *testing.T) {
	tracer := NewMetricsTracer()
	handler := func(topic string, payload interface{}) {}

	// 先订阅再取消订阅
	tracer.OnSubscribe("test.topic", handler)
	tracer.OnUnsubscribe("test.topic", handler)

	metrics := tracer.GetMetrics()
	countMap := metrics["subscriber_count"].(map[string]int32)
	assert.Zero(t, countMap["test.topic"])
}

func TestMetricsTracer_OnError(t *testing.T) {
	tracer := NewMetricsTracer()

	// 测试错误计数
	tracer.OnError("test.topic", ErrNoSubscriber)
	metrics := tracer.GetMetrics()
	assert.Equal(t, int64(1), metrics["error_count"])
	assert.Equal(t, int64(1), metrics["message_count"])

	// 测试其他错误
	tracer.OnError("test.topic", errors.New("other error"))
	metrics = tracer.GetMetrics()
	assert.Equal(t, int64(1), metrics["error_count"])
}

func TestMetricsTracer_OnComplete(t *testing.T) {
	tracer := NewMetricsTracer()

	metadata := CompleteMetadata{
		StartTime:      time.Now(),
		EndTime:        time.Now().Add(time.Second),
		ProcessingTime: time.Second,
		HandlerCount:   2,
		Success:        true,
	}

	// 测试处理时间统计
	tracer.OnComplete("test.topic", metadata)
	metrics := tracer.GetMetrics()
	assert.Equal(t, time.Second, metrics["processing_time"])

	// 测试小于1ms的处理时间
	metadata.ProcessingTime = time.Microsecond
	tracer.OnComplete("test.topic", metadata)
	metrics = tracer.GetMetrics()
	assert.Equal(t, time.Millisecond+time.Second, metrics["processing_time"])
}

func TestMetricsTracer_OnQueueFull(t *testing.T) {
	tracer := NewMetricsTracer()

	// 测试队列满计数
	tracer.OnQueueFull("test.topic", 100)
	tracer.OnQueueFull("test.topic", 200)

	metrics := tracer.GetQueueMetrics()
	assert.NotNil(t, metrics["test.topic"])
	assert.Equal(t, int64(200), metrics["test.topic"]["max_size"])
	assert.Equal(t, int64(2), metrics["test.topic"]["full_count"])
	assert.Equal(t, int64(150), metrics["test.topic"]["avg_size"]) // (100+200)/2
	assert.Equal(t, int64(2), metrics["test.topic"]["sample_count"])
}

func TestMetricsTracer_OnSlowConsumer(t *testing.T) {
	tracer := NewMetricsTracer()

	// 测试慢消费者统计
	tracer.OnSlowConsumer("test.topic", time.Second)
	tracer.OnSlowConsumer("test.topic", 2*time.Second)

	metrics := tracer.GetLatencyMetrics()
	assert.NotNil(t, metrics["test.topic"])
	assert.Equal(t, 2*time.Second, metrics["test.topic"]["max_latency"])
	assert.Equal(t, 1500*time.Millisecond, metrics["test.topic"]["avg_latency"]) // (1s+2s)/2
	assert.Equal(t, int64(2), metrics["test.topic"]["slow_count"])
	assert.Equal(t, int64(2), metrics["test.topic"]["sample_count"])
}

func TestMetricsTracer_GetMetrics(t *testing.T) {
	tracer := NewMetricsTracer()

	// 生成一些测试数据
	tracer.OnPublish("test.topic", "payload", PublishMetadata{})
	tracer.OnError("test.topic", ErrNoSubscriber)
	tracer.OnComplete("test.topic", CompleteMetadata{ProcessingTime: time.Second})

	// 验证指标
	metrics := tracer.GetMetrics()
	assert.NotNil(t, metrics)
	assert.Equal(t, int64(2), metrics["message_count"])
	assert.Equal(t, int64(1), metrics["error_count"])
	assert.Equal(t, time.Second, metrics["processing_time"])
}

func TestMetricsTracer_Concurrent(t *testing.T) {
	tracer := NewMetricsTracer()
	concurrency := 100

	// 并发测试
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			tracer.OnPublish("test.topic", "payload", PublishMetadata{})
			tracer.OnQueueFull("test.topic", 100)
			tracer.OnSlowConsumer("test.topic", time.Second)
		}()
	}

	wg.Wait()

	// 验证并发操作后的指标
	metrics := tracer.GetMetrics()
	assert.Equal(t, int64(concurrency), metrics["message_count"])

	queueMetrics := tracer.GetQueueMetrics()
	assert.Equal(t, int64(concurrency), queueMetrics["test.topic"]["sample_count"])

	latencyMetrics := tracer.GetLatencyMetrics()
	assert.Equal(t, int64(concurrency), latencyMetrics["test.topic"]["sample_count"])
}
