package runtime

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Ensure installs a missing optional runtime only when a configured feature
// needs it. Restic remains a core dependency installed by the base installer.
func Ensure(tool string) error {
	if _, err := exec.LookPath(tool); err == nil {
		return nil
	}
	if filepath.Base(tool) != "rclone" {
		return fmt.Errorf("缺少运行时组件 %s", tool)
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("配置 WebDAV 需要安装 rclone，请使用 sudo sbackup 重新进入菜单")
	}
	script, err := installerScript()
	if err != nil {
		return err
	}
	cmd := exec.Command(script, "--rclone-only", "--update")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("安装 rclone: %w", err)
	}
	if _, err := exec.LookPath(tool); err != nil {
		return fmt.Errorf("rclone 安装完成但当前 PATH 中仍不可用")
	}
	return nil
}

func installerScript() (string, error) {
	candidates := []string{
		"/usr/local/share/sbackup/scripts/install-runtime-tools.sh",
		"scripts/install-runtime-tools.sh",
	}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), "..", "share", "sbackup", "scripts", "install-runtime-tools.sh"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("找不到组件安装脚本，请重新运行 SBackup 一键安装脚本")
}
