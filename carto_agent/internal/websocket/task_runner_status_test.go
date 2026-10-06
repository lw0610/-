package websocket

import "testing"

func TestDetermineToolStatus_PayloadSuccessFalseOverridesKeyword(t *testing.T) {
	content := `{"info":"请检查 QGIS 版本或安装必要的插件","output":"算法 qgis:barchart 在当前 QGIS 版本中不可用","success":false}`

	result := determineToolStatus("bar_chart", content)

	if result.Status != "failed" {
		t.Fatalf("expected status failed, got %s", result.Status)
	}
	if result.Source != "payload_success" {
		t.Fatalf("expected source payload_success, got %s", result.Source)
	}
	if result.Success == nil || *result.Success {
		t.Fatalf("expected success=false, got %v", result.Success)
	}
}

func TestDetermineToolStatus_PayloadSuccessTrueWinsEvenWithFailureKeywords(t *testing.T) {
	content := `{"success":true,"info":"已处理失败重试逻辑并成功完成"}`

	result := determineToolStatus("custom_tool", content)

	if result.Status != "completed" {
		t.Fatalf("expected status completed, got %s", result.Status)
	}
	if result.Source != "payload_success" {
		t.Fatalf("expected source payload_success, got %s", result.Source)
	}
	if result.Success == nil || !*result.Success {
		t.Fatalf("expected success=true, got %v", result.Success)
	}
}

func TestDetermineToolStatus_SkillPrefixSuccess(t *testing.T) {
	result := determineToolStatus("skill", "正在启动 Skill：demo")

	if result.Status != "completed" {
		t.Fatalf("expected status completed, got %s", result.Status)
	}
	if result.Source != "skill_prefix" {
		t.Fatalf("expected source skill_prefix, got %s", result.Source)
	}
	if result.Success == nil || !*result.Success {
		t.Fatalf("expected success=true, got %v", result.Success)
	}
}

func TestDetermineToolStatus_StrongFailurePrefixFallback(t *testing.T) {
	result := determineToolStatus("data_export_vector", "error: failed to open layer")

	if result.Status != "failed" {
		t.Fatalf("expected status failed, got %s", result.Status)
	}
	if result.Source != "text_fallback" {
		t.Fatalf("expected source text_fallback, got %s", result.Source)
	}
	if result.Success == nil || *result.Success {
		t.Fatalf("expected success=false, got %v", result.Success)
	}
}

func TestDetermineToolStatus_NonStructuredTextReturnsUnknown(t *testing.T) {
	result := determineToolStatus("data_export_vector", "处理中，等待进一步结果")

	if result.Status != "unknown" {
		t.Fatalf("expected status unknown, got %s", result.Status)
	}
	if result.Source != "unknown" {
		t.Fatalf("expected source unknown, got %s", result.Source)
	}
	if result.Success != nil {
		t.Fatalf("expected success=nil for unknown status, got %v", result.Success)
	}
}

func TestDetermineToolStatus_PayloadStatusCompleted(t *testing.T) {
	content := `{"status":"completed","message":"done"}`

	result := determineToolStatus("custom_tool", content)

	if result.Status != "completed" {
		t.Fatalf("expected status completed, got %s", result.Status)
	}
	if result.Source != "payload_status" {
		t.Fatalf("expected source payload_status, got %s", result.Source)
	}
	if result.Success == nil || !*result.Success {
		t.Fatalf("expected success=true, got %v", result.Success)
	}
}

func TestDetermineToolStatus_PayloadErrorTextMarksFailure(t *testing.T) {
	content := `{"error":"provider unavailable","message":"retry later"}`

	result := determineToolStatus("custom_tool", content)

	if result.Status != "failed" {
		t.Fatalf("expected status failed, got %s", result.Status)
	}
	if result.Source != "payload_error" {
		t.Fatalf("expected source payload_error, got %s", result.Source)
	}
	if result.Success == nil || *result.Success {
		t.Fatalf("expected success=false, got %v", result.Success)
	}
}

func TestDetermineToolStatus_NestedResultSuccessIsRecognized(t *testing.T) {
	content := `{"result":{"success":true,"status":"completed"}}`

	result := determineToolStatus("custom_tool", content)

	if result.Status != "completed" {
		t.Fatalf("expected status completed, got %s", result.Status)
	}
	if result.Source != "payload_result_success" {
		t.Fatalf("expected source payload_result_success, got %s", result.Source)
	}
	if result.Success == nil || !*result.Success {
		t.Fatalf("expected success=true, got %v", result.Success)
	}
}

func TestDetermineToolStatus_StrongSuccessPrefixFallback(t *testing.T) {
	result := determineToolStatus("data_export_vector", "执行成功，已生成输出文件")

	if result.Status != "completed" {
		t.Fatalf("expected status completed, got %s", result.Status)
	}
	if result.Source != "text_fallback" {
		t.Fatalf("expected source text_fallback, got %s", result.Source)
	}
	if result.Success == nil || !*result.Success {
		t.Fatalf("expected success=true, got %v", result.Success)
	}
}

func TestExtractToolPayloadErrorMetadata(t *testing.T) {
	payload, ok := parseToolPayload(`{"success":false,"error":"实例桥接未就绪","code":"TOOL_NOT_READY","retryable":true,"provider_diagnostics":{"provider_id":"qgis_bridge"}}`)
	if !ok {
		t.Fatal("expected structured payload")
	}
	code, retryable, diagnostics := extractToolPayloadErrorMetadata(payload)
	if code != "TOOL_NOT_READY" {
		t.Fatalf("unexpected error code: %q", code)
	}
	if retryable == nil || !*retryable {
		t.Fatalf("expected retryable=true, got %#v", retryable)
	}
	if diagnostics["provider_id"] != "qgis_bridge" {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
}
