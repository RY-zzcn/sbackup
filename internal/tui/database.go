package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sbackup/internal/config"
	"sbackup/internal/terminal"
)

func databaseMenu(c *config.Config, configPath string, in *bufio.Reader, save func() error) {
	for {
		printHeader("数据库来源管理")
		if len(c.Databases) == 0 {
			fmt.Println("  尚未配置数据库来源。")
		} else {
			for i, database := range c.Databases {
				fmt.Printf("  %d  %s %s %s\n", i+1, padDisplay(truncate(database.Name, 20), 22), padDisplay(database.Type, 12), database.ID)
			}
		}
		fmt.Println()
		printMenuItem("1", "新增数据库来源", "安全保存连接凭据")
		printMenuItem("2", "查看数据库配置", "不显示密码内容")
		printMenuItem("3", "删除数据库来源", "仅允许删除未被任务引用的来源")
		printMenuItem("0", "返回任务管理", "")
		printHint("数据库备份会先导出到临时目录，再与文件来源一起写入 Restic 快照。")
		fmt.Print("\n  请选择 › ")
		choice, _ := in.ReadString('\n')
		switch strings.TrimSpace(choice) {
		case "1":
			addDatabase(c, configPath, in, save)
		case "2":
			if database, ok := selectDatabase(c, in); ok {
				showDatabase(in, *database)
			}
		case "3":
			deleteDatabase(c, in, save)
		case "0", "":
			return
		default:
			printWarning("无效选择，请输入菜单编号。")
		}
	}
}

func addDatabase(c *config.Config, configPath string, in *bufio.Reader, save func() error) {
	fmt.Println("\n  数据库类型:")
	fmt.Println("  1  PostgreSQL")
	fmt.Println("  2  MySQL / MariaDB")
	fmt.Println("  3  SQLite")
	fmt.Print("  请选择，直接回车取消 › ")
	choice, _ := in.ReadString('\n')
	typeName := map[string]string{"1": "postgres", "2": "mysql", "3": "sqlite"}[strings.TrimSpace(choice)]
	if typeName == "" {
		return
	}
	id := ask(in, "来源 ID（英文小写）", typeName)
	name := ask(in, "显示名称", strings.ToUpper(typeName)+" 备份")
	if id == "" {
		return
	}
	if databaseExists(c, id) {
		printFailure("数据库来源 ID 已存在。")
		return
	}
	database := config.Database{ID: id, Name: name, Type: typeName, ConnectTimeout: "15s"}
	var credentialPath string
	if typeName == "sqlite" {
		database.Path = ask(in, "SQLite 数据库文件绝对路径", "/srv/app/data/app.db")
		if !filepath.IsAbs(database.Path) {
			printFailure("SQLite 路径必须是绝对路径。")
			return
		}
	} else {
		database.Host = ask(in, "数据库主机", "127.0.0.1")
		defaultPort := 5432
		if typeName == "mysql" {
			defaultPort = 3306
		}
		database.Port = askNonNegativeInt(in, "数据库端口", defaultPort)
		database.Database = ask(in, "数据库名称", "app")
		database.Username = ask(in, "数据库用户名", "backup")
		password, err := terminal.ReadPassword("数据库密码: ", in)
		if err != nil || password == "" {
			printFailure("未读取到数据库密码，已取消。")
			return
		}
		credentialPath = filepath.Join(filepath.Dir(configPath), "secrets", "databases", id+".json")
		database.CredentialFile = credentialPath
		if typeName == "postgres" {
			database.Format = "custom"
			database.SSLMode = ask(in, "SSL 模式", "prefer")
		} else {
			database.SingleTransaction = true
		}
		if err := writeJSONSecret(credentialPath, map[string]string{"password": password}); err != nil {
			printFailure("保存数据库凭据失败: " + err.Error())
			return
		}
	}
	old := append([]config.Database{}, c.Databases...)
	c.Databases = append(c.Databases, database)
	if err := c.Validate(); err != nil {
		c.Databases = old
		if credentialPath != "" {
			_ = os.Remove(credentialPath)
		}
		printFailure("数据库配置无效: " + err.Error())
		return
	}
	if err := save(); err != nil {
		c.Databases = old
		if credentialPath != "" {
			_ = os.Remove(credentialPath)
		}
		printFailure("保存配置失败: " + err.Error())
		return
	}
	printSuccess("数据库来源已添加。可在任务编辑中引用 ID: " + id)
}

func databaseExists(c *config.Config, id string) bool {
	if _, ok := c.Storage(id); ok {
		return true
	}
	if _, ok := c.Job(id); ok {
		return true
	}
	for _, notification := range c.Notifications {
		if notification.ID == id {
			return true
		}
	}
	for i := range c.Databases {
		if c.Databases[i].ID == id {
			return true
		}
	}
	return false
}

func selectDatabase(c *config.Config, in *bufio.Reader) (*config.Database, bool) {
	if len(c.Databases) == 0 {
		printWarning("尚未配置数据库来源。")
		return nil, false
	}
	for i, database := range c.Databases {
		fmt.Printf("  %d  %s (%s, %s)\n", i+1, database.Name, database.ID, database.Type)
	}
	fmt.Print("  输入编号，直接回车返回 › ")
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(c.Databases) {
		return nil, false
	}
	return &c.Databases[n-1], true
}

func showDatabase(in *bufio.Reader, database config.Database) {
	printHeader("数据库来源配置")
	field("名称", database.Name)
	field("来源 ID", database.ID)
	field("类型", database.Type)
	if database.Type == "sqlite" {
		field("文件路径", database.Path)
	} else {
		field("主机", database.Host)
		field("端口", strconv.Itoa(database.Port))
		field("数据库", database.Database)
		field("用户名", database.Username)
		field("凭据文件", database.CredentialFile)
		field("连接超时", emptyLabel(database.ConnectTimeout))
		if database.Type == "postgres" {
			field("SSL 模式", emptyLabel(database.SSLMode))
			field("导出格式", emptyLabel(database.Format))
		} else {
			field("单事务", yesNo(database.SingleTransaction))
		}
	}
	pause(in)
}

func deleteDatabase(c *config.Config, in *bufio.Reader, save func() error) {
	database, ok := selectDatabase(c, in)
	if !ok {
		return
	}
	for _, job := range c.Jobs {
		for _, id := range job.Sources.Databases {
			if id == database.ID {
				printFailure("该来源正在被任务 " + job.Name + " 使用，请先从任务配置中移除。")
				return
			}
		}
	}
	id, credentialPath := database.ID, database.CredentialFile
	fmt.Printf("  输入数据库来源 ID %s 确认删除 › ", styled(ansiBold+ansiRed, id))
	line, _ := in.ReadString('\n')
	if strings.TrimSpace(line) != id {
		printWarning("已取消删除。")
		return
	}
	old := append([]config.Database{}, c.Databases...)
	for i := range c.Databases {
		if c.Databases[i].ID == id {
			c.Databases = append(c.Databases[:i], c.Databases[i+1:]...)
			break
		}
	}
	if err := save(); err != nil {
		c.Databases = old
		printFailure("保存配置失败: " + err.Error())
		return
	}
	if credentialPath != "" {
		_ = os.Remove(credentialPath)
	}
	printSuccess("数据库来源已删除。")
}

func writeJSONSecret(path string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	removeOnError := true
	defer func() {
		_ = f.Close()
		if removeOnError {
			_ = os.Remove(path)
		}
	}()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	removeOnError = false
	return nil
}
