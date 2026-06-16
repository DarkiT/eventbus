package eventbus

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopicTrie_Insert(t *testing.T) {
	trie := newTopicTrie()

	// 基本插入
	assert.True(t, trie.Insert("user.created"))
	assert.True(t, trie.Insert("user.updated"))
	assert.True(t, trie.Insert("order.created"))

	// 重复插入
	assert.False(t, trie.Insert("user.created"))

	// 通配符插入
	assert.True(t, trie.Insert("user.*"))
	assert.True(t, trie.Insert("user.+.detail"))
	assert.True(t, trie.Insert("order.#"))

	assert.Equal(t, 6, trie.Len())
}

func TestTopicTrie_Insert_InvalidPattern(t *testing.T) {
	trie := newTopicTrie()

	// 空主题
	assert.False(t, trie.Insert(""))
	assert.False(t, trie.Insert("   "))

	// # 不在末尾（应该失败）
	assert.False(t, trie.Insert("user.#.detail"))
}

func TestTopicTrie_Match_ExactMatch(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("user.created")
	trie.Insert("user.updated")
	trie.Insert("order.created")

	matches := trie.Match("user.created")
	assert.Equal(t, []string{"user.created"}, matches)

	matches = trie.Match("user.deleted")
	assert.Empty(t, matches)
}

func TestTopicTrie_Match_SingleWildcard(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("user.*")
	trie.Insert("user.+.detail")

	// * 匹配末尾单层
	matches := trie.Match("user.created")
	assert.Contains(t, matches, "user.*")

	// + 匹配中间单层
	matches = trie.Match("user.123.detail")
	assert.Contains(t, matches, "user.+.detail")

	// 不匹配多层
	matches = trie.Match("user.123.456.detail")
	assert.NotContains(t, matches, "user.+.detail")
}

func TestTopicTrie_Match_MultiWildcard(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("order.#")
	trie.Insert("user.event.#")

	// # 匹配单层
	matches := trie.Match("order.created")
	assert.Contains(t, matches, "order.#")

	// # 匹配多层
	matches = trie.Match("order.item.created")
	assert.Contains(t, matches, "order.#")

	matches = trie.Match("order.item.detail.updated")
	assert.Contains(t, matches, "order.#")

	// 前缀不匹配
	matches = trie.Match("user.created")
	assert.NotContains(t, matches, "order.#")

	// user.event.# 匹配
	matches = trie.Match("user.event.login")
	assert.Contains(t, matches, "user.event.#")

	matches = trie.Match("user.event.login.success")
	assert.Contains(t, matches, "user.event.#")
}

func TestTopicTrie_Match_MixedPatterns(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("user.created")
	trie.Insert("user.*")
	trie.Insert("user.#")
	trie.Insert("+.created")

	matches := trie.Match("user.created")
	assert.Len(t, matches, 4)
	assert.Contains(t, matches, "user.created")
	assert.Contains(t, matches, "user.*")
	assert.Contains(t, matches, "user.#")
	assert.Contains(t, matches, "+.created")
}

func TestTopicTrie_Match_GlobalWildcard(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("#")

	// # 单独使用匹配所有
	matches := trie.Match("any.topic")
	assert.Contains(t, matches, "#")

	matches = trie.Match("a.b.c.d.e")
	assert.Contains(t, matches, "#")
}

func TestTopicTrie_MatchOne(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("user.*")
	trie.Insert("order.#")

	assert.True(t, trie.MatchOne("user.created"))
	assert.True(t, trie.MatchOne("order.item.created"))
	assert.False(t, trie.MatchOne("product.created"))
}

func TestTopicTrie_Remove(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("user.created")
	trie.Insert("user.updated")
	trie.Insert("user.*")

	assert.Equal(t, 3, trie.Len())

	// 移除存在的模式
	assert.True(t, trie.Remove("user.created"))
	assert.Equal(t, 2, trie.Len())

	// 移除不存在的模式
	assert.False(t, trie.Remove("user.created"))
	assert.False(t, trie.Remove("user.deleted"))

	// 验证匹配
	matches := trie.Match("user.created")
	assert.NotContains(t, matches, "user.created")
	assert.Contains(t, matches, "user.*")
}

func TestTopicTrie_Has(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("user.created")
	trie.Insert("user.*")

	assert.True(t, trie.Has("user.created"))
	assert.True(t, trie.Has("user.*"))
	assert.False(t, trie.Has("user.updated"))
}

func TestTopicTrie_Patterns(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("user.created")
	trie.Insert("user.*")
	trie.Insert("order.#")

	patterns := trie.Patterns()
	assert.Len(t, patterns, 3)
	assert.Contains(t, patterns, "user.created")
	assert.Contains(t, patterns, "user.*")
	assert.Contains(t, patterns, "order.#")
}

func TestTopicTrie_Clear(t *testing.T) {
	trie := newTopicTrie()
	trie.Insert("user.created")
	trie.Insert("user.*")

	assert.Equal(t, 2, trie.Len())

	trie.Clear()
	assert.Equal(t, 0, trie.Len())
	assert.Empty(t, trie.Patterns())
	assert.Empty(t, trie.Match("user.created"))
}

func TestTopicTrie_Concurrent(t *testing.T) {
	trie := newTopicTrie()
	var wg sync.WaitGroup
	concurrency := 100

	// 并发插入
	wg.Add(concurrency)
	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			trie.Insert("topic." + string(rune('a'+idx%26)))
		}(i)
	}
	wg.Wait()

	// 并发匹配
	wg.Add(concurrency)
	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			trie.Match("topic." + string(rune('a'+idx%26)))
		}(i)
	}
	wg.Wait()

	// 并发读写
	wg.Add(concurrency * 2)
	for i := range concurrency {
		go func(idx int) {
			defer wg.Done()
			trie.Insert("concurrent." + string(rune('a'+idx%26)))
		}(i)
		go func(idx int) {
			defer wg.Done()
			trie.Match("concurrent." + string(rune('a'+idx%26)))
		}(i)
	}
	wg.Wait()
}

func TestTopicTrie_NormalizeSeparators(t *testing.T) {
	trie := newTopicTrie()

	// 使用 / 分隔符
	trie.Insert("user/created")
	trie.Insert("user/*")

	// 应该被规范化为 . 分隔符
	assert.True(t, trie.Has("user.created"))
	assert.True(t, trie.Has("user.*"))

	// 匹配时也应该规范化
	matches := trie.Match("user/updated")
	assert.Contains(t, matches, "user.*")
}

func BenchmarkTopicTrie_Insert(b *testing.B) {
	trie := newTopicTrie()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.Insert("user.event.created")
	}
}

func BenchmarkTopicTrie_Match_Exact(b *testing.B) {
	trie := newTopicTrie()
	for i := range 100 {
		trie.Insert("topic." + string(rune('a'+i%26)) + ".event")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.Match("topic.a.event")
	}
}

func BenchmarkTopicTrie_Match_Wildcard(b *testing.B) {
	trie := newTopicTrie()
	trie.Insert("user.*")
	trie.Insert("user.+.detail")
	trie.Insert("order.#")
	for i := range 100 {
		trie.Insert("topic." + string(rune('a'+i%26)) + ".event")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.Match("user.created")
	}
}

func BenchmarkTopicTrie_MatchOne(b *testing.B) {
	trie := newTopicTrie()
	trie.Insert("user.*")
	trie.Insert("order.#")
	for i := range 100 {
		trie.Insert("topic." + string(rune('a'+i%26)) + ".event")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		trie.MatchOne("user.created")
	}
}

func TestTopicTrie_SingleWildcards_DoNotOverrideEachOther(t *testing.T) {
	trie := newTopicTrie()
	require.True(t, trie.Insert("user.+"))
	require.True(t, trie.Insert("user.*"))

	matches := trie.Match("user.created")
	assert.Contains(t, matches, "user.+")
	assert.Contains(t, matches, "user.*")

	require.True(t, trie.Remove("user.+"))
	matches = trie.Match("user.created")
	assert.NotContains(t, matches, "user.+")
	assert.Contains(t, matches, "user.*")

	require.True(t, trie.Remove("user.*"))
	assert.Empty(t, trie.Match("user.created"))
}
