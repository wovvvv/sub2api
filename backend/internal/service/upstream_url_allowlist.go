package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func resolveEffectiveUpstreamURLAllowlist(settingService *SettingService, cfg *config.Config) config.URLAllowlistConfig {
	if settingService != nil {
		return settingService.GetEffectiveUpstreamURLAllowlist(context.Background())
	}
	if cfg != nil {
		return cfg.Security.URLAllowlist
	}
	return config.URLAllowlistConfig{
		Enabled:           false,
		AllowPrivateHosts: true,
		AllowInsecureHTTP: true,
	}
}
