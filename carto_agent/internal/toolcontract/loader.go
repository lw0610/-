package toolcontract

import (
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

func LoadFS(fsys fs.FS, roots ...string) ([]Contract, error) {
	if fsys == nil {
		return nil, nil
	}
	paths := make([]string, 0)
	for _, root := range roots {
		trimmed := strings.TrimSpace(root)
		if trimmed == "" {
			continue
		}
		info, err := fs.Stat(fsys, trimmed)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat tool contract path %s: %w", trimmed, err)
		}
		if !info.IsDir() {
			paths = append(paths, trimmed)
			continue
		}
		if err := fs.WalkDir(fsys, trimmed, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			ext := strings.ToLower(path.Ext(current))
			if ext == ".yaml" || ext == ".yml" {
				paths = append(paths, current)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk tool contract path %s: %w", trimmed, err)
		}
	}
	sort.Strings(paths)
	contracts := make([]Contract, 0, len(paths))
	for _, filePath := range paths {
		contract, err := loadFile(fsys, filePath)
		if err != nil {
			return nil, err
		}
		if contract.APIVersion == "" && contract.Kind == "" && contract.Metadata.Name == "" {
			continue
		}
		contracts = append(contracts, contract)
	}
	if err := ValidateContracts(contracts); err != nil {
		return nil, err
	}
	return contracts, nil
}

func loadFile(fsys fs.FS, filePath string) (Contract, error) {
	raw, err := fs.ReadFile(fsys, filePath)
	if err != nil {
		return Contract{}, fmt.Errorf("read tool contract %s: %w", filePath, err)
	}
	var contract Contract
	if err := yaml.Unmarshal(raw, &contract); err != nil {
		return Contract{}, fmt.Errorf("parse tool contract %s: %w", filePath, err)
	}
	contract.SourcePath = filePath
	contract.normalize()
	if err := ValidateContract(contract); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

func isNotExist(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "does not exist")
}
