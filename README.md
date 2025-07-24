# EventBus 事件总线库

[![Go Reference](https://pkg.go.dev/badge/github.com/darkit/eventbus.svg)](https://pkg.go.dev/github.com/darkit/eventbus)
[![Go Report Card](https://goreportcard.com/badge/github.com/darkit/eventbus)](https://goreportcard.com/report/github.com/darkit/eventbus)
[![Coverage Status](https://coveralls.io/repos/github/DarkiT/eventbus/badge.svg?branch=master)](https://coveralls.io/github/DarkiT/eventbus?branch=master)
[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

EventBus 是一个高性能的 Go 事件总线库，提供了事件发布/订阅、事件追踪、过滤器、中间件等功能。基于写时复制(Copy-on-Write)机制实现高并发性能。

## 功能特性
- 🚀 同步/异步事件发布
- 📊 事件追踪和监控
- 🔍 事件过滤器
- 🔧 处理中间件
- ⏱️ 超时控制和上下文支持
- 🔒 线程安全
- 🎯 泛型管道
- 🌟 主题通配符匹配
- 📁 分组支持
- ⚡ 优先级订阅
- 📈 性能统计
- 🏥 健康检查

## 安装
```bash
go get github.com/darkit/eventbus
```

## 快速开始
```go
// 创建事件总线
bus := eventbus.NewBuffered(1024)
defer bus.Close()

// 优先级订阅
bus.SubscribeWithPriority("user.created", func(topic string, payload any) {
    fmt.Printf("高优先级处理: %v\n", payload)
}, 10)

bus.Subscribe("user.created", func(topic string, payload any) {
    fmt.Printf("普通处理: %v\n", payload)
})

// 发布事件
bus.Publish("user.created", map[string]string{"name": "John"})

// 带上下文的发布
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
bus.PublishWithContext(ctx, "user.created", map[string]string{"name": "Jane"})
```

## 核心组件

### EventBus
事件总线是该库的核心组件，提供事件的发布和订阅功能。
```go
// 创建事件总线
bus := eventbus.New()                    // 无缓冲
bus := eventbus.NewBuffered(1024)        // 带缓冲

// 订阅事件
bus.Subscribe("topic", handler)                           // 普通订阅
bus.SubscribeWithPriority("topic", handler, priority)     // 优先级订阅

// 发布事件
bus.Publish("topic", payload)                            // 异步发布
bus.PublishSync("topic", payload)                        // 同步发布
bus.PublishWithContext(ctx, "topic", payload)            // 带上下文发布

// 统计和健康检查
stats := bus.GetStats()                                   // 获取统计信息
err := bus.HealthCheck()                                  // 健康检查
```

### 事件追踪
通过实现 EventTracer 接口来追踪事件的生命周期：
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
```

### 泛型管道 (Pipe)
类型安全的消息传递管道：
```go
pipe := eventbus.NewPipe[int]()              // 无缓冲
pipe := eventbus.NewBufferedPipe[int](100)   // 带缓冲

// 优先级订阅
pipe.SubscribeWithPriority(func(val int) {
    fmt.Printf("高优先级处理: %d\n", val)
}, 10)

pipe.Subscribe(func(val int) {
    fmt.Printf("普通处理: %d\n", val)
})

// 发布消息
pipe.Publish(42)                             // 异步发布
pipe.PublishSync(42)                         // 同步发布
pipe.PublishWithContext(ctx, 42)             // 带上下文发布

// 统计信息
stats := pipe.GetStats()                     // 获取统计信息
```

### 全局单例
提供全局单例模式使用：
```go
// 基本使用
eventbus.Subscribe("topic", handler)
eventbus.SubscribeWithPriority("topic", handler, priority)
eventbus.Publish("topic", payload)
eventbus.PublishWithContext(ctx, "topic", payload)


中间件和过滤器
eventbus.AddFilter(filter)
eventbus.Use(middleware)
eventbus.SetTracer(tracer)

// 健康检查和清理
eventbus.HealthCheck()
eventbus.Close()
```
## 高级特性

### 主题匹配
支持使用通配符来订阅多个主题：
- `*` 匹配单个层级
- `#` 匹配多个层级

```go
// 匹配 user.login, user.logout 等
bus.Subscribe("user.*", handler)

// 匹配 system.cpu.high, system.memory.low 等
bus.Subscribe("system.#", handler)
```

### 分组支持
使用 `/` 来组织主题层级：
```go
// 订阅所有邮件通知
bus.Subscribe("notifications/email/*", handler)

// 订阅所有短信通知
bus.Subscribe("notifications/sms/*", handler)
```

### 错误处理
```go
var (
    ErrHandlerIsNotFunc  = errors.New("处理器必须是一个函数")
    ErrHandlerParamNum   = errors.New("处理器必须有且仅有两个参数")
    ErrHandlerFirstParam = errors.New("处理器的第一个参数必须是字符串类型")
    ErrChannelClosed     = errors.New("通道已关闭")
    ErrNoSubscriber      = errors.New("主题没有找到订阅者")
    ErrPublishTimeout    = errors.New("发布操作超时")
    ErrEventBusClosed    = errors.New("事件总线已关闭")
)
```

## 性能优化

### 基准测试
```bash
go test -bench=. -benchmem
```

### 优化建议
1. 缓冲区大小选择
   - 无缓冲：实时性高，但可能阻塞
   - 带缓冲：吞吐量高，但延迟增加

2. 发布方式选择
   - Publish：异步非阻塞，适合高并发
   - PublishSync：同步阻塞，适合需要即时反馈
   - PublishWithContext：带上下文发布，支持超时和取消

## 最佳实践

1. 主题命名
   - 使用点号(.)或斜杠(/)作为分隔符
   - 采用层级结构组织主题
   - 避免过深的层级嵌套

2. 通配符使用
   - `*` 适用于匹配已知的单个层级
   - `#` 适用于匹配未知数量的层级
   - 避免过多使用通配符，可能影响性能

3. 错误处理
```go
if err := bus.Publish("topic", payload); err != nil {
    switch err {
    case ErrChannelClosed:
        // 处理通道关闭
        log.Println("事件总线通道已关闭")
    case ErrPublishTimeout:
        // 处理超时
        log.Println("事件发布超时")
    case ErrEventBusClosed:
        // 处理事件总线关闭
        log.Println("事件总线已关闭")
    default:
        // 处理其他错误
        log.Printf("发布事件时发生错误: %v", err)
    }
}
```

## 文档

### 📊 架构设计
- [系统架构图](docs/images/架构图.md) - 系统整体架构和组件关系
- [业务流程图](docs/images/流程图.md) - 关键业务流程的详细说明
- [时序图](docs/images/时序图.md) - 组件间交互的时序关系

### 🔧 示例代码
- [基础示例](examples/main.go) - 使用示例

## 性能基准

```bash
# 运行性能测试
go test -bench=. -benchmem

# 示例结果
BenchmarkEventBusPublishSync-383    2836099    421.2 ns/op
```

## 贡献
欢迎提交 Issue 和 Pull Request。

## 许可证
[MIT License](LICENSE)