package cmd

import (
	"bufio"
	"os"
	"strings"

	"github.com/socks5-proxy/user"
	"go.uber.org/zap"
)

func HandleCommandLine() {
	scanner := bufio.NewScanner(os.Stdin)
	zap.S().Info("Type 'show <username>' to view traffic, 'exit' to quit")
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
				zap.S().Infof("User %s not found", username)
				continue
			}

			// 使用 atomic 保证读取安全
			upload, download := u.GetFlow()
			zap.S().Infof("User: %s, Upload: %d bytes, Download: %d bytes",
				username, upload, download)
		} else {
			zap.S().Info("Invalid command. Usage: show <username>")
		}
	}
}
