package main

import (
	"flag"
	"net/http"
	"time"

	"github.com/socks5-proxy/cmd"
	"github.com/socks5-proxy/metrics"
	"github.com/socks5-proxy/metrics_v2"
	"github.com/socks5-proxy/socks"
	"github.com/socks5-proxy/socks_tun"
	"github.com/socks5-proxy/utils"
	"go.uber.org/zap"
)

var (
	blockedTTL  time.Duration
	blockedFile string

	wsAddr   string
	interval time.Duration
)

func main() {
	var listenAddr string

	utils.InitLogger()
	defer utils.Sync()

	flag.StringVar(&wsAddr, "ws-addr", ":8080", "websocket address to listen on")
	flag.StringVar(&listenAddr, "listen", ":1080", "local address to listen on")
	flag.DurationVar(&blockedTTL, "blocked-ttl", 10*time.Minute, "duration to block an address")
	flag.StringVar(&blockedFile, "blocked-file", "blocked.txt", "file to store permanently blocked addresses")
	flag.DurationVar(&interval, "interval", 1*time.Second, "interval for metrics aggregation, it indecates both the frequency of generating new time series points and the window size for smoothing the metrics")
	flag.Parse()

	socks.NewBlockedCache(blockedTTL, blockedFile)
	zap.L().Info("starting socks5 server")
	// 启动全局流量聚合器，每5秒生成一个时间序列点并上报到当前用户
	// todo 参数可配置化
	// go metrics.StartMetricsAggregatorV2(interval, 0.3) // 传入 interval 和 smoothingAlpha 参数

	// 启动命令行接口为 goroutine，使其能并发接收 stdin 输入
	go cmd.HandleCommandLine()

	// 启动 WebSocket 服务器
	go startWebSocketServer(wsAddr, interval)

	// 启动 Kafka 处理器
	go metrics.StartKafkaHandler()

	// 启动流量聚合器（替代原来的 KafkaHandler 和 WebSocket）
	if err := metrics_v2.StartAggregator(interval); err != nil {
		zap.S().Fatalf("failed to start aggregator: %v", err)
	}

	tunDevice := socks_tun.NewTunDevice(0x400000)
	tunDevice.Start("SocksTun0")

	// 启动服务器（阻塞）
	socks.RunServer(listenAddr)
}

func startWebSocketServer(addr string, interval time.Duration) {
	// 注册静态文件和 metrics 路由
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// todo interval 参数可以配置化
		metrics.WsHandler(w, r, interval) // 传入 interval 参数
	})
	http.HandleFunc("/metrics/series", metrics.SeriesHandler)
	http.HandleFunc("/metrics/latest", metrics.LatestHandler)
	// 静态文件使用当前目录（项目根），index.html 在根目录
	http.Handle("/", http.FileServer(http.Dir(".")))

	zap.S().Infof("HTTP/WebSocket server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		zap.S().Errorf("http server failed: %v", err)
	}
}
