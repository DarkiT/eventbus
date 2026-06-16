# Release Notes

## 2026-04-21 · 生产化收口版本

### 本次发布重点

这次发布的目标不是新增大功能，而是把 EventBus / Pipe 的运行边界、超时语义、文档一致性与上层调用体验一起收口到可生产交付状态。

### 核心改进

#### 1. 同步 / 异步行为一致性
- 修复同步发布路径中过度去重的问题。
- 现在同一个 handler 在不同订阅关系下会与异步路径保持一致，不会被同步路径误吞。

#### 2. 响应式同步发布超时可控
- `PublishSyncAll`、`PublishSyncAny`、`PublishSyncAnyValue` 现在都会严格尊重 timeout 上限。
- 即使 handler 不主动检查 `ctx.Done()`，调用方也不会被无限阻塞。

#### 3. Topic pattern 更安全
- 非法 wildcard pattern 现在会被明确拒绝。
- `*` 视为单层通配符，必须独占一个层级。
- `#` 仍然只能出现在最后一个层级。

#### 4. 订阅回收更干净
- `Unsubscribe` / `UnsubscribeAll` 后若 topic 已无 handler，会自动回收内部 channel 与索引。
- 长生命周期服务的 `channel_count` 与 topic 查询结果会更接近真实运行态。

#### 5. Pipe 上层调用更优雅
- 新增推荐订阅方式：`SubscribeWithResponseContextHandle`
- 新增推荐结果方式：
  - `PublishSyncAllResultWithContext`
  - `PublishSyncAnyResultWithContext`
- 保留便捷包装：
  - `PublishSyncAllWithContext`
  - `PublishSyncAnyWithContext`

### 推荐给调用方的使用方式

#### EventBus
- 需要完整聚合结果：`PublishSyncAll` / `PublishSyncAny`
- 需要最快首个成功值：`PublishSyncAnyValue`
- 需要 timeout / cancel：优先使用 `WithContext` 版本

#### Pipe
- 需要取消句柄：`SubscribeWithResponseContextHandle`
- 需要完整诊断信息：`PublishSyncAllResultWithContext` / `PublishSyncAnyResultWithContext`
- 只关心返回值：`PublishSyncAllWithContext` / `PublishSyncAnyWithContext`

### 兼容性说明

- 本次没有移除既有公开 API。
- Pipe 的新 API 是增量增强；旧方法仍可用。
- 文档中的推荐路径已切换到新 API，但旧调用方式不会立刻失效。

### 发布验证

已完成以下验证：

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
