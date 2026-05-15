package metrics

type Point struct {
	Ts            int64  `json:"ts"`           // unix timestamp
	UploadSpeed   uint64 `json:"upload_speed"` // bytes in this interval
	DownloadSpeed uint64 `json:"download_speed"`
}
