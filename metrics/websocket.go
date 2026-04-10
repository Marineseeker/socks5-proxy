package metrics

import (
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
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
		log.Println("upgrader error:", err)
		return
	}
	defer conn.Close()

	log.Println("Client Connected")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		point := GetLastPoint()
		if err := conn.WriteJSON(point); err != nil {
			log.Println("write error:", err)
			return
		}
	}

	log.Println("Client Disconnected")
}
