package zap_initializer

import (
	"encoding/json"
	"fmt"
	"os"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

// InitLogger 读取配置并替换 zap 的全局实例
func InitLogger() {
	configBytes, err := os.ReadFile("config.yaml")
	if err != nil {
		// 初始加载失败通常是致命的
		panic(fmt.Sprintf("failed to read config: %v", err))
	}

	// 1. 将 YAML 转换为通用 map
	var configMap map[string]interface{}
	if err := yaml.Unmarshal(configBytes, &configMap); err != nil {
		panic(fmt.Sprintf("failed to unmarshal yaml: %v", err))
	}

	// 2. 转换为 JSON (这是为了适配 zap.Config 的标准 json 标签)
	data, err := json.Marshal(configMap)
	if err != nil {
		panic(err)
	}

	// 3. 反序列化为 zap 的 Config 结构体
	var zapConfig zap.Config
	if err := json.Unmarshal(data, &zapConfig); err != nil {
		panic(fmt.Sprintf("invalid zap config: %v", err))
	}

	// 4. 构建 Logger 实例
	logger, err := zapConfig.Build()
	if err != nil {
		panic(fmt.Sprintf("failed to build zap: %v", err))
	}

	// 核心步骤：将这个 logger 替换为 zap 的全局单例
	zap.ReplaceGlobals(logger)
}

func Sync() {
	// 直接同步全局单例
	_ = zap.L().Sync()
}
