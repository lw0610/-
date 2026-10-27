package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/acquire"
	"github.com/autocarto/carto_agent/internal/config"
	"github.com/gorilla/mux"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type storeView struct {
	acquire.StoreRecord
	ReferencingRuns []string `json:"referencing_runs"`
}

func (a *API) dataStore() (*acquire.Store, string, error) {
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		return nil, "", err
	}
	cfg := config.AcquisitionConfig{}.WithDefaults()
	if a.configService != nil {
		cfg = a.configService.GetAcquisitionConfig()
	}
	return acquire.NewStore(filepath.Join(home, "data", "acquired"), int64(cfg.StoreMaxGB*1024*1024*1024)), home, nil
}
func storeReferences(ctx context.Context, home string) (map[string][]string, error) {
	refs := map[string][]string{}
	root := filepath.Join(home, "data", "sessions")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return refs, nil
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// WalkDir does not follow directory symlinks; refuse file symlinks too.
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || entry.Name() != "run.json" {
			return nil
		}
		if filepath.Base(filepath.Dir(filepath.Dir(path))) != "flows" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 64*1024*1024 {
			return fmt.Errorf("流程运行记录过大，无法核对数据仓引用")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var run struct {
			ID          string `json:"flow_run_id"`
			Acquisition struct {
				Items []struct {
					StoreKey string `json:"store_key"`
				} `json:"items"`
			} `json:"acquisition"`
		}
		if err := json.Unmarshal(raw, &run); err != nil {
			return fmt.Errorf("流程运行记录无法读取，不能确认数据仓引用")
		}
		if run.ID == "" {
			run.ID = filepath.Base(filepath.Dir(path))
		}
		for _, item := range run.Acquisition.Items {
			if item.StoreKey != "" {
				found := false
				for _, id := range refs[item.StoreKey] {
					if id == run.ID {
						found = true
					}
				}
				if !found {
					refs[item.StoreKey] = append(refs[item.StoreKey], run.ID)
				}
			}
		}
		return nil
	})
	return refs, err
}
func (a *API) handleDataStore(w http.ResponseWriter, r *http.Request) {
	if !ensureHTTPControlContractVersion(w, r, "data_store") {
		return
	}
	store, home, err := a.dataStore()
	if err != nil {
		flowError(w, err)
		return
	}
	records := []acquire.StoreRecord{}
	if _, err = os.Stat(store.Root); err == nil {
		records, err = store.List(r.Context())
	} else if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		flowError(w, err)
		return
	}
	refs, err := storeReferences(r.Context(), home)
	if err != nil {
		flowError(w, err)
		return
	}
	if r.Method == http.MethodGet {
		items := []storeView{}
		var used int64
		for _, record := range records {
			runs := refs[record.Key]
			if runs == nil {
				runs = []string{}
			}
			items = append(items, storeView{record, runs})
			used += record.Bytes
		}
		Success(w, map[string]any{"items": items, "used_bytes": used, "max_bytes": store.MaxBytes, "directory": store.Root})
		return
	}
	if r.URL.Query().Get("confirm") != "true" {
		ErrorWithCode(w, 400, "CONFIRM_REQUIRED", "请先确认删除获取的数据副本")
		return
	}
	key := mux.Vars(r)["key"]
	if key == "" && strings.HasSuffix(r.URL.Path, "/clean-expired") {
		removed, retained := []string{}, []string{}
		for _, record := range records {
			if !record.Expired {
				continue
			}
			if len(refs[record.Key]) > 0 {
				retained = append(retained, record.Key)
				continue
			}
			if err := store.Delete(r.Context(), record.Key); err != nil {
				flowError(w, err)
				return
			}
			removed = append(removed, record.Key)
		}
		Success(w, map[string]any{"deleted": removed, "retained_referenced": retained})
		return
	}
	found := false
	for _, record := range records {
		if record.Key == key {
			found = true
			break
		}
	}
	if !found {
		ErrorWithCode(w, 404, "DATA_STORE_NOT_FOUND", "数据仓条目不存在")
		return
	}
	if err := store.Delete(r.Context(), key); err != nil {
		flowError(w, err)
		return
	}
	Success(w, map[string]bool{"deleted": true})
}
