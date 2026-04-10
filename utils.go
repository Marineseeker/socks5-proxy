package main

import (
	"errors"
	"io"
	"strings"
	"sync"
)

var pool = sync.Pool{
	New: func() any {
		b := make([]byte, 64*1024)
		return &b
	},
}

func isIgnorableError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "use of closed network connection") {
		return true
	}
	if strings.Contains(msg, "connection reset by peer") {
		return true
	}
	if strings.Contains(msg, "broken pipe") {
		return true
	}
	if strings.Contains(msg, "i/o timeout") {
		return true
	}
	return false
}
