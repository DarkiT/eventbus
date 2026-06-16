# Changelog

All notable changes to this project will be documented in this file.

## 2026-04-21

### Fixed
- 修复同步发布路径错误去重，保证同步 / 异步在重复订阅与 wildcard 命中下的行为一致。
- 修复 `PublishSyncAll` / `PublishSyncAny` / `PublishSyncAnyValue` 的超时硬上限，避免非协作 handler 长时间拖住调用方。
- 修复 topic pattern 边界校验，拒绝非法 wildcard 组合与嵌入式通配符片段。
- 修复 `Unsubscribe` / `UnsubscribeAll` 后空 channel 残留问题，回收 `channels` 与 `topicIndex` 中的空节点。
- 修复并发回收与重订阅交错时的 stale channel 问题，补齐重试与 compare-and-delete 保护。
- 修复 Pipe 的默认 timeout 在 Context 响应式发布中的缺失，保证默认 timeout 也能生效。

### Added
- 新增 `Pipe` 推荐订阅 API：
  - `SubscribeWithResponseContextHandle`
  - `SubscribeWithResponseContextHandleAndPriority`
- 新增 `Pipe` 结果版 Context API：
  - `PublishSyncAllResultWithContext`
  - `PublishSyncAnyResultWithContext`
- 保留 Pipe 便捷版 Context API：
  - `PublishSyncAllWithContext` 返回 `map[string]any`
  - `PublishSyncAnyWithContext` 返回首个成功结果
- 补充回归测试，覆盖：
  - timeout 硬上限
  - duplicate subscription 一致性
  - empty channel prune
  - Pipe 结果版 / handle 版 API

### Documentation
- 更新 `README.md` 中 Pipe 推荐调用方式与 timeout 错误判断示例。
- 更新 `docs/API接口设计.md`，补充 Pipe 的结果版 / 便捷版双 API 说明。
- 更新 `docs/MQTT_COMPATIBILITY.md` 与 helper skill 中 `*` 通配符语义说明。
- 更新分析 / 优化类文档，使之与当前实现保持一致。

### Verification
- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
