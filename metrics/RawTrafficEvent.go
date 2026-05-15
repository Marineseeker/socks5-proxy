// metrics/events.go (新增文件)
package metrics

// RawTrafficEvent 表示原始的流量事件
type RawTrafficEvent struct {
	Timestamp int64  `json:"timestamp"`  // 事件发生时间
	ClientIP  string `json:"client_ip"`  // 客户端IP
	IsUpload  bool   `json:"is_upload"`  // true=上传, false=下载
	ByteCount uint64 `json:"byte_count"` // 转发成功的字节数
}
