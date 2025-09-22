// Package file 提供客户端排序功能
// 实现了基于客户端流量数据的排序算法，支持按不同字段进行升序或降序排序
package file

import (
	"reflect"
	"sort"
	"sync"
)

// Pair 排序键值对结构体
// 用于存储排序所需的关键信息，包括排序字段、客户端ID、排序顺序和流量数据
type Pair struct {
	key        string // 排序字段名称
	cId        int    // 客户端ID
	order      string // 排序顺序 ("asc"升序 或 "desc"降序)
	clientFlow *Flow  // 客户端流量数据指针
}

// PairList Pair切片类型
// 实现了sort.Interface接口，用于对客户端数据进行排序
type PairList []*Pair

// Swap 交换两个元素的位置
// 实现sort.Interface接口的Swap方法
func (p PairList) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

// Len 返回切片长度
// 实现sort.Interface接口的Len方法
func (p PairList) Len() int { return len(p) }

// Less 比较两个元素的大小
// 实现sort.Interface接口的Less方法，根据order字段决定排序顺序
// 参数:
//   i, j - 要比较的两个元素的索引
// 返回值:
//   bool - 如果i应该排在j前面则返回true
func (p PairList) Less(i, j int) bool {
	// 如果是降序排列
	if p[i].order == "desc" {
		// 使用反射获取流量数据中指定字段的值进行比较
		return reflect.ValueOf(*p[i].clientFlow).FieldByName(p[i].key).Int() < reflect.ValueOf(*p[j].clientFlow).FieldByName(p[j].key).Int()
	}
	// 默认升序排列
	return reflect.ValueOf(*p[i].clientFlow).FieldByName(p[i].key).Int() > reflect.ValueOf(*p[j].clientFlow).FieldByName(p[j].key).Int()
}

// sortClientByKey 根据指定字段对客户端进行排序
// 将sync.Map中的客户端数据转换为PairList，然后按指定字段排序并返回客户端ID列表
// 参数:
//   m - 存储客户端数据的sync.Map
//   sortKey - 排序字段名称（如"ExportFlow"、"InletFlow"等）
//   order - 排序顺序（"asc"升序 或 "desc"降序）
// 返回值:
//   []int - 按排序顺序排列的客户端ID列表
func sortClientByKey(m sync.Map, sortKey, order string) (res []int) {
	// 创建空的PairList用于存储排序数据
	p := make(PairList, 0)
	// 遍历sync.Map中的所有客户端
	m.Range(func(key, value interface{}) bool {
		// 将客户端数据转换为Pair结构并添加到列表中
		p = append(p, &Pair{sortKey, value.(*Client).Id, order, value.(*Client).Flow})
		return true
	})
	// 对PairList进行排序
	sort.Sort(p)
	// 提取排序后的客户端ID列表
	for _, v := range p {
		res = append(res, v.cId)
	}
	return
}
