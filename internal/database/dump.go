package database

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"sbackup/internal/config"
	"sbackup/internal/executor"
)

func Dump(ctx context.Context, c *config.Config, d config.Database, targetDir string, logger *executor.Logger) (string, error) {
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return "", err
	}
	switch d.Type {
	case "postgres":
		return dumpPostgres(ctx, c, d, targetDir, logger)
	case "mysql":
		return dumpMySQL(ctx, c, d, targetDir, logger)
	case "sqlite":
		return dumpSQLite(ctx, c, d, targetDir, logger)
	default:
		return "", fmt.Errorf("不支持的数据库类型 %s", d.Type)
	}
}
func credentials(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]string
	if json.Unmarshal(b, &m) == nil {
		return m, nil
	}
	m = map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		p := strings.SplitN(strings.TrimSpace(line), "=", 2)
		if len(p) == 2 {
			m[p[0]] = p[1]
		}
	}
	return m, nil
}
func dumpPostgres(ctx context.Context, c *config.Config, d config.Database, dir string, l *executor.Logger) (string, error) {
	creds, err := credentials(d.CredentialFile)
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, d.ID+".dump")
	pgpass := filepath.Join(dir, "."+d.ID+".pgpass")
	host := d.Host
	if host == "" {
		host = "localhost"
	}
	port := d.Port
	if port == 0 {
		port = 5432
	}
	line := strings.Join([]string{pgpassEscape(host), strconv.Itoa(port), pgpassEscape(d.Database), pgpassEscape(d.Username), pgpassEscape(creds["password"])}, ":") + "\n"
	if err := os.WriteFile(pgpass, []byte(line), 0600); err != nil {
		return "", err
	}
	defer os.Remove(pgpass)
	args := []string{"-h", host, "-p", strconv.Itoa(port), "-U", d.Username, "-F", "c", "-f", out, d.Database}
	res := executor.Run(ctx, l, "database-dump", c.Tools.PGDumpPath, args, []string{"PGPASSFILE=" + pgpass, "PGSSLMODE=" + d.SSLMode}, nil)
	if res.Err != nil {
		return "", fmt.Errorf("PostgreSQL 导出失败: %w", res.Err)
	}
	return out, nil
}
func dumpMySQL(ctx context.Context, c *config.Config, d config.Database, dir string, l *executor.Logger) (string, error) {
	creds, err := credentials(d.CredentialFile)
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, d.ID+".sql")
	defaults := filepath.Join(dir, "."+d.ID+".cnf")
	port := d.Port
	if port == 0 {
		port = 3306
	}
	content := fmt.Sprintf("[client]\nhost=%s\nport=%d\nuser=%s\npassword=%s\n", mySQLConfigValue(d.Host), port, mySQLConfigValue(d.Username), mySQLConfigValue(creds["password"]))
	if err := os.WriteFile(defaults, []byte(content), 0600); err != nil {
		return "", err
	}
	defer os.Remove(defaults)
	args := []string{"--defaults-extra-file=" + defaults, "--quick"}
	if d.SingleTransaction {
		args = append(args, "--single-transaction")
	}
	args = append(args, d.Database)
	cmd := exec.CommandContext(ctx, c.Tools.MySQLDumpPath, args...)
	f, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	cmd.Stdout = f
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if l != nil {
		l.Log("info", "database-dump", "执行 mysqldump（凭据已隐藏）")
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("MySQL 导出失败: %s", executor.Redact(stderr.String()))
	}
	return out, nil
}

func pgpassEscape(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `:`, `\:`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, "\r", `\r`)
}

func mySQLConfigValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return strings.ReplaceAll(value, "\r", `\r`)
}
func dumpSQLite(ctx context.Context, c *config.Config, d config.Database, dir string, l *executor.Logger) (string, error) {
	outPath := filepath.Join(dir, d.ID+".db")
	res := executor.Run(ctx, l, "database-dump", c.Tools.SQLitePath, []string{d.Path, ".timeout 5000", ".backup " + outPath}, nil, nil)
	if res.Err != nil {
		return "", fmt.Errorf("SQLite 在线备份失败: %w", res.Err)
	}
	if err := os.Chmod(outPath, 0600); err != nil {
		return "", err
	}
	return outPath, nil
}
