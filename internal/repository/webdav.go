package repository

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var remoteNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ConfigureWebDAV writes a minimal rclone WebDAV remote without putting the
// plaintext password in command arguments or retaining it in project files.
func ConfigureWebDAV(rclonePath, configPath, remoteName, endpoint, vendor, username, password string) error {
	if !remoteNamePattern.MatchString(remoteName) {
		return fmt.Errorf("rclone remote 名称只能包含字母、数字、横线和下划线")
	}
	if configPath == "" {
		return fmt.Errorf("rclone 配置路径不能为空")
	}
	for label, value := range map[string]string{"WebDAV 地址": endpoint, "服务类型": vendor, "用户名": username} {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("%s 不能包含换行", label)
		}
	}
	u, err := url.ParseRequestURI(endpoint)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return fmt.Errorf("WebDAV 地址必须是有效的 HTTPS URL")
	}
	if vendor == "" {
		vendor = "other"
	}
	existing, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	section := "[" + remoteName + "]"
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == section {
			return fmt.Errorf("rclone remote %s 已存在，拒绝覆盖", remoteName)
		}
	}
	cmd := exec.Command(rclonePath, "obscure", "-")
	cmd.Stdin = strings.NewReader(password + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	obscured, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("处理 WebDAV 密码: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return err
	}
	content := strings.TrimRight(string(existing), "\n")
	if content != "" {
		content += "\n\n"
	}
	content += fmt.Sprintf("[%s]\ntype = webdav\nurl = %s\nvendor = %s\nuser = %s\npass = %s\n", remoteName, endpoint, vendor, username, strings.TrimSpace(string(obscured)))
	tmp, err := os.CreateTemp(filepath.Dir(configPath), ".rclone-*.conf")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if len(existing) > 0 {
		_ = os.WriteFile(configPath+".bak", existing, 0600)
	}
	return os.Rename(tmpPath, configPath)
}
