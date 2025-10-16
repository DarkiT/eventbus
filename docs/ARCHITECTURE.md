# EventBus 架构设计文档

> **版本**: v0.2.0  
> **更新时间**: 2025-10-16  
> **状态**: 生产就绪

## 📋 文档概述

本文档详细描述了 EventBus 事件总线库的系统架构、核心组件设计、性能特性以及最佳实践。

---

## 🏗️ 系统架构

### 总体架构图

```
┌─────────────────────────────────────────────────────────┐
│                     应用层（Application）                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │  发布者   │  │  订阅者   │  │  管理器   │              │
│  └──────────┘  └──────────┘  └──────────┘              │
└─────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│                   核心层（EventBus Core）                │
│  ┌──────────────────────────────────────────────────┐   │
│  │  EventBus 主组件                                  │   │
│  │  • 事件发布/订阅  • 优先级处理  • 通配符匹配    │   │
│  └──────────────────────────────────────────────────┘   │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │  Filter   │  │Middleware│  │  Tracer  │              │
│  └──────────┘  └──────────┘  └──────────┘              │
└─────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────┐
│                  基础设施层（Infrastructure）             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│  │  CowMap   │  │   Pipe   │  │  Channel │              │
│  │ (写时复制) │  │(泛型管道)│  │(优先级队列)│             │
│  └──────────┘  └──────────┘  └──────────┘              │
└─────────────────────────────────────────────────────────┘
```

### 设计原则

1. **高性能**：写时复制（COW）机制优化读多写少场景
2. **类型安全**：泛型管道提供编译时类型检查
3. **可扩展**：插件化的过滤器和中间件架构
4. **易用性**：简洁直观的 API 设计
5. **可观测**：完整的事件追踪和监控能力

---

## 🔧 核心组件

### 1. EventBus 主组件

**职责**：
- 事件发布和订阅管理
- 主题路由和通配符匹配
- 优先级处理和并发控制
- 生命周期管理

**核心结构**：
```go
type EventBus struct {
    bufferSize  int              // 缓冲区大小
    channels    *cowMap          // 通道映射表（写时复制）
    timeout     time.Duration    // 默认超时时间
    filters     []EventFilter    // 事件过滤器链
    middlewares []Middleware     // 中间件链
    tracer      EventTracer      // 事件追踪器
    lock        sync.RWMutex     // 读写锁
    closed      atomic.Bool      // 关闭状态标志
}
```

**关键特性**：
- ✅ 支持同步/异步发布
- ✅ 响应式同步发布（PublishSyncAll/Any）
- ✅ 完整的 Context 支持
- ✅ MQTT 兼容的通配符匹配（+、*、#）
- ✅ 处理器优先级排序

### 2. CowMap（写时复制映射）

**设计理念**：
- 优化读多写少场景
- 读操作零锁竞争
- 写操作触发完整复制

**实现机制**：
```go
type cowMap struct {
    mu    sync.RWMutex
    items atomic.Value  // 存储 map[interface{}]interface{}
}

// 读操作（无锁）
func (c *cowMap) Load(key interface{}) (interface{}, bool) {
    items := c.items.Load().(map[interface{}]interface{})
    value, ok := items[key]
    return value, ok
}

// 写操作（复制）
func (c *cowMap) Store(key, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    oldMap := c.items.Load().(map[interface{}]interface{})
    newMap := maps.Clone(oldMap)  // 完整复制
    newMap[key] = value
    c.items.Store(newMap)
}
```

**性能特点**：
- 读操作：13.30 ns/op，0 内存分配
- 写操作：适合低频更新场景
- 并发读取：优于 sync.Map 约 20-30%

### 3. Pipe（泛型管道）

**设计目标**：
- 提供类型安全的消息传递
- 支持编译时类型检查
- 极低延迟的性能

**核心结构**：
```go
type Pipe[T any] struct {
    sync.RWMutex
    bufferSize int
    channel    chan T
    handlers   []*HandlerWithPriority[T]
    handlerMap map[uintptr]*HandlerWithPriority[T]
    closed     atomic.Bool
    stopCh     chan struct{}
    timeout    time.Duration
    ctx        context.Context
    cancel     context.CancelFunc
}
```

**性能优势**：
- 同步发布：47.98 ns/op
- 内存开销：8 B/op
- 分配次数：1 次

### 4. Channel（优先级通道）

**职责**：
- 管理订阅者处理器
- 实现优先级排序
- 处理并发派发

**优先级机制**：
```go
type channel struct {
    // 普通处理器（优先级排序）
    handlers []*HandlerWithPriority
    
    // 响应式处理器
    responseHandlers     []*ResponseHandlerWithPriority
    responseMap          map[string]*responseHandler
    
    // 状态管理
    ch     chan any
    closed atomic.Bool
    ctx    context.Context
    cancel context.CancelFunc
}
```

### 5. Filter（智能过滤器）

**功能**：
- 主题阻断
- 频率限流（窗口算法）
- 内容过滤

**实现示例**：
```go
type SmartFilter struct {
    // 限流配置
    limits map[string]*RateLimiter
    window time.Duration
    
    // 阻断列表
    blockedTopics map[string]bool
    mu sync.RWMutex
}
```

### 6. Middleware（中间件）

**能力**：
- 数据转换
- 性能监控
- 日志记录
- 错误拦截

**使用示例**：
```go
type Middleware interface {
    Before(topic string, payload any) any
    After(topic string, payload any)
}
```

### 7. Tracer（事件追踪）

**追踪点**：
- OnPublish：发布事件
- OnSubscribe：订阅事件
- OnUnsubscribe：取消订阅
- OnError：错误发生
- OnComplete：处理完成
- OnQueueFull：队列满
- OnSlowConsumer：慢消费者

---

## 🚀 性能特性

### 性能基准

| 组件 | 操作类型 | 延迟 | 内存 | 分配次数 |
|------|---------|------|------|----------|
| EventBus | 同步发布 | 595 ns/op | 111 B/op | 3 |
| EventBus | 异步发布 | 1,406 ns/op | 128 B/op | 2 |
| Pipe | 同步发布 | 47.98 ns/op | 8 B/op | 1 |
| CowMap | 读操作 | 13.30 ns/op | 0 B/op | 0 |

### 性能优化策略

1. **读优化**
   - 写时复制（COW）机制
   - 原子操作避免锁竞争
   - 无锁读取路径

2. **写优化**
   - 智能容量预分配
   - 相同值更新避免复制
   - 批量操作支持

3. **并发优化**
   - Goroutine 池
   - Channel 通信
   - 上下文控制

---

## 🔐 并发安全设计

### 并发模型

```
发布线程                订阅线程               管理线程
   ↓                      ↓                      ↓
┌────────┐           ┌────────┐            ┌────────┐
│ Publish│           │Handler │            │Subscribe│
└────────┘           └────────┘            └────────┘
     ↓                    ↑                      ↓
     └────→ [Channel] ────┘              [CowMap写锁]
              (无锁读)                     (串行化写)
```

### 锁策略

1. **CowMap**：
   - 读操作：无锁（atomic.Value）
   - 写操作：互斥锁保护

2. **Channel**：
   - 处理器列表：读写锁
   - 消息通道：Go 原生并发安全

3. **EventBus**：
   - 配置修改：读写锁
   - 状态标志：原子操作

### 竞态检测

所有代码通过 `go test -race` 验证，确保无数据竞争。

---

## 📊 数据流

### 事件发布流程

```
1. 应用调用 Publish()
        ↓
2. 过滤器链检查（Filter）
        ↓
3. 中间件前置处理（Before）
        ↓
4. 主题匹配（通配符支持）
        ↓
5. 处理器按优先级排序
        ↓
6. 并发/顺序执行处理器
        ↓
7. 中间件后置处理（After）
        ↓
8. 追踪器记录（OnComplete）
```

### 响应式发布流程

```
1. PublishSyncAll/Any 调用
        ↓
2. Context 创建（带超时）
        ↓
3. 并发执行响应式处理器
        ↓
4. 收集返回值和错误
        ↓
5. 根据策略判断成功/失败
   • All: 全部成功才返回
   • Any: 任一成功即返回
        ↓
6. 返回 SyncResult
```

---

## 🛡️ 错误处理

### 错误分类

1. **参数错误**：
   - ErrHandlerIsNotFunc
   - ErrHandlerParamNum
   - ErrInvalidTopic

2. **状态错误**：
   - ErrChannelClosed
   - ErrEventBusClosed

3. **运行时错误**：
   - ErrPublishTimeout
   - ErrNoSubscriber

### 错误处理策略

```go
// 1. 同步错误：立即返回
if err := bus.Subscribe(topic, handler); err != nil {
    return err
}

// 2. 异步错误：通过 Tracer 报告
tracer.OnError(topic, err)

// 3. 超时错误：Context 控制
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
err := bus.PublishSyncWithContext(ctx, topic, payload)
```

---

## 📈 可观测性

### 健康检查

```go
status := bus.HealthCheck()
// 返回：
// - Healthy: bool
// - Message: string
// - Details: map[string]interface{}
```

### 统计信息

```go
stats := bus.GetStats()
// 包含：
// - SubscriberCount: 订阅者数量
// - TopicCount: 主题数量
// - QueueDepth: 队列深度
// - ProcessedEvents: 已处理事件数
```

### 事件追踪

```go
type MetricsTracer struct {
    publishCount    map[string]int64
    errorCount      map[string]int64
    avgLatency      map[string]time.Duration
    subscribeCount  map[string]int32
}
```

---

## 🎯 使用场景

### 适用场景

✅ **推荐使用**：
- 微服务事件通信
- 实时数据处理
- 状态变更通知
- 用户行为追踪
- 业务流程编排

### 谨慎场景

⚠️ **需要评估**：
- 频繁订阅/取消订阅（CowMap写性能限制）
- 大量动态主题（内存开销）
- 超大消息负载（建议使用消息队列）

---

## 🔄 扩展性

### 插件化架构

```go
// 自定义过滤器
type MyFilter struct{}
func (f *MyFilter) Filter(topic string, payload any) bool {
    // 实现过滤逻辑
}
bus.AddFilter(&MyFilter{})

// 自定义中间件
type MyMiddleware struct{}
func (m *MyMiddleware) Before(topic string, payload any) any {
    // 前置处理
}
bus.Use(&MyMiddleware{})

// 自定义追踪器
type MyTracer struct{}
func (t *MyTracer) OnPublish(topic string, payload any, meta PublishMetadata) {
    // 追踪逻辑
}
bus.SetTracer(&MyTracer{})
```

### 未来扩展方向

- 🔮 分布式事件总线
- 🔮 持久化消息队列
- 🔮 集群模式支持
- 🔮 多传输协议支持

---

## 📚 最佳实践

### 1. 缓冲区配置

```go
// 高吞吐量场景
bus := eventbus.NewBuffered(10000)

// 低延迟场景
bus := eventbus.New()  // 无缓冲

// 平衡配置
bus := eventbus.NewBuffered(1024)
```

### 2. 优先级使用

```go
// 关键业务：高优先级
bus.SubscribeWithPriority("order.payment", criticalHandler, 10)

// 日志记录：低优先级
bus.SubscribeWithPriority("order.payment", logHandler, 1)
```

### 3. Context 管理

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// 同步发布：整个链路受 Context 控制
err := bus.PublishSyncWithContext(ctx, topic, payload)

// 异步发布：仅投递前检查 Context
err := bus.PublishAsyncWithContext(ctx, topic, payload)
```

### 4. 资源清理

```go
// 应用退出时关闭
defer bus.Close()

// 不需要的订阅及时取消
bus.Unsubscribe(topic, handler)
```

---

## 🔗 相关文档

- [API 接口设计](./API接口设计.md) - 详细的 API 说明
- [性能评估报告](./性能评估报告.md) - 性能基准测试
- [项目概述](./项目概述.md) - 项目介绍
- [MQTT 兼容性](./MQTT_COMPATIBILITY.md) - MQTT 通配符支持

---

**文档版本**: v0.2.0  
**最后更新**: 2025-10-16  
**维护者**: EventBus Team
