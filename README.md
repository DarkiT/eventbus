# EventBus 事件总线库

[![Go Reference](https://pkg.go.dev/badge/github.com/darkit/eventbus.svg)](https://pkg.go.dev/github.com/darkit/eventbus)
[![Go Report Card](https://goreportcard.com/badge/github.com/darkit/eventbus)](https://goreportcard.com/report/github.com/darkit/eventbus)
[![Coverage Status](https://coveralls.io/repos/github/DarkiT/eventbus/badge.svg?branch=master)](https://coveralls.io/github/DarkiT/eventbus?branch=master)
[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

EventBus 是一个高性能的 Go 事件总线库，提供了事件发布/订阅、事件追踪、过滤器、中间件等功能。基于写时复制(Copy-on-Write)机制实现高并发性能。

## 功能特性
- 同步/异步事件发布
- 事件追踪和监控
- 事件过滤器
- 处理中间件
- 超时控制
- 线程安全
- 泛型管道
- 主题通配符
- 分组支持

## 安装
```bash
go get github.com/darkit/eventbus
```

## 快速开始
```go
// 创建事件总线
bus := eventbus.New()

// 订阅事件
bus.Subscribe("user.created", func(topic string, payload interface{}) {
    fmt.Printf("User created: %v\n", payload)
})

// 发布事件
bus.Publish("user.created", map[string]string{"name": "John"})
```

## 核心组件

### EventBus
事件总线是该库的核心组件，提供事件的发布和订阅功能。
```go
// 创建事件总线
bus := eventbus.New()                    // 无缓冲
bus := eventbus.NewBuffered(1024)        // 带缓冲

// 订阅事件
bus.Subscribe("topic", handler)

// 发布事件
bus.Publish("topic", payload)            // 异步发布
bus.PublishSync("topic", payload)        // 同步发布
bus.PublishWithTimeout("topic", payload, timeout) // 带超时发布
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

pipe.Subscribe(func(val int) {
    // 处理消息
})

pipe.Publish(42)
```

### 全局单例
提供全局单例模式使用：
```go
eventbus.Subscribe("topic", handler)
eventbus.Publish("topic", payload)
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
    ErrHandlerIsNotFunc  = errors.New("handler is not func")
    ErrHandlerParamNum   = errors.New("handler param num error")
    ErrChannelClosed     = errors.New("channel closed")
    ErrNoSubscriber      = errors.New("no subscriber")
    ErrPublishTimeout    = errors.New("publish timeout")
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
   - PublishWithTimeout：带超时控制，防止阻塞

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
    case ErrPublishTimeout:
        // 处理超时
    default:
        // 处理其他错误
    }
}
```

## 贡献
欢迎提交 Issue 和 Pull Request。

## 许可证
[MIT License](LICENSE)