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
		{"精确匹配", "chat.room1", "chat.room1", true},
		{"单层通配符", "chat.*.message", "chat.room1.message", true},
		{"多层通配符", "chat.#", "chat.room1.message", true},
		{"混合分隔符", "chat/room1.message", "chat.room1.message", true},
		{"不匹配长度", "chat.*.message", "chat.room1.message.extra", false},
		{"不匹配内容", "chat.room1", "chat.room2", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.match, matchTopic(tt.pattern, tt.topic))
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
