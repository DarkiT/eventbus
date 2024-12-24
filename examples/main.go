package main

import (
	"fmt"
	"log"
	"time"

	"github.com/darkit/eventbus"
)

// MyTracer 定义事件追踪器
type MyTracer struct{}

func (t *MyTracer) OnPublish(topic string, payload any, metadata eventbus.PublishMetadata) {
	log.Printf("[Tracer] Published: topic=%s, async=%v, queueSize=%d\n",
		topic, metadata.Async, metadata.QueueSize)
}

func (t *MyTracer) OnSubscribe(topic string, handler any) {
	log.Printf("[Tracer] Subscribed: topic=%s\n", topic)
}

func (t *MyTracer) OnUnsubscribe(topic string, handler any) {
	log.Printf("[Tracer] Unsubscribed: topic=%s\n", topic)
}

func (t *MyTracer) OnError(topic string, err error) {
	log.Printf("[Tracer] Error: topic=%s, error=%v\n", topic, err)
}

func (t *MyTracer) OnComplete(topic string, metadata eventbus.CompleteMetadata) {
	log.Printf("[Tracer] Completed: topic=%s, processingTime=%v\n",
		topic, metadata.ProcessingTime)
}

func (t *MyTracer) OnQueueFull(topic string, size int) {
	log.Printf("[Tracer] Queue Full: topic=%s, size=%d\n", topic, size)
}

func (t *MyTracer) OnSlowConsumer(topic string, latency time.Duration) {
	log.Printf("[Tracer] Slow Consumer: topic=%s, latency=%v\n", topic, latency)
}

// 定义事件过滤器
type MyFilter struct{}

func (f *MyFilter) Filter(topic string, payload any) bool {
	// 过滤掉包含 "test" 的主题
	return topic != "test"
}

// 定义中间件
type LoggingMiddleware struct{}

// Before 在事件处理前执行
func (m *LoggingMiddleware) Before(topic string, payload any) any {
	log.Printf("[Middleware] Before processing: topic=%s\n", topic)
	return payload // 返回可能修改过的 payload
}

// After 在事件处理后执行
func (m *LoggingMiddleware) After(topic string, payload any) {
	log.Printf("[Middleware] After processing: topic=%s\n", topic)
}

func main() {
	// 1. EventBus 基本使用
	fmt.Println("=== EventBus Basic Usage ===")

	// 创建带缓冲的事件总线
	bus := eventbus.NewBuffered(1024)
	defer bus.Close()

	// 设置追踪器
	bus.SetTracer(&MyTracer{})

	// 添加过滤器
	bus.AddFilter(&MyFilter{})

	// 添加中间件
	bus.Use(&LoggingMiddleware{})

	// 订阅处理器
	bus.Subscribe("user.created", func(topic string, payload any) {
		user := payload.(map[string]string)
		fmt.Printf("User created: %v\n", user)
	})

	// 带优先级的订阅
	bus.SubscribeWithPriority("user.created", func(topic string, payload any) {
		fmt.Println("High priority handler executed first")
	}, 1)

	// 发布事件
	bus.Publish("user.created", map[string]string{"name": "John"})
	bus.PublishSync("user.created", map[string]string{"name": "Jane"})
	bus.PublishWithTimeout("user.created", map[string]string{"name": "Bob"}, 5*time.Second)

	// 2. 泛型管道使用
	fmt.Println("\n=== Generic Pipe Usage ===")

	// 创建整数类型的管道
	pipe := eventbus.NewBufferedPipe[int](100)
	defer pipe.Close()

	// 设置超时
	pipe.SetTimeout(3 * time.Second)

	// 订阅处理器
	pipe.Subscribe(func(val int) {
		fmt.Printf("Received number: %d\n", val)
	})

	// 发布消息
	pipe.Publish(42)
	pipe.PublishSync(100)

	// 3. 全局单例使用
	fmt.Println("\n=== Singleton Usage ===")

	// 订阅全局事件
	eventbus.Subscribe("global.event", func(topic string, payload any) {
		fmt.Printf("Global event received: %v\n", payload)
	})

	// 发布全局事件
	eventbus.Publish("global.event", "Hello World")
	eventbus.PublishSync("global.event", "Hello Again")

	// 4. 错误处理示例
	fmt.Println("\n=== Error Handling ===")

	// 无效的处理器
	err := bus.Subscribe("topic", "not a function")
	if err == eventbus.ErrHandlerIsNotFunc {
		fmt.Println("Error: Handler must be a function")
	}

	// 发布到已关闭的通道
	pipe.Close()
	if err := pipe.Publish(200); err == eventbus.ErrChannelClosed {
		fmt.Println("Error: Channel is closed")
	}

	// 5. 并发示例
	fmt.Println("\n=== Concurrent Usage ===")

	done := make(chan bool)
	go func() {
		for i := 0; i < 5; i++ {
			bus.Publish("counter", i)
			time.Sleep(100 * time.Millisecond)
		}
		done <- true
	}()

	bus.Subscribe("counter", func(topic string, payload any) {
		fmt.Printf("Counter: %d\n", payload)
	})

	<-done

	// 等待一会儿让异步事件完成
	time.Sleep(time.Second)
}
