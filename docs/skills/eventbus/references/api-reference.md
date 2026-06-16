# API 参考（经源码核验）

本文件是 `github.com/darkit/eventbus` 全量公开 API 的最终权威，所有签名、字段、方法名均逐条对照源码核验。SKILL.md 与其他 reference 若与此冲突，以此为准。

> **易错点提醒**：`SyncResult` 用 `FailureCount`，而 `BatchResult`/`PipeBatchResult` 用 `FailedCount`——拼写不同，别混。

## 目录

- [构造与配置](#构造与配置)
- [订阅](#订阅)
- [发布](#发布)
- [响应式发布](#响应式发布)
- [批量发布](#批量发布)
- [结果与元数据类型](#结果与元数据类型)
- [Admin 管理接口](#admin-管理接口)
- [Pipe[T] 泛型管道](#pipet-泛型管道)
- [TopicGroup 分组](#topicgroup-分组)
- [全局单例包级函数](#全局单例包级函数)
- [过滤器 Filter](#过滤器-filter)
- [中间件 Middleware](#中间件-middleware)
- [追踪器 Tracer](#追踪器-tracer)
- [通配符与主题](#通配符与主题)
- [错误类型](#错误类型)
- [处理器签名校验规则](#处理器签名校验规则)

---

## 构造与配置

```go
// 创建事件总线。bufferSize 为 variadic：
//   不传 / 传 0    → 无缓冲（同步交付语义，实时性最高）
//   传负数         → 默认缓冲 runtime.NumCPU()*64
//   传正数        → 指定缓冲
func New(bufferSize ...int) *EventBus

// 动态调整总线级默认超时（影响 PublishSyncAll/Any 等）
func (e *EventBus) SetTimeout(timeout time.Duration)

// 全局默认总线实例（包级单例，见"全局单例"小节）
var DefaultBus = New(defaultBufferSize)

// 常量
const DefaultTimeout = 5 * time.Second          // 默认超时
var   defaultBufferSize = runtime.NumCPU() * 64 // 默认缓冲（包级私有）
```

**关闭**：`func (e *EventBus) Close()` —— 关闭后所有发布返回 `ErrEventBusClosed`，后台 goroutine 退出。务必 `defer bus.Close()`。

---

## 订阅

普通处理器签名**必须**是 `func(topic string, payload any)`（恰好两参，第一参 string，无返回值）。签名不符会返回错误而非 panic，但订阅会静默失败——务必检查返回的 `error`。

```go
func (e *EventBus) Subscribe(topic string, handler any) error                              // 默认优先级 0
func (e *EventBus) SubscribeOnce(topic string, handler any) error                          // 触发一次后自动退订
func (e *EventBus) SubscribeWithPriority(topic string, handler any, priority int) error    // 数字越大越先执行
func (e *EventBus) SubscribeOnceWithPriority(topic string, handler any, priority int) error

func (e *EventBus) SubscribeWithFilter(topic string, handler any, filter EventFilter) error
func (e *EventBus) SubscribeWithFilterAndPriority(topic string, handler any, filter EventFilter, priority int) error

// 响应式：处理器返回 (any, error)，配合 PublishSyncAll/Any 拿返回值
func (e *EventBus) SubscribeWithResponse(topic string, handler ResponseHandler) error
func (e *EventBus) SubscribeWithResponseAndPriority(topic string, handler ResponseHandler, priority int) error
func (e *EventBus) SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error
func (e *EventBus) SubscribeWithResponseContextAndPriority(topic string, handler ResponseHandlerWithContext, priority int) error

// 可靠投递：处理器返回 error 或 panic 时自动重试，耗尽后可进入死信回调
func (e *EventBus) SubscribeReliable(topic string, handler ReliableHandler, opts ...RetryOption) error

type ReliableHandler func(ctx context.Context, topic string, payload any) error
type RetryOption func(*retryConfig)
type BackoffFunc func(attempt int) time.Duration
type DeadLetterHandler func(topic string, payload any, err error)

func WithMaxAttempts(n int) RetryOption
func WithBackoff(b BackoffFunc) RetryOption
func WithRetryIf(f func(error) bool) RetryOption
func WithDeadLetter(h DeadLetterHandler) RetryOption

func ConstantBackoff(d time.Duration) BackoffFunc
func LinearBackoff(start time.Duration) BackoffFunc
func ExponentialBackoff(start, max time.Duration) BackoffFunc

// 取消订阅
func (e *EventBus) Unsubscribe(topic string, handler any) error
func (e *EventBus) UnsubscribeAll(topic string) error
```

**优先级语义**：数字越大优先级越高；同优先级按订阅顺序稳定执行。普通发布只调用普通/可靠处理器；响应式发布只调用响应式处理器，二者分开排序和分发。

**可靠投递默认值**：零配置为 3 次尝试、`ExponentialBackoff(100ms, 1s)`（含 jitter）、默认所有错误可重试。异步路径重试使用 channel 生命周期 `ctx`：`Shutdown` 等待已入队消息尽力完成，`Close` 立即取消。

---

## 发布

```go
// 异步：成功写入 topic channel 后返回；无缓冲/队列满时会等待、超时或响应 ctx 取消
func (e *EventBus) Publish(topic string, payload any) error
func (e *EventBus) PublishWithContext(ctx context.Context, topic string, payload any) error
func (e *EventBus) PublishAsyncWithContext(ctx context.Context, topic string, payload any) error  // PublishWithContext 的显式别名

// 同步：阻塞到所有匹配处理器执行完毕（但仍不返回处理器结果，结果要走响应式 API）
func (e *EventBus) PublishSync(topic string, payload any) error
func (e *EventBus) PublishSyncWithContext(ctx context.Context, topic string, payload any) error
```

---

## 响应式发布

`PublishSyncAll/Any` 不要求目标 topic 必须有响应处理器；无处理器时返回 `SyncResult{HandlerCount:0}`：
`PublishSyncAll` 视为空集合全成功，`PublishSyncAny` 视为无成功处理器。`PublishSyncAnyValue` 因必须返回首个成功值，无处理器时返回 `ErrNoSubscriber`。

```go
// 全部成功才算成功（事务语义）
func (e *EventBus) PublishSyncAll(topic string, payload any) (*SyncResult, error)
func (e *EventBus) PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)

// 任一成功即成功（容错语义）
func (e *EventBus) PublishSyncAny(topic string, payload any) (*SyncResult, error)
func (e *EventBus) PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)

// 更轻量：直接返回首个成功处理器的返回值（不构造完整 SyncResult）
func (e *EventBus) PublishSyncAnyValue(topic string, payload any) (any, error)
func (e *EventBus) PublishSyncAnyValueWithContext(ctx context.Context, topic string, payload any) (any, error)
```

**微妙语义**：当事件被过滤器（`EventFilter`）拦截时，响应式发布返回 `HandlerCount: 0`——**被过滤不算总线错误**。如果你需要"无订阅者/被过滤"也算失败，要单独检查 `res.HandlerCount == 0`。

---

## 批量发布

减少逐条发布的锁与分发开销，按 topic 分组后批量投递。

```go
func (e *EventBus) PublishBatch(messages []BatchMessage) (*BatchResult, error)       // 异步
func (e *EventBus) PublishBatchSync(messages []BatchMessage) (*BatchResult, error)   // 同步

type BatchMessage struct {
    Topic   string
    Payload any
    Context context.Context
}
```

部分失败时返回的 `error` 是 `*BatchError`（其 `.Result` 指向 `BatchResult`），可用 `errors.As` 判定。

---

## 结果与元数据类型

```go
// 响应式同步发布结果（PublishSyncAll/Any）—— 注意是 FailureCount
type SyncResult struct {
    Success      bool            // 整体是否成功
    HandlerCount int             // 匹配到的处理器总数
    SuccessCount int
    FailureCount int             // ← FailureCount（不是 FailedCount）
    Results      []HandlerResult
    TotalTime    time.Duration
}

type HandlerResult struct {
    HandlerID string
    Success   bool
    Result    any            // 处理器返回值（响应式处理器才有）
    Error     error
    Duration  time.Duration
}

// 批量发布结果 —— 注意是 FailedCount
type BatchResult struct {
    Results      []BatchItemResult
    SuccessCount int
    FailedCount  int             // ← FailedCount（和 SyncResult 拼写不同！）
    FirstError   error
}

type BatchItemResult struct {
    Topic        string
    Err          error
    Duration     time.Duration
    HandlerCount int
    Skipped      bool            // 被过滤器跳过
}

type BatchError struct {
    Result *BatchResult
}
```

---

## Admin 管理接口

```go
func (e *EventBus) Close()                                // 立即关闭，不保证排空队列
func (e *EventBus) Shutdown(ctx context.Context) error     // 拒绝新发布，排空已接收/已入队/处理中消息后关闭
func (e *EventBus) HealthCheck() error                    // 健康检查（已关闭则报错）
func (e *EventBus) GetStats() map[string]any              // 运行统计快照
func (e *EventBus) GetTopics() []string                   // 所有已注册 topic
func (e *EventBus) GetSubscriberCount(topic string) (int, error)
func (e *EventBus) HasSubscribers(topic string) bool
```

---

## Pipe[T] 泛型管道

单数据流、类型安全、无 topic 概念。处理器签名是**单参** `func(payload T)`，与 EventBus 的两参处理器完全不同。

### 构造

```go
func NewPipe[T any]() *Pipe[T]                                            // 无缓冲
func NewBufferedPipe[T any](bufferSize int) *Pipe[T]                      // 带缓冲
func NewPipeWithTimeout[T any](timeout time.Duration) *Pipe[T]            // 无缓冲 + 自定义超时
func NewBufferedPipeWithTimeout[T any](bufferSize int, timeout time.Duration) *Pipe[T]
```

### 处理器类型

```go
type Handler[T any]                             func(payload T)
type PipeResponseHandler[T any]                 func(payload T) (any, error)
type PipeResponseHandlerWithContext[T any]      func(ctx context.Context, payload T) (any, error)
type PipeResponseCancel                         func()   // 用于撤销响应式订阅
```

### 订阅

```go
func (p *Pipe[T]) Subscribe(handler Handler[T]) error
func (p *Pipe[T]) SubscribeOnce(handler Handler[T]) error
func (p *Pipe[T]) SubscribeWithPriority(handler Handler[T], priority int) error
func (p *Pipe[T]) SubscribeOnceWithPriority(handler Handler[T], priority int) error
func (p *Pipe[T]) SubscribeWithOptions(handler Handler[T], opts ...SubscribeOption) error

// 响应式（返回取消句柄，调用即退订）
func (p *Pipe[T]) SubscribeWithResponse(handler PipeResponseHandler[T]) (PipeResponseCancel, error)
func (p *Pipe[T]) SubscribeWithResponseAndPriority(handler PipeResponseHandler[T], priority int) (PipeResponseCancel, error)
func (p *Pipe[T]) SubscribeWithResponseContext(handler PipeResponseHandlerWithContext[T]) error
func (p *Pipe[T]) SubscribeWithResponseContextAndPriority(handler PipeResponseHandlerWithContext[T], priority int) error
func (p *Pipe[T]) SubscribeWithResponseContextHandle(handler PipeResponseHandlerWithContext[T]) (PipeResponseCancel, error)
func (p *Pipe[T]) SubscribeWithResponseContextHandleAndPriority(handler PipeResponseHandlerWithContext[T], priority int) (PipeResponseCancel, error)

func (p *Pipe[T]) Unsubscribe(handler Handler[T]) error
func (p *Pipe[T]) UnsubscribeByID(customID string) error
```

### 发布

```go
func (p *Pipe[T]) Publish(payload T) error
func (p *Pipe[T]) PublishWithContext(ctx context.Context, payload T) error
func (p *Pipe[T]) PublishSync(payload T) error
func (p *Pipe[T]) PublishBatch(payloads []T) (*PipeBatchResult, error)
func (p *Pipe[T]) PublishBatchSync(payloads []T) (*PipeBatchResult, error)

// 响应式
func (p *Pipe[T]) PublishSyncAll(payload T) (*PipeSyncResult, error)
func (p *Pipe[T]) PublishSyncAny(payload T) (*PipeSyncResult, error)
func (p *Pipe[T]) PublishSyncAllWithContext(ctx context.Context, payload T) (map[string]any, error)          // 返回结果 map
func (p *Pipe[T]) PublishSyncAllResultWithContext(ctx context.Context, payload T) (*PipeSyncResult, error)   // 返回完整结果
func (p *Pipe[T]) PublishSyncAnyWithContext(ctx context.Context, payload T) (any, error)
func (p *Pipe[T]) PublishSyncAnyResultWithContext(ctx context.Context, payload T) (*PipeSyncResult, error)
```

### 管理 / 结果

```go
func (p *Pipe[T]) Close()
func (p *Pipe[T]) GetStats() map[string]any

type PipeSyncResult struct {     // 字段同 SyncResult，FailureCount
    Success      bool
    HandlerCount int
    SuccessCount int
    FailureCount int
    Results      []PipeHandlerResult
    TotalTime    time.Duration
}
type PipeHandlerResult struct {
    HandlerID string
    Success   bool
    Result    any
    Error     error
    Duration  time.Duration
}
type PipeBatchResult struct {    // ← FailedCount
    Count        int
    SuccessCount int
    FailedCount  int
    FirstError   error
}

// 订阅选项（SubscribeWithOptions 用）
type SubscribeOption func(*subscribeOptions)
func WithPriority(priority int) SubscribeOption      // 设置优先级
func WithHandlerID(id string) SubscribeOption         // 自定义 ID，用于闭包去重（同一闭包重复订阅默认会被去重）
```

---

## TopicGroup 分组

实现 topic 前缀隔离（多租户 / 命名空间）。`NewGroup` 是**唯一**入口——不存在 `bus.Group()` 方法（旧资料写错了）。

```go
func (e *EventBus) NewGroup(prefix string) *TopicGroup

// 组内会自动把 prefix 拼到 topic 前（用标准分隔符 . 连接）
func (g *TopicGroup) NewSubGroup(prefix string) *TopicGroup            // 嵌套子组
func (g *TopicGroup) Prefix() string

// 发布
func (g *TopicGroup) Publish(topic string, payload any) error
func (g *TopicGroup) PublishSync(topic string, payload any) error
func (g *TopicGroup) PublishWithContext(ctx context.Context, topic string, payload any) error
func (g *TopicGroup) PublishSyncWithContext(ctx context.Context, topic string, payload any) error
func (g *TopicGroup) PublishSyncAll(topic string, payload any) (*SyncResult, error)
func (g *TopicGroup) PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)
func (g *TopicGroup) PublishSyncAny(topic string, payload any) (*SyncResult, error)
func (g *TopicGroup) PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)
func (g *TopicGroup) PublishSyncAnyValue(topic string, payload any) (any, error)
func (g *TopicGroup) PublishSyncAnyValueWithContext(ctx context.Context, topic string, payload any) (any, error)

// 订阅
func (g *TopicGroup) Subscribe(topic string, handler any) error
func (g *TopicGroup) SubscribeWithPriority(topic string, handler any, priority int) error
func (g *TopicGroup) SubscribeWithResponse(topic string, handler ResponseHandler) error
func (g *TopicGroup) SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error
func (g *TopicGroup) SubscribeWithFilter(topic string, handler any, filter EventFilter) error

// 管理
func (g *TopicGroup) Unsubscribe(topic string, handler any) error
func (g *TopicGroup) UnsubscribeAll(topic string) error
func (g *TopicGroup) GetSubscriberCount(topic string) (int, error)
func (g *TopicGroup) HasSubscribers(topic string) bool
```

---

## 全局单例包级函数

`eventbus` 包提供一组包级函数，直接操作 `DefaultBus`，省去手动管理实例。适合简单场景与脚本；**测试用 `ResetSingleton()` 重置**避免用例间污染。

```go
var DefaultBus = New(defaultBufferSize)

func Subscribe(topic string, handler any) error
func SubscribeWithPriority(topic string, handler any, priority int) error
func SubscribeOnce(topic string, handler any) error
func SubscribeOnceWithPriority(topic string, handler any, priority int) error
func SubscribeWithResponse(topic string, handler ResponseHandler) error
func SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error
func SubscribeReliable(topic string, handler ReliableHandler, opts ...RetryOption) error
func Unsubscribe(topic string, handler any) error
func UnsubscribeAll(topic string) error
func Publish(topic string, payload any) error
func PublishSync(topic string, payload any) error
func PublishWithContext(ctx context.Context, topic string, payload any) error
func PublishAsyncWithContext(ctx context.Context, topic string, payload any) error
func PublishSyncWithContext(ctx context.Context, topic string, payload any) error
func PublishSyncAll(topic string, payload any) (*SyncResult, error)
func PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)
func PublishSyncAny(topic string, payload any) (*SyncResult, error)
func PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error)
func PublishSyncAnyValue(topic string, payload any) (any, error)
func PublishSyncAnyValueWithContext(ctx context.Context, topic string, payload any) (any, error)
func AddFilter(filter EventFilter)
func SetTracer(tracer EventTracer)
func Use(middleware IMiddleware)
func GetStats() map[string]any
func NewGroup(prefix string) *TopicGroup
func Close()
func Shutdown(ctx context.Context) error
func HealthCheck() error
func ResetSingleton()   // 测试专用：重置单例
```

---

## 过滤器 Filter

```go
type EventFilter interface {
    Filter(topic string, payload any) bool   // true 放行，false 拦截
}

// 函数式适配器，便于直接传函数
type FilterFunc func(topic string, payload any) bool

// 内置实现：限流 + 主题阻断
func NewSmartFilter() *SmartFilter   // 默认窗口 1 分钟

func (f *SmartFilter) SetLimit(topic string, limit int)    // 窗口内最大可通过次数；limit<=0 清除
func (f *SmartFilter) BlockTopic(topic string)             // 阻断该 topic 及其子层级（前缀匹配 blocked+"."）
func (f *SmartFilter) UnblockTopic(topic string)
func (f *SmartFilter) SetWindow(window time.Duration)      // 调整限流窗口
func (f *SmartFilter) StartCleanup(interval time.Duration) // 启动后台清理过期计数器
func (f *SmartFilter) Stop()                               // 停止后台清理

func (e *EventBus) AddFilter(filter EventFilter)
```

> `SetLimit` / `BlockTopic` 内部会 `normalizeTopic`，所以传 `order/create` 和 `order.create` 等价。

---

## 中间件 Middleware

```go
type IMiddleware interface {
    Before(topic string, payload any) any   // 处理前调用，可改写 payload（返回值作为新 payload）
    After(topic string, payload any)        // 处理后调用
}

type MiddlewareFunc struct {                // 函数式适配器
    BeforeFn func(topic string, payload any) any
    AfterFn  func(topic string, payload any)
}

func NewMiddleware() *Middleware
func (m *Middleware) SetTransformer(fn func(topic string, payload any) any)  // 设置全局 payload 转换
func (m *Middleware) GetStats() map[string]TopicStat                        // 性能统计快照
func (m *Middleware) Reset()

type TopicStat struct {
    Count     int
    TotalTime time.Duration
}

func (e *EventBus) Use(middleware IMiddleware)
```

---

## 追踪器 Tracer

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

// 内置指标收集实现
func NewMetricsTracer() *MetricsTracer   // 默认慢消费阈值 1s
func (m *MetricsTracer) GetMetrics() map[string]any
func (m *MetricsTracer) GetQueueMetrics() map[string]map[string]int64
func (m *MetricsTracer) GetLatencyMetrics() map[string]map[string]map[string]any

func (e *EventBus) SetTracer(tracer EventTracer)

type PublishMetadata struct {
    Timestamp   time.Time
    Async       bool
    QueueSize   int
    PublisherID string
}
type CompleteMetadata struct {
    StartTime      time.Time
    EndTime        time.Time
    ProcessingTime time.Duration
    HandlerCount   int
    Success        bool
}
```

---

## 通配符与主题

```go
var TopicSeparators = []string{".", "/"}   // 支持的两种分隔符，内部统一标准化为 "."
```

- `+` 或 `*`：匹配**单层**。
- `#`：匹配**多层**，且必须在**末尾**。
- 分隔符 `.` 与 `/` 等价，可混用（`order.create/paid` → 规范化为 `order.create.paid`）。
- 空 topic / 非法格式返回 `ErrInvalidTopic`。

---

## 错误类型

```go
var (
    ErrHandlerIsNotFunc     = errors.New("handler is not a function")
    ErrHandlerParamNum      = errors.New("handler must have exactly two parameters")
    ErrHandlerFirstParam    = errors.New("handler's first parameter must be string")
    ErrHandlerReturnNum     = errors.New("handler must not return values")
    ErrResponseReturnNum    = errors.New("response handler must return (any, error)")
    ErrResponseReturnType   = errors.New("response handler second return value must be error")
    ErrNoSubscriber         = errors.New("no subscriber found for topic")
    ErrChannelClosed        = errors.New("channel is closed")
    ErrPublishTimeout       = errors.New("publish operation timed out")
    ErrInvalidTopic         = errors.New("invalid topic format")
    ErrEventBusClosed       = errors.New("event bus is closed")
    ErrShuttingDown         = errors.New("event bus is shutting down")
    ErrDuplicateHandler     = errors.New("duplicate handler ID")
)

// 用附加上下文包装错误（保持 %w 链）
func WrapError(err error, format string, args ...any) error
```

---

## 处理器签名校验规则

订阅时反射校验，不符即返回上表错误（订阅静默失败，**务必检查 error**）：

| 载体 | 普通处理器 | 响应式处理器 |
|---|---|---|
| **EventBus / TopicGroup** | `func(topic string, payload any)`（两参，无返回） | `func(topic string, payload any) (any, error)` 或带 `ctx` 首参 |
| **Pipe[T]** | `func(payload T)`（单参，无返回） | `func(payload T) (any, error)` 或带 `ctx` 首参 |

判定要点：EventBus 处理器第一参是 topic 字符串；Pipe 处理器没有 topic（Pipe 只有一条数据流）。
