package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func busHandlerOne(topic string, val int) {
}

func busHandlerTwo(topic string, val int) {
}

func Test_newChannel(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)
	assert.NotNil(t, ch.stopCh)
	assert.NotNil(t, ch.handlers)
	ch.close()

	bufferedCh := newChannel("test_topic", 100, bus)
	assert.NotNil(t, bufferedCh)
	assert.NotNil(t, bufferedCh.channel)
	assert.Equal(t, 100, cap(bufferedCh.channel))
	assert.Equal(t, "test_topic", bufferedCh.topic)
	assert.NotNil(t, bufferedCh.stopCh)
	assert.NotNil(t, bufferedCh.handlers)
	bufferedCh.close()

	bufferedZeroCh := newChannel("test_topic", 0, bus)
	assert.NotNil(t, bufferedZeroCh)
	assert.NotNil(t, bufferedZeroCh.channel)
	assert.Equal(t, "test_topic", bufferedZeroCh.topic)
	assert.NotNil(t, bufferedZeroCh.stopCh)
	assert.NotNil(t, bufferedZeroCh.handlers)
	bufferedZeroCh.close()
}

func Test_channelSubscribe(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)

	err := ch.subscribe(busHandlerOne)
	assert.Nil(t, err)
	ch.close()
	err = ch.subscribe(busHandlerTwo)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_channelUnsubscribe(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)

	err := ch.subscribe(busHandlerOne)
	assert.Nil(t, err)
	err = ch.unsubscribe(busHandlerOne)
	assert.Nil(t, err)

	err = ch.subscribe(busHandlerOne)
	assert.Nil(t, err)
	ch.close()
	err = ch.unsubscribe(busHandlerTwo)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_channelClose(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)

	err := ch.subscribe(busHandlerOne)
	assert.Nil(t, err)
	ch.close()
	assert.Equal(t, 0, len(ch.handlers))
	ch.close()
}

func Test_channelPublish(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)
	ch.subscribe(busHandlerOne)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < 100; i++ {
			err := ch.publishAsync(i)
			assert.Nil(t, err)
		}
		wg.Done()
	}()
	wg.Wait()

	// 等待消息处理完成
	time.Sleep(time.Millisecond)
	ch.close()
	err := ch.publishAsync(1)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_channelPublishSync(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)
	ch.subscribe(busHandlerOne)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < 100; i++ {
			err := ch.publishSync(i)
			assert.Nil(t, err)
		}
		wg.Done()
	}()
	wg.Wait()

	err := ch.publishSync(nil)
	assert.Nil(t, err)
	time.Sleep(time.Millisecond)
	ch.close()
	err = ch.publishSync(1)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_New(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)
	bus.Close()
}

func Test_NewWithBuffer(t *testing.T) {
	// 测试带缓冲的事件总线
	bus := New(100)
	assert.NotNil(t, bus)
	assert.Equal(t, 100, bus.bufferSize)
	assert.NotNil(t, bus.channels)
	bus.Close()

	// 测试传入0表示无缓冲
	busZero := New(0)
	assert.NotNil(t, busZero)
	assert.Equal(t, -1, busZero.bufferSize)
	assert.NotNil(t, busZero.channels)
	busZero.Close()

	// 测试传入负数使用默认缓冲大小
	busNegative := New(-1)
	assert.NotNil(t, busNegative)
	assert.Equal(t, defaultBufferSize, busNegative.bufferSize)
	assert.NotNil(t, busNegative.channels)
	busNegative.Close()

	// 测试不传参数默认无缓冲
	busDefault := New()
	assert.NotNil(t, busDefault)
	assert.Equal(t, -1, busDefault.bufferSize)
	assert.NotNil(t, busDefault.channels)
	busDefault.Close()
}

func Test_EventBusSubscribe(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)

	err := bus.Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)
	err = bus.Subscribe("testtopic", 1)
	assert.Equal(t, ErrHandlerIsNotFunc, err)

	err = bus.Subscribe("testtopic", func(topic string) error {
		return nil
	})
	assert.Equal(t, ErrHandlerParamNum, err)

	err = bus.Subscribe("testtopic", func(topic int, payload int) error {
		return nil
	})
	assert.Equal(t, ErrHandlerFirstParam, err)
	bus.Close()

	err = bus.Unsubscribe("testtopic", busHandlerTwo)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_EventBusUnsubscribe(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)

	err := bus.Unsubscribe("testtopic", busHandlerOne)
	assert.Equal(t, ErrNoSubscriber, err)

	err = bus.Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)

	err = bus.Unsubscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)
	bus.Close()

	err = bus.Unsubscribe("testtopic", busHandlerTwo)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_EventBusPublish(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)

	err := bus.Publish("testtopic", 1)
	assert.Nil(t, err)

	err = bus.Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < 100; i++ {
			err := bus.Publish("testtopic", i)
			assert.Nil(t, err)
		}
		wg.Done()
	}()
	wg.Wait()
	bus.Close()
}

func Test_EventBusPublishSync(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)

	err := bus.PublishSync("testtopic", 1)
	assert.Nil(t, err)

	err = bus.Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < 100; i++ {
			err := bus.PublishSync("testtopic", i)
			assert.Nil(t, err)
		}
		wg.Done()
	}()
	wg.Wait()
	bus.Close()
}

func Test_EventBusPrioritySubscription(t *testing.T) {
	bus := New()
	defer bus.Close()

	var order []int
	var mu sync.Mutex

	// 添加不同优先级的处理器
	bus.SubscribeWithPriority("test", func(topic string, payload any) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	}, 1)

	bus.SubscribeWithPriority("test", func(topic string, payload any) {
		mu.Lock()
		order = append(order, 10)
		mu.Unlock()
	}, 10)

	bus.SubscribeWithPriority("test", func(topic string, payload any) {
		mu.Lock()
		order = append(order, 5)
		mu.Unlock()
	}, 5)

	// 同步发布以确保顺序
	err := bus.PublishSync("test", "message")
	require.NoError(t, err)

	// 验证执行顺序（高优先级先执行）
	mu.Lock()
	expected := []int{10, 5, 1}
	assert.Equal(t, expected, order)
	mu.Unlock()
}

func Test_EventBusContextPublish(t *testing.T) {
	bus := New()
	defer bus.Close()

	received := make(chan bool, 1)
	bus.Subscribe("test", func(topic string, payload any) {
		time.Sleep(2 * time.Second) // 模拟慢处理
		received <- true
	})

	// 测试超时
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := bus.PublishWithContext(ctx, "test", "message")
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func Test_EventBusAsyncPublish(t *testing.T) {
	bus := New(10)
	defer bus.Close()

	var received int32
	bus.Subscribe("test", func(topic string, payload any) {
		atomic.AddInt32(&received, 1)
	})

	// 异步发布多条消息
	for i := 0; i < 5; i++ {
		err := bus.Publish("test", i)
		assert.NoError(t, err)
	}

	// 等待处理完成
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(5), atomic.LoadInt32(&received))
}

func BenchmarkEventBusPublish(b *testing.B) {
	bus := New()
	bus.Subscribe("testtopic", busHandlerOne)

	b.ResetTimer()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < b.N; i++ {
			bus.Publish("testtopic", i)
		}
		wg.Done()
	}()
	wg.Wait()
	bus.Close()
}

func BenchmarkEventBusPublishSync(b *testing.B) {
	bus := New()
	bus.Subscribe("testtopic", busHandlerOne)

	b.ResetTimer()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < b.N; i++ {
			bus.PublishSync("testtopic", i)
		}
		wg.Done()
	}()
	wg.Wait()
	bus.Close()
}

func BenchmarkHighConcurrency(b *testing.B) {
	bus := New()
	topics := []string{"topic1", "topic2", "topic3"}

	for _, topic := range topics {
		bus.Subscribe(topic, func(t string, p interface{}) {})
	}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			topic := topics[len(topics)%3] // 简化随机选择
			bus.Publish(topic, "test")
		}
	})
}

func TestEventBus_Filters(t *testing.T) {
	bus := New()
	filtered := false

	// 创建过滤器
	filter := &testFilter{
		filterFunc: func(topic string, payload any) bool {
			filtered = true
			return false // 返回 false 表示消息被过滤掉
		},
	}

	bus.AddFilter(filter)
	err := bus.Publish("test", 1)
	assert.NoError(t, err)
	assert.True(t, filtered)
}

func TestEventBus_Middleware(t *testing.T) {
	bus := New()
	count := 0

	// 创建中间件
	middleware := &testMiddleware{
		beforeFunc: func(topic string, payload any) any {
			count++
			return payload
		},
		afterFunc: func(topic string, payload any) {
			count++
		},
	}

	bus.Use(middleware)
	bus.Subscribe("test", func(topic string, payload int) {})
	bus.Publish("test", 1)

	time.Sleep(50 * time.Millisecond) // 等待异步处理
	assert.Equal(t, 2, count)
}

// 测试辅助类型
type testFilter struct {
	filterFunc func(topic string, payload any) bool
}

func (f *testFilter) Filter(topic string, payload any) bool {
	return f.filterFunc(topic, payload)
}

type testMiddleware struct {
	beforeFunc func(topic string, payload any) any
	afterFunc  func(topic string, payload any)
}

func (m *testMiddleware) Before(topic string, payload any) any {
	return m.beforeFunc(topic, payload)
}

func (m *testMiddleware) After(topic string, payload any) {
	m.afterFunc(topic, payload)
}

func Test_EventBusTracer(t *testing.T) {
	bus := New()

	// 创建一个测试追踪器
	tracer := &mockTracer{}
	bus.SetTracer(tracer)

	// 定义一个处理器变量以便重用
	handler := func(topic string, msg interface{}) {}

	// 测试订阅
	err := bus.Subscribe("test", handler)
	assert.Nil(t, err)

	// 测试发布
	err = bus.Publish("test", "message")
	assert.Nil(t, err)

	// 测试取消订阅 - 使用相同的处理器变量
	err = bus.Unsubscribe("test", handler)
	assert.Nil(t, err)
}

func Test_EventBusClose(t *testing.T) {
	bus := New()

	// 订阅一些处理器
	err := bus.Subscribe("test", func(topic string, msg interface{}) {})
	assert.Nil(t, err)

	// 发布一些消息
	err = bus.Publish("test", "message")
	assert.Nil(t, err)

	// 关闭事件总线
	bus.Close()

	// 测试关闭后的操作
	err = bus.Publish("test", "message")
	assert.Equal(t, ErrChannelClosed, err)

	err = bus.Subscribe("test", func(topic string, msg interface{}) {})
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_EventBusHealthCheck(t *testing.T) {
	bus := New()

	// 测试正常状态
	err := bus.HealthCheck()
	assert.Nil(t, err)

	// 关闭事件总线
	bus.Close()

	// 测试关闭后的状态
	err = bus.HealthCheck()
	assert.Equal(t, ErrChannelClosed, err)
}

// Mock 实现
type mockTracer struct{}

func (m *mockTracer) OnSubscribe(topic string, handler interface{})                     {}
func (m *mockTracer) OnUnsubscribe(topic string, handler interface{})                   {}
func (m *mockTracer) OnPublish(topic string, msg interface{}, metadata PublishMetadata) {}
func (m *mockTracer) OnError(topic string, err error)                                   {}
func (m *mockTracer) OnQueueFull(topic string, queueSize int)                           {}
func (m *mockTracer) OnSlowConsumer(topic string, latency time.Duration)                {}
func (m *mockTracer) OnComplete(topic string, metadata CompleteMetadata)                {}
