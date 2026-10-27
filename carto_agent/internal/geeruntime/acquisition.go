package geeruntime

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BuildAcquisitionScript reads only shipped acquisition templates. Dynamic data
// crosses the boundary as encoded JSON, never interpolated Python expressions.
func BuildAcquisitionScript(assetsRoot, template string, params map[string]any) (string, error) {
	allowed := map[string]bool{"dem.py": true, "landcover.py": true, "optical.py": true, "population.py": true, "nightlight.py": true}
	if !allowed[template] {
		return "", fmt.Errorf("不允许的GEE系统获取模板")
	}
	root := filepath.Join(assetsRoot, "tools", "system", "data-acquisition", "assets", "gee")
	common, err := os.ReadFile(filepath.Join(root, "common.py"))
	if err != nil {
		return "", err
	}
	source, err := os.ReadFile(filepath.Join(root, template))
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	return "import json, base64\nparams=json.loads(base64.b64decode(\"" + base64.StdEncoding.EncodeToString(encoded) + "\"))\n" + string(common) + "\n" + string(source), nil
}
