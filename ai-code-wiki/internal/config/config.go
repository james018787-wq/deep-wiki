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
	Server    ServerConfig    `yaml:"server"`
	MySQL     MySQLConfig     `yaml:"mysql"`
	Redis     RedisConfig     `yaml:"redis"`
	Git       GitConfig       `yaml:"git"`
	Vector    VectorConfig    `yaml:"vector"`
	TaskQueue TaskQueueConfig `yaml:"task_queue"`
	Filter    FilterConfig    `yaml:"filter"`
	LLM       LLMConfig       `yaml:"llm"`
}

// RedisConfig 会话记忆 Redis 配置。
type RedisConfig struct {
	Addr     string `yaml:"addr"`     // redis 连接地址，如 redis:6379（REDIS_ADDR）
	Password string `yaml:"password"` // redis 密码（可选，REDIS_PASSWORD）
	DB       int    `yaml:"db"`       // 逻辑库编号（REDIS_DB，默认 0）
	TTLDays  int    `yaml:"ttl_days"` // 会话过期天数（REDIS_TTL_DAYS，默认 7）
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
// 多仓库支持后仓库地址/分支已迁移到 code_repo 表（经 /api/v1/repo/register 注册），
// 此处仅保留克隆目录根（GIT_CLONE_DIR，默认 /app/repo_cache），每个仓库独立子目录。
type GitConfig struct {
	CloneDir string `yaml:"clone_dir"`
}

// FilterConfig 解析流水线文件过滤规则（逗号分隔字符串，空则回退到默认规则）。
type FilterConfig struct {
	IgnoreDirs   string `yaml:"ignore_dirs"`    // 忽略目录名，如 vendor,node_modules,mock,fixture（FILTER_IGNORE_DIRS）
	IgnoreFileRe string `yaml:"ignore_file_re"` // 忽略文件正则（匹配相对路径），如 _test\.go$（FILTER_IGNORE_FILE_REGEX）
	AllowExts    string `yaml:"allow_exts"`     // 允许的代码文件后缀，如 go,php（FILTER_ALLOW_EXTS）
}

// VectorConfig 向量库配置。
type VectorConfig struct {
	Engine     string `yaml:"engine"` // 向量引擎驱动：chroma / milvus（VECTOR_DRIVER 覆盖）
	Host       string `yaml:"host"`   // chroma: CHROMA_URL 解析；milvus: MILVUS_HOST
	Port       int    `yaml:"port"`   // milvus: MILVUS_PORT，默认 19530
	Collection string `yaml:"collection"`
	Dim        int    `yaml:"dim"`      // embedding 向量维度（Milvus 建集合用，MILVUS_DIM 覆盖）
	User       string `yaml:"user"`     // Milvus 用户名（可选，开启鉴权时必填，MILVUS_USER）
	Password   string `yaml:"password"` // Milvus 密码（可选，MILVUS_PASSWORD）
}

// TaskQueueConfig 异步任务队列配置。
type TaskQueueConfig struct {
	Driver      string `yaml:"driver"`       // 队列驱动：memory / rabbitmq（TASK_QUEUE_DRIVER）
	RabbitMQURL string `yaml:"rabbitmq_url"` // RabbitMQ 连接地址（RABBITMQ_URL）
	QueueName   string `yaml:"queue_name"`   // 队列名（TASK_QUEUE_NAME，默认 ai-code-wiki-task）
	MaxRetry    int    `yaml:"max_retry"`    // 任务最大重试次数（TASK_QUEUE_MAX_RETRY，默认 3）
	Concurrency int    `yaml:"concurrency"`  // 消费协程数（TASK_QUEUE_CONCURRENCY，默认 2）
}

// LLMConfig 大模型配置。
type LLMConfig struct {
	Provider        string `yaml:"provider"` // openai / ollama / deepseek ...
	BaseURL         string `yaml:"base_url"`
	APIKey          string `yaml:"api_key"`
	Model           string `yaml:"model"`
	Timeout         int    `yaml:"timeout"`            // LLM 调用超时（秒），默认 60（LLM_TIMEOUT 覆盖）
	MaxCallsPerTask int    `yaml:"max_calls_per_task"` // 单次解析任务 LLM 生成调用预算上限（0=不限，LLM_MAX_CALLS_PER_TASK）
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

	// 向量引擎驱动：VECTOR_DRIVER=chroma/milvus（默认 chroma）
	if v := envStr("VECTOR_DRIVER", ""); v != "" {
		c.Vector.Engine = v
	}

	// 向量库（CHROMA_URL 形如 http://chroma:8000，仅 chroma 引擎使用）
	if url := envStr("CHROMA_URL", ""); url != "" {
		host, port := parseURLHostPort(url, 8000)
		c.Vector.Host = host
		c.Vector.Port = port
	}

	// Milvus 连接参数（VECTOR_DRIVER=milvus 时生效）：
	//   MILVUS_HOST / MILVUS_PORT / MILVUS_COLLECTION / MILVUS_DIM / MILVUS_USER / MILVUS_PASSWORD
	if v := envStr("MILVUS_HOST", ""); v != "" {
		c.Vector.Host = v
	}
	c.Vector.Port = envInt("MILVUS_PORT", c.Vector.Port)
	if v := envStr("MILVUS_COLLECTION", ""); v != "" {
		c.Vector.Collection = v
	}
	if v := envInt("MILVUS_DIM", 0); v > 0 {
		c.Vector.Dim = v
	}
	c.Vector.User = envStr("MILVUS_USER", c.Vector.User)
	c.Vector.Password = envStr("MILVUS_PASSWORD", c.Vector.Password)

	// 异步任务队列（TASK_QUEUE_DRIVER=memory/rabbitmq，生产环境用 rabbitmq）
	c.TaskQueue.Driver = envStr("TASK_QUEUE_DRIVER", c.TaskQueue.Driver)
	c.TaskQueue.RabbitMQURL = envStr("RABBITMQ_URL", c.TaskQueue.RabbitMQURL)
	c.TaskQueue.QueueName = envStr("TASK_QUEUE_NAME", c.TaskQueue.QueueName)
	c.TaskQueue.MaxRetry = envInt("TASK_QUEUE_MAX_RETRY", c.TaskQueue.MaxRetry)
	c.TaskQueue.Concurrency = envInt("TASK_QUEUE_CONCURRENCY", c.TaskQueue.Concurrency)

	// 会话记忆 Redis（REDIS_ADDR / REDIS_PASSWORD / REDIS_DB / REDIS_TTL_DAYS）
	c.Redis.Addr = envStr("REDIS_ADDR", c.Redis.Addr)
	c.Redis.Password = envStr("REDIS_PASSWORD", c.Redis.Password)
	c.Redis.DB = envInt("REDIS_DB", c.Redis.DB)
	c.Redis.TTLDays = envInt("REDIS_TTL_DAYS", c.Redis.TTLDays)

	// 解析流水线文件过滤规则（逗号分隔；未设置时回退到 yaml/默认值）
	c.Filter.IgnoreDirs = envStr("FILTER_IGNORE_DIRS", c.Filter.IgnoreDirs)
	c.Filter.IgnoreFileRe = envStr("FILTER_IGNORE_FILE_REGEX", c.Filter.IgnoreFileRe)
	c.Filter.AllowExts = envStr("FILTER_ALLOW_EXTS", c.Filter.AllowExts)

	// LLM 服务地址（Python 微服务）与调用超时
	c.LLM.BaseURL = envStr("LLM_SERVICE_URL", c.LLM.BaseURL)
	c.LLM.Timeout = envInt("LLM_TIMEOUT", c.LLM.Timeout)
	c.LLM.MaxCallsPerTask = envInt("LLM_MAX_CALLS_PER_TASK", c.LLM.MaxCallsPerTask)

	// 克隆目录根（多仓库：每个仓库独立子目录 {root}/{repo_name}）
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
