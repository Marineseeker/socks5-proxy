package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/socks5-proxy/user"
)

func HandleCommandLine() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("Type 'show <username>' to view traffic, 'exit' to quit")
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
				fmt.Printf("User %s not found\n", username)
				continue
			}

			// 使用 atomic 保证读取安全
			upload, download := u.GetFlow()
			fmt.Printf("User: %s, Upload: %d bytes, Download: %d bytes\n",
				username, upload, download)
		} else {
			fmt.Println("Invalid command. Usage: show <username>")
		}
	}
}
