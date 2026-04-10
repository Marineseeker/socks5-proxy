package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/socks5-proxy/cmd"
	"github.com/socks5-proxy/metrics"
)

var (
	blockedTTL  time.Duration
	blockedFile string
	blocked     *blockedCache
	wsAddr      string
)

func main() {
	var listenAddr string

	flag.StringVar(&wsAddr, "ws-addr", ":8080", "websocket address to listen on")
	flag.StringVar(&listenAddr, "listen", ":1080", "local address to listen on")
	flag.DurationVar(&blockedTTL, "blocked-ttl", 10*time.Minute, "duration to block an address")
	flag.StringVar(&blockedFile, "blocked-file", "blocked.txt", "file to store permanently blocked addresses")
	flag.Parse()

	blocked = newBlockedCache(blockedTTL, blockedFile)
	log.Printf("starting socks5 server")
	// 启动全局流量聚合器，每5秒生成一个时间序列点并上报到当前用户
	go metrics.StartMetricsAggregator(1 * time.Second)
	// 启动命令行接口为 goroutine，使其能并发接收 stdin 输入
	go cmd.HandleCommandLine()
	// 启动 WebSocket 服务器
	go startWebSocketServer(wsAddr)
	// 启动服务器（阻塞）
	runServer(listenAddr)
}

func startWebSocketServer(addr string) {
	// 注册静态文件和 metrics 路由
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		metrics.WsHandler(w, r, 1*time.Second) // 传入 interval 参数
	})
	http.HandleFunc("/metrics/series", metrics.SeriesHandler)
	http.HandleFunc("/metrics/latest", metrics.LatestHandler)
	// 静态文件使用当前目录（项目根），index.html 在根目录
	http.Handle("/", http.FileServer(http.Dir(".")))

	log.Printf("HTTP/WebSocket server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}
