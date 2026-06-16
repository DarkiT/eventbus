# 测试与基准套路

eventbus 的测试关键点：**同步语义要用 `PublishSync*` 才能稳定断言**（`Publish` 是异步的，测试里直接断言会有时序竞争）；**并发测试必须跑 `-race`**；**单例测试要用 `ResetSingleton()`**。本文件套路均经源码核验。

## 1. 表驱动单元测试

```go
func TestSubscribe(t *testing.T) {
    cases := []struct {
        name    string
        topic   string
        handler any
        wantErr error
    }{
        {"valid", "x", func(string, any) {}, nil},
        {"not_func", "x", 123, eventbus.ErrHandlerIsNotFunc},
        {"one_param", "x", func(any) {}, eventbus.ErrHandlerParamNum},
        {"first_not_string", "x", func(int, any) {}, eventbus.ErrHandlerFirstParam},
        {"has_return", "x", func(string, any) error { return nil }, eventbus.ErrHandlerReturnNum},
    }
    for _, c := range cases {
        t.Run(c.name, func(t *testing.T) {
            bus := eventbus.New()
            defer bus.Close()
            err := bus.Subscribe(c.topic, c.handler)
            if !errors.Is(err, c.wantErr) {
                t.Fatalf("got %v, want %v", err, c.wantErr)
            }
        })
    }
}
```

> 反射校验错误是**返回 error**（不是 panic）。订阅失败若不检查，后续发布会拿到 `ErrNoSubscriber`——测试里要明确断言错误类型。

## 2. 同步发布断言

异步 `Publish` 在测试中不可直接断言（处理器可能还没跑）。用 `PublishSync` 保证处理器执行完毕后再检查。

```go
func TestPublishSync(t *testing.T) {
    bus := eventbus.New()
    defer bus.Close()

    var got any
    _ = bus.Subscribe("inc", func(topic string, p any) { got = p })

    _ = bus.PublishSync("inc", 42)
    if got != 42 {
        t.Fatalf("got %v, want 42", got)
    }
}
```

## 3. 响应式测试（成功 / 失败 / 超时）

```go
func TestPublishSyncAll(t *testing.T) {
    bus := eventbus.New()
    defer bus.Close()

    _ = bus.SubscribeWithResponse("order", func(string, any) (any, error) {
        return "ok", nil
    })
    _ = bus.SubscribeWithResponse("order", func(string, any) (any, error) {
        return nil, errors.New("boom")
    })

    res, err := bus.PublishSyncAll("order", nil)
    if err != nil {
        t.Fatal(err)
    }
    if res.Success {
        t.Fatal("应失败")
    }
    if res.FailureCount != 1 || res.SuccessCount != 1 || res.HandlerCount != 2 {
        t.Fatalf("计数错: %+v", res)
    }
    // 找到失败处理器
    var failed error
    for _, h := range res.Results {
        if !h.Success { failed = h.Error }
    }
    if failed == nil { t.Fatal("未捕获失败原因") }
}

func TestPublishSyncAllTimeout(t *testing.T) {
    bus := eventbus.New()
    defer bus.Close()
    bus.SetTimeout(50 * time.Millisecond)

    _ = bus.SubscribeWithResponse("slow", func(string, any) (any, error) {
        time.Sleep(time.Second)
        return nil, nil
    })

    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()
    _, err := bus.PublishSyncAllWithContext(ctx, "slow", nil)
    if !errors.Is(err, context.DeadlineExceeded) {
        t.Fatalf("want DeadlineExceeded, got %v", err)
    }
}
```

## 4. Pipe 测试

```go
func TestPipe(t *testing.T) {
    pipe := eventbus.NewBufferedPipe[int](8)
    defer pipe.Close()

    var sum int
    _ = pipe.Subscribe(func(v int) { sum += v })

    _ = pipe.PublishSync(1)
    _ = pipe.PublishSync(2)
    if sum != 3 {
        t.Fatalf("got %d, want 3", sum)
    }

    // 响应式
    cancel, _ := pipe.SubscribeWithResponse(func(v int) (any, error) { return v * 10, nil })
    defer cancel()
    res, _ := pipe.PublishSyncAll(5)
    if !res.Success {
        t.Fatal(res.Results)
    }
}
```

> 注意 Pipe 处理器是**单参** `func(v int)`，写成 `func(topic string, v int)` 会校验失败。

## 5. SubscribeOnce 测试

```go
func TestSubscribeOnce(t *testing.T) {
    bus := eventbus.New()
    defer bus.Close()

    var n int
    _ = bus.SubscribeOnce("e", func(string, any) { n++ })

    _ = bus.PublishSync("e", nil)
    _ = bus.PublishSync("e", nil)   // 第二次无处理器
    if n != 1 {
        t.Fatalf("got %d, want 1", n)
    }
}
```

## 6. 基准测试

```go
func BenchmarkPublish(b *testing.B) {
    bus := eventbus.New(1024)
    defer bus.Close()
    _ = bus.Subscribe("e", func(string, any) {})

    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = bus.Publish("e", i)
    }
}

// 分场景 sub-bench
func BenchmarkPublishFlavors(b *testing.B) {
    makeBus := func() *eventbus.EventBus {
        bus := eventbus.New(1024)
        _ = bus.Subscribe("e", func(string, any) {})
        return bus
    }
    b.Run("Async", func(b *testing.B) {
        bus := makeBus(); defer bus.Close()
        b.ReportAllocs()
        for i := 0; i < b.N; i++ { _ = bus.Publish("e", i) }
    })
    b.Run("Sync", func(b *testing.B) {
        bus := makeBus(); defer bus.Close()
        b.ReportAllocs()
        for i := 0; i < b.N; i++ { _ = bus.PublishSync("e", i) }
    })
}
```

> 基准里**务必 `defer bus.Close()`**，否则每次迭代泄漏 goroutine，b.N 一大就会拖垮结果甚至 OOM。

## 7. 竞态测试（必须 `-race`）

```go
// 跑法：go test -race -run TestConcurrent
func TestConcurrentSubscribePublish(t *testing.T) {
    bus := eventbus.New(256)
    defer bus.Close()

    var wg sync.WaitGroup
    // 并发订阅
    for i := 0; i < 50; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            _ = bus.Subscribe("e", func(string, any) {})
        }()
    }
    // 并发发布
    for i := 0; i < 50; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            _ = bus.Publish("e", i)
        }(i)
    }
    wg.Wait()
}
```

## 8. Mock Tracer

```go
type mockTracer struct {
    mu       sync.Mutex
    errors   []error
    publishes int
}
func (m *mockTracer) OnPublish(string, any, eventbus.PublishMetadata) { m.mu.Lock(); m.publishes++; m.mu.Unlock() }
func (m *mockTracer) OnSubscribe(string, any)                         {}
func (m *mockTracer) OnUnsubscribe(string, any)                       {}
func (m *mockTracer) OnError(_ string, err error)                     { m.mu.Lock(); m.errors = append(m.errors, err); m.mu.Unlock() }
func (m *mockTracer) OnComplete(string, eventbus.CompleteMetadata)    {}
func (m *mockTracer) OnQueueFull(string, int)                         {}
func (m *mockTracer) OnSlowConsumer(string, time.Duration)            {}

func TestTracerOnError(t *testing.T) {
    bus := eventbus.New()
    defer bus.Close()
    mt := &mockTracer{}
    bus.SetTracer(mt)

    _ = bus.SubscribeWithResponse("x", func(string, any) (any, error) { return nil, errors.New("e") })
    _, _ = bus.PublishSyncAll("x", nil)

    if len(mt.errors) == 0 {
        t.Fatal("应捕获错误事件")
    }
}
```

## 9. 过滤器测试

```go
func TestSmartFilter(t *testing.T) {
    bus := eventbus.New()
    defer bus.Close()

    f := eventbus.NewSmartFilter()
    f.BlockTopic("blocked")
    bus.AddFilter(f)

    var hit bool
    _ = bus.Subscribe("blocked", func(string, any) { hit = true })
    _ = bus.PublishSync("blocked", nil)
    if hit {
        t.Fatal("被阻断的 topic 不应触达处理器")
    }
}
```

## 10. SubscribeReliable 重试/死信测试

```go
func TestSubscribeReliableDeadLetter(t *testing.T) {
    bus := eventbus.New(8)
    defer bus.Close()

    var attempts atomic.Int32
    dlq := make(chan error, 1)
    _ = bus.SubscribeReliable("reliable",
        func(ctx context.Context, topic string, payload any) error {
            attempts.Add(1)
            return errors.New("boom")
        },
        eventbus.WithMaxAttempts(3),
        eventbus.WithBackoff(eventbus.ConstantBackoff(time.Millisecond)),
        eventbus.WithDeadLetter(func(topic string, payload any, err error) {
            dlq <- err
        }),
    )

    _ = bus.Publish("reliable", nil)
    select {
    case err := <-dlq:
        if err == nil || attempts.Load() != 3 { t.Fatalf("bad retry state") }
    case <-time.After(time.Second):
        t.Fatal("dead letter timeout")
    }
}
```

## 11. Shutdown 排空测试

```go
func TestShutdownDrains(t *testing.T) {
    bus := eventbus.New(32)
    var got atomic.Int32
    _ = bus.Subscribe("drain", func(string, any) {
        time.Sleep(time.Millisecond)
        got.Add(1)
    })

    for i := 0; i < 10; i++ { _ = bus.Publish("drain", i) }

    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()
    if err := bus.Shutdown(ctx); err != nil {
        t.Fatal(err)
    }
    if got.Load() != 10 { t.Fatalf("not drained: %d", got.Load()) }
    if !errors.Is(bus.Publish("drain", nil), eventbus.ErrEventBusClosed) {
        t.Fatal("shutdown 后应拒绝发布")
    }
}
```

## 12. 单例测试隔离

```go
func TestSingleton(t *testing.T) {
    eventbus.ResetSingleton()          // 每个用例先重置，避免互相污染
    var got any
    _ = eventbus.Subscribe("e", func(string, any) { got = 1 })
    _ = eventbus.PublishSync("e", nil)
    if got != 1 { t.Fatal("missed") }
    eventbus.ResetSingleton()          // 收尾
}
```

## 13. 测试陷阱速查

| 现象 | 原因 | 对策 |
|---|---|---|
| 偶发断言失败 | `Publish` 异步，断言早于处理器 | 测试用 `PublishSync` / `PublishSyncAll` |
| `-race` 报数据竞争 | 处理器写共享变量未加锁 | 处理器内用 `sync.Mutex`/`atomic` 保护共享态 |
| b.N 大时变慢/OOM | 基准没 `Close`，goroutine 累积 | 基准里 `defer bus.Close()` |
| 订阅"没生效" | 签名不符，返回 error 被忽略 | 检查 `Subscribe` 返回值 |
| `ErrNoSubscriber` | topic 拼错 / 用了通配符但发布精确 topic | 核对 topic 与通配符规则 |
| 单例用例互相影响 | `DefaultBus` 全局共享 | `ResetSingleton()` 隔离 |
| Shutdown 偶发少计数 | 只等队列 len，没等处理中 handler | 用 `Shutdown` 回归测试覆盖可靠重试/慢 handler |
