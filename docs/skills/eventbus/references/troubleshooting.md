# 诊断指南

按"症状 → 根因 → 定位 → 修复 → 预防"组织。所有 API 引用见 [`api-reference.md`](./api-reference.md)。

## 0. 先备好的排障工具

| 工具 | 用途 |
|---|---|
| `go test -race ./...` | 数据竞争（订阅/发布并发、处理器共享态） |
| `go test -bench=. -benchmem` | 性能基线 + 分配 |
| `bus.GetStats()` | 运行时统计快照 |
| `MetricsTracer.GetMetrics()` / `GetQueueMetrics()` / `GetLatencyMetrics()` | 消息数、错误数、队列、延迟、慢消费 |
| `bridge.Stats()`（`BridgeStats`） | 跨节点：丢弃、去重、编解码错误、队列长度 |
| `go tool pprof` | CPU / goroutine / heap profile |
| `delve (dlv)` | 死锁现场断点 |

## 1. 死锁（卡住不动）

**症状**：`PublishSync` / `PublishSyncAll` 永不返回，或整个进程 hang。

**根因**：同步分发在处理器内又**同步发布同一 topic**，形成等待环。
```go
bus.Subscribe("t", func(topic string, p any) {
    bus.PublishSync("t", p)   // ❌ 等自己分发完，自己又在等自己
})
```

**定位**：`dlv` attach，看 goroutine 栈是否有 eventbus 分发栈递归；`go tool pprof` goroutine profile 看大量 goroutine 阻塞在同一锁。

**修复**：处理器内改用异步 `Publish`，或发到**不同 topic**。需要结果聚合就用 `PublishSyncAll` 把多个订阅器挂同一 topic，而不是嵌套同步发布。

**预防**：记住"处理器内只异步发布"。

## 2. goroutine / 内存泄漏

**症状**：进程 RSS 持续上涨，或 goroutine 数（`runtime.NumGoroutine()`）只增不减。

**根因**：`New` / `NewPipe` 创建后**没 `Close`**。每个总线/Pipe 有后台 goroutine 与 channel，不关就永久存活。

**定位**：`go tool pprof http://.../debug/pprof/goroutine`，看大量 `eventbus.(*EventBus)` / `(*Pipe[T]).loop` 栈。

**修复**：每个 `New` 配 `defer bus.Close()`，每个 `NewPipe*` 配 `defer pipe.Close()`。Bridge 配 `defer bridge.Close()`。

**预防**：把 `Close` 当成构造的一部分，写完 `New` 立刻补 `defer`。

## 3. 发布超时（ErrPublishTimeout）

**症状**：`Publish` 返回 `ErrPublishTimeout`，或 `PublishSyncAll` 超时。

**根因**：topic 的 channel 缓冲满 + 处理器消费慢（慢消费者），发布方阻塞直到超时。

**定位**：`MetricsTracer.OnSlowConsumer` 回调；`GetQueueMetrics()` 看队列堆积；`OnQueueFull` 回调。

**修复**：
- 调大缓冲：`eventbus.New(更大的值)` 或 `NewBufferedPipe[T](更大)`
- 处理器内把重活丢 goroutine / worker pool，别在处理器里阻塞
- 真的需要背压就接受超时并降级

**预防**：处理器要"快进快出"；长耗时操作异步化。

## 4. Panic（类型断言 / 处理器崩溃）

**症状**：进程崩溃，或 `PublishSyncAll` 的 `HandlerResult.Error` 里出现 panic 信息。

**根因**：
- `payload.(T)` 裸断言，类型不符即 panic
- 处理器内访问 nil / 越界

**定位**：panic 栈。响应式路径下 `PublishSyncAll` 会 `recover` 并把 panic 塞进 `HandlerResult.Error`（见 `examples/response`），但**异步 `Publish` 的处理器 panic 不一定被兜住**，取决于分发实现。

**修复**：用 `v, ok := payload.(T)` 安全断言；或改用 `Pipe[T]` 从源头消除 `any`。处理器内对不可信数据防御性编程。

**预防**：同类型流一律用 `Pipe[T]`；必须用 `any` 时永远 `ok` 断言。

## 5. 性能差

**症状**：吞吐远低于预期（异步理论上 ~1.6M msg/s，Pipe ~20M msg/s 量级）。

**根因与修复**：

| 原因 | 修复 |
|---|---|
| 缓冲太小，频繁阻塞 | 按并发量调大缓冲（高并发 1024+） |
| 通配符 `#` 滥用，匹配范围爆炸 | 热点 topic 用精确匹配，`#` 仅用于低频监控 |
| 慢消费者拖慢分发 | 处理器异步化（见问题 3） |
| 高频 `any` 装箱/断言开销 | 同类型流换 `Pipe[T]` |
| 每条都 `New` 一个 bus | 复用实例 / 用全局单例 |

**定位**：`go test -bench -benchmem` 对比；`MetricsTracer` 延迟分桶看 p99。

## 6. 消息"丢失"（其实多半是时序或 topic 问题）

**症状**：发布了，但处理器没收到。

**根因**：
- 用异步 `Publish` 后**立即退出进程**，处理器还没跑
- topic 无订阅者，`Publish` 返回 `ErrNoSubscriber` 被忽略
- 通配符订阅与发布 topic 不匹配（如订阅 `sensor/+/temp` 却发布 `sensor/room1/temp/extra`）
- `SmartFilter` 把 topic 阻断了（含子层级）

**修复**：退出前用 `PublishSync` 确保处理完成；检查 `Publish` 返回的 `error`；核对通配符规则（见 SKILL.md）；排查 filter。

**预防**：关键路径用同步发布并检查错误。

## 7. 订阅"没生效"（静默失败）

**症状**：`Subscribe` 后发布，处理器从不触发，但也没报错。

**根因**：处理器签名不符，`Subscribe` 返回了 error 但被忽略。常见错误签名见 [`api-reference.md#处理器签名校验规则`](./api-reference.md#处理器签名校验规则)。

**修复**：检查并处理 `Subscribe` 的返回 error；对照签名规则修正。

**预防**：**永远不忽略 `Subscribe` 返回值**。

## 8. 跨节点（adapter）问题

详见 [`adapter-bridge.md`](./adapter-bridge.md) 的语义边界。

| 症状 | 根因 | 修复 |
|---|---|---|
| 消费者收到重复消息 | at-least-once 投递 | 应用层按 `Message.ID` / 业务键幂等 |
| 自己发的事件又被自己处理 | 防回环未生效（Origin 空？） | 确认 `WithNodeID` 固定；检查 transport 是否保留 Origin |
| `DecodeErrors` 高 | JSONCodec 类型不匹配 / `json.Number` | 核对两端 payload 类型；数字用 `json.Number` 兼容 |
| `OutboundDropped` 增长 | 出站队列满 | 调 `WithOutboundBuffer` / `WithOutboundWorkers` |
| `ErrWildcardUnsupported` | transport 不支持 wildcard 却订了通配符 | 换支持 wildcard 的 transport，或订阅精确 topic |
| `Start` 报错 | Bridge 已启动 / pattern 非法 | Bridge 只能 Start 一次；pattern 不能为空 |
| 跨节点 SyncAll 不工作 | v1 **不支持**跨节点响应式 | 改 best-effort 异步 + 应用层编排 |

**定位**：`bridge.Stats()` 看各计数器；接 `WithErrorHandler` 打日志。

## 快速决策树

```
卡住不动              → 死锁（问题 1）：查处理器内是否同步发布同 topic
内存/goroutine 涨     → 泄漏（问题 2）：查 Close
返回 ErrPublishTimeout→ 超时（问题 3）：查缓冲 + 慢消费者
进程崩溃 / Error 有 panic → 问题 4：查类型断言
吞吐低                → 性能（问题 5）：查缓冲/通配符/Pipe
发了没收到            → 问题 6：查同步退出 / topic / filter
订阅无效              → 问题 7：查 Subscribe 返回值与签名
跨节点异常            → 问题 8：查 BridgeStats + 幂等
```
