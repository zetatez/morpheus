package cli

import (
	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/internal/configstore"
)

func applyStoredModelConfig(cfg *config.Config) {
	store := configstore.NewStore("")
	current, ok, err := store.Current()
	if err != nil || !ok {
		return
	}
	if current.Provider != "" {
		cfg.Planner.Provider = current.Provider
	}
	if current.Model != "" {
		cfg.Planner.Model = current.Model
	}
	if current.APIKey != "" {
		cfg.Planner.APIKey = current.APIKey
	}
	if current.Endpoint != "" {
		cfg.Planner.Endpoint = current.Endpoint
	}
}
