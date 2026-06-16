package eventbus

import "slices"

// getResponseHandlers 获取主题的所有响应处理器
func (e *EventBus) getResponseHandlers(normalizedTopic string) []*HandlerInfo {
	var responseHandlers []*HandlerInfo
	processedChannels := make(map[*channel]bool) // 防止重复处理

	// 直接匹配
	if ch, ok := e.channels.Load(normalizedTopic); ok {
		channel := ch.(*channel)
		processedChannels[channel] = true
		channel.RLock()
		responseHandlers = append(responseHandlers, slices.Clone(channel.responseHandlers)...)
		channel.RUnlock()
	}

	// 使用 Trie 树查找所有匹配的通配符模式
	matchedPatterns := e.topicIndex.Match(normalizedTopic)
	for _, pattern := range matchedPatterns {
		if pattern == normalizedTopic {
			continue // 已经处理过精确匹配
		}
		if ch, ok := e.channels.Load(pattern); ok {
			channel := ch.(*channel)
			if processedChannels[channel] {
				continue
			}
			processedChannels[channel] = true
			channel.RLock()
			responseHandlers = append(responseHandlers, slices.Clone(channel.responseHandlers)...)
			channel.RUnlock()
		}
	}

	return responseHandlers
}

// getHandlers 获取主题匹配的所有普通处理器
func (e *EventBus) getHandlers(normalizedTopic string) []*HandlerInfo {
	var handlers []*HandlerInfo
	processedChannels := make(map[*channel]bool)

	// 直接匹配
	if ch, ok := e.channels.Load(normalizedTopic); ok {
		channel := ch.(*channel)
		processedChannels[channel] = true
		channel.RLock()
		handlers = append(handlers, slices.Clone(channel.handlers)...)
		channel.RUnlock()
	}

	// 使用 Trie 树查找所有匹配的通配符模式
	matchedPatterns := e.topicIndex.Match(normalizedTopic)
	for _, pattern := range matchedPatterns {
		if pattern == normalizedTopic {
			continue // 已经处理过精确匹配
		}
		if ch, ok := e.channels.Load(pattern); ok {
			channel := ch.(*channel)
			if processedChannels[channel] {
				continue
			}
			processedChannels[channel] = true
			channel.RLock()
			handlers = append(handlers, slices.Clone(channel.handlers)...)
			channel.RUnlock()
		}
	}

	return handlers
}
