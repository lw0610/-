package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// MediaCapabilities describes both the selected model and the actual adapter.
// A provider label alone is not evidence that a particular model can see images.
type MediaCapabilities struct {
	Model string
	Image bool
	PDF   bool
}

func MediaCapabilitiesForProvider(provider, name string, vision bool) MediaCapabilities {
	info, _ := GetProviderInfo(provider)
	// The installed Gemini adapter supports document parts. The installed
	// OpenAI/Claude/Ollama adapters do not encode PDF document parts; report
	// this rather than stripping the file or claiming that it has been read.
	return MediaCapabilities{Model: name, Image: vision, PDF: vision && info.BackendType == "gemini"}
}

type mediaCapabilityModel struct {
	inner model.ToolCallingChatModel
	caps  MediaCapabilities
}

func NewMediaCapabilityModel(inner model.ToolCallingChatModel, caps MediaCapabilities) model.ToolCallingChatModel {
	return &mediaCapabilityModel{inner: inner, caps: caps}
}

func (m *mediaCapabilityModel) prepare(input []*schema.Message) []*schema.Message {
	// Normalize tool attachments before applying model capabilities. Otherwise a
	// rejected attachment can disappear when an inner sanitizer preserves the
	// tool's existing Content, or an older MultiContent part can bypass the check.
	output := SanitizeMessages(input)
	for _, message := range output {
		if message == nil {
			continue
		}
		for index, part := range message.UserInputMultiContent {
			var reason string
			switch {
			case part.Type == schema.ChatMessagePartTypeImageURL && part.Image != nil && !m.caps.Image:
				reason = "当前模型配置未声明支持图片输入，图片内容未发送给该模型"
			case part.Type == schema.ChatMessagePartTypeFileURL && part.File != nil && strings.EqualFold(part.File.MIMEType, "application/pdf") && !m.caps.PDF:
				reason = "当前模型或接入适配器不支持直接读取 PDF，PDF 内容未发送给该模型"
			}
			if reason != "" {
				message.UserInputMultiContent[index] = schema.MessageInputPart{
					Type: schema.ChatMessagePartTypeText,
					Text: fmt.Sprintf("MODEL_MEDIA_UNSUPPORTED (%s): %s。原始文件仍保留；不要声称已看过或反复截图重试。请使用 document_extract 提取表格或执行本机 OCR，再用 file_read 读取真实结果；没有可用工具时如实报告能力缺口，不更改统计年份、口径或编造数值。", m.caps.Model, reason),
				}
			}
		}
	}
	return output
}

func (m *mediaCapabilityModel) Generate(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.Message, error) {
	return m.inner.Generate(ctx, m.prepare(input), options...)
}

func (m *mediaCapabilityModel) Stream(ctx context.Context, input []*schema.Message, options ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.inner.Stream(ctx, m.prepare(input), options...)
}

func (m *mediaCapabilityModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound, err := m.inner.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return NewMediaCapabilityModel(bound, m.caps), nil
}
