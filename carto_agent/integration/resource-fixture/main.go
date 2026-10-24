// Command resource-fixture exposes the production resolver to cross-language tests.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/autocarto/carto_agent/internal/resource"
	"github.com/autocarto/carto_agent/internal/resource/bindings"
)

func main() { os.Exit(run()) }

func run() int {
	typ := flag.String("type", "", "资源类型")
	location := flag.String("path", "", "资源路径")
	id := flag.String("id", "", "资源编号")
	name := flag.String("name", "", "资源名称")
	builtin := flag.String("builtin", "", "测试内置库")
	user := flag.String("user", "", "测试用户库")
	flowDir := flag.String("flow-dir", "", "测试流程目录")
	baseDir := flag.String("base-dir", "", "本次数据基准目录覆盖")
	flowID := flag.String("flow-id", "fixture-flow", "测试流程编号")
	flag.Parse()
	temporary, err := os.MkdirTemp("", "resource-fixture-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer os.RemoveAll(temporary)
	library := resource.NewLibrary(*builtin, *user, bindings.New(filepath.Join(temporary, "bindings.yaml")))
	resolved := library.Resolve(context.Background(), resource.Ref{Type: *typ, Location: *location, ID: *id, Name: *name, BaseDir: *baseDir}, resource.Scope{FlowID: *flowID, FlowDir: *flowDir})
	handle := resolved.Handle()
	if handle == nil {
		_ = json.NewEncoder(os.Stderr).Encode(resolved.Problems)
		return 2
	}
	if err := json.NewEncoder(os.Stdout).Encode(handle); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
