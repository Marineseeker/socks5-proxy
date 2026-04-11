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

func WsHandler(w http.ResponseWriter, r *http.Request, interval time.Duration) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.S().Errorf("upgrader error: %v", err)
		return
	}
	defer conn.Close()

	zap.S().Info("Client Connected")
	ch := Subscribe()
	defer Unsubscribe(ch)
	for pt := range ch {
		if err := conn.WriteJSON(pt); err != nil {
			zap.S().Errorf("failed to write point to websocket: %v", err)
			break
		}
	}
	zap.S().Info("Client Disconnected")
}
