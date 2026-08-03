package repository

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	runtimeutil "sbackup/internal/runtime"
	"strings"

	"sbackup/internal/config"
	"sbackup/internal/executor"
)

type Runtime struct {
	Repository   string
	PasswordFile string
	Env          []string
	RcloneArgs   []string
}

func Build(c *config.Config, s config.Storage) (Runtime, error) {
	r := Runtime{PasswordFile: s.PasswordFile}
	switch s.Type {
	case "local":
		r.Repository = s.RepositoryPath
	case "webdav":
		if s.WebDAV == nil {
			return r, fmt.Errorf("WebDAV 配置为空")
		}
		root := strings.TrimSuffix(s.WebDAV.RemoteRoot, "/")
		repo := strings.TrimPrefix(s.RepositoryPath, "/")
		r.Repository = "rclone:" + s.WebDAV.RemoteName + ":" + root
		if repo != "" {
			r.Repository += "/" + repo
		}
		conf := s.WebDAV.RcloneConfig
		if conf == "" {
			conf = "/etc/sbackup/rclone.conf"
		}
		r.Env = append(r.Env, "RCLONE_CONFIG="+conf)
		if s.WebDAV.Transfers > 0 {
			r.Env = append(r.Env, fmt.Sprintf("RCLONE_TRANSFERS=%d", s.WebDAV.Transfers))
		}
		if s.WebDAV.Checkers > 0 {
			r.Env = append(r.Env, fmt.Sprintf("RCLONE_CHECKERS=%d", s.WebDAV.Checkers))
		}
		if s.WebDAV.Timeout != "" {
			r.Env = append(r.Env, "RCLONE_TIMEOUT="+s.WebDAV.Timeout)
		}
		if s.WebDAV.Retries > 0 {
			r.Env = append(r.Env, fmt.Sprintf("RCLONE_LOW_LEVEL_RETRIES=%d", s.WebDAV.Retries))
		}
		if s.WebDAV.RetriesSleep != "" {
			r.Env = append(r.Env, "RCLONE_RETRIES_SLEEP="+s.WebDAV.RetriesSleep)
		}
	case "sftp":
		if s.SFTP == nil {
			return r, fmt.Errorf("SFTP 配置为空")
		}
		port := s.SFTP.Port
		if port == 0 {
			port = 22
		}
		r.Repository = fmt.Sprintf("sftp:%s@%s:%s", s.SFTP.Username, s.SFTP.Host, s.SFTP.Path)
		r.Env = append(r.Env, fmt.Sprintf("RESTIC_SFTP_COMMAND=ssh -p %d -i %s", port, s.SFTP.KeyFile))
	case "s3":
		if s.S3 == nil {
			return r, fmt.Errorf("S3 配置为空")
		}
		r.Repository = "s3:" + strings.TrimSuffix(s.S3.Endpoint, "/") + "/" + s.S3.Bucket + "/" + strings.TrimPrefix(s.S3.Prefix, "/")
		if s.S3.CredentialFile != "" {
			b, err := os.ReadFile(s.S3.CredentialFile)
			if err != nil {
				return r, err
			}
			var creds map[string]string
			if err := json.Unmarshal(b, &creds); err != nil {
				return r, err
			}
			r.Env = append(r.Env, "AWS_ACCESS_KEY_ID="+creds["access_key_id"], "AWS_SECRET_ACCESS_KEY="+creds["secret_access_key"])
		}
		if s.S3.Region != "" {
			r.Env = append(r.Env, "AWS_DEFAULT_REGION="+s.S3.Region)
		}
	default:
		return r, fmt.Errorf("不支持的存储类型 %s", s.Type)
	}
	return r, nil
}

func ResticBase(rt Runtime) []string {
	return []string{"-r", rt.Repository, "--password-file", rt.PasswordFile}
}

// CreatePasswordFile creates a Restic password file without overwriting an
// existing secret. An empty password generates a random 256-bit secret.
func CreatePasswordFile(path, password string) (string, bool, error) {
	if path == "" {
		return "", false, fmt.Errorf("密码文件路径不能为空")
	}
	generated := password == ""
	if generated {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return "", false, fmt.Errorf("生成随机密码: %w", err)
		}
		password = base64.RawURLEncoding.EncodeToString(buf)
	}
	password = strings.TrimSuffix(strings.TrimSuffix(password, "\n"), "\r")
	if len(password) < 16 {
		return "", false, fmt.Errorf("Restic 密码至少需要 16 个字符")
	}
	if strings.ContainsAny(password, "\r\n") {
		return "", false, fmt.Errorf("Restic 密码不能包含换行")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", false, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return "", false, fmt.Errorf("密码文件已存在，拒绝覆盖: %s", path)
		}
		return "", false, err
	}
	removeOnError := true
	defer func() {
		_ = f.Close()
		if removeOnError {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.WriteString(password + "\n"); err != nil {
		return "", false, err
	}
	if err := f.Sync(); err != nil {
		return "", false, err
	}
	if err := f.Close(); err != nil {
		return "", false, err
	}
	removeOnError = false
	return password, generated, nil
}
func Test(ctx context.Context, c *config.Config, s config.Storage, logger *executor.Logger) error {
	if s.Type == "webdav" {
		if err := runtimeutil.Ensure(c.Tools.RclonePath); err != nil {
			return err
		}
	}
	rt, err := Build(c, s)
	if err != nil {
		return err
	}
	res := executor.Run(ctx, logger, "storage-test", c.Tools.ResticPath, append(ResticBase(rt), "snapshots", "--json"), rt.Env, nil)
	if res.Err != nil {
		return fmt.Errorf("仓库连接失败: %w", res.Err)
	}
	return nil
}
func Init(ctx context.Context, c *config.Config, s config.Storage, logger *executor.Logger) error {
	if s.Type == "webdav" {
		if err := runtimeutil.Ensure(c.Tools.RclonePath); err != nil {
			return err
		}
	}
	rt, err := Build(c, s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(rt.PasswordFile), 0700); err != nil {
		return err
	}
	if _, err := os.Stat(rt.PasswordFile); os.IsNotExist(err) {
		return fmt.Errorf("仓库密码文件不存在: %s", rt.PasswordFile)
	}
	res := executor.Run(ctx, logger, "repository-init", c.Tools.ResticPath, append(ResticBase(rt), "init"), rt.Env, nil)
	if res.Err != nil {
		return fmt.Errorf("初始化仓库失败: %w", res.Err)
	}
	return nil
}
