package main

import (
	"flag"
	"net/http"
	"time"

	"github.com/socks5-proxy/cmd"
	"github.com/socks5-proxy/metrics"
	"github.com/socks5-proxy/socks"
	"github.com/socks5-proxy/socks_tun"
	"github.com/socks5-proxy/utils"
	"go.uber.org/zap"
)

var (
	blockedTTL  time.Duration
	blockedFile string

	httpAddr string
	interval time.Duration
)

func main() {
	var listenAddr string

	utils.InitLogger()
	defer utils.Sync()

	flag.StringVar(&httpAddr, "http-addr", ":8080", "HTTP static file server address to listen on")
	flag.StringVar(&listenAddr, "listen", ":1080", "local address to listen on")
	flag.DurationVar(&blockedTTL, "blocked-ttl", 10*time.Minute, "duration to block an address")
	flag.StringVar(&blockedFile, "blocked-file", "blocked.txt", "file to store permanently blocked addresses")
	flag.DurationVar(&interval, "interval", 1*time.Second, "interval for metrics aggregation, it indecates both the frequency of generating new time series points and the window size for smoothing the metrics")
	flag.Parse()

	socks.NewBlockedCache(blockedTTL, blockedFile)
	zap.L().Info("starting socks5 server")

	go cmd.HandleCommandLine()

	go startHTTPServer(httpAddr)

	if err := metrics.StartAggregator(interval); err != nil {
		zap.S().Fatalf("failed to start aggregator: %v", err)
	}

	tunDevice := socks_tun.NewTunDevice(0x400000)
	tunDevice.Start("SocksTun0")

	socks.RunServer(listenAddr)
}

func startHTTPServer(addr string) {
	http.Handle("/", http.FileServer(http.Dir(".")))

	zap.S().Infof("HTTP server listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		zap.S().Errorf("http server failed: %v", err)
	}
}
