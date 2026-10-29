package fileops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/autocarto/carto_agent/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type documentOptionsKey struct{}
type DocumentOptions struct {
	Config   config.AcquisitionConfig
	Vision   bool
	Download func(context.Context, string) (map[string]any, error)
}

func WithDocumentOptions(ctx context.Context, options DocumentOptions) context.Context {
	return context.WithValue(ctx, documentOptionsKey{}, options)
}

func (w workspace) extract(ctx context.Context, args map[string]any) (any, error) {
	options, _ := ctx.Value(documentOptionsKey{}).(DocumentOptions)
	input := stringArg(args, "path")
	var download map[string]any
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		if options.Download == nil {
			return nil, fmt.Errorf("当前角色未提供在线下载能力，请先用 web_download_file 下载原件")
		}
		var err error
		download, err = options.Download(ctx, input)
		if err != nil {
			return nil, err
		}
		input, _ = download["path"].(string)
	}
	path, err := w.resolve(input, false)
	if err != nil {
		return nil, err
	}
	output := stringArg(args, "output_dir")
	if output == "" {
		if err = os.MkdirAll(w.output, 0700); err != nil {
			return nil, err
		}
		output, err = os.MkdirTemp(w.output, "document-")
		if err != nil {
			return nil, err
		}
	}
	output, err = w.resolve(output, true)
	if err != nil {
		return nil, err
	}
	cfg := options.Config.WithDefaults()
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		return nil, err
	}
	script := filepath.Join(assets, "tools", "system", "file-operations", "handlers", "document_extract.py")
	request := map[string]any{"path": path, "output_dir": output, "vision": options.Vision, "ocr_engine": cfg.OCREngine, "ocr_path": cfg.OCRPath, "ocr_languages": cfg.OCRLanguages, "pages": stringArg(args, "pages"), "table_hint": stringArg(args, "table_hint")}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, cfg.DocumentPython, script)
	command.Stdin = bytes.NewReader(raw)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var result map[string]any
	if err = json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("文档提取无法运行：%v；%s", runErr, stderr.String())
	}
	if runErr != nil || result["success"] != true {
		return nil, fmt.Errorf("文档提取失败：%v；%v", result["error"], result["installation"])
	}
	if download != nil {
		result["download"] = download
	}
	return result, nil
}
