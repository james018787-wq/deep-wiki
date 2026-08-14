// Package config 负责读取与承载服务全局配置。
package config

import (
	"net/url"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 全局配置根节点，对应 config/config.yaml。
type Config struct {
	Server ServerConfig `yaml:"server"`
	MySQL  MySQLConfig  `yaml:"mysql"`
	Git    GitConfig    `yaml:"git"`
	Vector VectorConfig `yaml:"vector"`
	LLM    LLMConfig    `yaml:"llm"`
}

// ServerConfig 服务运行配置。
type ServerConfig struct {
	Name string `yaml:"name"`
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"` // debug / release / test
}

// MySQLConfig 数据库连接配置。
type MySQLConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	User            string `yaml:"user"`
	Password        string `yaml:"password"`
	Database        string `yaml:"database"`
	Charset         string `yaml:"charset"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"`
}

// GitConfig 代码仓库配置。
type GitConfig struct {
	RepoURL      string `yaml:"repo_url"`
	CloneDir     string `yaml:"clone_dir"`
	DefaultBranch string `yaml:"default_branch"`
}

// VectorConfig 向量库配置。
type VectorConfig struct {
	Engine     string `yaml:"engine"` // redis / milvus / faiss ...
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	Collection string `yaml:"collection"`
}

// LLMConfig 大模型配置。
type LLMConfig struct {
	Provider string `yaml:"provider"` // openai / ollama / deepseek ...
	BaseURL  string `yaml:"base_url"`
	APIKey   string `yaml:"api_key"`
	Model    string `yaml:"model"`
}

// Load 从指定路径加载配置文件。
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.ApplyEnv()
	return cfg, nil
}

// ApplyEnv 用环境变量覆盖配置（docker-compose 场景）。
// 约定：环境变量优先级高于 yaml 配置，未设置的环境变量不影响 yaml 值。
func (c *Config) ApplyEnv() {
	// 服务
	c.Server.Port = envInt("SERVER_PORT", c.Server.Port)
	c.Server.Mode = envStr("SERVER_MODE", c.Server.Mode)

	// 数据库（docker-compose 使用 DB_HOST / DB_PORT / DB_USER / DB_PASSWORD / DB_NAME）
	c.MySQL.Host = envStr("DB_HOST", c.MySQL.Host)
	c.MySQL.Port = envInt("DB_PORT", c.MySQL.Port)
	c.MySQL.User = envStr("DB_USER", c.MySQL.User)
	c.MySQL.Password = envStr("DB_PASSWORD", c.MySQL.Password)
	c.MySQL.Database = envStr("DB_NAME", c.MySQL.Database)

	// 向量库（CHROMA_URL 形如 http://chroma:8000）
	if url := envStr("CHROMA_URL", ""); url != "" {
		host, port := parseURLHostPort(url, 8000)
		c.Vector.Host = host
		c.Vector.Port = port
	}

	// LLM 服务地址（Python 微服务）
	c.LLM.BaseURL = envStr("LLM_SERVICE_URL", c.LLM.BaseURL)

	// Git 仓库
	c.Git.CloneDir = envStr("GIT_CLONE_DIR", c.Git.CloneDir)
}

// envStr 读取字符串环境变量，为空时返回默认值。
func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envInt 读取整数环境变量，解析失败时返回默认值。
func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// parseURLHostPort 解析形如 http://host:port 的地址，返回 host 与 port。
func parseURLHostPort(raw string, defaultPort int) (string, int) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return "", defaultPort
	}
	port := defaultPort
	if u.Port() != "" {
		if n, err := strconv.Atoi(u.Port()); err == nil {
			port = n
		}
	}
	return u.Hostname(), port
}
