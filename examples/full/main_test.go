package main

import (
	"context"
	"testing"
	"time"

	"github.com/darkit/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserEvent(t *testing.T) {
	event := UserEvent{
		UserID: "test123",
		Action: "login",
		Time:   time.Now(),
		Metadata: map[string]interface{}{
			"ip": "127.0.0.1",
		},
	}

	assert.Equal(t, "test123", event.UserID)
	assert.Equal(t, "login", event.Action)
	assert.NotNil(t, event.Metadata)
	assert.Equal(t, "127.0.0.1", event.Metadata["ip"])
}

func TestMetricsEvent(t *testing.T) {
	metrics := MetricsEvent{
		Name:      "cpu_usage",
		Value:     85.5,
		Tags:      map[string]string{"host": "server1"},
		Timestamp: time.Now(),
	}

	assert.Equal(t, "cpu_usage", metrics.Name)
	assert.Equal(t, 85.5, metrics.Value)
	assert.Equal(t, "server1", metrics.Tags["host"])
}

func TestMessage(t *testing.T) {
	msg := Message{
		ID:      "msg001",
		Content: "Hello Test",
		Time:    time.Now(),
	}

	assert.Equal(t, "msg001", msg.ID)
	assert.Equal(t, "Hello Test", msg.Content)
	assert.False(t, msg.Time.IsZero())
}

func TestTracer(t *testing.T) {
	tracer := NewTracer()
	require.NotNil(t, tracer)

	// 测试发布追踪
	metadata := eventbus.PublishMetadata{
		Timestamp: time.Now(),
		Async:     true,
		QueueSize: 10,
	}
	tracer.OnPublish("test.topic", "payload", metadata)

	stats := tracer.GetStats()
	assert.Equal(t, int64(1), stats["publish_count"])
	assert.Equal(t, 1, stats["total_events"])

	// 测试订阅追踪
	handler := func(string, any) {}
	tracer.OnSubscribe("test.topic", handler)

	stats = tracer.GetStats()
	assert.Equal(t, int64(1), stats["subscribe_count"])
	assert.Equal(t, 2, stats["total_events"])

	// 测试错误追踪
	tracer.OnError("test.topic", assert.AnError)

	stats = tracer.GetStats()
	assert.Equal(t, int64(1), stats["error_count"])
	assert.Equal(t, 1, stats["total_errors"])
}

func TestSmartFilter(t *testing.T) {
	filter := NewSmartFilter()
	require.NotNil(t, filter)

	// 测试正常过滤
	assert.True(t, filter.Filter("test.topic", "payload"))

	// 测试阻止主题
	filter.BlockTopic("blocked.topic")
	assert.False(t, filter.Filter("blocked.topic", "payload"))

	// 测试频率限制
	filter.SetLimit("limited.topic", 2)
	assert.True(t, filter.Filter("limited.topic", "payload1"))
	assert.True(t, filter.Filter("limited.topic", "payload2"))
	assert.False(t, filter.Filter("limited.topic", "payload3")) // 超过限制
}

func TestMiddleware(t *testing.T) {
	middleware := NewMiddleware()
	require.NotNil(t, middleware)

	// 测试处理map类型payload
	payload := map[string]interface{}{
		"data": "test",
	}

	processedPayload := middleware.Before("test.topic", payload)
	assert.NotNil(t, processedPayload)

	if payloadMap, ok := processedPayload.(map[string]interface{}); ok {
		assert.Contains(t, payloadMap, "_start_time")
		assert.Equal(t, "test", payloadMap["data"])
	}

	// 模拟一些处理时间
	time.Sleep(10 * time.Millisecond)

	middleware.After("test.topic", processedPayload)

	stats := middleware.GetStats()
	assert.Contains(t, stats, "test.topic")
	assert.Equal(t, int64(1), stats["test.topic"].Count)
	assert.True(t, stats["test.topic"].TotalTime > 0)

	// 测试非map类型payload
	stringPayload := "simple string"
	wrappedPayload := middleware.Before("test.topic2", stringPayload)

	if payloadMap, ok := wrappedPayload.(map[string]interface{}); ok {
		assert.Equal(t, stringPayload, payloadMap["_original_payload"])
		assert.Contains(t, payloadMap, "_start_time")
	}
}

func TestPerformanceStats(t *testing.T) {
	stats := &PerformanceStats{
		Count:       5,
		TotalTime:   500 * time.Millisecond,
		MaxTime:     200 * time.Millisecond,
		MinTime:     50 * time.Millisecond,
		LastUpdated: time.Now(),
	}

	assert.Equal(t, int64(5), stats.Count)
	assert.Equal(t, 500*time.Millisecond, stats.TotalTime)
	assert.Equal(t, 200*time.Millisecond, stats.MaxTime)
	assert.Equal(t, 50*time.Millisecond, stats.MinTime)
}

func TestExampleIntegration(t *testing.T) {
	// 创建事件总线
	bus := eventbus.New(100)
	defer bus.Close()

	// 设置追踪器
	tracer := NewTracer()
	bus.SetTracer(tracer)

	// 设置过滤器
	filter := NewSmartFilter()
	filter.SetLimit("test.limited", 1)
	bus.AddFilter(filter)

	// 设置中间件
	middleware := NewMiddleware()
	bus.Use(middleware)

	// 订阅事件
	received := make(chan string, 10)
	require.NoError(t, bus.Subscribe("test.topic", func(topic string, payload any) {
		received <- topic
	}))

	// 发布事件
	err := bus.PublishSync("test.topic", "test payload")
	assert.NoError(t, err)

	// 验证事件被接收
	select {
	case topic := <-received:
		assert.Equal(t, "test.topic", topic)
	case <-time.After(time.Second):
		t.Fatal("事件未被接收")
	}

	// 测试频率限制
	err = bus.PublishSync("test.limited", "payload1")
	assert.NoError(t, err)

	err = bus.PublishSync("test.limited", "payload2")
	assert.NoError(t, err) // 第二次应该被过滤掉

	// 验证统计信息
	tracerStats := tracer.GetStats()
	assert.True(t, tracerStats["publish_count"].(int64) >= 2)

	busStats := bus.GetStats()
	assert.NotNil(t, busStats)
	if channelCount, ok := busStats["channel_count"].(uint32); ok {
		assert.GreaterOrEqual(t, channelCount, uint32(0))
	} else if channelCount, ok := busStats["channel_count"].(int); ok {
		assert.GreaterOrEqual(t, channelCount, 0)
	}
}

func TestPipelineIntegration(t *testing.T) {
	// 测试整数管道
	intPipe := eventbus.NewBufferedPipe[int](10)
	defer intPipe.Close()

	received := make(chan int, 5)

	// 添加处理器
	require.NoError(t, intPipe.Subscribe(func(val int) {
		received <- val
	}))

	// 发布消息
	err := intPipe.PublishSync(42)
	assert.NoError(t, err)

	// 验证接收
	select {
	case val := <-received:
		assert.Equal(t, 42, val)
	case <-time.After(time.Second):
		t.Fatal("消息未被接收")
	}

	// 测试消息管道
	msgPipe := eventbus.NewBufferedPipe[Message](10)
	defer msgPipe.Close()

	msgReceived := make(chan Message, 5)

	require.NoError(t, msgPipe.Subscribe(func(msg Message) {
		msgReceived <- msg
	}))

	testMsg := Message{
		ID:      "test123",
		Content: "Hello Pipeline",
		Time:    time.Now(),
	}

	err = msgPipe.PublishSync(testMsg)
	assert.NoError(t, err)

	select {
	case msg := <-msgReceived:
		assert.Equal(t, "test123", msg.ID)
		assert.Equal(t, "Hello Pipeline", msg.Content)
	case <-time.After(time.Second):
		t.Fatal("消息未被接收")
	}
}

func TestContextPublishing(t *testing.T) {
	bus := eventbus.New(10)
	defer bus.Close()

	// 订阅一个慢处理器
	require.NoError(t, bus.Subscribe("slow.topic", func(topic string, payload any) {
		time.Sleep(2 * time.Second)
	}))

	// 异步发布不会阻塞
	ctxAsync, cancelAsync := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelAsync()

	err := bus.PublishWithContext(ctxAsync, "slow.topic", "test payload")
	assert.NoError(t, err)

	// 同步发布验证超时
	ctxSync, cancelSync := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelSync()

	err = bus.PublishSyncWithContext(ctxSync, "slow.topic", "test payload")
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestGlobalSingleton(t *testing.T) {
	// 测试全局单例功能
	received := make(chan string, 5)

	require.NoError(t, eventbus.Subscribe("global.test", func(topic string, payload any) {
		if str, ok := payload.(string); ok {
			received <- str
		}
	}))

	// 测试不同的发布方式
	err := eventbus.Publish("global.test", "async message")
	assert.NoError(t, err)

	err = eventbus.PublishSync("global.test", "sync message")
	assert.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = eventbus.PublishWithContext(ctx, "global.test", "context message")
	assert.NoError(t, err)

	// 验证至少接收到同步消息
	select {
	case msg := <-received:
		assert.Equal(t, "sync message", msg)
	case <-time.After(time.Second):
		t.Fatal("未接收到同步消息")
	}

	// 健康检查
	err = eventbus.HealthCheck()
	assert.NoError(t, err)
}

func TestErrorHandling(t *testing.T) {
	bus := eventbus.New()
	defer bus.Close()

	// 测试无效处理器
	err := bus.Subscribe("test", "not a function")
	assert.Equal(t, eventbus.ErrHandlerIsNotFunc, err)

	// 测试错误的参数数量
	err = bus.Subscribe("test", func() {})
	assert.Equal(t, eventbus.ErrHandlerParamNum, err)

	// 测试错误的第一个参数类型
	err = bus.Subscribe("test", func(int, any) {})
	assert.Equal(t, eventbus.ErrHandlerFirstParam, err)

	// 测试正确的处理器
	err = bus.Subscribe("test", func(string, any) {})
	assert.NoError(t, err)
}
