package metrics

// TrafficMetric 表示某客户端在一个统计周期内的流量数据
type TrafficMetric struct {
	Timestamp     int64  `json:"timestamp"`
	ClientIP      string `json:"client_ip"`
	UploadBytes   uint64 `json:"upload_bytes"`
	DownloadBytes uint64 `json:"download_bytes"`
}

// ClientCounter 单个客户端的流量计数器
type ClientCounter struct {
	Upload   uint64
	Download uint64
}
