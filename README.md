# EventBus 事件总线库

[![Go Reference](https://pkg.go.dev/badge/github.com/darkit/eventbus.svg)](https://pkg.go.dev/github.com/darkit/eventbus)
[![Go Report Card](https://goreportcard.com/badge/github.com/darkit/eventbus)](https://goreportcard.com/report/github.com/darkit/eventbus)
[![Coverage Status](https://coveralls.io/repos/github/DarkiT/eventbus/badge.svg?branch=master)](https://coveralls.io/github/DarkiT/eventbus?branch=master)
[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

EventBus 是一个高性能的 Go 事件总线库，基于优化的写时复制(Copy-on-Write)机制实现高并发性能。提供事件发布/订阅、事件追踪、过滤器、中间件等企业级功能。

## ✨ 功能特性

- 🚀 **高性能异步/同步事件发布** - 优化的COW机制，读操作零锁竞争
- 📊 **完整事件追踪监控** - 生命周期追踪、性能指标、错误监控
- 🔍 **智能事件过滤器** - 频率限制、内容过滤、主题阻断、订阅级过滤
- 🔧 **灵活处理中间件** - 性能监控、日志记录、数据转换
- ⏱️ **超时控制和上下文支持** - Context传播、超时处理、取消机制
- 🔒 **线程安全设计** - 原子操作、读写锁、竞态检测通过
- 🎯 **类型安全泛型管道** - 强类型消息传递、编译时类型检查
- 🌟 **完整MQTT通配符支持** - 支持 `+`、`*`、`#` 三种通配符和混合分隔符
- 🌲 **Trie树高效匹配** - 通配符匹配性能提升约100倍
- 🎯 **响应式同步发布** - PublishSyncAll/PublishSyncAny 及 WithContext 版本，支持处理器返回值与上下文透传
- 📁 **分组和命名空间支持** - 层级化主题管理、嵌套子组、权限控制
- ⚡ **优先级订阅机制** - 处理器优先级排序、有序执行
- 📈 **实时性能统计** - 吞吐量、延迟、队列状态监控
- 🏥 **健康检查和故障恢复** - 系统状态监控、自动故障处理
- 🔎 **主题查询API** - GetTopics、GetSubscriberCount、HasSubscribers
- 🔁 **可靠投递与重试** - SubscribeReliable 支持失败自动重试（指数/jitter 退避）、可重试错误过滤与死信回调
- 🛑 **优雅关闭** - Shutdown 等待已接收发布并排空队列后再关闭，Close 立即关闭，适用于平滑发版/重启
- 🔌 **外部系统适配器** - Bridge / Transport / Codec 三层抽象，消息编解码、去重和上下文透传

## 📦 安装

```bash
go get github.com/darkit/eventbus
```

**系统要求**: Go 1.26+

## 🚀 快速开始

### 基础使用

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/darkit/eventbus"
)

func main() {
    // 创建事件总线
    bus := eventbus.New(1024) // 缓冲大小1024
    defer bus.Close()

    // 优先级订阅（数字越大优先级越高）
    bus.SubscribeWithPriority("user.created", func(topic string, payload any) {
        fmt.Printf("🔴 高优先级处理: %v\n", payload)
    }, 10)

    bus.Subscribe("user.created", func(topic string, payload any) {
        fmt.Printf("🔵 普通处理: %v\n", payload)
    })

    // 异步发布
    bus.Publish("user.created", map[string]string{"name": "John"})

    // 同步发布
    bus.PublishSync("user.created", map[string]string{"name": "Jane"})

    // 带上下文的发布
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    bus.PublishWithContext(ctx, "user.created", map[string]string{"name": "Alice"})
}
```

### 通配符订阅

```go
// + 中间单层通配符：匹配任意一个层级（MQTT标准）
bus.Subscribe("sensor/+/temperature", func(topic string, payload any) {
    fmt.Printf("温度传感器 %s: %v\n", topic, payload)
})

// * 单层通配符：匹配任意一个层级
bus.Subscribe("alert/*", func(topic string, payload any) {
    fmt.Printf("告警事件 %s: %v\n", topic, payload)
})

// # 多层通配符：匹配零个或多个层级（MQTT标准）
bus.Subscribe("system/#", func(topic string, payload any) {
    fmt.Printf("系统事件 %s: %v\n", topic, payload)
})

// 混合分隔符支持：. 和 / 可以混合使用
bus.Subscribe("notifications/email/*", func(topic string, payload any) {
    fmt.Printf("邮件通知: %v\n", payload)
})

// 发布消息（保持原始主题格式）
bus.Publish("sensor/room1/temperature", "25°C")     // 匹配第一个订阅
bus.Publish("alert/fire", "房间1发生火灾")             // 匹配第二个订阅  
bus.Publish("system/cpu/high", "CPU使用率过高")        // 匹配第三个订阅
bus.Publish("notifications/email/welcome", "欢迎邮件") // 匹配第四个订阅
```

### 一次性订阅

> **使用场景**: 等待特定事件完成初始化、接收一次性通知等场景

```go
// 基础一次性订阅 - 处理器执行一次后自动取消
bus.SubscribeOnce("app.initialized", func(topic string, payload any) {
    fmt.Println("应用初始化完成，开始加载模块")
    // 此处理器只会执行一次，无论发布多少次事件
})

// 一次性优先级订阅 - 确保在其他处理器之前执行
bus.SubscribeOnceWithPriority("config.loaded", func(topic string, payload any) {
    config := payload.(map[string]any)
    fmt.Printf("配置加载完成: %v\n", config)
}, 100)

// 并发安全 - 多个 goroutine 同时发布，处理器仍只执行一次
go bus.Publish("startup.complete", "ready")
go bus.Publish("startup.complete", "ready")
go bus.Publish("startup.complete", "ready")
// 处理器只执行一次

// 全局便捷方法
eventbus.SubscribeOnce("system.ready", func(topic string, payload any) {
    fmt.Println("系统就绪")
})
```

### 批量发布

> **适用场景**: 同一时刻发布多条消息，减少锁争用与队列检查开销

```go
msgs := []eventbus.BatchMessage{
    {Topic: "order.created", Payload: map[string]any{"id": "A-1"}},
    {Topic: "order.created", Payload: map[string]any{"id": "A-2"}},
    {Topic: "order.paid", Payload: map[string]any{"id": "A-1"}},
}

// 异步批量发布
result, err := bus.PublishBatch(msgs)
if err != nil {
    fmt.Printf("批量发布失败: %v\n", err)
}
fmt.Printf("成功: %d, 失败: %d\n", result.SuccessCount, result.FailedCount)

// 同步批量发布
syncResult, err := bus.PublishBatchSync(msgs)
if err != nil {
    fmt.Printf("批量同步发布失败: %v\n", err)
}
fmt.Printf("同步成功: %d, 失败: %d\n", syncResult.SuccessCount, syncResult.FailedCount)
```

### 响应式同步发布

> **类型安全**: EventBus 引入了具体的函数类型定义，提供编译时类型检查
> - `ResponseHandler`: `func(topic string, payload any) (any, error)`
> - `ResponseHandlerWithContext`: `func(ctx context.Context, topic string, payload any) (any, error)`

```go
// 订阅支持返回值的处理器（不带context）
bus.SubscribeWithResponse("order/process", func(topic string, payload any) (any, error) {
    order := payload.(map[string]any)
    // 处理订单逻辑
    if order["amount"].(float64) > 1000 {
        return nil, errors.New("金额超限")
    }
    return map[string]any{"status": "success", "order_id": order["id"]}, nil
})

// 订阅支持context的响应式处理器
bus.SubscribeWithResponseContext("order/validate", func(ctx context.Context, topic string, payload any) (any, error) {
    // 可以使用context进行超时控制或取消
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
        // 执行验证逻辑
        return map[string]any{"valid": true}, nil
    }
})

bus.SubscribeWithResponse("order/process", func(topic string, payload any) (any, error) {
    // 库存检查
    return map[string]any{"inventory": "sufficient"}, nil
})

// PublishSyncAll: 所有处理器必须成功才算成功
result, err := bus.PublishSyncAll("order/process", map[string]any{
    "id": "ORDER-001", 
    "amount": 299.99,
})

if err != nil {
    log.Printf("发布超时: %v", err)
} else if result.Success {
    fmt.Printf("✅ 订单处理成功! 耗时: %v\n", result.TotalTime)
    fmt.Printf("📊 统计: %d/%d 处理器成功\n", result.SuccessCount, result.HandlerCount)
    
    // 查看处理器返回值
    for _, handlerResult := range result.Results {
        if handlerResult.Success {
            fmt.Printf("处理器响应: %v (耗时: %v)\n", 
                handlerResult.Result, handlerResult.Duration)
        }
    }
} else {
    fmt.Printf("❌ 订单处理失败: %d/%d 处理器成功\n", 
        result.SuccessCount, result.HandlerCount)
    
    // 查看失败原因
    for _, handlerResult := range result.Results {
        if !handlerResult.Success {
            fmt.Printf("处理器失败: %v\n", handlerResult.Error)
        }
    }
}

// PublishSyncAny: 任一处理器成功即算成功
result, err = bus.PublishSyncAny("notification/send", map[string]any{
    "recipient": "user@example.com",
    "message": "订单确认",
})

if result.Success {
    fmt.Printf("✅ 通知发送成功! 耗时: %v\n", result.TotalTime)
} else {
    fmt.Printf("❌ 所有通知渠道都失败了\n")
}
```

## 🏗️ 核心组件

### EventBus - 事件总线

```go
// 创建选项
bus := eventbus.New()                    // 无缓冲，实时性最高
bus := eventbus.New(1024)               // 指定缓冲，固定大小
bus := eventbus.New(-1)                 // 智能缓冲（CPU核心数*64，推荐）

// 订阅管理
bus.Subscribe("topic", handler)                           // 普通订阅
bus.SubscribeWithPriority("topic", handler, priority)     // 优先级订阅
bus.SubscribeOnce("topic", handler)                       // 一次性订阅（执行一次后自动取消）
bus.SubscribeOnceWithPriority("topic", handler, priority) // 一次性优先级订阅
bus.SubscribeWithResponse("topic", responseHandler)       // 响应式订阅（不带context）
bus.SubscribeWithResponseContext("topic", handler)        // 响应式订阅（带context）
bus.SubscribeReliable("topic", reliableHandler)           // 可靠订阅（自动重试与死信）
bus.SubscribeWithFilter("topic", handler, filter)         // 带过滤器订阅
bus.SubscribeWithFilterAndPriority("topic", handler, filter, priority) // 带过滤器和优先级订阅
bus.Unsubscribe("topic", handler)                        // 取消订阅
bus.UnsubscribeAll("topic")                              // 取消所有订阅

// 发布选项
bus.Publish("topic", payload)                            // 异步发布
bus.PublishSync("topic", payload)                        // 同步发布
bus.PublishWithContext(ctx, "topic", payload)            // 带上下文异步发布
bus.PublishSyncWithContext(ctx, "topic", payload)        // 带上下文同步发布
bus.PublishAsyncWithContext(ctx, "topic", payload)       // 显式异步（等同 PublishWithContext）
// 说明：若异步发布时缓冲已满，Publish 会阻塞等待直至 DefaultTimeout（默认 5 秒）
//       或 ctx 截止时间到来，也可响应取消信号；成功写入后立即返回。

// 响应式发布
result, err := bus.PublishSyncAll("topic", payload)                 // 所有处理器成功才算成功（默认 5 秒超时）
result, err := bus.PublishSyncAny("topic", payload)                 // 任一处理器成功即算成功，返回完整 SyncResult（默认 5 秒超时）
value, err  := bus.PublishSyncAnyValue("topic", payload)            // 返回首个成功处理器的结果，适合真正低延迟场景
result, err := bus.PublishSyncAllWithContext(ctx, "topic", payload) // 继承调用方 ctx，必要时自动补充默认超时
result, err := bus.PublishSyncAnyWithContext(ctx, "topic", payload) // 任一处理器成功后 best-effort 取消其他处理器，并等待结果收敛
value, err  := bus.PublishSyncAnyValueWithContext(ctx, "topic", payload) // 返回首个成功结果，并立即返回给调用方

// 提示：当 ctx 未设置截止时间时，WithContext 变体会自动叠加默认超时；
// 如果需要自定义时限，可在调用前通过 context.WithTimeout/WithDeadline 设置专属超时。

// 系统管理
stats := bus.GetStats()                                   // 获取统计信息
err := bus.HealthCheck()                                  // 健康检查
topics := bus.GetTopics()                                 // 获取所有主题
count, _ := bus.GetSubscriberCount("topic")               // 获取订阅者数量
hasSubscribers := bus.HasSubscribers("topic")             // 检查是否有订阅者
bus.SetTimeout(10 * time.Second)                          // 设置超时时间
bus.Shutdown(ctx)                                         // 优雅排空后关闭
bus.Close()                                              // 关闭总线
```

### 泛型管道 (Pipe) - 类型安全

> **类型安全**: Pipe 提供强类型的响应式处理器，编译时类型检查
> - `PipeResponseHandler[T]`: `func(payload T) (any, error)`
> - `PipeResponseHandlerWithContext[T]`: `func(ctx context.Context, payload T) (any, error)`

```go
// 创建类型安全的管道
intPipe := eventbus.NewPipe[int]()                         // 无缓冲
msgPipe := eventbus.NewBufferedPipe[Message](100)          // 带缓冲
customPipe := eventbus.NewBufferedPipeWithTimeout[int](10, 3*time.Second) // 自定义超时

// 普通订阅处理
intPipe.SubscribeWithPriority(func(val int) {
    fmt.Printf("高优先级处理: %d\n", val)
}, 10)

intPipe.Subscribe(func(val int) {
    fmt.Printf("普通处理: %d\n", val)
})

// 使用选项订阅（支持自定义 ID，用于闭包去重）
intPipe.SubscribeWithOptions(func(val int) {
    fmt.Printf("带自定义ID的处理: %d\n", val)
}, eventbus.WithHandlerID("my-handler"), eventbus.WithPriority(5))

// 通过自定义 ID 取消订阅
intPipe.UnsubscribeByID("my-handler")

// 一次性订阅 - 处理器执行一次后自动取消
intPipe.SubscribeOnce(func(val int) {
    fmt.Printf("一次性处理: %d\n", val)
    // 只会执行一次，即使多次发布
})

// 一次性优先级订阅
intPipe.SubscribeOnceWithPriority(func(val int) {
    fmt.Printf("高优先级一次性处理: %d\n", val)
}, 10)

// 响应式订阅（不带 Context）
cancelResponse, err := intPipe.SubscribeWithResponse(func(val int) (any, error) {
    if val < 0 {
        return nil, errors.New("负数不被支持")
    }
    return val * 2, nil // 返回处理结果
})

// 响应式订阅（带 Context 支持，支持超时和取消）
cancelResponseCtx, err := intPipe.SubscribeWithResponseContextHandle(func(ctx context.Context, val int) (any, error) {
    select {
    case <-ctx.Done():
        return nil, ctx.Err()  // 响应超时或取消
    default:
        if val < 0 {
            return nil, errors.New("负数不被支持")
        }
        return val * 2, nil
    }
})

// 发布消息
intPipe.Publish(42)                             // 异步
intPipe.PublishSync(42)                         // 同步
intPipe.PublishWithContext(ctx, 42)             // 带上下文

// 响应式发布
result, err := intPipe.PublishSyncAll(42)       // 所有处理器成功才算成功
if err != nil {
    log.Printf("发布超时: %v", err)
} else if result.Success {
    fmt.Printf("✅ 处理成功! 耗时: %v\n", result.TotalTime)
    for _, handlerResult := range result.Results {
        if handlerResult.Success {
            fmt.Printf("处理器返回: %v\n", handlerResult.Result)
        }
    }
}

result, err = intPipe.PublishSyncAny(42)        // 任一处理器成功即算成功

// 带 Context 的响应式发布（支持自定义超时和取消）
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()

result, err = intPipe.PublishSyncAllResultWithContext(ctx, 42)  // 所有处理器成功，继承ctx超时
if err != nil {
    if errors.Is(err, eventbus.ErrPublishTimeout) || errors.Is(err, context.DeadlineExceeded) {
        log.Printf("发布超时: %v", err)
    }
}

result, err = intPipe.PublishSyncAnyResultWithContext(ctx, 42)  // 任一成功后返回完整结果

// 便捷包装：保留更轻量的返回形状，适合上层只关心结果值
responseMap, err := intPipe.PublishSyncAllWithContext(ctx, 42)  // map[handlerID]result
value, err := intPipe.PublishSyncAnyWithContext(ctx, 42)        // 首个成功结果

// 注意：PublishSyncAll/PublishSyncAny 使用默认5秒超时
// WithContext 变体会继承调用方的 ctx，若 ctx 未设置超时会自动补充默认超时

// 取消订阅
cancelResponse()                                // 取消响应式订阅
cancelResponseCtx()                             // 取消带 Context 的响应式订阅

// 管理
stats := intPipe.GetStats()                     // 统计信息
intPipe.Close()                                // 关闭管道
```

### 全局单例 - 便捷访问

```go
import "github.com/darkit/eventbus"

// 直接使用全局实例
eventbus.Subscribe("global.event", handler)
eventbus.SubscribeReliable("global.reliable", reliableHandler)
eventbus.Publish("global.event", payload)
eventbus.PublishSync("global.event", payload)
eventbus.PublishWithContext(ctx, "global.event", payload)
eventbus.PublishSyncWithContext(ctx, "global.event", payload)

// 系统管理
eventbus.HealthCheck()              // 健康检查
eventbus.Shutdown(ctx)              // 优雅关闭全局实例
eventbus.Close()                    // 关闭全局实例
```

## 🔧 高级功能

### 事件追踪器 - 监控和调试

```go
type EventTracer interface {
    OnPublish(topic string, payload any, metadata PublishMetadata)
    OnSubscribe(topic string, handler any)
    OnUnsubscribe(topic string, handler any)
    OnError(topic string, err error)
    OnComplete(topic string, metadata CompleteMetadata)
    OnQueueFull(topic string, size int)
    OnSlowConsumer(topic string, latency time.Duration)
}

// 使用内置的指标追踪器
tracer := eventbus.NewMetricsTracer()
bus.SetTracer(tracer)

// 获取指标
metrics := tracer.GetMetrics()
fmt.Printf("发布次数: %d\n", metrics["message_count"])
fmt.Printf("错误次数: %d\n", metrics["error_count"])
```

### 智能过滤器 - 流量控制

```go
filter := eventbus.NewSmartFilter()

filter.SetLimit("user.login", 100)        // 每分钟最多 100 次登录事件
filter.SetWindow(30 * time.Second)         // 自定义限流窗口（默认 1 分钟）
filter.BlockTopic("internal.test")        // 阻断测试主题及其子主题

// 启动后台清理协程，定期清理过期计数器（避免内存泄漏）
filter.StartCleanup(5 * time.Minute)
defer filter.Stop()                        // 程序退出时停止清理

bus.AddFilter(filter)                      // 注册智能过滤器

// 动态调整：
filter.UnblockTopic("internal.test")      // 解除阻断
filter.SetLimit("user.login", 0)          // 移除针对 user.login 的限流

// 自定义过滤器：直接使用 FilterFunc 包装函数
bus.AddFilter(eventbus.FilterFunc(func(topic string, payload any) bool {
    return !strings.Contains(topic, "spam")
}))
```

### 订阅级过滤器

```go
// 为特定订阅添加过滤器，只有通过过滤的消息才会触发处理器
filter := eventbus.FilterFunc(func(topic string, payload any) bool {
    if data, ok := payload.(map[string]any); ok {
        return data["priority"] == "high"
    }
    return false
})

bus.SubscribeWithFilter("order.created", func(topic string, payload any) {
    fmt.Println("只处理高优先级订单")
}, filter)

// 带优先级的过滤订阅
bus.SubscribeWithFilterAndPriority("order.created", handler, filter, 10)
```

### 主题组和嵌套分组

```go
// 创建主题组
userGroup := bus.NewGroup("user")
userGroup.Subscribe("login", handler)      // 实际订阅 "user.login"
userGroup.Publish("logout", payload)       // 实际发布 "user.logout"

// 嵌套子组
adminGroup := userGroup.NewSubGroup("admin")
adminGroup.Subscribe("action", handler)    // 实际订阅 "user.admin.action"

// 获取组信息
fmt.Println(adminGroup.Prefix())           // 输出: "user.admin"

// 组内查询
count, _ := userGroup.GetSubscriberCount("login")
hasSubscribers := userGroup.HasSubscribers("login")
```

### 主题查询 API

```go
// 获取所有已注册的主题
topics := bus.GetTopics()
for _, topic := range topics {
    fmt.Printf("主题: %s\n", topic)
}

// 获取指定主题的订阅者数量
count, err := bus.GetSubscriberCount("user.login")
if err == nil {
    fmt.Printf("订阅者数量: %d\n", count)
}

// 检查主题是否有订阅者
if bus.HasSubscribers("user.login") {
    fmt.Println("有订阅者")
}
```

### 性能中间件 - 监控和增强

```go
middleware := eventbus.NewMiddleware()

// 可选：对负载进行统一转换
middleware.SetTransformer(func(topic string, payload any) any {
    if msg, ok := payload.(string); ok {
        return strings.TrimSpace(msg)
    }
    return payload
})

bus.Use(middleware)

// 获取性能统计
stats := middleware.GetStats()
for topic, stat := range stats {
    avg := stat.TotalTime / time.Duration(stat.Count)
    fmt.Printf("主题 %s: 执行 %d 次，平均耗时 %v\n", topic, stat.Count, avg)
}

middleware.Reset() // 清除历史统计
```

### 可靠投递 - 重试与死信

SubscribeReliable 订阅可靠处理器：处理失败（返回 error 或 panic）时按配置自动重试，
所有尝试均失败或遇到不可重试错误后触发死信回调。**零配置即用安全默认**。

```go
// ① 零配置：3 次尝试 + 指数 jitter 退避（100ms 起、封顶 1s）+ 全错误重试
bus.SubscribeReliable("order.created", func(ctx context.Context, topic string, payload any) error {
    return processOrder(ctx, payload) // 返回 error 触发重试，nil 表示成功
})

// ② 进阶：自定义最大次数、退避策略、可重试错误过滤与死信
bus.SubscribeReliable("order.created", handler,
    eventbus.WithMaxAttempts(5),
    eventbus.WithBackoff(eventbus.ExponentialBackoff(100*time.Millisecond, time.Second)),
    eventbus.WithRetryIf(func(err error) bool {
        return !errors.Is(err, ErrInvalidOrder) // 校验类错误不重试
    }),
    eventbus.WithDeadLetter(func(topic string, payload any, err error) {
        log.Printf("死信: topic=%s err=%v", topic, err) // 落库/告警
    }),
)

// 退避构造器：ConstantBackoff / LinearBackoff / ExponentialBackoff（带 jitter 与封顶）
```

> 同步重试语义：某条消息重试期间，同主题后续消息会排队等待，请据此控制 handler
> 耗时与退避时长。重试全程尊重 context，drain/超时/取消可中断退避等待。

### 优雅关闭 - Shutdown

Shutdown 在关闭前等待已通过接收检查的发布完成，并排空已入队/处理中消息
（或 ctx 超时后强制关闭），适用于平滑发版/重启；Close 保持立即关闭语义不变（测试已锁定）。

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := bus.Shutdown(ctx); err != nil {
    log.Printf("Shutdown 因 %v 提前关闭", err) // nil=正常排空，DeadlineExceeded=超时
}
// bus.Close() 仍为立即关闭，二者职责分离
```

## 🎯 使用场景

### 响应式事务处理 (PublishSyncAll)

```go
// 订单处理：所有步骤都必须成功
bus.SubscribeWithResponse("order/create", func(topic string, payload any) (any, error) {
    order := payload.(Order)
    
    // 库存检查
    if !checkInventory(order.ProductID, order.Quantity) {
        return nil, errors.New("库存不足")
    }
    return map[string]any{"step": "inventory", "status": "ok"}, nil
})

bus.SubscribeWithResponse("order/create", func(topic string, payload any) (any, error) {
    order := payload.(Order)
    
    // 支付处理
    transactionID, err := processPayment(order.Amount, order.PaymentMethod)
    if err != nil {
        return nil, fmt.Errorf("支付失败: %w", err)
    }
    return map[string]any{"step": "payment", "transaction_id": transactionID}, nil
})

bus.SubscribeWithResponse("order/create", func(topic string, payload any) (any, error) {
    order := payload.(Order)
    
    // 发货安排
    trackingID, err := arrangeShipping(order)
    if err != nil {
        return nil, fmt.Errorf("发货失败: %w", err)
    }
    return map[string]any{"step": "shipping", "tracking_id": trackingID}, nil
})

// 创建订单 - 必须所有步骤都成功
result, err := bus.PublishSyncAll("order/create", Order{
    ID:            "ORDER-123",
    ProductID:     "PROD-456", 
    Quantity:      2,
    Amount:        299.99,
    PaymentMethod: "credit_card",
})

if err != nil {
    log.Printf("订单处理超时: %v", err)
} else if result.Success {
    // 所有步骤都成功
    log.Printf("✅ 订单创建成功，耗时: %v", result.TotalTime)
    
    // 提取各步骤结果
    var transactionID, trackingID string
    for _, handlerResult := range result.Results {
        if handlerResult.Success {
            stepResult := handlerResult.Result.(map[string]any)
            switch stepResult["step"] {
            case "payment":
                transactionID = stepResult["transaction_id"].(string)
            case "shipping":
                trackingID = stepResult["tracking_id"].(string)
            }
        }
    }
    
    // 发送确认邮件
    sendOrderConfirmation(transactionID, trackingID)
} else {
    // 部分步骤失败，需要回滚
    log.Printf("❌ 订单创建失败: %d/%d 步骤成功", result.SuccessCount, result.HandlerCount)
    
    for _, handlerResult := range result.Results {
        if !handlerResult.Success {
            log.Printf("步骤失败: %v", handlerResult.Error)
        }
    }
    
    // 执行回滚逻辑
    rollbackOrder("ORDER-123")
}
```

### 高可用通知服务 (PublishSyncAny)

```go
// 多渠道通知：任一渠道成功即可
bus.SubscribeWithResponse("notification/send", func(topic string, payload any) (any, error) {
    notification := payload.(Notification)
    
    // 邮件通知（主渠道）
    err := emailService.Send(notification.Recipient, notification.Subject, notification.Body)
    if err != nil {
        return nil, fmt.Errorf("邮件发送失败: %w", err)
    }
    return map[string]any{"channel": "email", "message_id": "EMAIL-123"}, nil
})

bus.SubscribeWithResponse("notification/send", func(topic string, payload any) (any, error) {
    notification := payload.(Notification)
    
    // 短信通知（备用渠道）
    messageID, err := smsService.Send(notification.Phone, notification.Body)
    if err != nil {
        return nil, fmt.Errorf("短信发送失败: %w", err)
    }
    return map[string]any{"channel": "sms", "message_id": messageID}, nil
})

bus.SubscribeWithResponse("notification/send", func(topic string, payload any) (any, error) {
    notification := payload.(Notification)
    
    // 推送通知（备用渠道）
    pushID, err := pushService.Send(notification.UserID, notification.Title, notification.Body)
    if err != nil {
        return nil, fmt.Errorf("推送发送失败: %w", err)
    }
    return map[string]any{"channel": "push", "message_id": pushID}, nil
})

// 发送通知 - 任一渠道成功即可
result, err := bus.PublishSyncAny("notification/send", Notification{
    Recipient: "user@example.com",
    Phone:     "+1234567890",
    UserID:    "USER-789",
    Subject:   "订单确认",
    Title:     "订单已创建",
    Body:      "您的订单 ORDER-123 已成功创建",
})

if err != nil {
    log.Printf("通知发送超时: %v", err)
} else if result.Success {
    log.Printf("✅ 通知发送成功，耗时: %v", result.TotalTime)
    
    // 记录成功的渠道
    for _, handlerResult := range result.Results {
        if handlerResult.Success {
            channelResult := handlerResult.Result.(map[string]any)
            log.Printf("通过 %s 渠道发送成功，消息ID: %s", 
                channelResult["channel"], channelResult["message_id"])
        }
    }
} else {
    log.Printf("❌ 所有通知渠道都失败了")
    
    // 记录所有失败原因
    for _, handlerResult := range result.Results {
        if !handlerResult.Success {
            log.Printf("渠道失败: %v", handlerResult.Error)
        }
    }
    
    // 触发告警
    alertService.SendAlert("通知系统全部失败", "所有通知渠道都无法使用")
}
```

### 微服务通信

```go
// 服务间事件通信
bus.Subscribe("order.#", func(topic string, payload any) {
    switch topic {
    case "order.created":
        // 处理订单创建
    case "order.payment.completed":
        // 处理支付完成
    }
})

bus.Publish("order.created", OrderEvent{
    OrderID: "123",
    UserID:  "456",
    Amount:  99.99,
})
```

### 系统监控

```go
// 系统指标收集
bus.Subscribe("metrics.#", func(topic string, payload any) {
    if metric, ok := payload.(MetricEvent); ok {
        // 发送到监控系统
        prometheus.RecordMetric(metric)
    }
})

// 发布CPU使用率
bus.Publish("metrics.cpu.usage", MetricEvent{
    Name:  "cpu_usage",
    Value: 85.5,
    Tags:  map[string]string{"host": "server1"},
})
```

### 用户行为追踪

```go
// 用户行为分析
bus.Subscribe("user.action.*", func(topic string, payload any) {
    action := payload.(UserAction)
    analytics.Track(action.UserID, action.Event, action.Properties)
})

bus.Publish("user.action.click", UserAction{
    UserID: "user123",
    Event:  "button_click",
    Properties: map[string]any{
        "button_id": "checkout",
        "page":      "product_detail",
    },
})
```

## ⚡ 性能优化

### 基准测试结果

```bash
# 运行性能测试
go test -bench=. -benchmem

# 最新基准测试结果
BenchmarkEventBusPublishSync-383         2,125,314    595.0 ns/op   111 B/op    3 allocs/op
BenchmarkEventBusPublish-383               710,479   1827 ns/op     407 B/op    7 allocs/op
BenchmarkPipePublishSync-383            28,528,402     47.98 ns/op     8 B/op    1 allocs/op
BenchmarkCowMapLoad-383                 87,791,270     13.30 ns/op     0 B/op    0 allocs/op

# 响应式发布性能
BenchmarkPublishSyncAll-383                  5,067    198,873 ns/op   8,961 B/op  134 allocs/op
BenchmarkPublishSyncAny-383                 24,356     48,654 ns/op   5,811 B/op   87 allocs/op

# Pipe 响应式发布性能
BenchmarkPipePublishSyncAll-383            143,481      6,989 ns/op   1,291 B/op   34 allocs/op
BenchmarkPipeTraditionalPublishSync-383  9,023,946        157.4 ns/op     48 B/op    1 allocs/op
```

### 性能优化建议

1. **缓冲区配置**
   ```go
   // 实时性优先：无缓冲（最低延迟）
   bus := eventbus.New()        // 或 eventbus.New(0)
   
   // 性能优先：智能自动缓冲（推荐）
   bus := eventbus.New(-1)      // CPU核心数 * 64，平衡性能和内存
   
   // 高吞吐量：大缓冲（内存充足场景）
   bus := eventbus.New(10000)   // 自定义大缓冲区
   
   // 轻量级：小缓冲（资源受限场景）
   bus := eventbus.New(256)     // 适度缓冲
   ```

2. **发布方式选择**
   ```go
   // 高吞吐量：异步发布
   bus.Publish("topic", payload)
   
   // 即时反馈：同步发布
   bus.PublishSync("topic", payload)
   
   // 超时控制：带上下文发布
   ctx, cancel := context.WithTimeout(context.Background(), time.Second)
   defer cancel()
   bus.PublishWithContext(ctx, "topic", payload)
   ```

3. **订阅优化**
   ```go
   // 避免过多通配符订阅
   bus.Subscribe("user.login", handler)     // 好
   bus.Subscribe("user.*", handler)         // 可接受
   bus.Subscribe("#", handler)              // 避免，影响性能
   ```

## 🌐 MQTT 兼容性

EventBus 完全支持 MQTT 3.1.1 和 MQTT 5.0 规范的主题过滤器，提供三种通配符：

### 通配符类型

| 通配符 | 类型 | 说明 | 示例 |
|--------|------|------|------|
| `+` | 单层级 | 匹配任意一个层级，可在任意位置 | `sensor/+/temp` 匹配 `sensor/room1/temp` |
| `*` | 单层级 | 匹配任意一个层级，必须独占一个层级 | `alert/*` 匹配 `alert/fire` |
| `#` | 多层级 | 匹配零个或多个层级，只能在末尾 | `system/#` 匹配 `system/cpu/high` |

### 分隔符支持

```go
// 支持点分隔符 (.)
bus.Subscribe("sensor.room1.temperature", handler)

// 支持斜杠分隔符 (/)  
bus.Subscribe("sensor/room1/temperature", handler)

// 支持混合分隔符
bus.Subscribe("sensor/room1.temperature", handler)  // 内部统一处理
```

详细的MQTT兼容性说明请参考 [MQTT_COMPATIBILITY.md](docs/MQTT_COMPATIBILITY.md)

## 🛡️ 错误处理

### 标准错误类型

```go
import "github.com/darkit/eventbus"

// 错误处理示例
if err := bus.Publish("topic", payload); err != nil {
    switch {
    case errors.Is(err, eventbus.ErrInvalidTopic):
        log.Println("主题格式无效或为空")
    case errors.Is(err, eventbus.ErrChannelClosed):
        log.Println("通道已关闭")
    case errors.Is(err, eventbus.ErrPublishTimeout):
        log.Println("发布超时")
    case errors.Is(err, eventbus.ErrEventBusClosed):
        log.Println("事件总线已关闭")
    case errors.Is(err, eventbus.ErrShuttingDown):
        log.Println("事件总线正在优雅关闭，已拒绝新发布")
    case errors.Is(err, eventbus.ErrNoSubscriber):
        log.Println("没有订阅者")
    default:
        log.Printf("未知错误: %v", err)
    }
}
```

当发布返回 `ErrPublishTimeout` 时，通常表示目标主题的缓冲通道已满且在默认 5 秒内未能腾出空间；如果调用方提供了 `ctx`，则以 `ctx` 的截止时间或取消信号为准。可通过增大缓冲区、加快订阅者处理速度或调整 `ctx` 超时时间来缓解。

### 错误恢复

```go
// 设置追踪器处理错误
tracer := &ErrorRecoveryTracer{}
bus.SetTracer(tracer)

type ErrorRecoveryTracer struct{}

func (t *ErrorRecoveryTracer) OnError(topic string, err error) {
    // 记录错误日志
    log.Printf("事件处理错误 [%s]: %v", topic, err)
    
    // 发送告警
    alerting.SendAlert("EventBus Error", err.Error())
    
    // 尝试重试或降级处理
    if isRetryableError(err) {
        // 重试逻辑
    }
}
```

## 🏗️ 架构设计

### 核心架构

```text
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Publishers    │────│    EventBus      │────│   Subscribers   │
│                 │    │                  │    │                 │
│ • HTTP Handler  │    │ • Topic Router   │    │ • Log Handler   │
│ • Cron Jobs     │    │ • Filter Chain   │    │ • DB Handler    │
│ • External APIs │    │ • Middleware     │    │ • Email Service │
└─────────────────┘    │ • COW Map        │    └─────────────────┘
                       │ • Priority Queue │
                       │ • Health Monitor │
                       └──────────────────┘
                              │
                    ┌─────────┴─────────┐
                    │     Monitoring    │
                    │                   │
                    │ • Event Tracer    │
                    │ • Metrics         │
                    │ • Performance     │
                    └───────────────────┘
```

### 设计原则

- **解耦**: 发布者和订阅者完全解耦
- **可扩展**: 支持中间件和过滤器扩展
- **高性能**: 优化的COW机制，最小化锁竞争
- **类型安全**: 泛型管道提供编译时类型检查
- **可观测**: 完整的监控和追踪能力

## 📚 完整示例

查看示例代码获取完整的使用示例：

- [完整功能示例](examples/full/main.go) - 包括优先级订阅、通配符匹配、错误处理、泛型管道、全局单例、中间件和过滤器
- [响应式发布示例](examples/response/main.go) - PublishSyncAll/PublishSyncAny 完整演示
- [适配器使用示例](adapter/README.md) - Bridge 桥接外部系统的完整示例

```bash
# 运行完整示例
go run examples/full/main.go

# 运行响应式示例
go run examples/response/main.go

# 运行示例测试
go test ./examples/full/ ./examples/response/
```

## 🔧 开发工具

### Makefile 命令

```bash
# 格式化代码
make fmt

# 代码检查
make lint
make vet

# 运行测试
make test
make test-race

# 性能测试
make benchmark

# 构建项目
make build

# 查看帮助
make help
```

### 项目结构

```text
eventbus/
├── README.md              # 项目说明
├── CHANGELOG.md           # 版本变更日志
├── golangci.yml           # 代码质量检查配置
├── go.mod / go.sum        # Go 模块定义和依赖
├── doc.go                 # 包级文档注释
├── eventbus*.go           # EventBus 核心模块（admin/batch/channel/helpers/match/publish/response/subscribe）
├── retry.go               # 重试机制
├── topic_trie.go          # 通配符匹配树
├── pipe*.go               # 泛型管道
├── cowmap*.go             # 写时复制 Map
├── group*.go              # 主题分组
├── singleton*.go          # 全局单例
├── tracer*.go             # 事件追踪器
├── interfaces.go          # 接口定义
├── errors.go              # 错误类型
├── *_test.go              # 单元测试/基准测试文件
├── adapter/               # 外部系统适配器
│   ├── README.md          # 适配器使用说明
│   ├── DESIGN.md          # 设计文档
│   ├── bridge.go          # 事件桥接器
│   ├── transport.go       # 传输抽象层
│   ├── codec.go           # 消息编解码
│   ├── message.go         # 消息模型
│   ├── context.go         # 请求上下文
│   ├── dedupe.go          # 消息去重
│   └── options.go         # 可配置选项
├── examples/              # 使用示例
│   ├── full/              # 完整功能示例
│   │   ├── main.go        # 完整示例代码
│   │   └── main_test.go   # 示例测试
│   └── response/          # 响应式发布示例
│       └── main.go        # 响应式示例代码
├── docs/                  # 项目文档
│   ├── API接口设计.md      # API 接口设计文档
│   ├── ARCHITECTURE.md    # 架构设计文档
│   ├── MQTT_COMPATIBILITY.md  # MQTT 兼容性文档
│   ├── RELEASE_NOTES.md   # 发布说明
│   ├── images/            # 架构图表
│   │   ├── 架构图.md      # 系统架构图
│   │   ├── 流程图.md      # 业务流程图
│   │   └── 时序图.md      # 时序交互图
│   └── skills/            # EventBus 技能文档
│       └── eventbus/
│           ├── SKILL.md
│           ├── assets/
│           └── references/
└── Makefile               # 构建脚本
```

### 核心源码分层

| 模块 | 文件 | 职责 |
|------|------|------|
| EventBus 骨架 | `eventbus.go` | 结构体定义、trace/config 管理、总线级治理入口 |
| 通道管理 | `eventbus_channel.go` | 内部 channel worker 生命周期、队列投递、处理器注册/退订 |
| 订阅入口 | `eventbus_subscribe.go` | `Subscribe*` / `SubscribeOnce*` / filter 订阅入口 |
| 发布核心 | `eventbus_publish.go` | 普通 `Publish*`、`deliverMessage`、filter/middleware 应用 |
| 批量操作 | `eventbus_batch.go` | `PublishBatch*`、批量分组与计数语义 |
| 匹配聚合 | `eventbus_match.go` | 普通/响应处理器匹配聚合 |
| 响应式发布 | `eventbus_response.go` | 响应式同步发布核心收敛逻辑 |
| 响应 API | `eventbus_response_api.go` | `PublishSyncAll` / `PublishSyncAny` / `PublishSyncAnyValue` 对外 facade |
| 辅助函数 | `eventbus_helpers.go` | handler 签名校验、`callHandler`、共享 helper |
| 管理接口 | `eventbus_admin.go` | 关闭、退订、统计、健康检查等管理接口 |
| 重试机制 | `retry.go` | 指数退避重试、可重试错误过滤、死信回调 |
| 匹配树 | `topic_trie.go` | Trie 树通配符匹配、`+` / `#` 语义、混合分隔符 |
| 泛型管道 | `pipe*.go` | 类型安全泛型管道、响应式处理器 |
| 写时复制 | `cowmap*.go` | 高并发读优化 COW Map |
| 主题分组 | `group*.go` | 层级化主题组、嵌套子组 |
| 全局单例 | `singleton*.go` | 全局事件总线实例、生命周期管理 |
| 事件追踪 | `tracer*.go` | 生命周期追踪、性能指标、错误监控 |
| 接口定义 | `interfaces.go` | 核心接口类型定义 |
| 错误类型 | `errors.go` | 标准化错误定义 |
| 外部适配器 | `adapter/` | Bridge / Transport / Codec 三层抽象，桥接外部系统 |

## 📊 文档资源

### 架构设计文档
- [架构设计文档](docs/ARCHITECTURE.md) - 完整架构方案与设计决策
- [系统架构图](docs/images/架构图.md) - 系统整体架构和组件关系
- [业务流程图](docs/images/流程图.md) - 关键业务流程详细说明
- [时序图](docs/images/时序图.md) - 组件间交互时序关系

### 开发文档
- [MQTT 兼容性说明](docs/MQTT_COMPATIBILITY.md) - 完整的 MQTT 通配符支持文档
- [API 接口设计](docs/API接口设计.md) - API 接口详细设计文档
- [发布说明](docs/RELEASE_NOTES.md) - 各版本发布说明
- [变更日志](CHANGELOG.md) - 版本变更记录
- [EventBus 技能文档](docs/skills/eventbus/) - 开发者使用指南和最佳实践

## 🤝 贡献指南

我们欢迎所有形式的贡献！

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add some amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 开启 Pull Request

### 贡献要求

- 遵循 Go 代码规范
- 添加适当的测试用例
- 更新相关文档
- 通过所有 CI 检查

## 📜 许可证

本项目采用 [MIT License](LICENSE) 许可证。

## 🙏 致谢

感谢所有贡献者对 EventBus 项目的支持和贡献！

---

**EventBus** - 让事件驱动架构更简单、更高效！ 🚀
