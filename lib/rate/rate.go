// Package rate 实现了基于令牌桶算法的速率限制器
// 用于控制网络流量或API请求的速率，防止系统过载
package rate

import (
	"sync/atomic"
	"time"
)

// Rate 基于令牌桶算法的速率限制器
// 令牌桶算法允许突发流量，同时限制平均速率
type Rate struct {
	bucketSize        int64     // 令牌桶的最大容量
	bucketSurplusSize int64     // 当前桶中剩余的令牌数量
	bucketAddSize     int64     // 每秒向桶中添加的令牌数量（速率限制值）
	stopChan          chan bool // 用于停止速率限制器的通道
	NowRate           int64     // 当前的实际使用速率
}

// NewRate 创建一个新的速率限制器
// addSize: 每秒允许的最大令牌数量（即速率限制值）
// 桶的容量设置为addSize的2倍，允许一定程度的突发流量
func NewRate(addSize int64) *Rate {
	return &Rate{
		bucketSize:        addSize * 2,  // 桶容量为速率的2倍
		bucketSurplusSize: 0,            // 初始时桶为空
		bucketAddSize:     addSize,      // 每秒添加的令牌数
		stopChan:          make(chan bool),
	}
}

// Start 启动速率限制器
// 启动后台goroutine定期向桶中添加令牌
func (s *Rate) Start() {
	go s.session()
}

// add 向令牌桶中添加指定数量的令牌
// size: 要添加的令牌数量
// 如果添加后会超过桶的容量，则只添加到桶满为止
func (s *Rate) add(size int64) {
	if res := s.bucketSize - s.bucketSurplusSize; res < s.bucketAddSize {
		atomic.AddInt64(&s.bucketSurplusSize, res)
		return
	}
	atomic.AddInt64(&s.bucketSurplusSize, size)
}

// ReturnBucket 将令牌返回到桶中
// size: 要返回的令牌数量
// 通常在操作取消或失败时调用，将已消耗的令牌归还
func (s *Rate) ReturnBucket(size int64) {
	s.add(size)
}

// Stop 停止速率限制器
// 停止后台的令牌添加goroutine
func (s *Rate) Stop() {
	s.stopChan <- true
}

// Get 从令牌桶中获取指定数量的令牌
// size: 需要获取的令牌数量
// 如果桶中令牌不足，会阻塞等待直到有足够的令牌
// 这是速率限制的核心方法，调用者会被限制在指定的速率内
func (s *Rate) Get(size int64) {
	// 如果桶中有足够的令牌，直接扣除并返回
	if s.bucketSurplusSize >= size {
		atomic.AddInt64(&s.bucketSurplusSize, -size)
		return
	}
	
	// 如果令牌不足，每100毫秒检查一次
	ticker := time.NewTicker(time.Millisecond * 100)
	for {
		select {
		case <-ticker.C:
			if s.bucketSurplusSize >= size {
				atomic.AddInt64(&s.bucketSurplusSize, -size)
				ticker.Stop()
				return
			}
		}
	}
}

// session 后台会话，定期向令牌桶添加令牌并计算当前速率
// 每秒执行一次：
// 1. 计算当前实际使用速率
// 2. 向桶中添加新的令牌
func (s *Rate) session() {
	ticker := time.NewTicker(time.Second * 1)
	for {
		select {
		case <-ticker.C:
			// 计算当前速率：如果桶中令牌少于添加量，说明在使用中
			if rs := s.bucketAddSize - s.bucketSurplusSize; rs > 0 {
				s.NowRate = rs
			} else {
				// 如果桶中令牌很多，显示桶的使用情况
				s.NowRate = s.bucketSize - s.bucketSurplusSize
			}
			// 每秒向桶中添加指定数量的令牌
			s.add(s.bucketAddSize)
		case <-s.stopChan:
			ticker.Stop()
			return
		}
	}
}
