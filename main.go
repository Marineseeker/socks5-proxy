package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/socks5-proxy/metrics"
	"github.com/socks5-proxy/user"
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
	metrics.StartMetricsAggregator(1 * time.Second)
	// 启动命令行接口为 goroutine，使其能并发接收 stdin 输入
	go commandLineInterface()
	// 启动 WebSocket 服务器
	go startWebSocketServer(wsAddr)
	// 启动服务器（阻塞）
	runServer(listenAddr)
}

func startWebSocketServer(addr string) {
	// 注册静态文件和 metrics 路由
	http.HandleFunc("/ws", metrics.WsHandler)
	http.HandleFunc("/metrics/series", metrics.SeriesHandler)
	http.HandleFunc("/metrics/latest", metrics.LatestHandler)
	// 静态文件使用当前目录（项目根），index.html 在根目录
	http.Handle("/", http.FileServer(http.Dir(".")))

	log.Printf("HTTP/WebSocket server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}

func commandLineInterface() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Type 'show <username>' to view traffic, 'exit' to quit")
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "exit" {
			os.Exit(0)
		}

		parts := strings.Fields(line)
		if len(parts) == 2 && parts[0] == "show" {
			username := parts[1]
			u := user.GetCurrentUser()
			if u == nil {
				fmt.Printf("User %s not found\n", username)
				continue
			}

			// 使用 atomic 保证读取安全
			upload, download := u.GetFlow()
			fmt.Printf("User: %s, Upload: %d bytes, Download: %d bytes\n",
				username, upload, download)
		} else {
			fmt.Println("Invalid command. Usage: show <username>")
		}
	}
}
