package cli

import (
	"fmt"
	"os"

	"github.com/luisdvss/docty/internal/config"
)

var debugPrinted bool

// loadConfig applies the --config flag, loads config.toml + env, and then
// applies command-line overrides (which have the highest precedence).
func loadConfig() (config.Config, error) {
	return loadConfigWith(false)
}

// loadConfigAllowMissing is like loadConfig but accepts a --config path that
// does not exist yet. It is used by commands that create the config file.
func loadConfigAllowMissing() (config.Config, error) {
	return loadConfigWith(true)
}

func loadConfigWith(allowMissing bool) (config.Config, error) {
	cfg, _, err := loadConfigWithOrigins(allowMissing)
	return cfg, err
}

func loadConfigWithOrigins(allowMissing bool) (config.Config, map[string]config.Source, error) {
	if allowMissing {
		if err := applyConfigFlagAllowMissing(); err != nil {
			return config.Config{}, nil, err
		}
	} else if err := applyConfigFlag(); err != nil {
		return config.Config{}, nil, err
	}
	cfg, origins, err := config.LoadWithOrigins()
	if err != nil {
		return cfg, origins, exitWith(2, "%v", err)
	}
	if flagLang != "" {
		cfg.DefaultLang = flagLang
		origins["default_lang"] = config.SourceFlag
	}
	if err := validateStreamFlag(); err != nil {
		return cfg, origins, err
	}
	cfgPager = cfg.Pager
	cfgBootstrap = cfg.Bootstrap
	cfgFormat = cfg.Format
	if cfg.Format == "json" {
		flagJSON = true
	}
	if flagVerbose && !debugPrinted {
		debugPrinted = true
		fmt.Fprintf(os.Stderr, "debug: config=%s data=%s cache=%s lang=%s format=%s pager=%s bootstrap=%v\n",
			config.Path(), config.DataDir(), config.CacheDir(), cfg.DefaultLang, cfg.Format, cfg.Pager, cfg.Bootstrap)
	}
	return cfg, origins, nil
}
