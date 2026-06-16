# 代码模式速查（按场景）

按真实场景组织的现成代码片段，所有签名经源码核验，可直接复制改用。需要查精确签名时回到 [`api-reference.md`](./api-reference.md)。

## 1. 基础发布订阅

```go
bus := eventbus.New()       // 无缓冲；高吞吐用 eventbus.New(-1) 或指定正数缓冲
defer bus.Close()

if err := bus.Subscribe("user.created", func(topic string, payload any) {
    fmt.Printf("[%s] %v\n", topic, payload)
}); err != nil {
    log.Fatal(err)          // 签名校验失败会在此暴露，务必检查
}

_ = bus.Publish("user.created", map[string]any{"id": 1, "name": "Alice"})
```

## 2. 优先级订阅

数字越大越先执行；同优先级按订阅顺序稳定执行。

```go
_ = bus.SubscribeWithPriority("log", func(topic string, p any) {
    fmt.Println("高优先级先跑")
}, 10)
_ = bus.SubscribeWithPriority("log", func(topic string, p any) {
    fmt.Println("低优先级后跑")
}, 1)
_ = bus.PublishSync("log", "msg")   // 用 Sync 保证观察到的顺序
```

## 3. SubscribeOnce（触发即退订）

适合"首次匹配"场景，如初始化广播。

```go
_ = bus.SubscribeOnce("boot.ready", func(topic string, p any) {
    fmt.Println("只触发一次")
})
_ = bus.Publish("boot.ready", nil)
_ = bus.Publish("boot.ready", nil)   // 第二次无处理器
```

## 4. 事务编排（全部成功）

用 `SubscribeWithResponse` + `PublishSyncAll`。任一失败即整体失败，便于回滚。

```go
_ = bus.SubscribeWithResponse("order.submit", func(topic string, p any) (any, error) {
    return reserveStock(p)
})
_ = bus.SubscribeWithResponse("order.submit", func(topic string, p any) (any, error) {
    return charge(p)
})

res, err := bus.PublishSyncAll("order.submit", order)
if err != nil {
    return err                 // 总线级错误
}
if !res.Success {
    for _, h := range res.Results {
        if !h.Success {
            log.Printf("步骤失败: %v", h.Error)
        }
    }
    return compensate(res)     // 用 res.Results 决定补偿哪些
}
// res.Results[i].Result 拿到每个步骤的返回值
```

## 5. 容错通知（任一成功）

多通道并发，任一成功即可；只要首成功值用 `PublishSyncAnyValue`。

```go
_ = bus.SubscribeWithResponse("notify", sendEmail)
_ = bus.SubscribeWithResponse("notify", sendSMS)

res, _ := bus.PublishSyncAny("notify", msg)
if res.Success {
    log.Printf("%d/%d 通道成功", res.SuccessCount, res.HandlerCount)
}

// 更轻量：直接拿首个成功返回值
first, err := bus.PublishSyncAnyValue("notify", msg)
if err == nil {
    log.Printf("首个成功: %v", first)
}
```

## 6. 类型安全管道 Pipe[T]

同类型高频数据流首选，编译期类型检查，零 `any` 断言。

```go
type Sensor struct{ ID string; Value float64 }

pipe := eventbus.NewBufferedPipe[Sensor](1024)   // 带缓冲
defer pipe.Close()

_ = pipe.Subscribe(func(s Sensor) {            // 单参，类型即 Sensor
    process(s)
})
_ = pipe.Publish(Sensor{ID: "temp-1", Value: 25.5})

// 响应式 Pipe：拿返回值
cancel, _ := pipe.SubscribeWithResponse(func(s Sensor) (any, error) {
    return s.Value * 2, nil
})
defer cancel()
pres, _ := pipe.PublishSyncAll(Sensor{ID: "x", Value: 10})
```

> 闭包去重：同一闭包字面量重复 `Subscribe` 默认会被去重；需要分别注册时用 `SubscribeWithOptions(handler, eventbus.WithHandlerID("id-a"))`。

## 7. 通配符路由

```go
_ = bus.Subscribe("sensor/+/temp", func(topic string, p any) { /* 单层 */ })
_ = bus.Subscribe("system/#", func(topic string, p any) { /* 多层 */ })
_ = bus.Publish("sensor/room1/temp", 25.5)   // 命中第一条
_ = bus.Publish("system/cpu/high", 88)        // 命中第二条
```

## 8. SmartFilter 限流 + 阻断

```go
f := eventbus.NewSmartFilter()              // 默认窗口 1min
f.SetLimit("api.request", 100)              // 每窗口最多放行 100 次
f.BlockTopic("debug.verbose")               // 阻断该 topic 及子层级（debug.verbose.*）
f.StartCleanup(time.Minute)                 // 启动后台清理过期计数器
defer f.Stop()
bus.AddFilter(f)

// 函数式过滤器（一次性逻辑）
bus.AddFilter(eventbus.FilterFunc(func(topic string, p any) bool {
    return !strings.HasPrefix(topic, "internal.")  // 拦截 internal 前缀
}))
```

## 9. Middleware 统计 + 转换

```go
mw := eventbus.NewMiddleware()
mw.SetTransformer(func(topic string, payload any) any {
    if m, ok := payload.(map[string]any); ok {
        m["transformed_at"] = time.Now().Unix()  // 统一注入字段
    }
    return payload
})
bus.Use(mw)

// 查看每 topic 的 Count / TotalTime
for topic, stat := range mw.GetStats() {
    log.Printf("%s: count=%d total=%v", topic, stat.Count, stat.TotalTime)
}
```

## 10. MetricsTracer 指标追踪

```go
t := eventbus.NewMetricsTracer()   // 默认慢消费阈值 1s
bus.SetTracer(t)

m := t.GetMetrics()
log.Printf("messages=%v errors=%v", m["message_count"], m["error_count"])
// t.GetQueueMetrics()   每主题队列统计
// t.GetLatencyMetrics() 每主题延迟分桶

// 自定义 Tracer：实现 EventTracer 接口的 7 个方法即可
```

自定义 Tracer 最小实现：

```go
type logTracer struct{}
func (logTracer) OnPublish(topic string, payload any, m eventbus.PublishMetadata) {}
func (logTracer) OnSubscribe(topic string, handler any)                          {}
func (logTracer) OnUnsubscribe(topic string, handler any)                        {}
func (logTracer) OnError(topic string, err error)         { log.Printf("err %s: %v", topic, err) }
func (logTracer) OnComplete(topic string, m eventbus.CompleteMetadata)           {}
func (logTracer) OnQueueFull(topic string, size int)                              {}
func (logTracer) OnSlowConsumer(topic string, d time.Duration) { log.Printf("slow %s: %v", topic, d) }

bus.SetTracer(logTracer{})
```

## 11. TopicGroup 多租户

前缀自动拼接，命名空间隔离。

```go
tenantA := bus.NewGroup("tenantA")
tenantB := bus.NewGroup("tenantB")

_ = tenantA.Subscribe("order.created", func(topic string, p any) {})  // 实际订阅 tenantA.order.created
_ = tenantA.Publish("order.created", order)                            // 实际发布 tenantA.order.created

orders := tenantA.NewSubGroup("orders")   // 嵌套：tenantA.orders.*
_ = orders.Subscribe("created", h)

log.Println(tenantA.Prefix())             // "tenantA"
```

## 12. PublishBatch 批量发布

减少逐条开销，按 topic 分组投递。

```go
msgs := []eventbus.BatchMessage{
    {Topic: "order.created", Payload: order1},
    {Topic: "order.paid",    Payload: order1},
    {Topic: "order.created", Payload: order2},
}
res, err := bus.PublishBatchSync(msgs)   // 同步版本；异步用 PublishBatch
if err != nil {
    var be eventbus.BatchError
    if errors.As(err, &be) {
        log.Printf("批量失败 %d 条", be.Result.FailedCount)  // 注意是 FailedCount
    }
}
for _, r := range res.Results {
    if r.Err != nil {
        log.Printf("%s 失败: %v", r.Topic, r.Err)
    }
}
```

## 13. 全局单例（包级函数）

简单场景省去手动管理实例；测试用 `ResetSingleton()` 隔离。

```go
eventbus.Subscribe("evt", func(topic string, p any) {})  // 直接包级调用，操作 DefaultBus
eventbus.SubscribeReliable("evt.reliable", reliableHandler)
eventbus.Publish("evt", data)
eventbus.SetTracer(eventbus.NewMetricsTracer())
eventbus.NewGroup("g").Subscribe("x", h)
eventbus.Shutdown(context.Background()) // 平滑退出；测试后通常 ResetSingleton()
// 测试中：
eventbus.ResetSingleton()
```

## 14. SubscribeReliable 可靠投递

失败自动重试；重试期间同 topic 后续消息会排队等待，所以 handler 要控制耗时。

```go
_ = bus.SubscribeReliable("order.created",
    func(ctx context.Context, topic string, payload any) error {
        return processOrder(ctx, payload)
    },
    eventbus.WithMaxAttempts(5),
    eventbus.WithBackoff(eventbus.ExponentialBackoff(100*time.Millisecond, time.Second)),
    eventbus.WithRetryIf(func(err error) bool {
        return !errors.Is(err, ErrInvalidOrder) // 校验类错误不重试
    }),
    eventbus.WithDeadLetter(func(topic string, payload any, err error) {
        log.Printf("dead letter topic=%s err=%v", topic, err)
    }),
)
```

## 15. Shutdown 优雅关闭

`Shutdown(ctx)` 会先拒绝新发布，再等待已通过接收检查的发布完成入队/分发，并排空已入队/处理中消息；超时后退化为立即关闭。

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := bus.Shutdown(ctx); err != nil {
    log.Printf("shutdown early: %v", err)
}
// Close 是立即关闭；需要排空时不要用 Close 替代 Shutdown。
```

## 16. Context 超时控制

```go
bus.SetTimeout(3 * time.Second)   // 总线级默认超时

ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
defer cancel()
res, err := bus.PublishSyncAllWithContext(ctx, "order.submit", order)
if errors.Is(err, context.DeadlineExceeded) {
    log.Println("整体超时")
}
```

## 选型小结

| 场景 | 首选 |
|---|---|
| 单类型高频流 | `Pipe[T]` |
| 多步骤事务 | `SubscribeWithResponse` + `PublishSyncAll` |
| 容错/降级 | `PublishSyncAny` / `PublishSyncAnyValue` |
| 多租户 | `NewGroup` |
| 限流/熔断 | `SmartFilter` |
| 指标/排障 | `MetricsTracer` |
| 大批量投递 | `PublishBatch(Sync)` |
| 可靠投递/死信 | `SubscribeReliable` |
| 平滑发版/重启 | `Shutdown(ctx)` |
| 跨节点 | `adapter.NewBridge`（见 [adapter-bridge.md](./adapter-bridge.md)） |
