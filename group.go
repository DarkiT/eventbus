package eventbus

import "strings"

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

// NewGroup 创建一个新的主题组
func (e *EventBus) NewGroup(prefix string) *TopicGroup {
	return &TopicGroup{
		prefix: prefix,
		bus:    e,
	}
}

// Publish 在组内发布消息
func (g *TopicGroup) Publish(topic string, payload any) error {
	fullTopic := g.prefix + "." + topic
	return g.bus.Publish(normalizeTopic(fullTopic), payload)
}

// Subscribe 订阅组内的主题
func (g *TopicGroup) Subscribe(topic string, handler any) error {
	fullTopic := g.prefix + "." + topic
	return g.bus.Subscribe(normalizeTopic(fullTopic), handler)
}

// Unsubscribe 取消订阅组内的主题
func (g *TopicGroup) Unsubscribe(topic string, handler any) error {
	fullTopic := g.prefix + "." + topic
	return g.bus.Unsubscribe(normalizeTopic(fullTopic), handler)
}

// matchTopic 检查主题是否匹配通配符模式
func matchTopic(pattern, topic string) bool {
	// 统一分隔符
	pattern = normalizeTopic(pattern)
	topic = normalizeTopic(topic)

	// 处理多层通配符 #
	if pattern == "#" {
		return true
	}

	// 将模式和主题分割成部分
	patternParts := strings.Split(pattern, ".")
	topicParts := strings.Split(topic, ".")

	// 如果模式以 # 结尾，移除它并允许任意长度匹配
	if patternParts[len(patternParts)-1] == "#" {
		patternParts = patternParts[:len(patternParts)-1]
		return matchParts(patternParts, topicParts)
	}

	// 否则要求长度相等
	if len(patternParts) != len(topicParts) {
		return false
	}

	return matchParts(patternParts, topicParts)
}

func matchParts(patternParts, topicParts []string) bool {
	for i := 0; i < len(patternParts); i++ {
		if patternParts[i] == "*" {
			continue
		}
		if i >= len(topicParts) || patternParts[i] != topicParts[i] {
			return false
		}
	}
	return true
}
