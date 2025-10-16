package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/darkit/eventbus"
)

func must(err error) {
	if err != nil {
		log.Fatalf("操作失败: %v", err)
	}
}

// 用户事件结构
type UserEvent struct {
	UserID   string                 `json:"user_id"`
	Action   string                 `json:"action"`
	Time     time.Time              `json:"time"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// 系统指标事件
type MetricsEvent struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags"`
	Timestamp time.Time         `json:"timestamp"`
}

// 消息结构
type Message struct {
	ID      string    `json:"id"`
	Content string    `json:"content"`
	Time    time.Time `json:"time"`
}

// 增强型追踪器 - 结合了基础追踪和统计功能
type Tracer struct {
	mu      sync.RWMutex
	events  []string
	errors  []error
	metrics map[string]int64
}

func NewTracer() *Tracer {
	return &Tracer{
		events:  make([]string, 0),
		errors:  make([]error, 0),
		metrics: make(map[string]int64),
	}
}

func (t *Tracer) OnPublish(topic string, payload any, metadata eventbus.PublishMetadata) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, fmt.Sprintf("发布: %s (异步: %v, 队列大小: %d)",
		topic, metadata.Async, metadata.QueueSize))
	t.metrics["publish_count"]++
	// 只在非性能测试主题时输出日志
	if topic != "performance.test" {
		log.Printf("[追踪器] 发布事件: topic=%s, async=%v, queueSize=%d",
			topic, metadata.Async, metadata.QueueSize)
	}
}

func (t *Tracer) OnSubscribe(topic string, handler any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, fmt.Sprintf("订阅: %s", topic))
	t.metrics["subscribe_count"]++
	log.Printf("[追踪器] 订阅事件: topic=%s", topic)
}

func (t *Tracer) OnUnsubscribe(topic string, handler any) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, fmt.Sprintf("取消订阅: %s", topic))
	t.metrics["unsubscribe_count"]++
	log.Printf("[追踪器] 取消订阅: topic=%s", topic)
}

func (t *Tracer) OnError(topic string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.errors = append(t.errors, err)
	t.metrics["error_count"]++
	log.Printf("[追踪器] 错误 [%s]: %v", topic, err)
}

func (t *Tracer) OnComplete(topic string, metadata eventbus.CompleteMetadata) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.metrics["complete_count"]++
	log.Printf("[追踪器] 完成处理: topic=%s, 处理时间=%v", topic, metadata.ProcessingTime)
}

func (t *Tracer) OnQueueFull(topic string, size int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.metrics["queue_full_count"]++
	log.Printf("[追踪器] 队列满 [%s]: 大小 %d", topic, size)
}

func (t *Tracer) OnSlowConsumer(topic string, latency time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.metrics["slow_consumer_count"]++
	log.Printf("[追踪器] 慢消费者 [%s]: 延迟 %v", topic, latency)
}

func (t *Tracer) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	stats := make(map[string]interface{})
	for k, v := range t.metrics {
		stats[k] = v
	}
	stats["total_events"] = len(t.events)
	stats["total_errors"] = len(t.errors)
	return stats
}

// 智能过滤器 - 结合频率限制和内容过滤
type SmartFilter struct {
	mu        sync.RWMutex
	counters  map[string]int
	limits    map[string]int
	resetTime time.Time
	blocked   map[string]bool
}

func NewSmartFilter() *SmartFilter {
	return &SmartFilter{
		counters:  make(map[string]int),
		limits:    make(map[string]int),
		blocked:   make(map[string]bool),
		resetTime: time.Now().Add(time.Minute),
	}
}

func (f *SmartFilter) SetLimit(topic string, limit int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limits[topic] = limit
}

func (f *SmartFilter) BlockTopic(topic string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.blocked[topic] = true
}

func (f *SmartFilter) Filter(topic string, payload any) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	// 检查是否被阻止
	if f.blocked[topic] {
		log.Printf("[过滤器] 主题被阻止: %s", topic)
		return false
	}

	// 重置计数器
	if time.Now().After(f.resetTime) {
		f.counters = make(map[string]int)
		f.resetTime = time.Now().Add(time.Minute)
	}

	// 检查频率限制
	if limit, exists := f.limits[topic]; exists {
		f.counters[topic]++
		if f.counters[topic] > limit {
			log.Printf("[过滤器] 频率限制: 主题 %s 超过限制 %d", topic, limit)
			return false
		}
	}

	return true
}

// 增强型中间件 - 结合性能监控和日志记录
type Middleware struct {
	mu    sync.RWMutex
	stats map[string]*PerformanceStats
}

type PerformanceStats struct {
	Count       int64
	TotalTime   time.Duration
	MaxTime     time.Duration
	MinTime     time.Duration
	LastUpdated time.Time
}

func NewMiddleware() *Middleware {
	return &Middleware{
		stats: make(map[string]*PerformanceStats),
	}
}

func (m *Middleware) Before(topic string, payload any) any {
	// 只在非性能测试主题时输出日志
	if topic != "performance.test" {
		log.Printf("[中间件] 开始处理: topic=%s", topic)
	}

	// 在 payload 中添加开始时间
	if payloadMap, ok := payload.(map[string]interface{}); ok {
		payloadMap["_start_time"] = time.Now()
		return payloadMap
	}

	// 如果不是 map，创建一个包装
	return map[string]interface{}{
		"_original_payload": payload,
		"_start_time":       time.Now(),
	}
}

func (m *Middleware) After(topic string, payload any) {
	// 只在非性能测试主题时输出日志
	if topic != "performance.test" {
		log.Printf("[中间件] 完成处理: topic=%s", topic)
	}

	if payloadMap, ok := payload.(map[string]interface{}); ok {
		if startTime, exists := payloadMap["_start_time"]; exists {
			if start, ok := startTime.(time.Time); ok {
				duration := time.Since(start)
				m.updateStats(topic, duration)
				delete(payloadMap, "_start_time") // 清理临时数据
			}
		}
	}
}

func (m *Middleware) updateStats(topic string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats, exists := m.stats[topic]
	if !exists {
		stats = &PerformanceStats{
			MinTime: duration,
			MaxTime: duration,
		}
		m.stats[topic] = stats
	}

	stats.Count++
	stats.TotalTime += duration
	stats.LastUpdated = time.Now()

	if duration > stats.MaxTime {
		stats.MaxTime = duration
	}
	if duration < stats.MinTime {
		stats.MinTime = duration
	}
}

func (m *Middleware) GetStats() map[string]*PerformanceStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*PerformanceStats)
	for k, v := range m.stats {
		result[k] = &PerformanceStats{
			Count:       v.Count,
			TotalTime:   v.TotalTime,
			MaxTime:     v.MaxTime,
			MinTime:     v.MinTime,
			LastUpdated: v.LastUpdated,
		}
	}
	return result
}

func main() {
	fmt.Println("=== EventBus 完整功能演示 ===")

	// 1. 创建事件总线
	bus := eventbus.New(1024)
	defer bus.Close()

	// 2. 设置增强型追踪器（减少日志输出）
	tracer := NewTracer()
	bus.SetTracer(tracer)

	// 3. 设置智能过滤器
	smartFilter := NewSmartFilter()
	smartFilter.SetLimit("user.login", 10) // 每分钟最多10次登录事件
	smartFilter.BlockTopic("test")         // 阻止测试主题
	bus.AddFilter(smartFilter)

	// 4. 设置增强型中间件（减少日志输出）
	middleware := NewMiddleware()
	bus.Use(middleware)

	// 5. 演示优先级订阅
	fmt.Println("\n--- 优先级订阅演示 ---")

	// 低优先级处理器
	must(bus.SubscribeWithPriority("user.login", func(topic string, payload any) {
		fmt.Println("🔵 低优先级: 记录用户登录日志")
	}, 1))

	// 高优先级处理器
	must(bus.SubscribeWithPriority("user.login", func(topic string, payload any) {
		fmt.Println("🔴 高优先级: 验证用户权限")
	}, 10))

	// 中等优先级处理器
	must(bus.SubscribeWithPriority("user.login", func(topic string, payload any) {
		fmt.Println("🟡 中等优先级: 更新用户状态")
	}, 5))

	// 发布登录事件
	loginEvent := UserEvent{
		UserID: "user123",
		Action: "login",
		Time:   time.Now(),
		Metadata: map[string]interface{}{
			"ip":         "192.168.1.100",
			"user_agent": "Mozilla/5.0...",
		},
	}

	must(bus.PublishSync("user.login", map[string]interface{}{
		"event": loginEvent,
	}))

	// 5. 错误处理演示
	fmt.Println("\n--- 错误处理演示 ---")

	// 无效的处理器
	err := bus.Subscribe("invalid.handler", "not a function")
	if errors.Is(err, eventbus.ErrHandlerIsNotFunc) {
		fmt.Println("❌ 错误: 处理器必须是函数")
	}

	// 测试被阻止的主题
	must(bus.Subscribe("test", func(topic string, payload any) {
		fmt.Println("这不应该被执行")
	}))
	if err := bus.Publish("test", "blocked message"); err != nil {
		log.Printf("发布被阻止主题失败: %v", err)
	}

	// 6. 通配符和分组订阅演示
	fmt.Println("\n--- 通配符和分组订阅演示 ---")

	// 使用通配符订阅所有用户事件
	must(bus.Subscribe("user.*", func(topic string, payload any) {
		fmt.Printf("🔍 通配符用户事件: topic=%s\n", topic)
	}))

	// 使用通配符订阅所有系统事件
	must(bus.Subscribe("system.#", func(topic string, payload any) {
		fmt.Printf("🔍 通配符系统事件: topic=%s\n", topic)
	}))

	// 使用分组订阅
	must(bus.Subscribe("notifications/email/*", func(topic string, payload any) {
		fmt.Printf("📧 邮件通知: %v\n", payload)
	}))
	must(bus.Subscribe("notifications/sms/*", func(topic string, payload any) {
		fmt.Printf("📱 短信通知: %v\n", payload)
	}))

	// 发布不同类型的事件
	for topic, payload := range map[string]any{
		"user.logout":                    map[string]string{"username": "john"},
		"system.cpu.high":                85,
		"system.memory.low":              20,
		"notifications/email/welcome":    "欢迎使用我们的服务!",
		"notifications/sms/verification": "您的验证码是 123456",
	} {
		if err := bus.Publish(topic, payload); err != nil {
			log.Printf("发布事件失败: topic=%s err=%v", topic, err)
		}
	}

	// 7. 演示带上下文的发布
	fmt.Println("\n--- 带上下文发布演示 ---")

	must(bus.Subscribe("system.metrics", func(topic string, payload any) {
		time.Sleep(2 * time.Second) // 模拟慢处理器
		fmt.Println("📊 处理系统指标")
	}))

	// 带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = bus.PublishWithContext(ctx, "system.metrics", MetricsEvent{
		Name:      "cpu_usage",
		Value:     85.5,
		Tags:      map[string]string{"host": "server1"},
		Timestamp: time.Now(),
	})
	if err != nil {
		fmt.Printf("❌ 发布超时: %v\n", err)
	}

	// 8. 演示泛型管道功能
	fmt.Println("\n--- 泛型管道演示 ---")

	// 创建整数管道
	intPipe := eventbus.NewBufferedPipe[int](100)
	defer intPipe.Close()

	// 添加带优先级的处理器
	must(intPipe.SubscribeWithPriority(func(val int) {
		fmt.Printf("🔴 高优先级处理: %d\n", val)
	}, 10))

	must(intPipe.SubscribeWithPriority(func(val int) {
		fmt.Printf("🔵 低优先级处理: %d\n", val)
	}, 1))

	// 发布消息
	must(intPipe.PublishSync(42))

	// 创建结构体管道
	msgPipe := eventbus.NewBufferedPipe[Message](50)
	defer msgPipe.Close()

	must(msgPipe.Subscribe(func(msg Message) {
		fmt.Printf("📨 收到消息: ID=%s, 内容=%s\n", msg.ID, msg.Content)
	}))

	must(msgPipe.Publish(Message{
		ID:      "msg001",
		Content: "Hello, EventBus!",
		Time:    time.Now(),
	}))

	// 测试管道关闭错误处理
	msgPipe.Close()
	if err := msgPipe.Publish(Message{ID: "msg002", Content: "This should fail"}); errors.Is(err, eventbus.ErrChannelClosed) {
		fmt.Println("❌ 管道已关闭，无法发布消息")
	}

	// 9. 全局单例使用演示
	fmt.Println("\n--- 全局单例使用演示 ---")

	// 订阅全局事件
	must(eventbus.Subscribe("global.event", func(topic string, payload any) {
		fmt.Printf("🌍 全局事件: %v\n", payload)
	}))

	// 发布全局事件
	must(eventbus.Publish("global.event", "Hello World"))
	must(eventbus.PublishSync("global.event", "Hello Again"))

	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel2()
	must(eventbus.PublishWithContext(ctx2, "global.event", "Hello with Context"))

	// 10. 演示并发性能
	fmt.Println("\n--- 并发性能演示 ---")

	var wg sync.WaitGroup
	const numGoroutines = 100
	const messagesPerGoroutine = 100

	// 订阅处理器
	must(bus.Subscribe("performance.test", func(topic string, payload any) {
		// 模拟处理时间
		time.Sleep(time.Microsecond)
	}))

	start := time.Now()

	// 启动多个 goroutine 并发发布
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				if err := bus.Publish("performance.test", map[string]interface{}{
					"goroutine": id,
					"message":   j,
					"timestamp": time.Now(),
				}); err != nil {
					log.Printf("并发发布失败: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	totalMessages := numGoroutines * messagesPerGoroutine
	fmt.Printf("📈 性能测试完成: %d 条消息，耗时 %v，平均 %.2f 消息/秒\n",
		totalMessages, elapsed, float64(totalMessages)/elapsed.Seconds())

	// 11. 显示统计信息
	fmt.Println("\n--- 统计信息 ---")

	// 事件总线统计
	busStats := bus.GetStats()
	fmt.Printf("🚌 事件总线统计: %+v\n", busStats)

	// 追踪器统计
	tracerStats := tracer.GetStats()
	fmt.Printf("📊 追踪器统计: %+v\n", tracerStats)

	// 性能统计
	perfStats := middleware.GetStats()
	for topic, stats := range perfStats {
		if stats.Count > 0 {
			avgTime := stats.TotalTime / time.Duration(stats.Count)
			fmt.Printf("⚡ 主题 %s: 平均耗时 %v, 最大 %v, 最小 %v, 调用次数 %d\n",
				topic, avgTime, stats.MaxTime, stats.MinTime, stats.Count)
		}
	}

	// 管道统计
	pipeStats := intPipe.GetStats()
	fmt.Printf("🔧 整数管道统计: %+v\n", pipeStats)

	// 等待异步消息处理完成
	time.Sleep(time.Second)

	fmt.Println("\n=== 演示完成 ===")
}
