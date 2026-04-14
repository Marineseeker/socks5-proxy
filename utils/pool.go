package utils

import "sync"

var Pool = sync.Pool{
	New: func() any {
		b := make([]byte, 64*1024)
		return &b
	},
}
