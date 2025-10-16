# EventBus API接口设计文档

## 设计原则

### 1. 简洁性原则
- API接口设计简洁明了，易于理解和使用
- 避免过度设计，保持核心功能的纯粹性
- 提供合理的默认值，减少配置复杂度

### 2. 一致性原则
- 命名规范统一，遵循Go语言惯用法
- 错误处理方式一致
- 参数顺序和类型保持一致性

### 3. 扩展性原则
- 接口设计考虑未来扩展需求
- 使用接口抽象，支持不同实现
- 保持向后兼容性

### 4. 性能原则
- 关键路径优化，减少不必要的开销
- 支持并发安全，避免锁竞争
- 内存使用高效，避免内存泄漏

## 核心接口设计

### EventBus 主接口

```go
// EventBus 事件总线主接口
type EventBus struct {
    bufferSize  int                    // 缓冲区大小
    channels    *cowMap              // 通道映射表
    timeout     time.Duration         // 默认超时时间
    filters     []EventFilter         // 事件过滤器列表
    middlewares []Middleware          // 中间件列表
    tracer      EventTracer           // 事件追踪器
    lock        sync.RWMutex          // 读写锁
    closed      atomic.Bool           // 关闭状态
}
```

#### 构造函数
```go
// New 创建无缓冲事件总线
func New() *EventBus

// NewBuffered 创建带缓冲的事件总线
func NewBuffered(bufferSize int) *EventBus
```

**设计说明**:
- `New()` 创建无缓冲事件总线，适合实时性要求高的场景
- `NewBuffered()` 创建带缓冲的事件总线，适合高吞吐量场景
- 缓冲区大小为0或负数时，自动设置为默认值

#### 订阅接口
```go
// Subscribe 订阅主题
func (e *EventBus) Subscribe(topic string, handler any) error

// SubscribeWithPriority 带优先级订阅
func (e *EventBus) SubscribeWithPriority(topic string, handler any, priority int) error

// SubscribeWithResponse 响应式订阅（返回值 + 错误）
func (e *EventBus) SubscribeWithResponse(topic string, handler ResponseHandler) error

// SubscribeWithResponseContext 响应式订阅（支持 context 透传）
func (e *EventBus) SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error

// Unsubscribe 取消订阅
func (e *EventBus) Unsubscribe(topic string, handler any) error

// UnsubscribeAll 取消主题的所有订阅
func (e *EventBus) UnsubscribeAll(topic string) error
```

**设计说明**:
- `handler` 参数使用 `any` 类型，支持任意函数签名
- 普通订阅处理器签名固定为 `(topic string, payload any)`（可选附加 context）
- 响应式处理器需要返回 `(any, error)`，便于同步发布聚合结果
- 优先级数值越大，优先级越高
- 取消订阅需要传入相同的处理器函数引用

#### 发布接口
```go
// Publish 异步发布事件
func (e *EventBus) Publish(topic string, payload any) error

// PublishSync 同步发布事件
func (e *EventBus) PublishSync(topic string, payload any) error

// PublishWithContext 带上下文异步发布
func (e *EventBus) PublishWithContext(ctx context.Context, topic string, payload any) error

// PublishAsyncWithContext 带上下文异步发布（显式）
func (e *EventBus) PublishAsyncWithContext(ctx context.Context, topic string, payload any) error

// PublishSyncWithContext 带上下文同步发布
func (e *EventBus) PublishSyncWithContext(ctx context.Context, topic string, payload any) error

// PublishSyncAll 响应式同步发布（全部成功）
func (e *EventBus) PublishSyncAll(topic string, payload any) (*SyncResult, error)

// PublishSyncAllWithContext 响应式同步发布（全部成功，透传 context）
func (e *EventBus) PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)

// PublishSyncAny 响应式同步发布（任一成功）
func (e *EventBus) PublishSyncAny(topic string, payload any) (*SyncResult, error)

// PublishSyncAnyWithContext 响应式同步发布（任一成功，透传 context）
func (e *EventBus) PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)
```

**设计说明**:
- `Publish` 异步非阻塞发布，适合高并发场景
- `PublishSync` 同步阻塞发布，适合需要即时反馈的场景
- `PublishWithContext`/`PublishAsyncWithContext` 在投递前尊重 `ctx`（超时/取消），处理器会异步执行
- `PublishSyncWithContext` 在整个处理链中尊重 `ctx`，适合需要可取消的同步工作流
- 响应式同步接口汇总多个处理器返回值；当 `ctx` 未带超时时，会自动叠加默认超时（5 秒）以避免无限等待
- `PublishSyncAny` 在任意处理器成功后立即取消剩余处理器，可降低平均延迟

#### 管理接口
```go
// AddFilter 添加事件过滤器
func (e *EventBus) AddFilter(filter EventFilter)

// Use 添加中间件
func (e *EventBus) Use(middleware Middleware)

// SetTracer 设置事件追踪器
func (e *EventBus) SetTracer(tracer EventTracer)

// NewGroup 基于前缀创建主题组
func (e *EventBus) NewGroup(prefix string) *TopicGroup

// Close 关闭事件总线
func (e *EventBus) Close()

// HealthCheck 健康检查
func (e *EventBus) HealthCheck() error

// GetStats 获取统计信息
func (e *EventBus) GetStats() map[string]interface{}
```

### 泛型管道接口

```go
// Pipe 泛型管道，提供类型安全的消息传递
type Pipe[T any] struct {
    sync.RWMutex
    bufferSize int                              // 缓冲区大小
    channel    chan T                           // 消息通道
    handlers   []*HandlerWithPriority[T]        // 处理器列表
    handlerMap map[uintptr]*HandlerWithPriority[T] // 处理器映射
    closed     atomic.Bool                      // 关闭状态
    stopCh     chan struct{}                    // 停止信号
    timeout    time.Duration                   // 超时时间
    ctx        context.Context                 // 上下文
    cancel     context.CancelFunc              // 取消函数
}

// PipeResponseCancel 响应式处理器取消函数
type PipeResponseCancel func()
```

#### 构造函数
```go
// NewPipe 创建无缓冲管道
func NewPipe[T any]() *Pipe[T]

// NewPipeWithTimeout 创建无缓冲管道并自定义超时
func NewPipeWithTimeout[T any](timeout time.Duration) *Pipe[T]

// NewBufferedPipe 创建带缓冲的管道
func NewBufferedPipe[T any](bufferSize int) *Pipe[T]

// NewBufferedPipeWithTimeout 创建带缓冲管道并自定义超时
func NewBufferedPipeWithTimeout[T any](bufferSize int, timeout time.Duration) *Pipe[T]
```

#### 核心方法
```go
// Subscribe 订阅消息
func (p *Pipe[T]) Subscribe(handler Handler[T]) error

// SubscribeWithPriority 带优先级订阅
func (p *Pipe[T]) SubscribeWithPriority(handler Handler[T], priority int) error

// Publish 异步发布消息
func (p *Pipe[T]) Publish(payload T) error

// PublishSync 同步发布消息
func (p *Pipe[T]) PublishSync(payload T) error

// PublishWithContext 带上下文发布
func (p *Pipe[T]) PublishWithContext(ctx context.Context, payload T) error

// Close 关闭管道
func (p *Pipe[T]) Close()

// GetStats 获取统计信息
func (p *Pipe[T]) GetStats() map[string]interface{}

// SubscribeWithResponse 注册支持返回值的处理器，返回取消函数
func (p *Pipe[T]) SubscribeWithResponse(handler PipeResponseHandler[T]) (PipeResponseCancel, error)

// SubscribeWithResponseAndPriority 带优先级的响应式处理
func (p *Pipe[T]) SubscribeWithResponseAndPriority(handler PipeResponseHandler[T], priority int) (PipeResponseCancel, error)
```

### 接口抽象

#### EventFilter 事件过滤器接口与扩展类型
```go
type EventFilter interface {
    Filter(topic string, payload any) bool
}

// FilterFunc 允许使用函数快速实现 EventFilter
type FilterFunc func(topic string, payload any) bool

// SmartFilter 提供主题阻断 + 窗口限流能力
type SmartFilter struct {
    SetLimit(topic string, limit int)
    SetWindow(window time.Duration)
    BlockTopic(topic string)
    UnblockTopic(topic string)
}
```

**使用示例**:
```go
// 阻止包含 "internal" 的主题
bus.AddFilter(eventbus.FilterFunc(func(topic string, payload any) bool {
    return !strings.Contains(topic, "internal")
}))

// 针对登录事件设置限流与主题阻断
smart := eventbus.NewSmartFilter()
smart.SetLimit("user.login", 120)
smart.BlockTopic("internal.sandbox")
bus.AddFilter(smart)
```

#### Middleware 中间件接口
```go
// Middleware 定义事件处理中间件接口
type Middleware interface {
    // Before 在事件处理前执行，可以修改payload
    Before(topic string, payload any) any
    // After 在事件处理后执行
    After(topic string, payload any)
}
```

**使用示例**:
```go
middleware := eventbus.NewMiddleware()

middleware.SetTransformer(func(topic string, payload any) any {
    if msg, ok := payload.(string); ok {
        return strings.TrimSpace(msg)
    }
    return payload
})

bus.Use(middleware)

stats := middleware.GetStats()
for topic, stat := range stats {
    avg := stat.TotalTime / time.Duration(stat.Count)
    log.Printf("%s: 次数=%d, 平均耗时=%v", topic, stat.Count, avg)
}

middleware.Reset()
```

#### EventTracer 事件追踪接口
```go
// EventTracer 定义事件追踪接口
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

## 全局单例接口

```go
// 全局单例函数，简化使用
func Subscribe(topic string, handler any) error
func SubscribeWithPriority(topic string, handler any, priority int) error
func SubscribeWithResponse(topic string, handler ResponseHandler) error
func SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error
func Unsubscribe(topic string, handler any) error
func UnsubscribeAll(topic string) error
func Publish(topic string, payload any) error
func PublishSync(topic string, payload any) error
func PublishSyncAll(topic string, payload any) (*SyncResult, error)
func PublishSyncAny(topic string, payload any) (*SyncResult, error)
func PublishWithContext(ctx context.Context, topic string, payload any) error
func PublishSyncWithContext(ctx context.Context, topic string, payload any) error
func PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)
func PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)
func AddFilter(filter EventFilter)
func SetTracer(tracer EventTracer)
func Use(middleware Middleware)
func GetStats() map[string]interface{}
func NewGroup(prefix string) *TopicGroup
func Close()
func HealthCheck() error
```

## 错误处理设计

### 错误类型定义
```go
var (
    ErrHandlerIsNotFunc  = errors.New("处理器必须是一个函数")
    ErrHandlerParamNum   = errors.New("处理器必须有且仅有两个参数")
    ErrHandlerFirstParam = errors.New("处理器的第一个参数必须是字符串类型")
    ErrNoSubscriber      = errors.New("主题没有找到订阅者")
    ErrChannelClosed     = errors.New("通道已关闭")
    ErrPublishTimeout    = errors.New("发布操作超时")
    ErrInvalidTopic      = errors.New("主题格式无效")
    ErrEventBusClosed    = errors.New("事件总线已关闭")
)
```

### 错误处理策略
1. **参数验证错误**: 在调用时立即返回，不影响系统运行
2. **运行时错误**: 通过Tracer接口报告，不中断事件处理
3. **资源错误**: 返回明确的错误信息，便于问题定位
4. **超时错误**: 支持上下文取消，避免无限等待

## 主题匹配设计

### 通配符支持
- `*`: 匹配单个层级，如 `user.*` 匹配 `user.login`、`user.logout`
- `#`: 匹配多个层级，如 `system.#` 匹配 `system.cpu.high`、`system.memory.low`

### 主题分隔符
- 支持 `.` 和 `/` 作为主题分隔符
- 内部统一转换为 `.` 分隔符处理
- 保持与MQTT主题格式的兼容性

### 匹配算法
```go
// matchTopic 检查主题是否匹配通配符模式
func matchTopic(pattern, topic string) bool {
    // 统一分隔符
    pattern = normalizeTopic(pattern)
    topic = normalizeTopic(topic)
    
    // 处理多层通配符 #
    if pattern == "#" {
        return true
    }
    
    // 分割并匹配各部分
    patternParts := strings.Split(pattern, ".")
    topicParts := strings.Split(topic, ".")
    
    return matchParts(patternParts, topicParts)
}
```

## 性能优化设计

### 1. 写时复制(COW)优化
```go
// cowMap 实现写时复制的并发安全映射
type cowMap struct {
    mu    sync.RWMutex
    items atomic.Value
}
```

### 2. 原子操作优化
```go
// 使用原子操作避免锁竞争
type channel struct {
    closed atomic.Bool  // 原子布尔值
    // ...
}
```

### 3. 内存池优化
```go
// 对象复用，减少GC压力
var handlerInfoPool = sync.Pool{
    New: func() interface{} {
        return &HandlerInfo{}
    },
}
```

## 扩展性设计

### 1. 插件化架构
- 过滤器和中间件支持链式组合
- 追踪器支持多种实现
- 传输层支持不同协议

### 2. 配置化支持
```go
// Config 配置结构
type Config struct {
    BufferSize       int           // 缓冲区大小
    DefaultTimeout   time.Duration // 默认超时时间
    MaxSubscribers   int           // 最大订阅者数量
    EnableTracing    bool          // 是否启用追踪
    EnableMetrics    bool          // 是否启用指标收集
}
```

### 3. 分布式扩展预留
```go
// Transport 传输层接口（预留）
type Transport interface {
    Send(topic string, payload []byte) error
    Receive() (<-chan Message, error)
    Close() error
}
```

## API版本兼容性

### 版本策略
- 遵循语义化版本控制(SemVer)
- 主版本号变更表示不兼容的API变更
- 次版本号变更表示向后兼容的功能新增
- 修订版本号变更表示向后兼容的问题修复

### 兼容性保证
- 公开API保持向后兼容
- 废弃的API提供迁移指南
- 新功能通过可选参数或新方法提供
- 重大变更提供平滑的迁移路径

## 使用示例

### 基本使用
```go
// 创建事件总线
bus := eventbus.NewBuffered(1024)
defer bus.Close()

// 订阅事件
bus.Subscribe("user.created", func(topic string, payload any) {
    user := payload.(User)
    fmt.Printf("用户创建: %s\n", user.Name)
})

// 发布事件
bus.Publish("user.created", User{Name: "张三"})
```

### 高级使用
```go
// 带优先级和上下文的使用
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

bus.SubscribeWithPriority("order.created", highPriorityHandler, 10)
bus.SubscribeWithPriority("order.created", normalHandler, 1)

err := bus.PublishWithContext(ctx, "order.created", order)
if err != nil {
    log.Printf("发布失败: %v", err)
}
```

### 泛型管道使用
```go
// 类型安全的消息传递
pipe := eventbus.NewBufferedPipe[OrderEvent](100)
defer pipe.Close()

pipe.Subscribe(func(event OrderEvent) {
    fmt.Printf("订单事件: %+v\n", event)
})

pipe.Publish(OrderEvent{ID: "123", Status: "created"})
```
