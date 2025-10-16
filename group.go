package eventbus

import (
	"context"
	"strings"
)

// TopicSeparators 定义允许的主题分隔符
var TopicSeparators = []string{".", "/"}

// normalizeTopic 将主题中的所有分隔符统一为标准分隔符
func normalizeTopic(topic string) string {
	result := topic
	for _, sep := range TopicSeparators {
		if sep != "." {
			result = strings.ReplaceAll(result, sep, ".")
		}
	}
	return result
}

// TopicGroup 表示一个主题组
type TopicGroup struct {
	prefix string
	bus    *EventBus
}

// buildTopic 构建带前缀的标准主题
func (g *TopicGroup) buildTopic(topic string) string {
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
	return g.bus.Publish(g.buildTopic(topic), payload)
}

// Subscribe 订阅组内的主题
func (g *TopicGroup) Subscribe(topic string, handler any) error {
	return g.bus.Subscribe(g.buildTopic(topic), handler)
}

// Unsubscribe 取消订阅组内的主题
func (g *TopicGroup) Unsubscribe(topic string, handler any) error {
	return g.bus.Unsubscribe(g.buildTopic(topic), handler)
}

// UnsubscribeAll 取消组内主题的所有订阅
func (g *TopicGroup) UnsubscribeAll(topic string) error {
	return g.bus.UnsubscribeAll(g.buildTopic(topic))
}

// PublishSync 在组内同步发布消息
func (g *TopicGroup) PublishSync(topic string, payload any) error {
	return g.bus.PublishSync(g.buildTopic(topic), payload)
}

// PublishWithContext 在组内带上下文异步发布
func (g *TopicGroup) PublishWithContext(ctx context.Context, topic string, payload any) error {
	return g.bus.PublishWithContext(ctx, g.buildTopic(topic), payload)
}

// PublishSyncWithContext 在组内带上下文同步发布
func (g *TopicGroup) PublishSyncWithContext(ctx context.Context, topic string, payload any) error {
	return g.bus.PublishSyncWithContext(ctx, g.buildTopic(topic), payload)
}

// PublishSyncAll 组内响应式同步发布（全部成功）
func (g *TopicGroup) PublishSyncAll(topic string, payload any) (*SyncResult, error) {
	return g.bus.PublishSyncAll(g.buildTopic(topic), payload)
}

// PublishSyncAllWithContext 组内响应式同步发布（全部成功，透传上下文）
func (g *TopicGroup) PublishSyncAllWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	return g.bus.PublishSyncAllWithContext(ctx, g.buildTopic(topic), payload)
}

// PublishSyncAny 组内响应式同步发布（任一成功）
func (g *TopicGroup) PublishSyncAny(topic string, payload any) (*SyncResult, error) {
	return g.bus.PublishSyncAny(g.buildTopic(topic), payload)
}

// PublishSyncAnyWithContext 组内响应式同步发布（任一成功，透传上下文）
func (g *TopicGroup) PublishSyncAnyWithContext(ctx context.Context, topic string, payload any) (*SyncResult, error) {
	return g.bus.PublishSyncAnyWithContext(ctx, g.buildTopic(topic), payload)
}

// SubscribeWithPriority 组内带优先级订阅
func (g *TopicGroup) SubscribeWithPriority(topic string, handler any, priority int) error {
	return g.bus.SubscribeWithPriority(g.buildTopic(topic), handler, priority)
}

// SubscribeWithResponse 组内响应式订阅
func (g *TopicGroup) SubscribeWithResponse(topic string, handler ResponseHandler) error {
	return g.bus.SubscribeWithResponse(g.buildTopic(topic), handler)
}

// SubscribeWithResponseContext 组内带上下文的响应式订阅
func (g *TopicGroup) SubscribeWithResponseContext(topic string, handler ResponseHandlerWithContext) error {
	return g.bus.SubscribeWithResponseContext(g.buildTopic(topic), handler)
}

// matchTopic 检查主题是否匹配MQTT通配符模式
// 支持三种通配符：
// * - 匹配末尾的单层级 (如 xxx/xxx/*)
// + - 匹配中间的单层级 (如 xxx/+/xxx)
// # - 匹配多层级路径 (如 xxx/# 匹配 xxx/xxx、xxx/xxx/xxx)
func matchTopic(pattern, topic string) bool {
	// 统一分隔符
	pattern = normalizeTopic(pattern)
	topic = normalizeTopic(topic)

	// 处理完全通配符
	if pattern == "#" {
		return true
	}

	// 将模式和主题分割成部分
	patternParts := strings.Split(pattern, ".")
	topicParts := strings.Split(topic, ".")

	// 检查是否以 # 结尾（多层通配符）
	if len(patternParts) > 0 && patternParts[len(patternParts)-1] == "#" {
		// 移除 # 并检查前缀是否匹配
		prefixParts := patternParts[:len(patternParts)-1]
		if len(topicParts) < len(prefixParts) {
			return false
		}
		// 只需要匹配前缀部分
		return matchParts(prefixParts, topicParts[:len(prefixParts)])
	}

	// 检查是否以 * 结尾（末尾单层通配符）
	if len(patternParts) > 0 && patternParts[len(patternParts)-1] == "*" {
		// 长度必须相等
		if len(patternParts) != len(topicParts) {
			return false
		}
		// 匹配除最后一层外的所有部分
		prefixParts := patternParts[:len(patternParts)-1]
		return matchParts(prefixParts, topicParts[:len(prefixParts)])
	}

	// 普通匹配（包括 + 通配符）
	if len(patternParts) != len(topicParts) {
		return false
	}

	return matchParts(patternParts, topicParts)
}

// matchParts 匹配模式部分和主题部分
// 支持 + 通配符匹配单个层级
func matchParts(patternParts, topicParts []string) bool {
	for i := range len(patternParts) {
		if i >= len(topicParts) {
			return false
		}
		// + 和 * 都可以匹配单个层级
		if patternParts[i] == "+" || patternParts[i] == "*" {
			continue
		}
		if patternParts[i] != topicParts[i] {
			return false
		}
	}
	return true
}
