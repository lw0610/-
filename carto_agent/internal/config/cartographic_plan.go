package config

import "fmt"

type CartographicPlanConfig struct {
	Confirm string `yaml:"confirm" json:"confirm"`
}

func (c CartographicPlanConfig) WithDefaults() CartographicPlanConfig {
	if c.Confirm == "" {
		c.Confirm = "when_needed"
	}
	return c
}
func (c CartographicPlanConfig) Validate() error {
	switch c.Confirm {
	case "when_needed", "always", "never":
		return nil
	}
	return fmt.Errorf("计划确认必须为 when_needed、always 或 never")
}
func (cs *ConfigService) GetCartographicPlanConfig() CartographicPlanConfig {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.config.Plan.WithDefaults()
}
