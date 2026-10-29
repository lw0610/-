// resource-api-fixture is an isolated HTTP server for the TypeScript API contract tests.
// It owns its temporary directory and stops/cleans up when its stdin closes.
package main

import (
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
	guides := flow.NewGuideStore(store, filepath.Join(root, "guides.yaml"))
	store.SetLibrary(library)
	qml := "<qgis><layerGeometryType>1</layerGeometryType><renderer-v2 type=\"singleSymbol\"/></qgis>"
	for _, part := range []struct{ dir, id, name string }{{library.BuiltinDir, "style.first", "内置线样式"}, {library.UserDir, "style.second", "用户线样式"}} {
		folder := filepath.Join(part.dir, "styles", part.id)
		if err = write(filepath.Join(folder, "style.qml"), qml); err != nil {
			return err
		}
		if err = write(filepath.Join(folder, "resource.yaml"), "apiVersion: resource/v1\nid: "+part.id+"\ntype: style\nname: "+part.name+"\nentry: style.qml\nversion: 1.0.0\n"); err != nil {
			return err
		}
	}
	sources := map[string]string{"import": filepath.Join(root, "input/导入线样式.qml"), "replacement": filepath.Join(root, "input/替换线样式.qml"), "data_list": filepath.Join(root, "input/公共清单.csv"), "batch": filepath.Join(root, "batch"), "export": filepath.Join(root, "flow.zip"), "resource_export": filepath.Join(root, "style.zip")}
	for path, content := range map[string]string{
		sources["import"]:      qml,
		sources["replacement"]: "<qgis><layerGeometryType>1</layerGeometryType><renderer-v2 type=\"categorizedSymbol\"/></qgis>",
		sources["data_list"]:   "name,path,required\nsample,sample.geojson,false\n",
		filepath.Join(sources["batch"], "a/重复样式.qml"): qml,
		filepath.Join(sources["batch"], "b/重复样式.qml"): qml,
	} {
		if err = write(path, content); err != nil {
			return err
		}
	}
	if _, err = guides.Create("---\n编号: example\n名称: 自由正文流程\n说明: 接口验收\n---\n# 任意章节\n检查用户的数据，用样式「内置线样式」绘制。这里没有固定表格。\n", "example"); err != nil {
		return err
	}
	a := &api.API{}
	a.SetResourceLibrary(library)
	a.SetGuideStore(guides)
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
