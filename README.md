# EventBus 事件总线库

[![Go Reference](https://pkg.go.dev/badge/github.com/darkit/eventbus.svg)](https://pkg.go.dev/github.com/darkit/eventbus)
[![Go Report Card](https://goreportcard.com/badge/github.com/darkit/eventbus)](https://goreportcard.com/report/github.com/darkit/eventbus)
[![Coverage Status](https://coveralls.io/repos/github/DarkiT/eventbus/badge.svg?branch=master)](https://coveralls.io/github/DarkiT/eventbus?branch=master)
[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

EventBus 是一个高性能的 Go 事件总线库，基于优化的写时复制(Copy-on-Write)机制实现高并发性能。提供事件发布/订阅、事件追踪、过滤器、中间件等企业级功能。

## ✨ 功能特性

- 🚀 **高性能异步/同步事件发布** - 优化的COW机制，读操作零锁竞争
- 📊 **完整事件追踪监控** - 生命周期追踪、性能指标、错误监控
- 🔍 **智能事件过滤器** - 频率限制、内容过滤、主题阻断
- 🔧 **灵活处理中间件** - 性能监控、日志记录、数据转换
- ⏱️ **超时控制和上下文支持** - Context传播、超时处理、取消机制
- 🔒 **线程安全设计** - 原子操作、读写锁、竞态检测通过
- 🎯 **类型安全泛型管道** - 强类型消息传递、编译时类型检查
- 🌟 **完整MQTT通配符支持** - 支持 `+`、`*`、`#` 三种通配符和混合分隔符
- 🎯 **响应式同步发布** - PublishSyncAll/PublishSyncAny 及 WithContext 版本，支持处理器返回值与上下文透传
- 📁 **分组和命名空间支持** - 层级化主题管理、权限控制
- ⚡ **优先级订阅机制** - 处理器优先级排序、有序执行
- 📈 **实时性能统计** - 吞吐量、延迟、队列状态监控
- 🏥 **健康检查和故障恢复** - 系统状态监控、自动故障处理

## 📦 安装

```bash
go get github.com/darkit/eventbus
```

**系统要求**: Go 1.23+

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

// * 末尾单层通配符：匹配最后一个层级
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
bus.SubscribeWithResponse("topic", responseHandler)       // 响应式订阅（不带context）
bus.SubscribeWithResponseContext("topic", handler)        // 响应式订阅（带context）
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
result, err := bus.PublishSyncAny("topic", payload)                 // 任一处理器成功即算成功（默认 5 秒超时）
result, err := bus.PublishSyncAllWithContext(ctx, "topic", payload) // 继承调用方 ctx，必要时自动补充默认超时
result, err := bus.PublishSyncAnyWithContext(ctx, "topic", payload) // 任一处理器成功即返回并取消其他处理器

// 提示：当 ctx 未设置截止时间时，WithContext 变体会自动叠加默认超时；
// 如果需要自定义时限，可在调用前通过 context.WithTimeout/WithDeadline 设置专属超时。

// 系统管理
stats := bus.GetStats()                                   // 获取统计信息
err := bus.HealthCheck()                                  // 健康检查
bus.Close()                                              // 关闭总线
```

### 泛型管道 (Pipe) - 类型安全

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

// 响应式订阅
cancelResponse, err := intPipe.SubscribeWithResponse(func(val int) (any, error) {
    if val < 0 {
        return nil, errors.New("负数不被支持")
    }
    return val * 2, nil // 返回处理结果
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

// 注意：PublishSyncAll/PublishSyncAny 使用默认5秒超时
// 如需自定义超时，请在处理器内部使用 context

// 取消订阅
cancelResponse()                                // 取消响应式订阅

// 管理
stats := intPipe.GetStats()                     // 统计信息
intPipe.Close()                                // 关闭管道
```

### 全局单例 - 便捷访问

```go
import "github.com/darkit/eventbus"

// 直接使用全局实例
eventbus.Subscribe("global.event", handler)
eventbus.Publish("global.event", payload)
eventbus.PublishSync("global.event", payload)
eventbus.PublishWithContext(ctx, "global.event", payload)
eventbus.PublishSyncWithContext(ctx, "global.event", payload)

// 系统管理
eventbus.HealthCheck()              // 健康检查
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

bus.AddFilter(filter)                      // 注册智能过滤器

// 动态调整：
filter.UnblockTopic("internal.test")      // 解除阻断
filter.SetLimit("user.login", 0)          // 移除针对 user.login 的限流

// 自定义过滤器：直接使用 FilterFunc 包装函数
bus.AddFilter(eventbus.FilterFunc(func(topic string, payload any) bool {
    return !strings.Contains(topic, "spam")
}))
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
| `*` | 末尾单层级 | 匹配最后一个层级，只能在末尾 | `alert/*` 匹配 `alert/fire` |
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
    case errors.Is(err, eventbus.ErrChannelClosed):
        log.Println("通道已关闭")
    case errors.Is(err, eventbus.ErrPublishTimeout):
        log.Println("发布超时")
    case errors.Is(err, eventbus.ErrEventBusClosed):
        log.Println("事件总线已关闭")
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
├── go.mod                 # Go模块定义
├── *.go                   # 核心源码文件
├── *_test.go              # 单元测试文件
├── examples/              # 使用示例
│   ├── full/             # 完整功能示例
│   │   ├── main.go       # 完整示例代码
│   │   └── main_test.go  # 示例测试
│   └── response/         # 响应式发布示例
│       └── main.go       # 响应式示例代码
├── docs/                  # 项目文档
│   ├── images/           # 架构图表
│   │   ├── 架构图.md     # 系统架构图
│   │   ├── 流程图.md     # 业务流程图
│   │   └── 时序图.md     # 时序交互图
│   ├── MQTT_COMPATIBILITY.md  # MQTT兼容性文档
│   ├── API接口设计.md    # API接口详细设计文档
│   ├── 方案设计规则.md   # 方案设计规则
│   ├── 项目文档规范.md   # 文档编写规范
│   ├── 项目概述.md       # 项目总体介绍
│   ├── 性能评估报告.md   # 性能基准测试报告
│   └── ARCHITECTURE.md   # 系统架构说明
└── Makefile              # 构建脚本
```

## 📊 文档资源

### 架构设计文档
- [系统架构图](docs/images/架构图.md) - 系统整体架构和组件关系
- [业务流程图](docs/images/流程图.md) - 关键业务流程详细说明
- [时序图](docs/images/时序图.md) - 组件间交互时序关系

### 开发文档
- [MQTT兼容性说明](docs/MQTT_COMPATIBILITY.md) - 完整的MQTT通配符支持文档
- [性能评估报告](docs/性能评估报告.md) - 基准测试分析、性能瓶颈识别和优化建议
- [API接口设计](docs/API接口设计.md) - API接口详细设计文档

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
