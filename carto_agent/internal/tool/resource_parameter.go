package tool

import (
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/resource"
)

// ValidateResourceParameter shares the resource contract between tool and
// runtime skill definitions; the model-facing value is always a string.
func ValidateResourceParameter(kind, expected, defaultID string) error {
	if kind != "resource" {
		if expected != "" || defaultID != "" {
			return fmt.Errorf("resource_type 与 default_resource 只能用于 resource 参数")
		}
		return nil
	}
	parts := strings.SplitN(expected, ":", 2)
	spec, ok := resource.TypeInfo(parts[0])
	if !ok {
		return fmt.Errorf("resource_type 必须是有效资源类型")
	}
	if len(parts) == 2 {
		if _, exists := spec.Formats[parts[1]]; parts[0] != "layout" || !exists {
			return fmt.Errorf("resource_type 的格式限定无效：%s", expected)
		}
	}
	if defaultID != "" && (!resource.ValidID(defaultID) || !strings.HasPrefix(defaultID, parts[0]+".")) {
		return fmt.Errorf("default_resource 必须是同类型的有效资源编号")
	}
	return nil
}
