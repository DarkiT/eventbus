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
	handler := func(topic string, payload any) {}

	// 测试订阅计数
	tracer.OnSubscribe("test.topic", handler)
	tracer.OnSubscribe("test.topic", handler)

	metrics := tracer.GetMetrics()
	countMap := metrics["subscriber_count"].(map[string]int32)
	assert.Equal(t, int32(2), countMap["test.topic"])
}

func TestMetricsTracer_OnUnsubscribe(t *testing.T) {
	tracer := NewMetricsTracer()
	handler := func(topic string, payload any) {}

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
	assert.Equal(t, int64(0), metrics["message_count"])

	// 测试其他错误
	tracer.OnError("test.topic", errors.New("other error"))
	metrics = tracer.GetMetrics()
	assert.Equal(t, int64(2), metrics["error_count"])
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
	assert.InDelta(t, 29, metrics["test.topic"]["avg_size"], 1) // EMA: 0.1*100=10; 0.9*10+0.1*200=29
}

func TestMetricsTracer_OnSlowConsumer(t *testing.T) {
	tracer := NewMetricsTracer()

	// 测试慢消费者统计
	tracer.OnSlowConsumer("test.topic", time.Second)
	tracer.OnSlowConsumer("test.topic", 2*time.Second)

	metrics := tracer.GetLatencyMetrics()
	normal := metrics["normal"]
	assert.NotNil(t, normal["test.topic"])
	assert.Equal(t, 2*time.Second, normal["test.topic"]["max_latency"])
	assert.Equal(t, 1500*time.Millisecond, normal["test.topic"]["avg_latency"]) // (1s+2s)/2
	assert.Equal(t, int64(2), normal["test.topic"]["slow_count"])
	assert.Equal(t, int64(2), normal["test.topic"]["sample_count"])
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
	assert.Equal(t, int64(1), metrics["message_count"])
	assert.Equal(t, int64(1), metrics["error_count"])
	assert.Equal(t, time.Second, metrics["processing_time"])
}

func TestMetricsTracer_Concurrent(t *testing.T) {
	tracer := NewMetricsTracer()
	concurrency := 100

	// 并发测试
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
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
	// 同一 size 重复更新，EMA 应收敛接近 100，留出浮动空间
	assert.GreaterOrEqual(t, queueMetrics["test.topic"]["avg_size"], int64(80))

	latencyMetrics := tracer.GetLatencyMetrics()
	normal := latencyMetrics["normal"]
	assert.Equal(t, int64(concurrency), normal["test.topic"]["sample_count"])
}

func TestMetricsTracer_OnQueueFull_ConcurrentEMA(t *testing.T) {
	tracer := NewMetricsTracer()
	const (
		topic       = "ema.topic"
		concurrency = 500
		size        = 128
	)

	var wg sync.WaitGroup
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			tracer.OnQueueFull(topic, size)
		}()
	}
	wg.Wait()

	qm := tracer.GetQueueMetrics()[topic]
	assert.Equal(t, int64(concurrency), qm["full_count"])
	// 理论极限接近 size，允许少量数值抖动
	assert.InDelta(t, size, qm["avg_size"], 15)
}

// TestMetricsTracer_OnSlowConsumerResponse 测试响应式处理路径的慢消费统计
func TestMetricsTracer_OnSlowConsumerResponse(t *testing.T) {
	tracer := NewMetricsTracer()

	// 测试响应式慢消费者统计
	tracer.OnSlowConsumerResponse("test.topic", time.Second)
	tracer.OnSlowConsumerResponse("test.topic", 2*time.Second)

	metrics := tracer.GetLatencyMetrics()
	response := metrics["response"]
	assert.NotNil(t, response["test.topic"])
	assert.Equal(t, 2*time.Second, response["test.topic"]["max_latency"])
	assert.Equal(t, 1500*time.Millisecond, response["test.topic"]["avg_latency"]) // (1s+2s)/2
	assert.Equal(t, int64(2), response["test.topic"]["slow_count"])
	assert.Equal(t, int64(2), response["test.topic"]["sample_count"])
}

// TestMetricsTracer_OnSlowConsumerResponse_Concurrent 测试响应式慢消费统计的并发安全性
func TestMetricsTracer_OnSlowConsumerResponse_Concurrent(t *testing.T) {
	tracer := NewMetricsTracer()
	const concurrency = 100

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()
			tracer.OnSlowConsumerResponse("test.topic", time.Second)
		}()
	}

	wg.Wait()

	metrics := tracer.GetLatencyMetrics()
	response := metrics["response"]
	assert.Equal(t, int64(concurrency), response["test.topic"]["sample_count"])
	assert.Equal(t, int64(concurrency), response["test.topic"]["slow_count"])
	assert.Equal(t, time.Second, response["test.topic"]["max_latency"])
	assert.Equal(t, time.Second, response["test.topic"]["avg_latency"])
}

// TestMetricsTracer_OnSlowConsumerResponse_MultiTopic 测试多主题响应式慢消费统计
func TestMetricsTracer_OnSlowConsumerResponse_MultiTopic(t *testing.T) {
	tracer := NewMetricsTracer()

	// 不同主题的响应式慢消费
	tracer.OnSlowConsumerResponse("topic.a", time.Second)
	tracer.OnSlowConsumerResponse("topic.b", 2*time.Second)
	tracer.OnSlowConsumerResponse("topic.a", 3*time.Second)

	metrics := tracer.GetLatencyMetrics()
	response := metrics["response"]

	// 验证 topic.a
	assert.NotNil(t, response["topic.a"])
	assert.Equal(t, 3*time.Second, response["topic.a"]["max_latency"])
	assert.Equal(t, 2*time.Second, response["topic.a"]["avg_latency"]) // (1s+3s)/2
	assert.Equal(t, int64(2), response["topic.a"]["sample_count"])

	// 验证 topic.b
	assert.NotNil(t, response["topic.b"])
	assert.Equal(t, 2*time.Second, response["topic.b"]["max_latency"])
	assert.Equal(t, int64(1), response["topic.b"]["sample_count"])
}

// TestMetricsTracer_LatencyMetrics_Separation 测试普通和响应式延迟统计的分离
func TestMetricsTracer_LatencyMetrics_Separation(t *testing.T) {
	tracer := NewMetricsTracer()

	// 普通慢消费
	tracer.OnSlowConsumer("test.topic", time.Second)

	// 响应式慢消费
	tracer.OnSlowConsumerResponse("test.topic", 2*time.Second)

	metrics := tracer.GetLatencyMetrics()

	// 验证普通延迟统计
	normal := metrics["normal"]
	assert.NotNil(t, normal["test.topic"])
	assert.Equal(t, time.Second, normal["test.topic"]["max_latency"])
	assert.Equal(t, int64(1), normal["test.topic"]["sample_count"])

	// 验证响应式延迟统计
	response := metrics["response"]
	assert.NotNil(t, response["test.topic"])
	assert.Equal(t, 2*time.Second, response["test.topic"]["max_latency"])
	assert.Equal(t, int64(1), response["test.topic"]["sample_count"])
}
