package eventbus

import "context"

// PublishSyncAll 所有处理器成功才算成功。
//
// 响应式同步发布并发执行所有响应处理器，并在超时/取消后 best-effort 取消剩余处理器。
// 若用户处理器不响应 context 取消（如执行阻塞 IO），对应 goroutine 会继续运行至
// 自然结束、无法被强制中断；请确保响应式处理器定期检查 ctx，或接受这部分延迟回收。
func (e *EventBus) PublishSyncAll(topic string, payload any) (*SyncResult, error) {
	return e.publishSyncWithStrategy(context.Background(), topic, payload, "all", e.getTimeout())
}

// PublishSyncAllWithContext 所有处理器成功才算成功，且透传调用方上下文
func (e *EventBus) PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	return e.publishSyncWithStrategy(ctx, topic, payload, "all", e.getTimeout())
}

// PublishSyncAny 任一处理器成功即算成功，并在结果收敛后返回完整 SyncResult
func (e *EventBus) PublishSyncAny(topic string, payload any) (*SyncResult, error) {
	return e.publishSyncWithStrategy(context.Background(), topic, payload, "any", e.getTimeout())
}

// PublishSyncAnyWithContext 任一处理器成功即算成功，best-effort 取消其余处理器并等待结果收敛
func (e *EventBus) PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	return e.publishSyncWithStrategy(ctx, topic, payload, "any", e.getTimeout())
}

// PublishSyncAnyValue 返回首个成功处理器的结果，并在成功后立刻返回。
func (e *EventBus) PublishSyncAnyValue(topic string, payload any) (any, error) {
	return e.publishSyncAnyValueInternal(context.Background(), topic, payload, e.getTimeout())
}

// PublishSyncAnyValueWithContext 返回首个成功处理器的结果，成功后 best-effort 取消其余处理器。
func (e *EventBus) PublishSyncAnyValueWithContext(ctx context.Context, topic string, payload any) (any, error) {
	return e.publishSyncAnyValueInternal(ctx, topic, payload, e.getTimeout())
}
