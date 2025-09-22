// Package sheap 提供了一个基于 Go 标准库 container/heap 接口的最小堆实现。
//
// 该包主要用于 NPS 项目中的健康检查调度系统，通过维护一个按时间戳排序的最小堆，
// 实现对多个健康检查任务的高效调度。堆顶始终是下一个需要执行检查的任务。
//
// 使用示例：
//   h := &sheap.IntHeap{}
//   heap.Init(h)
//   heap.Push(h, int64(1234567890))  // 推入时间戳
//   nextTime := heap.Pop(h).(int64)  // 弹出最小时间戳
//
// 注意：本包实现的是最小堆，即堆顶元素是最小值。
package sheap

// IntHeap 是一个 int64 类型的最小堆实现。
// 它实现了 container/heap 包要求的 heap.Interface 接口，
// 包括 sort.Interface（Len、Less、Swap）和 Push、Pop 方法。
//
// 在 NPS 项目中，IntHeap 主要用于存储健康检查任务的下次执行时间戳，
// 通过最小堆的特性确保总是能快速获取到最早需要执行的任务。
//
// 类型定义：
//   - 底层数据结构：[]int64 切片
//   - 堆类型：最小堆（堆顶是最小值）
//   - 时间复杂度：Push/Pop 操作为 O(log n)，Peek 操作为 O(1)
type IntHeap []int64

// Len 返回堆中元素的数量。
// 这是 sort.Interface 接口的一部分，被 container/heap 包调用。
//
// 返回值：堆中 int64 元素的个数
func (h IntHeap) Len() int { return len(h) }

// Less 比较索引 i 和 j 处的元素大小，用于维护最小堆的性质。
// 这是 sort.Interface 接口的一部分，被 container/heap 包调用。
//
// 参数：
//   i, j: 要比较的两个元素的索引
//
// 返回值：
//   如果 h[i] < h[j] 返回 true，否则返回 false
//   由于返回 h[i] < h[j]，所以实现的是最小堆（小的元素优先级高）
func (h IntHeap) Less(i, j int) bool { return h[i] < h[j] }

// Swap 交换索引 i 和 j 处的两个元素。
// 这是 sort.Interface 接口的一部分，被 container/heap 包调用。
//
// 参数：
//   i, j: 要交换的两个元素的索引
func (h IntHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

// Push 向堆中添加一个新元素。
// 这是 heap.Interface 接口的一部分，由 container/heap.Push() 函数调用。
//
// 注意：
//   - 使用指针接收器是因为该方法会修改切片的长度，而不仅仅是内容
//   - 新元素被添加到切片的末尾，heap 包会自动调整其位置以维护堆的性质
//   - 调用方应该使用 heap.Push(h, x) 而不是直接调用此方法
//
// 参数：
//   x: 要添加的元素，类型为 interface{}，内部会断言为 int64
//
// 时间复杂度：O(log n)
func (h *IntHeap) Push(x interface{}) {
	// Push and Pop use pointer receivers because they modify the slice's length,
	// not just its contents.
	*h = append(*h, x.(int64))
}

// Pop 从堆中移除并返回最小元素（堆顶元素）。
// 这是 heap.Interface 接口的一部分，由 container/heap.Pop() 函数调用。
//
// 注意：
//   - 使用指针接收器是因为该方法会修改切片的长度
//   - 该方法移除并返回切片的最后一个元素，heap 包会在调用前将最小元素移到末尾
//   - 调用方应该使用 heap.Pop(h) 而不是直接调用此方法
//
// 返回值：
//   被移除的元素，类型为 interface{}，实际为 int64
//
// 时间复杂度：O(log n)
func (h *IntHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
