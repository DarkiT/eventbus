package eventbus

import (
	"context"
	"fmt"
	"time"
)

// BatchMessage 批量发布的消息
type BatchMessage struct {
	Topic   string
	Payload any
	Context context.Context
}

// BatchItemResult 单条消息的发布结果
type BatchItemResult struct {
	Topic        string        `json:"topic"`
	Err          error         `json:"error"`
	Duration     time.Duration `json:"duration"`
	HandlerCount int           `json:"handler_count"`
	Skipped      bool          `json:"skipped"`
}

// BatchResult 批量发布结果
type BatchResult struct {
	Results      []BatchItemResult `json:"results"`
	SuccessCount int               `json:"success_count"`
	FailedCount  int               `json:"failed_count"`
	FirstError   error             `json:"first_error"`
}

// BatchError 部分失败时返回的错误类型
type BatchError struct {
	Result *BatchResult
}

func (e BatchError) Error() string {
	if e.Result == nil {
		return "batch publish failed"
	}
	return fmt.Sprintf("batch publish failed: %d items failed", e.Result.FailedCount)
}

// batchGroup 按主题分组后的消息集合
type batchGroup struct {
	normalizedTopic string
	indices         []int
}

// groupBatchMessages 按主题分组，同时预先校验并规范化主题
func (e *EventBus) groupBatchMessages(messages []BatchMessage) ([]batchGroup, []BatchItemResult, error) {
	results := make([]BatchItemResult, len(messages))
	groups := make([]batchGroup, 0)
	indexMap := make(map[string]int) // normalizedTopic -> group index
	var firstErr error

	for i, msg := range messages {
		results[i].Topic = msg.Topic

		if err := validateTopic(msg.Topic); err != nil {
			results[i].Err = err
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		normalized, err := normalizeTopic(msg.Topic)
		if err != nil {
			results[i].Err = err
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		idx, ok := indexMap[normalized]
		if !ok {
			idx = len(groups)
			indexMap[normalized] = idx
			groups = append(groups, batchGroup{
				normalizedTopic: normalized,
				indices:         make([]int, 0),
			})
		}
		groups[idx].indices = append(groups[idx].indices, i)
	}

	return groups, results, firstErr
}

// collectMatchingChannels 获取与主题匹配的所有通道（包含通配符），去重
func (e *EventBus) collectMatchingChannels(normalizedTopic string) []*channel {
	var chans []*channel
	processed := make(map[*channel]bool)

	appendActive := func(channel *channel) {
		if channel == nil || processed[channel] || channel.handlerCount() == 0 {
			return
		}
		processed[channel] = true
		chans = append(chans, channel)
	}

	// 直接匹配
	if ch, ok := e.channels.Load(normalizedTopic); ok {
		appendActive(ch.(*channel))
	}

	// 使用 Trie 树查找所有匹配的通配符模式
	matchedPatterns := e.topicIndex.Match(normalizedTopic)
	for _, pattern := range matchedPatterns {
		if pattern == normalizedTopic {
			continue // 已经处理过精确匹配
		}
		if ch, ok := e.channels.Load(pattern); ok {
			appendActive(ch.(*channel))
		}
	}

	return chans
}

// PublishBatch 异步批量发布
func (e *EventBus) PublishBatch(messages []BatchMessage) (*BatchResult, error) {
	return e.publishBatchInternal(messages, true)
}

// PublishBatchSync 同步批量发布
func (e *EventBus) PublishBatchSync(messages []BatchMessage) (*BatchResult, error) {
	return e.publishBatchInternal(messages, false)
}

// publishBatchInternal 批量发布核心实现
func (e *EventBus) publishBatchInternal(messages []BatchMessage, async bool) (*BatchResult, error) {
	if err := e.beginPublish(); err != nil {
		return nil, err
	}
	defer e.endPublish()
	if len(messages) == 0 {
		return &BatchResult{}, nil
	}

	groups, results, _ := e.groupBatchMessages(messages)

	// 获取过滤器和中间件快照（无锁读取，COW 优化）
	filters := e.getFilters()
	middlewares := e.getMiddlewares()

	var firstError error

	for _, group := range groups {
		if len(group.indices) == 0 {
			continue
		}

		processed := make([]messageEnvelope, 0, len(group.indices))
		afterCbs := make([]func() error, 0, len(group.indices))

		for _, idx := range group.indices {
			msg := messages[idx]
			ctx := msg.Context
			if ctx == nil {
				ctx = context.Background()
			}

			payload := msg.Payload
			var allowed bool
			var after func() error
			var err error
			payload, allowed, after, err = applyFiltersAndMiddlewares(filters, middlewares, group.normalizedTopic, payload)
			if err != nil {
				results[idx].Err = err
				if firstError == nil {
					firstError = err
				}
				continue
			}
			if !allowed {
				results[idx].Skipped = true
				continue
			}

			if after != nil {
				afterCbs = append(afterCbs, after)
			}

			processed = append(processed, messageEnvelope{
				ctx:     ctx,
				topic:   msg.Topic,
				payload: payload,
			})
		}

		if len(processed) == 0 {
			if afterErr := runBatchAfterCallbacks(afterCbs); afterErr != nil {
				e.traceError(group.normalizedTopic, afterErr)
				if firstError == nil {
					firstError = afterErr
				}
			}
			continue
		}

		channels := e.collectMatchingChannels(group.normalizedTopic)
		if len(channels) == 0 {
			for _, idx := range group.indices {
				if results[idx].Err == nil && !results[idx].Skipped {
					results[idx].Err = ErrNoSubscriber
					if firstError == nil {
						firstError = ErrNoSubscriber
					}
				}
			}
			if afterErr := runBatchAfterCallbacks(afterCbs); afterErr != nil {
				e.traceError(group.normalizedTopic, afterErr)
				if firstError == nil {
					firstError = afterErr
				}
			}
			continue
		}

		for _, ch := range channels {
			start := time.Now()
			var err error
			if async {
				err = ch.publishBatchAsync(processed)
			} else {
				for _, env := range processed {
					err = ch.publishSync(env.ctx, env.topic, env.payload)
					if err != nil {
						break
					}
				}
			}
			duration := time.Since(start)
			handlerCount := ch.handlerCount()

			for _, idx := range group.indices {
				if results[idx].Skipped || results[idx].Err != nil {
					continue
				}
				results[idx].Duration += duration
				if err != nil {
					results[idx].Err = err
					if firstError == nil {
						firstError = err
					}
					continue
				}
				results[idx].HandlerCount += handlerCount
			}
		}

		if afterErr := runBatchAfterCallbacks(afterCbs); afterErr != nil {
			e.traceError(group.normalizedTopic, afterErr)
			if firstError == nil {
				firstError = afterErr
			}
			for _, idx := range group.indices {
				if results[idx].Skipped || results[idx].Err != nil {
					continue
				}
				results[idx].Err = afterErr
			}
		}
	}

	batchResult := &BatchResult{Results: results}
	for i := range results {
		if results[i].Err != nil {
			batchResult.FailedCount++
			if batchResult.FirstError == nil {
				batchResult.FirstError = results[i].Err
			}
		} else if !results[i].Skipped {
			batchResult.SuccessCount++
		}
	}

	if batchResult.FailedCount > 0 {
		return batchResult, BatchError{Result: batchResult}
	}

	return batchResult, firstError
}

func runBatchAfterCallbacks(afterCbs []func() error) error {
	var firstErr error
	for i := len(afterCbs) - 1; i >= 0; i-- {
		if err := afterCbs[i](); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
