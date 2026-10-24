package flowrun

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var reviewDimensions = []string{"overlap", "layout", "alignment_spacing", "visual_hierarchy", "typography", "color", "map_readability", "style_consistency"}

func reviewObject(value any) (map[string]any, error) {
	if text, ok := value.(string); ok {
		var result map[string]any
		if err := json.Unmarshal([]byte(text), &result); err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("审查结果必须是对象")
		}
		return result, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err = json.Unmarshal(raw, &result); err != nil || result == nil {
		return nil, fmt.Errorf("审查结果必须是对象")
	}
	return result, nil
}

func VerifyInitialReview(value any, root string) error {
	review, err := reviewObject(value)
	if err != nil {
		return err
	}
	dimensions, ok := review["dimension_results"].(map[string]any)
	if !ok || len(dimensions) != len(reviewDimensions) {
		return fmt.Errorf("初审必须包含8个规定维度")
	}
	hasIssue := false
	for _, name := range reviewDimensions {
		dimension, ok := dimensions[name].(map[string]any)
		if !ok {
			return fmt.Errorf("初审缺少维度 %s", name)
		}
		issue, ok := dimension["has_issue"].(bool)
		if !ok {
			return fmt.Errorf("%s.has_issue 必须是布尔值", name)
		}
		hasIssue = hasIssue || issue
	}
	suggestions, ok := review["suggestions"].([]any)
	if !ok {
		return fmt.Errorf("suggestions 必须是数组")
	}
	if hasIssue && len(suggestions) == 0 {
		return fmt.Errorf("存在问题的初审必须提供建议")
	}
	seen := map[string]bool{}
	for _, raw := range suggestions {
		s, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("建议必须是对象")
		}
		id, _ := s["suggestion_id"].(string)
		if strings.TrimSpace(id) == "" || seen[id] {
			return fmt.Errorf("建议ID为空或重复")
		}
		seen[id] = true
		category, _ := s["category"].(string)
		valid := false
		for _, name := range reviewDimensions {
			valid = valid || name == category
		}
		if !valid {
			return fmt.Errorf("建议 category 无效")
		}
		ids, ok := s["target_item_ids"].([]any)
		if !ok {
			return fmt.Errorf("target_item_ids 必须是数组")
		}
		for _, id := range ids {
			if _, ok := id.(string); !ok {
				return fmt.Errorf("目标元素ID必须是字符串")
			}
		}
		executable, ok := s["executable"].(bool)
		if !ok {
			return fmt.Errorf("executable 必须是布尔值")
		}
		adjustment, exists := s["adjustment"]
		if !executable {
			if !exists || adjustment != nil {
				return fmt.Errorf("不可执行建议的 adjustment 必须为null")
			}
		} else {
			a, ok := adjustment.(map[string]any)
			if !ok {
				return fmt.Errorf("可执行建议缺 adjustment")
			}
			name, _ := a["tool_name"].(string)
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("可执行建议缺 tool_name")
			}
		}
	}
	manifest, _ := review["review_manifest_path"].(string)
	if err := verifyReviewPath(root, manifest, true); err != nil {
		return fmt.Errorf("review_manifest_path: %w", err)
	}
	reserved, _ := review["reserved_final_image_path"].(string)
	if err := verifyReviewPath(root, reserved, false); err != nil {
		return fmt.Errorf("reserved_final_image_path: %w", err)
	}
	return nil
}

func VerifyFinalReview(value any) error {
	review, err := reviewObject(value)
	if err != nil {
		return err
	}
	if _, ok := review["final_passed"].(bool); !ok {
		return fmt.Errorf("final_passed 必须是布尔值")
	}
	if _, ok := review["remaining_issues"].([]any); !ok {
		return fmt.Errorf("remaining_issues 必须是数组")
	}
	if raw, exists := review["suggestion_results"]; exists {
		results, ok := raw.([]any)
		if !ok {
			return fmt.Errorf("suggestion_results 必须是数组")
		}
		for _, raw := range results {
			s, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("建议结果必须是对象")
			}
			switch s["status"] {
			case "correctly_applied", "partially_applied", "not_applied", "skipped", "regression":
			default:
				return fmt.Errorf("建议应用状态无效")
			}
		}
	}
	return nil
}
func resolvePath(root, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, path)
}
func verifyReviewPath(root, path string, existing bool) error {
	if strings.TrimSpace(path) == "" || strings.ContainsAny(path, "\x00\r\n") || strings.Contains(path, "://") {
		return fmt.Errorf("无效文件路径")
	}
	resolved := resolvePath(root, path)
	if existing {
		info, err := os.Stat(resolved)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("文件不存在或为空")
		}
	} else {
		if filepath.Base(resolved) == "." {
			return fmt.Errorf("缺少文件名")
		}
		info, err := os.Stat(filepath.Dir(resolved))
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("父路径不是目录")
		}
	}
	return nil
}
