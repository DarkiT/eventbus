# EventBus 事件总线库

[![Go Reference](https://pkg.go.dev/badge/github.com/darkit/eventbus.svg)](https://pkg.go.dev/github.com/darkit/eventbus)
[![Go Report Card](https://goreportcard.com/badge/github.com/darkit/eventbus)](https://goreportcard.com/report/github.com/darkit/eventbus)
[![Coverage Status](https://coveralls.io/repos/github/darkit/eventbus/badge.svg?branch=master)](https://coveralls.io/github/darkit/eventbus?branch=master)
[![MIT License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

EventBus 是一个高性能的 Go 事件总线库,提供了事件发布/订阅、事件追踪、过滤器、中间件等功能。基于写时复制(Copy-on-Write)机制实现高并发性能。

## 功能特性

- 同步/异步事件发布
- 事件追踪和监控
- 事件过滤器
- 处理中间件
- 超时控制
- 线程安全
- 泛型管道

## 安装

```bash
go get github.com/darkit/eventbus
```

## 核心组件

### EventBus

事件总线是该库的核心组件,提供事件的发布和订阅功能。

```go
// 创建事件总线
bus := eventbus.New()                    // 无缓冲
bus := eventbus.NewBuffered(1024)        // 带缓冲

// 订阅事件
bus.Subscribe("topic", func(topic string, payload interface{}) {
    // 处理事件
})

// 发布事件
bus.Publish("topic", payload)            // 异步发布
bus.PublishSync("topic", payload)        // 同步发布
bus.PublishWithTimeout("topic", payload, 5*time.Second) // 带超时发布

// 取消订阅
bus.Unsubscribe("topic", handler)

// 关闭事件总线
bus.Close()
```

### 事件追踪

通过实现 EventTracer 接口来追踪事件的生命周期:

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

// 设置追踪器
bus.SetTracer(tracer)
```

### 泛型管道 (Pipe)

类型安全的消息传递管道:

```go
// 创建管道
pipe := eventbus.NewPipe[int]()              // 无缓冲
pipe := eventbus.NewBufferedPipe[int](100)   // 带缓冲

// 订阅处理器
pipe.Subscribe(func(val int) {
    // 处理消息
})

// 发布消息
pipe.Publish(42)                             // 异步发布
pipe.PublishSync(42)                         // 同步发布

// 设置超时
pipe.SetTimeout(5 * time.Second)

// 获取订阅者数量
count := pipe.Len()

// 检查是否已关闭
closed := pipe.IsClosed()

// 关闭管道
pipe.Close()
```

### 全局单例

提供全局单例模式使用:

```go
// 订阅
eventbus.Subscribe("topic", handler)

// 发布
eventbus.Publish("topic", payload)
eventbus.PublishSync("topic", payload)

// 取消订阅
eventbus.Unsubscribe("topic", handler)

// 关闭
eventbus.Close()
```

## 错误处理

```go
var (
    ErrHandlerIsNotFunc  = errors.New("handler is not func")
    ErrHandlerParamNum   = errors.New("handler param num error")
    ErrHandlerFirstParam = errors.New("handler first param must be string")
    ErrChannelClosed     = errors.New("channel closed")
    ErrNoSubscriber      = errors.New("no subscriber")
    ErrPublishTimeout    = errors.New("publish timeout")
)
```

## 性能优化

1. 缓冲区大小选择
- 无缓冲:实时性高,但可能阻塞
- 带缓冲:吞吐量高,但延迟增加
- 建议根据实际场景调整缓冲区大小

2. 同步/异步发布
- Publish:异步非阻塞,适合高并发
- PublishSync:同步阻塞,适合需要即时反馈
- PublishWithTimeout:带超时控制,防止阻塞

## 最佳实践

1. 选择合适的发布方式
```go
// 不关心处理结果
bus.Publish("topic", payload)

// 需要等待处理完成
bus.PublishSync("topic", payload)

// 防止超时阻塞
bus.PublishWithTimeout("topic", payload, timeout)
```

2. 合理使用追踪器
```go
// 实现需要的方法即可,无需实现全部接口
type SimpleTracer struct {
    EventTracer
}

func (t *SimpleTracer) OnError(topic string, err error) {
    log.Printf("Error in topic %s: %v", topic, err)
}
```

3. 正确处理错误
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

## 性能测试

```bash
go test -bench=. -benchmem
```

## 性能分析

基于 Intel i5-10400 CPU @ 2.90GHz 的测试结果分析（单位：ns/op）：

### EventBus 性能

1. 事件发布：
- 异步发布 (Publish): 643.2 ns/op
- 同步发布 (PublishSync): 383.1 ns/op
- 高并发场景: 246.7 ns/op

2. 管道操作：
- 同步发布 (PipePublishSync): 52.41 ns/op
- 异步发布 (PipePublish): 1240 ns/op
- 原生通道 (GoChannel): 256.2 ns/op

3. 单例模式：
- 异步发布: 625.9 ns/op
- 同步发布: 620.2 ns/op

### 性能特点分析

1. EventBus 特点：
- 同步操作性能优于异步操作
- 在高并发场景下性能表现稳定
- Pipe 的同步操作性能最优

2. 使用建议：

```go
// 高性能同步操作：使用 Pipe
pipe := eventbus.NewPipe[T]()
pipe.PublishSync(data)

// 高并发场景：使用 EventBus
bus := eventbus.New()
bus.Publish("topic", payload)
```

### 性能优化建议

1. 根据场景选择合适的组件：
- 实时性要求高：使用同步操作
- 吞吐量要求高：使用异步操作

2. 缓冲区配置：
```go
// 高吞吐量场景
bus := eventbus.NewBuffered(1024)

// 实时性要求高的场景
bus := eventbus.New() // 无缓冲
```

## 贡献

欢迎提交 Issue 和 Pull Request。

## 许可证

[MIT License](LICENSE)