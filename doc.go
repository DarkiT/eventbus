// Package eventbus 提供高性能的事件总线库，基于优化的写时复制（Copy-on-Write）机制实现高并发性能。
//
// eventbus 支持事件发布/订阅、事件追踪、过滤器、中间件等企业级功能，
// 适用于需要松耦合通信的 Go 应用程序。
//
// 架构
//
// eventbus 采用发布-订阅模式，核心组件包括：
//
//   - EventBus: 事件总线实例，管理主题订阅和消息分发。
//   - channel: 每个主题对应一个 channel，负责处理器管理和消息分发。
//   - topicTrie: Trie 树索引，用于高效通配符匹配。
//   - cowMap: 写时复制 Map，实现无锁读取的订阅管理。
//
// 核心功能
//
//   - 高性能异步/同步发布: Publish、PublishSync 及 WithContext 版本。
//   - 灵活订阅: Subscribe、SubscribeOnce、SubscribeWithPriority 等。
//   - 响应式同步: PublishSyncAll、PublishSyncAny 支持处理器返回值。
//   - 通配符支持: 支持 `+`、`*`、`#` 三种通配符和混合分隔符（`.` 和 `/`）。
//   - 事件过滤器: EventFilter 接口，提供 SmartFilter 实现限流和主题阻断。
//   - 处理中间件: IMiddleware 接口，提供性能统计和负载转换。
//   - 优先级机制: 处理器按优先级排序，同优先级按订阅顺序稳定执行。
//   - 事件追踪: EventTracer 接口，监控发布、订阅、慢消费者等事件。
//   - 批量操作: PublishBatch 支持批量发布，减少锁争用。
//   - 分组管理: Group 支持层级化主题管理和命名空间隔离。
//   - 可靠投递: SubscribeReliable 支持失败自动重试（指数/jitter 退避）与死信回调。
//   - 优雅关闭: Shutdown 等待已接收发布并排空队列后再关闭，Close 立即关闭，二者职责分离。
//
// 快速开始
//
//	bus := eventbus.New(1024) // 创建事件总线，缓冲区大小 1024
//	defer bus.Close()
//
//	// 订阅事件
//	bus.Subscribe("user.created", func(topic string, payload any) {
//	    fmt.Printf("用户创建: %v\n", payload)
//	})
//
//	// 异步发布
//	bus.Publish("user.created", map[string]any{"name": "John"})
//
//	// 同步发布
//	err := bus.PublishSync("user.created", map[string]any{"name": "Jane"})
//
// 通配符订阅
//
//	// + 或 * 匹配单层: "sensor/+/temperature" 匹配 "sensor/room1/temperature"
//	// # 匹配多层: "system/#" 匹配 "system/cpu/high"
//	bus.Subscribe("sensor/+/temperature", handler)
//	bus.Subscribe("system/#", handler)
//
// 优先级订阅
//
//	// 数字越大优先级越高，同优先级按订阅顺序执行
//	bus.SubscribeWithPriority("order.created", handler, 10)
//	bus.SubscribeWithPriority("order.created", handler, 0)
//
// 响应式同步发布
//
//	// 订阅带返回值的处理器
//	bus.SubscribeWithResponse("user.get", func(topic string, payload any) (any, error) {
//	    return map[string]any{"id": 1, "name": "John"}, nil
//	})
//
//	// 同步等待所有处理器完成
//	result, err := bus.PublishSyncAllWithContext(context.Background(), "user.get", payload)
//
// 过滤器和中间件
//
//	// 添加智能过滤器（限流、阻断）
//	filter := eventbus.NewSmartFilter()
//	filter.SetLimit("order.created", 100) // 窗口内最多 100 次
//	filter.BlockTopic("system.debug")
//	bus.AddFilter(filter)
//
//	// 添加中间件（统计、转换）
//	middleware := eventbus.NewMiddleware()
//	bus.Use(middleware)
//
// 批量发布
//
//	msgs := []eventbus.BatchMessage{
//	    {Topic: "order.created", Payload: map[string]any{"id": "A-1"}},
//	    {Topic: "order.paid", Payload: map[string]any{"id": "A-1"}},
//	}
//	result, err := bus.PublishBatch(msgs)
//
// 分组管理
//
//	// 创建分组，实现命名空间隔离
//	group := bus.NewGroup("tenant-a")
//	group.Subscribe("order.created", handler)
//	group.Publish("order.created", payload)
//
// 可靠投递（重试与死信）
//
//	// 处理失败自动重试，零配置即用安全默认；耗尽后触发死信回调
//	bus.SubscribeReliable("order.created", func(ctx context.Context, topic string, payload any) error {
//	    return processOrder(ctx, payload)
//	})
//
//	// 自定义重试策略与死信
//	bus.SubscribeReliable("order.created", handler,
//	    eventbus.WithMaxAttempts(5),
//	    eventbus.WithBackoff(eventbus.ExponentialBackoff(100*time.Millisecond, time.Second)),
//	    eventbus.WithDeadLetter(func(topic string, payload any, err error) { log.Println(err) }),
//	)
//
// 优雅关闭
//
//	// Shutdown 等待已接收发布并排空已入队/处理中消息后再关闭；Close 立即关闭（语义不变）
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	if err := bus.Shutdown(ctx); err != nil {
//	    log.Printf("shutdown: %v", err)
//	}
//
// 事件追踪
//
//	// 实现 EventTracer 接口监控事件总线行为
//	bus.SetTracer(customTracer)
//
// 设计原则
//
//   - 高性能: COW 机制实现读操作零锁竞争。
//   - 类型安全: 编译时类型检查，避免运行时错误。
//   - 易用性: 简洁的 API 设计，快速上手。
//   - 可扩展: 过滤器、中间件、追踪器均可自定义。
//   - 并发安全: 原子操作、读写锁、竞态检测通过。
//
// 性能优化
//
//   - Trie 树通配符匹配性能提升约 100 倍。
//   - COW 机制避免读操作锁竞争。
//   - 原子操作减少锁使用。
//   - 对象池减少内存分配。
//
// 限制
//
//   - 处理器函数签名必须严格匹配（参数类型、数量、返回值）。
//   - 通配符匹配性能取决于 Trie 树实现。
//   - 批量发布不支持跨主题事务。
//
// 详见 README.md 了解详细使用指南和示例。
package eventbus
