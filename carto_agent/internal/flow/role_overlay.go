package flow

import (
	"context"
	"fmt"
)

// ValidateOverlayPackages rechecks the live catalog before a paused run grants anything.
func (c *Compiler) ValidateOverlayPackages(ctx context.Context, scheme string, ids []string) error {
	if c == nil || c.options.LoadEnvironment == nil {
		return fmt.Errorf("工具包目录不可用")
	}
	env, err := c.options.LoadEnvironment(ctx, scheme)
	if err != nil {
		return err
	}
	for _, id := range uniqueStrings(ids) {
		if !FlowPackageGrantable(id, env.Packages) {
			return fmt.Errorf("流程临时工具包已不可授予：%s", id)
		}
	}
	return nil
}
