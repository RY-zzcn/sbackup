package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"sbackup/internal/monitor"
)

var version = "dev"

func main() {
	listen := flag.String("listen", "127.0.0.1:8788", "")
	data := flag.String("data", "/var/lib/sbackup-monitor/state.json", "")
	nodeID := flag.String("add-node", "", "")
	nodeName := flag.String("node-name", "", "")
	nodeKey := flag.String("node-key", "", "")
	nodeKeyFile := flag.String("node-key-file", "", "从权限受限文件读取节点密钥")
	generateKey := flag.Bool("generate-key", false, "生成一次性节点密钥")
	adminUser := flag.String("admin-user", os.Getenv("SBACKUP_MONITOR_USER"), "")
	adminPass := flag.String("admin-password", os.Getenv("SBACKUP_MONITOR_PASSWORD"), "")
	showVersion := flag.Bool("version", false, "显示版本")
	flag.Parse()
	if *showVersion {
		fmt.Println("sbackup-monitor", version)
		return
	}
	s := monitor.New(monitor.Config{Listen: *listen, DataFile: *data, AdminUsername: *adminUser, AdminPassword: *adminPass})
	if *adminUser != "" && *adminPass == "" {
		fmt.Fprintln(os.Stderr, "设置 admin-user 时必须同时设置 admin-password")
		os.Exit(2)
	}
	if *nodeID != "" {
		key := *nodeKey
		if *nodeKeyFile != "" {
			b, err := os.ReadFile(*nodeKeyFile)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			key = string(b)
		}
		if *generateKey {
			b := make([]byte, 32)
			if _, err := rand.Read(b); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			key = hex.EncodeToString(b)
		}
		if err := s.AddNode(*nodeID, *nodeName, key); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("已添加节点", *nodeID)
		if *generateKey {
			fmt.Println("node_secret:", key)
			fmt.Println("请立即保存此密钥；监控端不会再次显示。")
		}
		return
	}
	fmt.Println("SBackup Monitor listening on", *listen)
	if err := s.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
