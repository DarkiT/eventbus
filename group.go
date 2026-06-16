package eventbus

import (
	"context"
	"strings"
)

// TopicSeparators 定义允许的主题分隔符，normalizeTopic 在规范化主题时读取。
//
// 注意：该变量为包级可变状态，仅应在 init 阶段或首次使用前设置；运行期并发修改
// 会与 normalizeTopic 的读取发生 data race。如需自定义分隔符，请在初始化时确定。
var TopicSeparators = []string{".", "/"}

// normalizeTopic 将主题中的所有分隔符统一为标准分隔符
// 处理边界情况：连续分隔符、前后分隔符、空白字符
func normalizeTopic(topic string) (string, error) {
	// 去除前后空白
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return "", ErrInvalidTopic
	}

	// 统一分隔符
	result := topic
	for _, sep := range TopicSeparators {
		if sep != "." {
			result = strings.ReplaceAll(result, sep, ".")
		}
	}

	// 去除前后分隔符
	result = strings.Trim(result, ".")

	// 合并连续分隔符
	for strings.Contains(result, "..") {
		result = strings.ReplaceAll(result, "..", ".")
	}

	// 验证结果
	if result == "" {
		return "", ErrInvalidTopic
	}

	if err := validateTopicPattern(result); err != nil {
		return "", err
	}

	return result, nil
}

// validateTopicPattern 校验主题 / 模式中通配符的位置是否合法。
// 规则：
// - `+` / `*` 可作为任意单层级通配符，但必须独占一个层级。
// - `#` 只能独占最后一个层级。
func validateTopicPattern(topic string) error {
	parts := strings.Split(topic, ".")
	for idx, part := range parts {
		switch part {
		case "+", "*":
			continue
		case "#":
			if idx != len(parts)-1 {
				return ErrInvalidTopic
			}
		default:
			if strings.ContainsAny(part, "+#*") {
				return ErrInvalidTopic
			}
		}
	}
	return nil
}

// TopicGroup 表示一个主题组
type TopicGroup struct {
	prefix string
	bus    *EventBus
}

// buildTopic 构建带前缀的标准主题
func (g *TopicGroup) buildTopic(topic string) (string, error) {
	switch {
	case g == nil:
		return normalizeTopic(topic)
	case g.prefix == "":
		return normalizeTopic(topic)
	case topic == "":
		return normalizeTopic(g.prefix)
	default:
		return normalizeTopic(g.prefix + "." + topic)
	}
}

// NewGroup 创建一个新的主题组
func (e *EventBus) NewGroup(prefix string) *TopicGroup {
	return &TopicGroup{
		prefix: prefix,
		bus:    e,
	}
}

// Publish 在组内发布消息
func (g *TopicGroup) Publish(topic string, payload any) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.Publish(built, payload)
}

// Subscribe 订阅组内的主题
func (g *TopicGroup) Subscribe(topic string, handler any) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.Subscribe(built, handler)
}

// Unsubscribe 取消订阅组内的主题
func (g *TopicGroup) Unsubscribe(topic string, handler any) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.Unsubscribe(built, handler)
}

// UnsubscribeAll 取消组内主题的所有订阅
func (g *TopicGroup) UnsubscribeAll(topic string) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.UnsubscribeAll(built)
}

// PublishSync 在组内同步发布消息
func (g *TopicGroup) PublishSync(topic string, payload any) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.PublishSync(built, payload)
}

// PublishWithContext 在组内带上下文异步发布
func (g *TopicGroup) PublishWithContext(ctx context.Context, topic string, payload any) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.PublishWithContext(ctx, built, payload)
}

// PublishSyncWithContext 在组内带上下文同步发布
func (g *TopicGroup) PublishSyncWithContext(ctx context.Context, topic string, payload any) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.PublishSyncWithContext(ctx, built, payload)
}

// PublishSyncAll 组内响应式同步发布（全部成功）
func (g *TopicGroup) PublishSyncAll(topic string, payload any) (*SyncResult, error) {
	built, err := g.buildTopic(topic)
	if err != nil {
		return nil, err
	}
	return g.bus.PublishSyncAll(built, payload)
}

// PublishSyncAllWithContext 组内响应式同步发布（全部成功，透传上下文）
func (g *TopicGroup) PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	built, err := g.buildTopic(topic)
	if err != nil {
		return nil, err
	}
	return g.bus.PublishSyncAllWithContext(ctx, built, payload)
}

// PublishSyncAny 组内响应式同步发布（任一成功）
func (g *TopicGroup) PublishSyncAny(topic string, payload any) (*SyncResult, error) {
	built, err := g.buildTopic(topic)
	if err != nil {
		return nil, err
	}
	return g.bus.PublishSyncAny(built, payload)
}

// PublishSyncAnyWithContext 组内响应式同步发布（任一成功，透传上下文）
func (g *TopicGroup) PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	built, err := g.buildTopic(topic)
	if err != nil {
		return nil, err
	}
	return g.bus.PublishSyncAnyWithContext(ctx, built, payload)
}

// PublishSyncAnyValue 组内快速返回首个成功处理器的结果。
func (g *TopicGroup) PublishSyncAnyValue(topic string, payload any) (any, error) {
	built, err := g.buildTopic(topic)
	if err != nil {
		return nil, err
	}
	return g.bus.PublishSyncAnyValue(built, payload)
}

// PublishSyncAnyValueWithContext 组内快速返回首个成功处理器的结果，支持上下文。
func (g *TopicGroup) PublishSyncAnyValueWithContext(ctx context.Context, topic string, payload any) (any, error) {
	built, err := g.buildTopic(topic)
	if err != nil {
		return nil, err
	}
	return g.bus.PublishSyncAnyValueWithContext(ctx, built, payload)
}

// SubscribeWithPriority 组内带优先级订阅
func (g *TopicGroup) SubscribeWithPriority(topic string, handler any, priority int) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.SubscribeWithPriority(built, handler, priority)
}

// SubscribeWithResponse 组内响应式订阅
func (g *TopicGroup) SubscribeWithResponse(topic string, handler ResponseHandler) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.SubscribeWithResponse(built, handler)
}

// SubscribeWithResponseContext 组内带上下文的响应式订阅
func (g *TopicGroup) SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.SubscribeWithResponseContext(built, handler)
}

// SubscribeWithFilter 组内带过滤器订阅
func (g *TopicGroup) SubscribeWithFilter(topic string, handler any, filter EventFilter) error {
	built, err := g.buildTopic(topic)
	if err != nil {
		return err
	}
	return g.bus.SubscribeWithFilter(built, handler, filter)
}

// NewSubGroup 创建嵌套子组
// 子组的前缀为父组前缀 + 子前缀
func (g *TopicGroup) NewSubGroup(prefix string) *TopicGroup {
	if g == nil {
		return nil
	}
	newPrefix, err := g.buildTopic(prefix)
	if err != nil {
		// 如果前缀无效，使用原始前缀
		newPrefix = g.prefix
	}
	return &TopicGroup{
		prefix: newPrefix,
		bus:    g.bus,
	}
}

// Prefix 返回当前组的前缀
func (g *TopicGroup) Prefix() string {
	if g == nil {
		return ""
	}
	return g.prefix
}

// GetSubscriberCount 返回组内指定主题的订阅者数量
func (g *TopicGroup) GetSubscriberCount(topic string) (int, error) {
	built, err := g.buildTopic(topic)
	if err != nil {
		return 0, err
	}
	return g.bus.GetSubscriberCount(built)
}

// HasSubscribers 检查组内指定主题是否有订阅者
func (g *TopicGroup) HasSubscribers(topic string) bool {
	built, err := g.buildTopic(topic)
	if err != nil {
		return false
	}
	return g.bus.HasSubscribers(built)
}
