// Package llm 多模型池调度（优先低价）。
//
// 职责：
//   - 维护模型池配置（config/model.yaml，支持热重载与 ${ENV} 密钥占位）；
//   - 调度器按「平均单价升序」优先低价，失败自动降级切换下一档；
//   - 熔断（Redis）与 RPM/TPM 分布式限流（Redis）由独立组件承载；
//   - 业务错误不切换模型，仅可重试错误（429/5xx/超时/网络）触发降级。
//
// 仅改造 LLM 调用层，RAG 检索链路（向量召回、prompt 组装）不涉及。
package llm

import (
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-code-wiki/pkg/logger"

	"gopkg.in/yaml.v3"
)

// ModelItem 单个模型配置项（对应 config/model.yaml 的 model_pool 元素）。
type ModelItem struct {
	Name        string  `yaml:"name"`        // 模型唯一标识（同时作为请求的 model 参数）
	Provider    string  `yaml:"provider"`    // 供应商：deepseek / aliyun / openai ...
	ApiKey      string  `yaml:"api_key"`     // 密钥，支持 ${ENV_VAR} 占位，加载时从环境变量替换
	BaseUrl     string  `yaml:"base_url"`    // OpenAI 兼容接口地址，如 https://api.deepseek.com/v1
	InputPrice  float64 `yaml:"input_price"` // 输入单价（元/1k tokens）
	OutputPrice float64 `yaml:"output_price"`
	MaxContext  int     `yaml:"max_context"` // 最大上下文 token 数
	Rpm         int     `yaml:"rpm"`         // 每分钟请求上限
	Tpm         int     `yaml:"tpm"`         // 每分钟 token 上限
	Enable      bool    `yaml:"enable"`      // 开关
}

// avgPrice 平均单价（调度排序依据）。
func (m *ModelItem) avgPrice() float64 {
	return (m.InputPrice + m.OutputPrice) / 2
}

// GlobalConfig 全局调度参数（对应 config/model.yaml 的 global 段）。
type GlobalConfig struct {
	MaxRetrySwitch            int     `yaml:"max_retry_switch"`             // 最多降级切换次数
	CircuitTTL                int     `yaml:"circuit_ttl_sec"`              // 熔断时长（秒）
	CircuitFailureThreshold   int     `yaml:"circuit_failure_threshold"`    // 连续 N 次可重试错误触发熔断
	RatelimitWindowSec        int     `yaml:"ratelimit_window_sec"`         // RPM/TPM 滑动窗口（秒）
	HighQualityPriceThreshold float64 `yaml:"high_quality_price_threshold"` // force_high_quality 时过滤低于该平均单价的模型
}

// defaultGlobal 默认全局参数（配置缺失时使用）。
func defaultGlobal() GlobalConfig {
	return GlobalConfig{
		MaxRetrySwitch:            2,
		CircuitTTL:                30,
		CircuitFailureThreshold:   3,
		RatelimitWindowSec:        60,
		HighQualityPriceThreshold: 0.2,
	}
}

// applyDefaults 对零值字段回填默认值。
func (g *GlobalConfig) applyDefaults() {
	def := defaultGlobal()
	if g.MaxRetrySwitch <= 0 {
		g.MaxRetrySwitch = def.MaxRetrySwitch
	}
	if g.CircuitTTL <= 0 {
		g.CircuitTTL = def.CircuitTTL
	}
	if g.CircuitFailureThreshold <= 0 {
		g.CircuitFailureThreshold = def.CircuitFailureThreshold
	}
	if g.RatelimitWindowSec <= 0 {
		g.RatelimitWindowSec = def.RatelimitWindowSec
	}
	if g.HighQualityPriceThreshold <= 0 {
		g.HighQualityPriceThreshold = def.HighQualityPriceThreshold
	}
}

// modelFile model.yaml 文件结构。
type modelFile struct {
	ModelPool []*ModelItem `yaml:"model_pool"`
	Global    GlobalConfig `yaml:"global"`
}

// reloadInterval 热重载轮询间隔。
const reloadInterval = 5 * time.Second

// ModelPool 模型池：加载 config/model.yaml 并热重载。
type ModelPool struct {
	path string

	mu     sync.RWMutex
	global GlobalConfig
	models []*ModelItem
	mtime  time.Time

	stop chan struct{}
}

// NewModelPool 加载模型池并启动热重载协程。
// 文件缺失/解析失败时仅告警并保留空池（Q&A 返回“模型池未配置”），不阻断服务启动。
func NewModelPool(path string) *ModelPool {
	p := &ModelPool{path: path, stop: make(chan struct{})}
	p.reload()
	go p.watch()
	return p
}

// Close 停止热重载协程。
func (p *ModelPool) Close() {
	close(p.stop)
}

// watch 周期轮询 model.yaml 修改时间，变更时热重载（无需重启服务）。
func (p *ModelPool) watch() {
	ticker := time.NewTicker(reloadInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			info, err := os.Stat(p.path)
			if err != nil {
				continue // 文件暂不存在，等待下次
			}
			p.mu.RLock()
			changed := !info.ModTime().Equal(p.mtime)
			p.mu.RUnlock()
			if changed {
				logger.Info(nil, "[llm] model.yaml 变更，热重载模型池: %s", p.path)
				p.reload()
			}
		}
	}
}

// reload 重新加载 model.yaml，失败时保留旧配置。
func (p *ModelPool) reload() {
	cfg, err := loadModelFile(p.path)
	if err != nil {
		logger.Error(nil, "[llm] 加载模型配置失败，保留旧配置: %v", err)
		return
	}
	cfg.Global.applyDefaults()
	for _, m := range cfg.ModelPool {
		m.ApiKey = expandEnv(m.ApiKey)
	}

	p.mu.Lock()
	p.global = cfg.Global
	p.models = cfg.ModelPool
	p.mtime = lastMtime(p.path)
	p.mu.Unlock()
	logger.Info(nil, "[llm] 模型池加载完成，共 %d 个模型", len(p.models))
}

// loadModelFile 读取并解析 model.yaml（错误返回原始错误，供 reload 告警）。
func loadModelFile(path string) (*modelFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg modelFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// lastMtime 返回文件修改时间；读取失败返回零值。
func lastMtime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// expandEnv 解析 ${ENV_VAR} 占位符为环境变量值；未定义时留空并告警。
func expandEnv(s string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	out := os.Expand(s, func(key string) string {
		v := os.Getenv(key)
		if v == "" {
			logger.Warn(nil, "[llm] 环境变量 %s 未配置，对应密钥为空", key)
		}
		return v
	})
	return out
}

// Global 返回全局调度参数（副本）。
func (p *ModelPool) Global() GlobalConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.global
}

// Get 按名称查询模型（仅启用模型；未启用返回 nil）。
func (p *ModelPool) Get(name string) *ModelItem {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, m := range p.models {
		if m.Name == name && m.Enable {
			return m
		}
	}
	return nil
}

// Candidates 返回按调度选项过滤并按平均单价升序排列的候选模型。
//
// 过滤规则：
//   - enable=false 排除；
//   - 预估上下文超过 max_context 的模型排除；
//   - force_high_quality=true 时，过滤平均单价低于阈值的低价模型。
func (p *ModelPool) Candidates(opt SchedulerOption) []*ModelItem {
	p.mu.RLock()
	defer p.mu.RUnlock()

	list := make([]*ModelItem, 0, len(p.models))
	for _, m := range p.models {
		if !m.Enable {
			continue
		}
		if opt.EstimatedTokenLen > 0 && m.MaxContext > 0 && m.MaxContext < opt.EstimatedTokenLen {
			continue
		}
		if opt.ForceHighQuality && m.avgPrice() < p.global.HighQualityPriceThreshold {
			continue
		}
		list = append(list, m)
	}
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].avgPrice() < list[j].avgPrice()
	})
	return list
}
