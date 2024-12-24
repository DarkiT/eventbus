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
	assert.Equal(t, 512, busZero.bufferSize)
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
