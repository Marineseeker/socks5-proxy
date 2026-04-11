package metrics

import (
	"sync"
)

type Broker struct {
	mu          sync.RWMutex
	subscribers map[chan Point]struct{}
}

var globalBroker = &Broker{
	subscribers: make(map[chan Point]struct{}),
}

func Subscribe() chan Point {
	globalBroker.mu.Lock()
	defer globalBroker.mu.Unlock()

	ch := make(chan Point, 100) // 带缓冲的通道，避免阻塞
	globalBroker.subscribers[ch] = struct{}{}
	return ch
}

func Unsubscribe(ch chan Point) {
	globalBroker.mu.Lock()
	defer globalBroker.mu.Unlock()

	delete(globalBroker.subscribers, ch)
	close(ch)
}

func broadcast(pt Point) {
	globalBroker.mu.RLock()
	defer globalBroker.mu.RUnlock()

	for ch := range globalBroker.subscribers {
		select {
		case ch <- pt:
		default:
			// 如果通道已满，跳过这个订阅者，避免阻塞
		}
	}
}
