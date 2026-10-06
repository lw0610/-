package websocket

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestSummarizeSchemaMessageContentUsesToolResultTextPart(t *testing.T) {
	msg := schema.ToolMessage("", "call-1", schema.WithToolName("buffer"))
	msg.UserInputMultiContent = []schema.MessageInputPart{
		{
			Type: schema.ChatMessagePartTypeText,
			Text: `{"success":true,"path":"/tmp/out.geojson"}`,
		},
		{
			Type: schema.ChatMessagePartTypeFileURL,
			File: &schema.MessageInputFile{
				Name: "out.geojson",
			},
		},
	}

	summary := summarizeSchemaMessageContent(msg)
	if summary != `{"success":true,"path":"/tmp/out.geojson"}`+"\n[文件] out.geojson" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestSummarizeSchemaMessageContentFallsBackToPlaceholder(t *testing.T) {
	msg := schema.ToolMessage("", "call-1", schema.WithToolName("preview"))
	msg.UserInputMultiContent = []schema.MessageInputPart{
		{
			Type:  schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{},
		},
	}

	if summary := summarizeSchemaMessageContent(msg); summary != "[图片]" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestMessageInputPartsPayloadEncodesParts(t *testing.T) {
	parts := []schema.MessageInputPart{
		{
			Type: schema.ChatMessagePartTypeText,
			Text: "hello",
		},
	}

	payload := messageInputPartsPayload(parts)
	if len(payload) != 1 {
		t.Fatalf("expected 1 payload item, got %d", len(payload))
	}
	if payload[0]["type"] != string(schema.ChatMessagePartTypeText) {
		t.Fatalf("unexpected payload type: %#v", payload[0])
	}
	if payload[0]["text"] != "hello" {
		t.Fatalf("unexpected payload text: %#v", payload[0])
	}
}
