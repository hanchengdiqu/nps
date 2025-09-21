// Package client 中的 health.go 负责客户端侧的健康检查调度与上报。
//
// 设计要点（重要）：
//  1. 支持多目标的 TCP/HTTP 健康检查，目标以逗号分隔存放于 HealthCheckTarget 中。
//  2. 使用一个最小堆（sheap.IntHeap）按“下一次检查时间”的时间戳进行调度，
//     每到期一次就触发相应 Health 的检查，并计算下次时间后重新入堆。
//  3. 对每个目标维护失败计数 HealthMap[v]：
//     - 当检查失败时计数 +1；
//     - 当检查成功且此前计数已达到阈值（HealthMaxFail）时，认为从失败恢复，向服务端上报恢复（"1"），并清零计数；
//     - 当失败计数累加到阈值的整数倍（即每达到 HealthMaxFail 次失败），认为处于/仍处于失败状态，向服务端上报失败（"0"）。
//  4. 通过 serverConn.SendHealthInfo(target, flag) 与服务端通信：flag="0" 表示失败（移除），flag="1" 表示恢复（添加）。
//  5. 为避免并发竞争，对单个 Health 的 HealthMap 访问需加锁（t.Lock()/Unlock()）。
//  6. 仅在 HealthMaxFail、HealthCheckTimeout、HealthCheckInterval 都为正时，才会调度该 Health。
//
// 注意：本文件仅新增了详细注释，不改变任何原有逻辑。
package client

import (
	"container/heap"
	"net"
	"net/http"
	"strings"
	"time"

	"ehang.io/nps/lib/conn"
	"ehang.io/nps/lib/file"
	"ehang.io/nps/lib/sheap"
	"github.com/astaxie/beego/logs"
	"github.com/pkg/errors"
)

// isStart 表示健康检查调度是否已经初始化启动。
var isStart bool

// serverConn 保存到服务端的连接，用于上报健康状态变化。
var serverConn *conn.Conn

// heathCheck 初始化并启动健康检查调度。
//
// 参数：
//   - healths: 需要被健康检查的配置切片，每个元素对应一组检查配置（同一个端口，多目标）。
//   - c:       与服务端通信的连接，用于上报结果。
//
// 返回：
//   - bool: 启动是否成功（当前实现恒为 true）。
//
// 行为：
//   - 若已启动过（isStart==true），仅为传入的每个 Health 重置 HealthMap（失败计数字典），然后返回。
//   - 若首次启动：
//   - 为每个 Health 设置初始下一次检查时间（当前时间 + Interval），并将该时间戳入堆；
//   - 为每个 Health 初始化 HealthMap；
//   - 启动调度循环 goroutine（session）。
func heathCheck(healths []*file.Health, c *conn.Conn) bool {
	serverConn = c
	if isStart {
		// 已经启动过的情况下，只需重置失败计数容器，避免旧状态干扰。
		for _, v := range healths {
			v.HealthMap = make(map[string]int)
		}
		return true
	}
	isStart = true

	// 最小堆：存放各 Health 的下一次检查 Unix 时间（秒）。
	h := &sheap.IntHeap{}
	for _, v := range healths {
		// 仅当三个关键参数都为正时才调度该 Health。
		if v.HealthMaxFail > 0 && v.HealthCheckTimeout > 0 && v.HealthCheckInterval > 0 {
			v.HealthNextTime = time.Now().Add(time.Duration(v.HealthCheckInterval) * time.Second)
			heap.Push(h, v.HealthNextTime.Unix())
			v.HealthMap = make(map[string]int)
		}
	}

	// 启动异步的调度循环。
	go session(healths, h)
	return true
}

// session 是健康检查的调度循环：
//   - 不断从最小堆中取出“最近的下一次检查时间”；
//   - 计算距离当前时间的剩余秒数，定时器到点后，
//     扫描所有 Health，将下一次检查时间已到期的项触发检查并重置其下一次时间，然后重新入堆。
//
// 说明：
// - 这里将时间戳作为 int64 存入最小堆，保证每次总是最先处理最近的到期时间。
// - 当堆为空时认为出错并退出（正常情况下不会为 0，因为初始化时至少推入了有效项）。
func session(healths []*file.Health, h *sheap.IntHeap) {
	for {
		if h.Len() == 0 {
			logs.Error("health check error")
			break
		}

		// 最近到期时间与当前时间的差值（单位：秒）。
		rs := heap.Pop(h).(int64) - time.Now().Unix()
		if rs <= 0 {
			// 若已经过期，则继续下一轮（等待下一次入堆的时间）。
			continue
		}

		// 等待到期。
		timer := time.NewTimer(time.Duration(rs) * time.Second)
		select {
		case <-timer.C:
			for _, v := range healths {
				// 检查是否到达该 Health 的下次检查时间。
				if v.HealthNextTime.Before(time.Now()) {
					// 更新下一次检查时间。
					v.HealthNextTime = time.Now().Add(time.Duration(v.HealthCheckInterval) * time.Second)
					// 触发检查（异步）。
					go check(v)
					// 将新的下一次检查时间重新放入堆中。
					heap.Push(h, v.HealthNextTime.Unix())
				}
			}
		}
	}
}

// check 对单个 Health 配置执行一次健康检查。
//
// 适用场景：单一端口、多目标（HealthCheckTarget 以逗号分隔）。
// 行为：
//   - 当 HealthCheckType=="tcp" 时，尝试以超时时间发起 TCP 连接；成功即认为健康。
//   - 否则按 HTTP 方式：GET http://{target}{HttpHealthUrl}，要求返回码为 200 才视为健康。
//   - 根据结果更新失败计数 HealthMap[target]，并在阈值到达或恢复时通过 serverConn 上报。
func check(t *file.Health) {
	arr := strings.Split(t.HealthCheckTarget, ",")
	var err error
	var rs *http.Response
	for _, v := range arr {
		if t.HealthCheckType == "tcp" {
			// TCP 健康检查：在指定超时时间内建立连接。
			var c net.Conn
			c, err = net.DialTimeout("tcp", v, time.Duration(t.HealthCheckTimeout)*time.Second)
			if err == nil {
				c.Close()
			}
		} else {
			// HTTP 健康检查：要求返回 200。
			client := &http.Client{}
			client.Timeout = time.Duration(t.HealthCheckTimeout) * time.Second
			rs, err = client.Get("http://" + v + t.HttpHealthUrl)
			if err == nil && rs.StatusCode != 200 {
				err = errors.New("status code is not match")
			}
		}

		// 下面更新失败计数与上报，需加锁保护。
		t.Lock()
		if err != nil {
			// 本次检查失败：计数 +1。
			t.HealthMap[v] += 1
		} else if t.HealthMap[v] >= t.HealthMaxFail {
			// 本次检查成功且此前失败计数已经达到阈值：认为从失败恢复，通知服务端恢复（"1"），并清零计数。
			serverConn.SendHealthInfo(v, "1")
			t.HealthMap[v] = 0
		}

		// 当失败计数达到阈值的整数倍（例如阈值=3，累计失败 3、6、9... 次）：
		// 向服务端上报失败（"0"）。
		if t.HealthMap[v] > 0 && t.HealthMap[v]%t.HealthMaxFail == 0 {
			serverConn.SendHealthInfo(v, "0")
		}
		t.Unlock()
	}
}
