package datasource

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

func marshalSource(source Source) ([]byte, error) { return yaml.Marshal(source) }

// SaveUser validates the exact draft and atomically persists only user-owned sources.
func (c *Catalog) SaveUser(root string, source Source) ([]Diagnostic, error) {
	raw, err := marshalSource(source)
	if err != nil {
		return nil, err
	}
	parsed, ds := c.ParseSource(raw, false)
	if hasError(ds) {
		return ds, fmt.Errorf("数据源内容无效")
	}
	if existing := c.Source(parsed.ID); existing != nil && existing.Builtin {
		return ds, fmt.Errorf("内置数据源只读")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return ds, err
	}
	file, err := os.CreateTemp(root, ".source-*.yaml")
	if err != nil {
		return ds, err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return ds, err
	}
	return ds, os.Rename(name, filepath.Join(root, parsed.ID+".yaml"))
}
func DeleteUser(root, id string) error {
	if !sourceID.MatchString(id) || !strings.HasPrefix(id, "user.") {
		return fmt.Errorf("只能删除用户数据源")
	}
	return os.Remove(filepath.Join(root, id+".yaml"))
}
