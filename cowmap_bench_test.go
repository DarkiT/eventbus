package eventbus

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

// 数据类型定义
var (
	benchIntSame    = 42
	benchIntSeq     = make([]int, 0, 1024)
	benchStrSame    = "constant-string"
	benchStrSeq     = make([]string, 0, 1024)
	benchStructSame = sampleStruct{A: 1, B: "x", C: []int{1, 2, 3}}
	benchStructSeq  = make([]sampleStruct, 0, 1024)
	benchSliceSame  = []int{1, 2, 3, 4, 5}
	benchSliceSeq   = make([][]int, 0, 1024)
)

func init() {
	for i := range 1024 {
		benchIntSeq = append(benchIntSeq, i)
		benchStrSeq = append(benchStrSeq, fmt.Sprintf("str-%d", i))
		benchStructSeq = append(benchStructSeq, sampleStruct{A: i, B: fmt.Sprintf("str-%d", i), C: []int{i, i + 1}})
		benchSliceSeq = append(benchSliceSeq, []int{i, i + 1, i + 2})
	}
}

// sampleStruct 同时包含可比较与不可比较字段
// 切片字段使其整体不可比较，需要 reflect.DeepEqual
// 注意：保持数据量小以避免基准开销被数据构造主导
// 中文注释解释意图，便于维护

type sampleStruct struct {
	A int
	B string
	C []int
}

// --- 基准公共逻辑 ---

type writeCase struct {
	name   string
	keyFmt string
	values []any
}

// runBench 复用核心逻辑，减少重复
func runBench(b *testing.B, enableValuesEqual bool, cases []writeCase) {
	b.Helper()
	cm := newCowMapWithEqual(enableValuesEqual)

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			// 预热：写入初值，确保「相同值」场景命中 valuesEqual
			if len(c.values) > 0 {
				cm.Store(c.keyFmt, c.values[0])
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				v := c.values[i%len(c.values)]
				cm.Store(c.keyFmt, v)
			}
		})
	}
}

// --- 4 组基准 ---

func BenchmarkStoreWithValuesEqual_SameValue(b *testing.B) {
	runBench(b, true, []writeCase{
		{name: "int", keyFmt: "k-int", values: []any{benchIntSame}},
		{name: "string", keyFmt: "k-str", values: []any{benchStrSame}},
		{name: "struct", keyFmt: "k-struct", values: []any{benchStructSame}},
		{name: "slice", keyFmt: "k-slice", values: []any{benchSliceSame}},
	})
}

func BenchmarkStoreWithValuesEqual_DiffValue(b *testing.B) {
	runBench(b, true, []writeCase{
		{name: "int", keyFmt: "k-int", values: toInterfaces(benchIntSeq)},
		{name: "string", keyFmt: "k-str", values: toInterfaces(benchStrSeq)},
		{name: "struct", keyFmt: "k-struct", values: toInterfaces(benchStructSeq)},
		{name: "slice", keyFmt: "k-slice", values: toInterfaces(benchSliceSeq)},
	})
}

func BenchmarkStoreWithoutValuesEqual_SameValue(b *testing.B) {
	runBench(b, false, []writeCase{
		{name: "int", keyFmt: "k-int", values: []any{benchIntSame}},
		{name: "string", keyFmt: "k-str", values: []any{benchStrSame}},
		{name: "struct", keyFmt: "k-struct", values: []any{benchStructSame}},
		{name: "slice", keyFmt: "k-slice", values: []any{benchSliceSame}},
	})
}

func BenchmarkStoreWithoutValuesEqual_DiffValue(b *testing.B) {
	runBench(b, false, []writeCase{
		{name: "int", keyFmt: "k-int", values: toInterfaces(benchIntSeq)},
		{name: "string", keyFmt: "k-str", values: toInterfaces(benchStrSeq)},
		{name: "struct", keyFmt: "k-struct", values: toInterfaces(benchStructSeq)},
		{name: "slice", keyFmt: "k-slice", values: toInterfaces(benchSliceSeq)},
	})
}

// --- 辅助函数 ---

func toInterfaces[T any](vals []T) []any {
	out := make([]any, len(vals))
	for i := range vals {
		out[i] = vals[i]
	}
	return out
}

// 确保编译器不优化掉值生成逻辑
var sink any

func Benchmark_valuesEqual_raw(b *testing.B) {
	pairs := []struct{ a, b any }{
		{benchIntSame, benchIntSame},
		{benchStrSame, benchStrSame},
		{benchStructSame, benchStructSame},
		{benchSliceSame, benchSliceSame},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := pairs[i%len(pairs)]
		sink = valuesEqual(p.a, p.b)
	}
}

// 确保不可比较类型调用 reflect.DeepEqual
func Test_valuesEqual_struct_slice(t *testing.T) {
	if !valuesEqual(benchStructSame, benchStructSame) {
		t.Fatalf("struct should equal")
	}
	if !valuesEqual(benchSliceSame, benchSliceSame) {
		t.Fatalf("slice should equal")
	}
	// 不同值应判定不相等
	if valuesEqual(benchStructSame, benchStructSeq[1]) {
		t.Fatalf("struct should not equal")
	}
	if valuesEqual(benchSliceSame, benchSliceSeq[1]) {
		t.Fatalf("slice should not equal")
	}
	// 类型不同直接 false
	if valuesEqual(benchIntSame, benchStrSame) {
		t.Fatalf("different type should not equal")
	}
}

// 避免静态未使用告警
func Test_noopt(t *testing.T) {
	sink = rand.Int()
	if reflect.ValueOf(sink).IsZero() { // 始终为 false，仅防优化
		t.Fatal("should not happen")
	}
}
