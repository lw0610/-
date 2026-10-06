package toolresult

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// Text 构造纯文本 ToolResult。
func Text(text string) *schema.ToolResult {
	return &schema.ToolResult{
		Parts: []schema.ToolOutputPart{
			{
				Type: schema.ToolPartTypeText,
				Text: text,
			},
		},
	}
}

// JSON 将任意结构转成 JSON 文本 ToolResult。
func JSON(value any) (*schema.ToolResult, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return Text(string(payload)), nil
}

// AppendText 追加文本 part。
func AppendText(result *schema.ToolResult, text string) *schema.ToolResult {
	if result == nil {
		result = &schema.ToolResult{}
	}
	result.Parts = append(result.Parts, schema.ToolOutputPart{
		Type: schema.ToolPartTypeText,
		Text: text,
	})
	return result
}

// AppendImageData 追加图片 part。
func AppendImageData(result *schema.ToolResult, data []byte, mimeType string) *schema.ToolResult {
	if result == nil {
		result = &schema.ToolResult{}
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	result.Parts = append(result.Parts, schema.ToolOutputPart{
		Type: schema.ToolPartTypeImage,
		Image: &schema.ToolOutputImage{
			MessagePartCommon: schema.MessagePartCommon{
				Base64Data: &encoded,
				MIMEType:   strings.TrimSpace(mimeType),
			},
		},
	})
	return result
}

// AppendAudioData 追加音频 part。
func AppendAudioData(result *schema.ToolResult, data []byte, mimeType string) *schema.ToolResult {
	if result == nil {
		result = &schema.ToolResult{}
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	result.Parts = append(result.Parts, schema.ToolOutputPart{
		Type: schema.ToolPartTypeAudio,
		Audio: &schema.ToolOutputAudio{
			MessagePartCommon: schema.MessagePartCommon{
				Base64Data: &encoded,
				MIMEType:   strings.TrimSpace(mimeType),
			},
		},
	})
	return result
}

// AppendFileData 追加内嵌文件 part。
func AppendFileData(result *schema.ToolResult, data []byte, mimeType string, extra map[string]any) *schema.ToolResult {
	if result == nil {
		result = &schema.ToolResult{}
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	result.Parts = append(result.Parts, schema.ToolOutputPart{
		Type: schema.ToolPartTypeFile,
		File: &schema.ToolOutputFile{
			MessagePartCommon: schema.MessagePartCommon{
				Base64Data: &encoded,
				MIMEType:   strings.TrimSpace(mimeType),
				Extra:      cloneExtra(extra),
			},
		},
	})
	return result
}

// AppendFileURL 追加文件链接 part。
func AppendFileURL(result *schema.ToolResult, rawURL string, mimeType string, extra map[string]any) *schema.ToolResult {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return result
	}
	if result == nil {
		result = &schema.ToolResult{}
	}
	result.Parts = append(result.Parts, schema.ToolOutputPart{
		Type: schema.ToolPartTypeFile,
		File: &schema.ToolOutputFile{
			MessagePartCommon: schema.MessagePartCommon{
				URL:      &trimmed,
				MIMEType: strings.TrimSpace(mimeType),
				Extra:    cloneExtra(extra),
			},
		},
	})
	return result
}

// LocalFileURL 将本地路径转换为 file:// URL。
func LocalFileURL(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "://") {
		return trimmed
	}
	return (&url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(trimmed),
	}).String()
}

func cloneExtra(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(extra))
	for key, value := range extra {
		cloned[key] = value
	}
	return cloned
}
