package utils

import (
	"errors"
	"io"
	"net"
	"strings"

	"go.uber.org/zap"
)

type NetErrorClass string

const (
	NetErrorNone             NetErrorClass = "none"
	NetErrorEOF              NetErrorClass = "eof"
	NetErrorClosedConnection NetErrorClass = "closed_connection"
	NetErrorConnectionReset  NetErrorClass = "connection_reset"
	NetErrorBrokenPipe       NetErrorClass = "broken_pipe"
	NetErrorTimeout          NetErrorClass = "timeout"
	NetErrorCanceled         NetErrorClass = "canceled"
	NetErrorTemporary        NetErrorClass = "temporary"
	NetErrorUnexpected       NetErrorClass = "unexpected"
)

type RelayErrorLog struct {
	Operation string
	Direction string
	Target    string
	Err       error
}

// ClassifyNetError 将底层网络错误归类，避免调用方只能做简单的 true/false 判断。
func ClassifyNetError(err error) NetErrorClass {
	if err == nil {
		return NetErrorNone
	}
	if errors.Is(err, io.EOF) {
		return NetErrorEOF
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return NetErrorTimeout
		}
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "use of closed network connection"),
		strings.Contains(msg, "net: connection closed"),
		strings.Contains(msg, "connection closed"):
		return NetErrorClosedConnection
	case strings.Contains(msg, "connection reset by peer"),
		strings.Contains(msg, "forcibly closed by the remote host"),
		strings.Contains(msg, "connection reset"):
		return NetErrorConnectionReset
	case strings.Contains(msg, "broken pipe"),
		strings.Contains(msg, "wsasend"),
		strings.Contains(msg, "wsaecconnaborted"):
		return NetErrorBrokenPipe
	case strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "deadline exceeded"):
		return NetErrorTimeout
	case strings.Contains(msg, "operation was canceled"),
		strings.Contains(msg, "operation canceled"),
		strings.Contains(msg, "context canceled"):
		return NetErrorCanceled
	default:
		return NetErrorUnexpected
	}
}

// IsExpectedNetError 表示该错误属于代理转发过程中常见、预期内的连接生命周期错误。
func IsExpectedNetError(err error) bool {
	switch ClassifyNetError(err) {
	case NetErrorNone,
		NetErrorEOF,
		NetErrorClosedConnection,
		NetErrorConnectionReset,
		NetErrorBrokenPipe,
		NetErrorCanceled:
		return true
	default:
		return false
	}
}

// IsIgnorableError 兼容旧调用。新代码优先使用 ClassifyNetError 或 IsExpectedNetError。
func IsIgnorableError(err error) bool {
	return IsExpectedNetError(err)
}

// LogRelayError 根据网络错误分类选择日志级别：预期内断开降为 Debug，超时/临时错误为 Warn，未知错误为 Error。
func LogRelayError(logger *zap.SugaredLogger, event RelayErrorLog) {
	if event.Err == nil {
		return
	}
	if logger == nil {
		logger = zap.S()
	}

	class := ClassifyNetError(event.Err)
	switch class {
	case NetErrorEOF,
		NetErrorClosedConnection,
		NetErrorConnectionReset,
		NetErrorBrokenPipe,
		NetErrorCanceled:
		logger.Debugf(
			"[relay] %s %s expected %s target=%s err=%v",
			event.Direction,
			event.Operation,
			class,
			event.Target,
			event.Err,
		)
	case NetErrorTimeout,
		NetErrorTemporary:
		logger.Warnf(
			"[relay] %s %s transient %s target=%s err=%v",
			event.Direction,
			event.Operation,
			class,
			event.Target,
			event.Err,
		)
	default:
		logger.Errorf(
			"[relay] %s %s unexpected %s target=%s err=%v",
			event.Direction,
			event.Operation,
			class,
			event.Target,
			event.Err,
		)
	}
}
