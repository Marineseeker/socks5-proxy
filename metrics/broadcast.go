// metrics/broadcast.go (重构)
package metrics

import (
	"sync"
)

type Broker struct {
	mu          sync.RWMutex
	subscribers map[chan *RawTrafficEvent]struct{} // 改为广播 RawTrafficEvent
}

var globalBroker = &Broker{
	subscribers: make(map[chan *RawTrafficEvent]struct{}),
}

func Subscribe() chan *RawTrafficEvent {
	globalBroker.mu.Lock()
	defer globalBroker.mu.Unlock()

	ch := make(chan *RawTrafficEvent, 100)
	globalBroker.subscribers[ch] = struct{}{}
	return ch
}

func Unsubscribe(ch chan *RawTrafficEvent) {
	globalBroker.mu.Lock()
	defer globalBroker.mu.Unlock()

	delete(globalBroker.subscribers, ch)
	close(ch)
}

// broadcast 现在广播原始流量事件
func BroadcastRawTraffic(event *RawTrafficEvent) {
	globalBroker.mu.RLock()
	defer globalBroker.mu.RUnlock()

	for ch := range globalBroker.subscribers {
		select {
		case ch <- event:
		default:
			// 通道已满，跳过这个订阅者
		}
	}
}
