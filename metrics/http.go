package metrics

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func setCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// SeriesHandler 返回全部时间序列点
func SeriesHandler(w http.ResponseWriter, r *http.Request) {
	setCORS(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	series := GetSeries()
	_ = json.NewEncoder(w).Encode(series)
}

// LatestHandler 返回自 since 之后的新点（query: ?since=unix_ts）
func LatestHandler(w http.ResponseWriter, r *http.Request) {
	setCORS(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	q := r.URL.Query().Get("since")
	var since int64
	if q != "" {
		if v, err := strconv.ParseInt(q, 10, 64); err == nil {
			since = v
		}
	}

	all := GetSeries()
	var out []Point
	for _, p := range all {
		if p.Ts > since {
			out = append(out, p)
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}
