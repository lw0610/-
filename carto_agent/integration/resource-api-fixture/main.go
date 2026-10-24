// resource-api-fixture is an isolated HTTP server for the TypeScript API contract tests.
// It owns its temporary directory and stops/cleans up when its stdin closes.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/autocarto/carto_agent/internal/api"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
	"github.com/gorilla/mux"
	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root, err := os.MkdirTemp("", "autocarto-resource-api-contract-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	for key, value := range map[string]string{"AUTOCARTO_PROJECT_ROOT": root, "AUTOCARTO_HOME": filepath.Join(root, "runtime"), "AUTOCARTO_ASSETS_ROOT": filepath.Join(root, "assets"), "AUTOCARTO_DISABLE_KEYRING": "1"} {
		if err = os.Setenv(key, value); err != nil {
			return err
		}
	}
	write := func(path, text string) error {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(text), 0600)
	}
	binding := bindings.New(filepath.Join(root, "runtime/config/resource_bindings.yaml"))
	library := resource.NewLibrary(filepath.Join(root, "builtin"), filepath.Join(root, "user"), binding)
	store := flow.NewStore(filepath.Join(root, "builtin-flows"), filepath.Join(root, "flows"), binding)
	manifest := func(path, id, name, entry string) error {
		return write(path, "apiVersion: resource/v1\nid: "+id+"\ntype: style\nname: "+name+"\nentry: "+entry+"\nversion: 1.0.0\n")
	}
	for _, part := range []struct{ dir, id string }{{library.BuiltinDir, "style.first"}, {library.UserDir, "style.second"}} {
		if err = write(filepath.Join(part.dir, "styles/说明.qml"), "<qgis><renderer-v2 type=\"singleSymbol\"/></qgis>"); err != nil {
			return err
		}
		if err = manifest(filepath.Join(part.dir, "styles/说明.resource.yaml"), part.id, "说明", "说明.qml"); err != nil {
			return err
		}
	}
	if err = write(filepath.Join(library.UserDir, "styles/旧样式.qml"), "<qgis/>"); err != nil {
		return err
	}
	sources := map[string]string{"import": filepath.Join(root, "input/导入说明.qml"), "replacement": filepath.Join(root, "input/替换说明.qml"), "data_list": filepath.Join(root, "input/公共清单.csv"), "base_dir": filepath.Join(root, "chosen-data"), "export": filepath.Join(root, "export.zip")}
	if err = write(sources["import"], "<qgis><renderer-v2 type=\"singleSymbol\"/></qgis>"); err != nil {
		return err
	}
	if err = write(sources["replacement"], "<qgis><renderer-v2 type=\"categorizedSymbol\"/></qgis>"); err != nil {
		return err
	}
	if err = write(sources["data_list"], "name,path,required\nsample,sample.geojson,false\n"); err != nil {
		return err
	}
	if err = write(filepath.Join(sources["base_dir"], "sample.geojson"), `{"type":"FeatureCollection","features":[]}`); err != nil {
		return err
	}
	makeFlow := func(id, declaration string) (*flow.StoredFlow, error) {
		return store.Create("---\n编号: "+id+"\n名称: "+id+"\n---\n## 使用的资源\n"+declaration+"\n## 步骤\n### 检查\n- 负责：用户\n- 做法：查看资源。\n", id)
	}
	if _, err = makeFlow("ambiguous", "- 样式「说明」：公共资源库"); err != nil {
		return err
	}
	local, err := makeFlow("legacy-flow", "- 参考「旧说明」：resources/docs/旧说明.md")
	if err != nil {
		return err
	}
	if err = write(filepath.Join(local.Dir, "resources/docs/旧说明.md"), "# flow legacy"); err != nil {
		return err
	}
	data, err := makeFlow("data-flow", "- 数据清单「数据」：resources/data/清单.csv（文件所在文件夹：「数据目录」）\n\n## 输入\n- 数据目录（必填，文件夹）：选择数据所在文件夹。")
	if err != nil {
		return err
	}
	if err = write(filepath.Join(data.Dir, "resources/data/清单.csv"), "name,path\nsample,sample.geojson\n"); err != nil {
		return err
	}
	for _, id := range []string{"bundled-install", "bundled-use"} {
		resourceID := "style.bundle"
		if id == "bundled-use" {
			resourceID = "style.localbundle"
			if err = write(filepath.Join(library.BuiltinDir, "styles/local.qml"), "<qgis/>"); err != nil {
				return err
			}
			if err = manifest(filepath.Join(library.BuiltinDir, "styles/local.resource.yaml"), resourceID, "本机已有样式", "local.qml"); err != nil {
				return err
			}
		}
		item, err := makeFlow(id, "- 样式「附带样式」：公共资源库/"+resourceID)
		if err != nil {
			return err
		}
		relative := "resources/_deps/" + resourceID
		bundle := filepath.Join(item.Dir, filepath.FromSlash(relative))
		if err = write(filepath.Join(bundle, "bundle.qml"), "<qgis/>"); err != nil {
			return err
		}
		if err = manifest(filepath.Join(bundle, "bundle.resource.yaml"), resourceID, "附带样式", "bundle.qml"); err != nil {
			return err
		}
		resolved := library.Resolve(context.Background(), resource.Ref{Type: "style", Location: filepath.Join(bundle, "bundle.qml")}, resource.Scope{FlowID: id, FlowDir: item.Dir})
		if resolved.Handle() == nil {
			return fmt.Errorf("bundle fixture invalid: %+v", resolved.Problems)
		}
		lock := resource.Lock{APIVersion: resource.LockAPIVersion, Resources: []resource.LockEntry{{Type: "style", Name: "附带样式", ID: resourceID, Version: "1.0.0", Digest: resolved.Digest, Source: "bundled", Bundled: relative}}}
		raw, err := yaml.Marshal(lock)
		if err != nil {
			return err
		}
		if err = write(filepath.Join(item.Dir, "flow.lock"), string(raw)); err != nil {
			return err
		}
	}
	a := &api.API{}
	a.SetResourceLibrary(library)
	a.SetFlowStore(store)
	a.SetFlowCompiler(flow.NewCompiler(flow.CompilerOptions{Store: store, Resources: library, CacheDir: filepath.Join(root, "cache"), LoadEnvironment: func(context.Context, string) (flow.Environment, error) {
		return flow.Environment{Scheme: "default"}, nil
	}}))
	router := mux.NewRouter()
	a.SetupRoutes(router)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	if err = json.NewEncoder(os.Stdout).Encode(map[string]any{"url": "http://" + listener.Addr().String(), "root": root, "sources": sources}); err != nil {
		return err
	}
	closed := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); close(closed) }()
	select {
	case <-closed:
		return nil
	case err := <-serveErrors:
		return err
	}
}
