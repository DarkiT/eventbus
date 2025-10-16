package eventbus

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTopicGroup(t *testing.T) {
	bus := New()
	group := bus.NewGroup("chat")

	// 测试基本的组发布/订阅
	var received string
	var mu sync.RWMutex
	handler := func(_ string, msg string) {
		mu.Lock()
		received = msg
		mu.Unlock()
	}

	err := group.Subscribe("room1", handler)
	assert.NoError(t, err)

	err = group.Publish("room1", "Hello")
	assert.NoError(t, err)
	time.Sleep(time.Millisecond)

	mu.RLock()
	result := received
	mu.RUnlock()
	assert.Equal(t, "Hello", result)

	// 测试取消订阅
	err = group.Unsubscribe("room1", handler)
	assert.NoError(t, err)
}

func TestWildcardMatching(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		topic   string
		match   bool
	}{
		// 精确匹配
		{"精确匹配", "chat.room1", "chat.room1", true},

		// + 通配符测试（中间单层级）
		{"+ 中间匹配", "chat.+.message", "chat.room1.message", true},
		{"+ 开头匹配", "+.room1.message", "chat.room1.message", true},
		{"+ 多个匹配", "chat.+.+", "chat.room1.message", true},
		{"+ 不匹配多层", "chat.+", "chat.room1.message", false},

		// * 通配符测试（末尾单层级）
		{"* 末尾匹配", "chat.room1.*", "chat.room1.message", true},
		{"* 末尾不匹配多层", "chat.*", "chat.room1.message", false},
		{"* 末尾完全匹配", "chat.*", "chat.room1", true},

		// # 通配符测试（多层级）
		{"# 完全通配", "#", "chat.room1.message", true},
		{"# 多层匹配", "chat.#", "chat.room1.message", true},
		{"# 单层匹配", "chat.#", "chat.room1", true},
		{"# 前缀匹配", "chat.room1.#", "chat.room1.message.user", true},
		{"# 不匹配前缀", "news.#", "chat.room1.message", false},

		// 混合分隔符测试
		{"混合分隔符 +", "chat/+/message", "chat.room1.message", true},
		{"混合分隔符 *", "chat/room1/*", "chat.room1.message", true},
		{"混合分隔符 #", "chat/#", "chat/room1/message", true},

		// 不匹配情况
		{"不匹配内容", "chat.room1", "chat.room2", false},
		{"长度不匹配", "chat.+.message", "chat.room1", false},
		{"前缀不匹配", "news.+", "chat.room1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.match, matchTopic(tt.pattern, tt.topic),
				"模式 '%s' 匹配主题 '%s' 应该返回 %v", tt.pattern, tt.topic, tt.match)
		})
	}
}

func TestTopicSeparators(t *testing.T) {
	bus := New()
	var received string
	var mu sync.RWMutex
	handler := func(_ string, msg string) {
		mu.Lock()
		received = msg
		mu.Unlock()
	}

	// 使用不同的分隔符订阅
	err := bus.Subscribe("chat.*.message", handler)
	assert.NoError(t, err)

	// 使用不同的分隔符发布
	tests := []struct {
		name  string
		topic string
	}{
		{"点分隔符", "chat.room1.message"},
		{"斜杠分隔符", "chat/room1/message"},
		{"混合分隔符", "chat.room1/message"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			received = ""
			mu.Unlock()

			err := bus.Publish(tt.topic, "Hello")
			assert.NoError(t, err)
			time.Sleep(time.Millisecond)

			mu.RLock()
			result := received
			mu.RUnlock()
			assert.Equal(t, "Hello", result)
		})
	}
}

func TestMultipleGroups(t *testing.T) {
	bus := New()
	chatGroup := bus.NewGroup("chat")
	newsGroup := bus.NewGroup("news")

	var chatMsg, newsMsg string
	var mu sync.RWMutex
	chatHandler := func(_ string, msg string) {
		mu.Lock()
		chatMsg = msg
		mu.Unlock()
	}
	newsHandler := func(_ string, msg string) {
		mu.Lock()
		newsMsg = msg
		mu.Unlock()
	}

	// 订阅不同组的主题
	err := chatGroup.Subscribe("room1", chatHandler)
	assert.NoError(t, err)
	err = newsGroup.Subscribe("tech", newsHandler)
	assert.NoError(t, err)

	// 发布到不同组
	err = chatGroup.Publish("room1", "Chat Message")
	assert.NoError(t, err)
	err = newsGroup.Publish("tech", "News Message")
	assert.NoError(t, err)

	time.Sleep(time.Millisecond)

	mu.RLock()
	chatResult := chatMsg
	newsResult := newsMsg
	mu.RUnlock()

	assert.Equal(t, "Chat Message", chatResult)
	assert.Equal(t, "News Message", newsResult)
}

func TestWildcardSubscriptions(t *testing.T) {
	bus := New()
	var messages []string
	var mu sync.Mutex
	handler := func(_ string, msg string) {
		mu.Lock()
		messages = append(messages, msg)
		mu.Unlock()
	}

	// 订阅使用通配符的主题
	err := bus.Subscribe("chat.#", handler)
	assert.NoError(t, err)

	// 发布到匹配的主题
	topics := []string{
		"chat.room1",
		"chat.room1.message",
		"chat/room2/user",
	}

	for _, topic := range topics {
		err := bus.Publish(topic, topic)
		assert.NoError(t, err)
	}

	time.Sleep(time.Millisecond)

	mu.Lock()
	messagesCopy := make([]string, len(messages))
	copy(messagesCopy, messages)
	mu.Unlock()

	assert.Equal(t, len(topics), len(messagesCopy))
	for i, topic := range topics {
		assert.Equal(t, topic, messagesCopy[i])
	}
}

// TestMQTTWildcardIntegration 测试MQTT通配符的完整功能
func TestMQTTWildcardIntegration(t *testing.T) {
	bus := New()

	// 用于收集消息的结构
	type Message struct {
		Pattern string
		Topic   string
		Payload string
	}

	var messages []Message
	var mu sync.Mutex

	// 创建处理器工厂
	createHandler := func(pattern string) func(string, string) {
		return func(topic string, payload string) {
			mu.Lock()
			messages = append(messages, Message{
				Pattern: pattern,
				Topic:   topic,
				Payload: payload,
			})
			mu.Unlock()
		}
	}

	// 订阅不同类型的通配符
	patterns := []string{
		"sensor/+/temperature", // + 中间通配符
		"sensor/room1/*",       // * 末尾通配符
		"system/#",             // # 多层通配符
		"alert/+/+",            // 多个 + 通配符
	}

	for _, pattern := range patterns {
		err := bus.Subscribe(pattern, createHandler(pattern))
		assert.NoError(t, err)
	}

	// 发布消息测试
	testCases := []struct {
		topic           string
		payload         string
		expectedMatches []string
	}{
		{
			"sensor/room1/temperature",
			"25°C",
			[]string{"sensor/+/temperature", "sensor/room1/*"},
		},
		{
			"sensor/room2/temperature",
			"23°C",
			[]string{"sensor/+/temperature"},
		},
		{
			"sensor/room1/humidity",
			"60%",
			[]string{"sensor/room1/*"},
		},
		{
			"system/cpu/high",
			"alert",
			[]string{"system/#"},
		},
		{
			"system/memory/low/critical",
			"warning",
			[]string{"system/#"},
		},
		{
			"alert/fire/room1",
			"emergency",
			[]string{"alert/+/+"},
		},
	}

	for _, tc := range testCases {
		// 清空消息收集器
		mu.Lock()
		messages = nil
		mu.Unlock()

		// 发布消息
		err := bus.Publish(tc.topic, tc.payload)
		assert.NoError(t, err)
		time.Sleep(time.Millisecond)

		// 检查匹配结果
		mu.Lock()
		receivedMessages := make([]Message, len(messages))
		copy(receivedMessages, messages)
		mu.Unlock()

		assert.Equal(t, len(tc.expectedMatches), len(receivedMessages),
			"主题 %s 应该匹配 %d 个模式", tc.topic, len(tc.expectedMatches))

		// 验证每个预期的模式都被匹配
		matchedPatterns := make(map[string]bool)
		for _, msg := range receivedMessages {
			matchedPatterns[msg.Pattern] = true
			assert.Equal(t, tc.topic, msg.Topic)
			assert.Equal(t, tc.payload, msg.Payload)
		}

		for _, expectedPattern := range tc.expectedMatches {
			assert.True(t, matchedPatterns[expectedPattern],
				"模式 %s 应该匹配主题 %s", expectedPattern, tc.topic)
		}
	}
}
