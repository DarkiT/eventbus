# EventBus

一个功能强大的 Go 语言事件总线库，支持异步事件处理、过滤器、中间件、优先级和主题分组等特性。

## 特性

- 异步事件处理
- 事件过滤器
- 处理中间件
- 订阅优先级
- 主题分组
- 主题通配符
- 多种分隔符支持 (. 和 /)

## 安装

```bash
go get github.com/darkit/eventbus
```

## 基础用法

### 创建事件总线

```go
// 创建默认事件总线
bus := eventbus.New()

// 创建带缓冲的事件总线
bus := eventbus.NewBuffered(100)
```

### 发布和订阅

```go
// 订阅事件
bus.Subscribe("user.created", func(topic string, user User) {
    fmt.Printf("New user created: %v\n", user)
})

// 发布事件
bus.Publish("user.created", User{Name: "John"})

// 同步发布
bus.PublishSync("user.created", User{Name: "John"})
```

## 高级特性

### 过滤器

```go
// 定义过滤器
type LogFilter struct{}

func (f *LogFilter) Filter(topic string, payload any) bool {
    fmt.Printf("Processing event: %s\n", topic)
    return true
}

// 添加过滤器
bus.AddFilter(&LogFilter{})
```

### 中间件

```go
// 定义中间件
type TimingMiddleware struct{}

func (m *TimingMiddleware) Before(topic string, payload any) any {
    // 前置处理
    return payload
}

func (m *TimingMiddleware) After(topic string, payload any) {
    // 后置处理
}

// 添加中间件
bus.AddMiddleware(&TimingMiddleware{})
```

### 优先级订阅

```go
// 高优先级订阅
bus.SubscribeWithPriority("user.created", handler, 10)

// 低优先级订阅
bus.SubscribeWithPriority("user.created", handler, 1)
```

### 主题分组

```go
// 创建主题组
userGroup := bus.NewGroup("user")

// 在组内订阅
userGroup.Subscribe("created", handler)

// 在组内发布
userGroup.Publish("created", user)
```

### 通配符订阅

```go
// 单层通配符
bus.Subscribe("user.*.created", handler)  // 匹配 user.admin.created, user.guest.created

// 多层通配符
bus.Subscribe("user.#", handler)  // 匹配所有 user. 开头的主题
```

### 多分隔符支持

```go
// 这些主题是等价的
bus.Subscribe("user.created", handler)
bus.Subscribe("user/created", handler)
```

### 超时发布

```go
// 带超时的发布
err := bus.PublishWithTimeout("user.created", user, 5*time.Second)
if err == ErrTimeout {
    log.Println("发布超时")
}
```

## 线程安全

EventBus 是线程安全的，可以在多个 goroutine 中安全使用。

## 错误处理

```go
if err := bus.Publish("topic", payload); err != nil {
    log.Printf("Failed to publish: %v", err)
}
```

## 性能考虑

- 使用 NewBuffered 创建带缓冲的事件总线可以提高性能
- 优先级订阅会略微影响性能
- 通配符匹配会带来一定的性能开销

## 许可证

MIT License
