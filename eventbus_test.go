package eventbus

import (
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	assert.Equal(t, uint32(0), ch.handlers.Len())
	ch.close()
}

func Test_channelPublish(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)
	ch.subscribe(busHandlerOne)

	// 启动消息处理循环
	go ch.loop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < 100; i++ {
			err := ch.publish(i)
			assert.Nil(t, err)
		}
		wg.Done()
	}()
	wg.Wait()

	// 等待消息处理完成
	time.Sleep(time.Millisecond)
	ch.close()
	err := ch.publish(1)
	assert.Equal(t, ErrChannelClosed, err)
}

func Test_channelPublishSync(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)
	ch.subscribe(busHandlerOne)

	// 启动消息处理循环
	go ch.loop()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < 100; i++ {
			err := ch.publish(i)
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

func Test_NewBuffered(t *testing.T) {
	bus := NewBuffered(100)
	assert.NotNil(t, bus)
	assert.Equal(t, 100, bus.bufferSize)
	assert.NotNil(t, bus.channels)
	bus.Close()

	busZero := NewBuffered(0)
	assert.NotNil(t, busZero)
	assert.Equal(t, 1024, busZero.bufferSize)
	assert.NotNil(t, busZero.channels)
	busZero.Close()
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
			topic := topics[rand.Intn(len(topics))]
			bus.Publish(topic, "test")
		}
	})
}

func TestEventBus_Filters(t *testing.T) {
	bus := New()
	defer bus.Close()

	// 创建测试过滤器
	filter := &TestFilter{allowTopic: "allowed"}
	bus.AddFilter(filter)

	received := false
	handler := func(topic string, payload int) {
		received = true
	}

	bus.Subscribe("allowed", handler)
	bus.Subscribe("blocked", handler)

	// 允许的主题应该能收到消息
	bus.Publish("allowed", 1)
	time.Sleep(time.Millisecond)
	assert.True(t, received)

	// 重置接收标志
	received = false

	// 被过滤的主题不应该收到消息
	bus.Publish("blocked", 1)
	time.Sleep(time.Millisecond)
	assert.False(t, received)
}

func TestEventBus_Middleware(t *testing.T) {
	bus := New()
	defer bus.Close()

	middleware := &TestMiddleware{}
	bus.Use(middleware)

	received := 0
	handler := func(topic string, payload int) {
		received = payload
	}

	bus.Subscribe("test", handler)
	bus.Publish("test", 1)
	time.Sleep(time.Millisecond)

	// 中间件应该将值加倍
	assert.Equal(t, 2, received)
	assert.True(t, middleware.afterCalled)
}

func TestEventBus_Priority(t *testing.T) {
	bus := New()
	defer bus.Close()

	var order []int
	handler1 := func(topic string, payload int) {
		order = append(order, 1)
	}
	handler2 := func(topic string, payload int) {
		order = append(order, 2)
	}
	handler3 := func(topic string, payload int) {
		order = append(order, 3)
	}

	// 按不同优先级注册处理器
	bus.SubscribeWithPriority("test", handler1, 1)
	bus.SubscribeWithPriority("test", handler2, 3)
	bus.SubscribeWithPriority("test", handler3, 2)

	bus.Publish("test", 0)
	time.Sleep(time.Millisecond)

	// 验证处理顺序是否按优先级从高到低
	assert.Equal(t, []int{2, 3, 1}, order)
}

// 测试用的过滤器
type TestFilter struct {
	allowTopic string
}

func (f *TestFilter) Filter(topic string, payload any) bool {
	return topic == f.allowTopic
}

// 测试用的中间件
type TestMiddleware struct {
	afterCalled bool
}

func (m *TestMiddleware) Before(topic string, payload any) any {
	if val, ok := payload.(int); ok {
		return val * 2
	}
	return payload
}

func (m *TestMiddleware) After(topic string, payload any) {
	m.afterCalled = true
}
