package metrics

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func WsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.S().Errorf("failed to upgrade http to websocket: %v", err)
		return
	}
	defer conn.Close()

	// 订阅原始流量事件
	ch := Subscribe()
	defer Unsubscribe(ch)

	done := make(chan struct{})

	// 消费者端计算 Point
	go func() {
		var lastTime time.Time
		var deltaU, deltaD uint64
		var smoothU, smoothD float64
		smoothingAlpha := 0.3

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case event := <-ch:
				// 累加原始字节数
				if event.IsUpload {
					deltaU += event.ByteCount
				} else {
					deltaD += event.ByteCount
				}
			case <-ticker.C:
				now := time.Now()
				elapsed := now.Sub(lastTime).Seconds()
				if elapsed <= 0 {
					elapsed = 1.0
				}
				lastTime = now
				// 计算即时速率
				instU := float64(deltaU) / elapsed
				instD := float64(deltaD) / elapsed
				// EMA 平滑
				if smoothU == 0 && smoothD == 0 {
					smoothU = instU
					smoothD = instD
				} else {
					smoothU = smoothingAlpha*instU + (1.0-smoothingAlpha)*smoothU
					smoothD = smoothingAlpha*instD + (1.0-smoothingAlpha)*smoothD
				}
				pt := Point{
					Ts:            now.Unix(),
					UploadSpeed:   uint64(smoothU),
					DownloadSpeed: uint64(smoothD),
				}
				if err := conn.WriteJSON(pt); err != nil {
					return
				}
				deltaU = 0
				deltaD = 0
			}
		}
	}()

	// 保持连接
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			close(done)
			break
		}
	}
}
