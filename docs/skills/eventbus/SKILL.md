---
name: eventbus
description: "Go 高性能事件总线库，核心能力：发布/订阅、MQTT 风格通配符路由（+ * #）、同步发布（全成功/任一成功/取首值）、可靠投递（自动重试/死信）、优雅关闭、泛型管道 Pipe[T]、优先级订阅、一次性订阅、事件过滤器（限流/阻断）、中间件、指标追踪、多租户分组、批量发布、全局单例包级函数，以及跨节点集群桥接（adapter 包）。适用于订阅、发布、可靠处理器、泛型管道、通配符匹配、集群桥接、性能调优等场景，提供经源码核验的 API 签名、代码模式、测试基准及诊断指南。"
---

# eventbus 开发助手

本技能是 `github.com/darkit/eventbus`（Go 1.26+，基于写时复制 COW + Trie 通配符的高性能事件总线）的开发指南。**所有签名、字段名、方法名均经源码核验**——这是它最大的价值：旧资料里常见的 `bus.Group()`、`SyncResult.FailedCount`、Pipe 处理器写成两参等都是**错的**，本技能已全部修正。

如果不确定某个 API 的精确签名，先查 [`references/api-reference.md`](./references/api-reference.md)，那是最终权威。

## 三秒选对 API

eventbus 有三套发布语义、两套订阅载体。先用这张表定位，避免用错导致性能差或语义错。

| 用户想要                              | 用什么                                    | 关键语义                                              |
| ------------------------------------- | ----------------------------------------- | ----------------------------------------------------- |
| 触发后不等结果，吞吐优先              | `bus.Publish(topic, payload)`             | 异步，成功入队即返回；队列满会等待/超时               |
| 等所有处理器跑完，但不关心返回值      | `bus.PublishSync(topic, payload)`         | 同步阻塞                                              |
| 多个步骤必须**全部成功**（事务/编排） | `bus.PublishSyncAll(topic, payload)`      | 任一失败 → `result.Success==false`，见 `FailureCount` |
| 多个通道**任一成功即可**（容错通知）  | `bus.PublishSyncAny(topic, payload)`      | 取第一个成功；`SuccessCount>=1` 即 `Success`          |
| 只想要**第一个成功返回值**            | `bus.PublishSyncAnyValue(topic, payload)` | 返回 `(any, error)`，比 SyncAny 更轻                  |
| 处理失败要自动重试并进死信            | `bus.SubscribeReliable(topic, handler)`   | 同步重试语义，默认 3 次指数 jitter 退避               |
| 发版/重启前排空队列                   | `bus.Shutdown(ctx)`                       | 拒绝新发布，等待已接收/已入队/处理中消息              |
| 类型安全 + 极致吞吐（同类型数据流）   | `eventbus.NewPipe[T](...)`                | 编译期类型检查，无 `any` 断言                         |
| 进程内逻辑 + 跨节点广播               | `adapter.NewBridge(bus, transport, ...)`  | best-effort，**不支持跨节点 SyncAll/SyncAny**         |
| 多租户/命名空间隔离                   | `bus.NewGroup("tenant-a")`                | 前缀自动拼接到 topic                                  |

**为什么这样分**：`Publish` 是 fire-and-forget，吞吐最高但调用方拿不到处理器结果；`PublishSync*` 系列要求处理器用 `SubscribeWithResponse`（返回 `(any, error)`）订阅，否则拿不到返回值。这是新人最常踩的点——想要"事务"却用普通 `Subscribe`，结果 `SyncResult.Results` 全是空。

## 处理器签名铁律（最常见的 panic 与静默失败来源）

eventbus 用反射严格校验处理器签名，**签名不对会直接返回错误**（不是 panic，但订阅静默失败）。两套载体签名体系不同，务必区分：

**EventBus / TopicGroup 普通处理器** —— 恰好两个参数，第一参必须是 `string`，**无返回值**：

```go
func(topic string, payload any)   // ✅ 正确
func(payload any)                 // ❌ ErrHandlerParamNum（参数数 ≠ 2）
func(topic int, payload any)      // ❌ ErrHandlerFirstParam（第一参非 string）
func(topic string, payload any) error  // ❌ ErrHandlerReturnNum（不能有返回值）
```

**EventBus 响应式处理器** —— 必须返回 `(any, error)`，否则 `PublishSyncAll/Any` 拿不到结果：

```go
bus.SubscribeWithResponse("order.create",
    func(topic string, payload any) (any, error) {      // 不带 ctx
        return result, nil
    })
bus.SubscribeWithResponseContext("order.create",
    func(ctx context.Context, topic string, payload any) (any, error) {  // 带 ctx
        return result, nil
    })
```

**EventBus 可靠处理器** —— 用于普通发布路径，返回 `error` 触发重试：

```go
bus.SubscribeReliable("order.created",
    func(ctx context.Context, topic string, payload any) error {
        return process(ctx, payload)
    })
```

**Pipe[T] 处理器** —— 单参，**没有 topic 参数**（Pipe 只有一个数据流，不需要 topic）：

```go
pipe.Subscribe(func(o Order) { ... })                              // 普通
pipe.SubscribeWithResponse(func(o Order) (any, error) { ... })     // 响应式
```

> 反射校验的完整错误清单见 [`references/api-reference.md`](./references/api-reference.md#错误类型)。

## 最小可用示例

下面六个场景覆盖 90% 的日常用法，代码均可直接编译运行。

**1. 基础发布订阅** —— 记住 `defer bus.Close()`，否则 goroutine 泄漏。

```go
bus := eventbus.New()            // 无参/传 0 = 无缓冲；传负数 = 默认缓冲（NumCPU*64）
defer bus.Close()

bus.Subscribe("user.created", func(topic string, payload any) {
    fmt.Printf("新用户: %v\n", payload)
})
bus.Publish("user.created", map[string]any{"id": 1, "name": "Alice"})
```

**2. 事务编排（全部成功才提交）** —— 用 `SubscribeWithResponse` + `PublishSyncAll`。

```go
bus.SubscribeWithResponse("order.process", checkInventory)
bus.SubscribeWithResponse("order.process", chargePayment)
bus.SubscribeWithResponse("order.process", arrangeShipping)

res, err := bus.PublishSyncAll("order.process", order)
if err != nil { return err }            // 总线级错误（已关闭/无效 topic）
if !res.Success {
    // res.FailureCount / res.SuccessCount / res.HandlerCount
    // res.Results[i].Error 拿到具体哪个处理器失败
    return rollback(res)                // 任一失败就回滚
}
```

**3. 容错通知（任一成功即可）** —— 邮件/短信/推送三路并发。

```go
res, err := bus.PublishSyncAny("notify.send", msg)
if res.Success {
    // res.SuccessCount >= 1 即成功；想直接拿返回值用 PublishSyncAnyValue
}

first, err := bus.PublishSyncAnyValue("notify.send", msg)  // 更轻：直接返回首个成功值
```

**4. 类型安全管道** —— 同类型高频数据流首选，避免 `any` 断言。

```go
pipe := eventbus.NewBufferedPipe[Order](1024)   // 带缓冲；NewPipe 无缓冲，NewBufferedPipeWithTimeout 可设超时
defer pipe.Close()

pipe.Subscribe(func(o Order) { handle(o) })     // 单参，类型即 Order
pipe.Publish(Order{ID: 1, Amount: 99.9})
```

**5. 通配符路由** —— MQTT 风格，分隔符 `.` 和 `/` 都支持。

```go
bus.Subscribe("sensor/+/temp", h)   // + 或 * 匹配单层：sensor/room1/temp ✓
bus.Subscribe("system/#", h)        // # 匹配多层（须在末尾）：system/cpu/high ✓
bus.Publish("sensor/room1/temp", 25.5)
```

**6. 可靠投递与优雅关闭** —— 失败自动重试，退出前排空。

```go
bus.SubscribeReliable("order.created", func(ctx context.Context, topic string, payload any) error {
    return processOrder(ctx, payload)
}, eventbus.WithMaxAttempts(5))

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := bus.Shutdown(ctx); err != nil {
    log.Printf("shutdown early: %v", err)
}
```

## 通配符与主题

- `+` 或 `*` —— 匹配**单层**：`order.+/done` 命中 `order.create/done`，不命中 `order.create.x/done`。
- `#` —— 匹配**多层**，且必须在**末尾**：`system/#` 命中 `system`、`system.cpu`、`system.cpu.high`。
- 分隔符 `.` 与 `/` 等价，内部统一标准化为 `.`（`TopicSeparators = []string{".", "/"}`）。混用 `order.create/paid` 会被规范化。
- 性能：精确匹配最快；`+`/`*` 次之；`#` 匹配范围大，热点 topic 慎用。详见 [`references/troubleshooting.md`](./references/troubleshooting.md#性能差)。

## 反模式（线上会炸，拦一下）

1. **同步发布里回调发布同一 topic → 死锁**。处理器内 `PublishSync` 自己订阅的 topic 会形成同步等待环。

   ```go
   bus.Subscribe("t", func(topic string, p any) { bus.PublishSync("t", p) }) // ❌ 死锁
   ```

   要么用 `Publish`（异步），要么换 topic。

2. **忘记 `defer bus.Close()`** → 后台 goroutine 与 channel 泄漏。每个 `New` / `NewPipe` 都要配 `Close`。

3. **`payload.(User)` 裸断言** → 类型不符即 panic。用 `v, ok := payload.(User)` 或改用 `Pipe[T]` 从源头消除。

4. **想要事务却用普通 `Subscribe`** → `PublishSyncAll` 的 `Results` 拿不到返回值，事务逻辑静默失效。必须 `SubscribeWithResponse`。

5. **跨节点用 `PublishSyncAll`** → adapter Bridge **不支持**跨节点 SyncAll/SyncAny（语义边界 v1）。跨节点只有 best-effort 异步 pub/sub，应用层需幂等。

6. **用 `Close` 期待排空队列** → `Close` 是立即关闭，平滑发版/重启用 `Shutdown(ctx)`。

## 深入某个主题时，读哪个文件

| 任务                                                        | 打开                                                               |
| ----------------------------------------------------------- | ------------------------------------------------------------------ |
| 查任意 API 的精确签名/字段/错误                             | [`references/api-reference.md`](./references/api-reference.md)     |
| 跨节点集群桥接（Bridge/Transport/Codec + 语义边界）         | [`references/adapter-bridge.md`](./references/adapter-bridge.md)   |
| 按场景找现成代码模式（过滤器/中间件/Tracer/分组/批量/单例） | [`references/patterns.md`](./references/patterns.md)               |
| 写测试、基准、竞态检测、Mock Tracer                         | [`references/testing.md`](./references/testing.md)                 |
| 排查死锁/泄漏/超时/panic/性能                               | [`references/troubleshooting.md`](./references/troubleshooting.md) |
| 拿一个能直接跑的完整示例                                    | [`assets/quickstart.go.txt`](./assets/quickstart.go.txt)           |

## 默认值速查

- `DefaultTimeout = 5 * time.Second` —— SyncAll/SyncAny 的默认超时；`bus.SetTimeout(d)` 动态调整。
- `New()` 无参 / `New(0)` = 无缓冲；`New(-1)` 等负数 = 缓冲 `runtime.NumCPU() * 64`。
- `SmartFilter` 默认限流窗口 `1 * time.Minute`；`BlockTopic` 会**连带阻断子层级**。
- `MetricsTracer` 默认慢消费阈值 `1 * time.Second`。
