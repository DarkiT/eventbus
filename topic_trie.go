package eventbus

import (
	"strings"
	"sync"
)

// topicTrie 主题 Trie 树，用于高效的通配符匹配
// 支持 MQTT 风格通配符：
// - + 匹配单层级
// - * 匹配单层级
// - # 匹配多层级（必须在末尾）
type topicTrie struct {
	mu       sync.RWMutex
	root     *trieNode
	patterns map[string]struct{} // 已注册的模式集合，用于快速查重
}

// trieNode Trie 树节点
type trieNode struct {
	children   map[string]*trieNode // 子节点映射
	isEnd      bool                 // 是否为模式终点
	hasMulti   bool                 // 是否有 # 通配符（多层匹配）
	hasPlus    bool                 // 是否有 + 通配符（单层匹配）
	hasStar    bool                 // 是否有 * 通配符（单层匹配）
	pattern    string               // 完整模式（仅在 isEnd=true 时有效）
	plusChild  *trieNode            // + 通配符子节点（优化：避免 map 查找）
	starChild  *trieNode            // * 通配符子节点（优化：避免 map 查找）
	multiChild *trieNode            // # 通配符子节点（优化：避免 map 查找）
}

// newTopicTrie 创建新的主题 Trie 树
func newTopicTrie() *topicTrie {
	return &topicTrie{
		root: &trieNode{
			children: make(map[string]*trieNode),
		},
		patterns: make(map[string]struct{}),
	}
}

// Insert 插入模式到 Trie 树
// 返回 true 表示新插入，false 表示已存在
func (t *topicTrie) Insert(pattern string) bool {
	normalized, err := normalizeTopic(pattern)
	if err != nil {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.patterns[normalized]; exists {
		return false
	}

	parts := strings.Split(normalized, ".")
	node := t.root

	for i, part := range parts {
		isLast := i == len(parts)-1

		switch part {
		case "#":
			if !isLast {
				return false
			}
			if node.multiChild == nil {
				node.multiChild = &trieNode{children: make(map[string]*trieNode)}
			}
			node.hasMulti = true
			node = node.multiChild
		case "+":
			if node.plusChild == nil {
				node.plusChild = &trieNode{children: make(map[string]*trieNode)}
			}
			node.hasPlus = true
			node = node.plusChild
		case "*":
			if node.starChild == nil {
				node.starChild = &trieNode{children: make(map[string]*trieNode)}
			}
			node.hasStar = true
			node = node.starChild
		default:
			if node.children[part] == nil {
				node.children[part] = &trieNode{children: make(map[string]*trieNode)}
			}
			node = node.children[part]
		}
	}

	node.isEnd = true
	node.pattern = normalized
	t.patterns[normalized] = struct{}{}
	return true
}

// Remove 从 Trie 树移除模式
// 返回 true 表示成功移除，false 表示不存在
func (t *topicTrie) Remove(pattern string) bool {
	normalized, err := normalizeTopic(pattern)
	if err != nil {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.patterns[normalized]; !exists {
		return false
	}

	parts := strings.Split(normalized, ".")
	t.removeRecursive(t.root, parts, 0)
	delete(t.patterns, normalized)
	return true
}

// removeRecursive 递归移除节点
func (t *topicTrie) removeRecursive(node *trieNode, parts []string, depth int) bool {
	if depth == len(parts) {
		if !node.isEnd {
			return false
		}
		node.isEnd = false
		node.pattern = ""
		return t.isNodeEmpty(node)
	}

	part := parts[depth]
	var child *trieNode

	switch part {
	case "#":
		child = node.multiChild
	case "+":
		child = node.plusChild
	case "*":
		child = node.starChild
	default:
		child = node.children[part]
	}

	if child == nil {
		return false
	}

	shouldDelete := t.removeRecursive(child, parts, depth+1)
	if shouldDelete {
		switch part {
		case "#":
			node.multiChild = nil
			node.hasMulti = false
		case "+":
			node.plusChild = nil
			node.hasPlus = false
		case "*":
			node.starChild = nil
			node.hasStar = false
		default:
			delete(node.children, part)
		}
	}

	return t.isNodeEmpty(node) && !node.isEnd
}

// isNodeEmpty 检查节点是否为空
func (t *topicTrie) isNodeEmpty(node *trieNode) bool {
	return len(node.children) == 0 && node.plusChild == nil && node.starChild == nil && node.multiChild == nil
}

// Match 查找所有匹配给定主题的模式
// 返回匹配的模式列表
func (t *topicTrie) Match(topic string) []string {
	normalized, err := normalizeTopic(topic)
	if err != nil {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	parts := strings.Split(normalized, ".")
	var matches []string
	t.matchRecursive(t.root, parts, 0, &matches)
	return matches
}

// matchRecursive 递归匹配
func (t *topicTrie) matchRecursive(node *trieNode, parts []string, depth int, matches *[]string) {
	if node == nil {
		return
	}

	if node.hasMulti && node.multiChild != nil && node.multiChild.isEnd {
		*matches = append(*matches, node.multiChild.pattern)
	}

	if depth == len(parts) {
		if node.isEnd {
			*matches = append(*matches, node.pattern)
		}
		return
	}

	part := parts[depth]

	if child, ok := node.children[part]; ok {
		t.matchRecursive(child, parts, depth+1, matches)
	}
	if node.hasPlus && node.plusChild != nil {
		t.matchRecursive(node.plusChild, parts, depth+1, matches)
	}
	if node.hasStar && node.starChild != nil {
		t.matchRecursive(node.starChild, parts, depth+1, matches)
	}
}

// MatchOne 检查是否有任何模式匹配给定主题
// 比 Match 更高效，找到第一个匹配即返回
func (t *topicTrie) MatchOne(topic string) bool {
	normalized, err := normalizeTopic(topic)
	if err != nil {
		return false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	parts := strings.Split(normalized, ".")
	return t.matchOneRecursive(t.root, parts, 0)
}

// matchOneRecursive 递归查找第一个匹配
func (t *topicTrie) matchOneRecursive(node *trieNode, parts []string, depth int) bool {
	if node == nil {
		return false
	}

	if node.hasMulti && node.multiChild != nil && node.multiChild.isEnd {
		return true
	}

	if depth == len(parts) {
		return node.isEnd
	}

	part := parts[depth]

	if child, ok := node.children[part]; ok && t.matchOneRecursive(child, parts, depth+1) {
		return true
	}
	if node.hasPlus && node.plusChild != nil && t.matchOneRecursive(node.plusChild, parts, depth+1) {
		return true
	}
	if node.hasStar && node.starChild != nil && t.matchOneRecursive(node.starChild, parts, depth+1) {
		return true
	}

	return false
}

// Has 检查模式是否已注册
func (t *topicTrie) Has(pattern string) bool {
	normalized, err := normalizeTopic(pattern)
	if err != nil {
		return false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	_, exists := t.patterns[normalized]
	return exists
}

// Patterns 返回所有已注册的模式
func (t *topicTrie) Patterns() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]string, 0, len(t.patterns))
	for p := range t.patterns {
		result = append(result, p)
	}
	return result
}

// Len 返回已注册模式数量
func (t *topicTrie) Len() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.patterns)
}

// Clear 清空 Trie 树
func (t *topicTrie) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.root = &trieNode{children: make(map[string]*trieNode)}
	t.patterns = make(map[string]struct{})
}
