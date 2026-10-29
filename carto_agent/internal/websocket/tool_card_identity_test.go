package websocket

import "testing"

func TestProjectionPairsConcurrentToolResultsByCallID(t *testing.T) {
	var block workflowBlock
	emit := func(phase, id, name, result string) {
		data := map[string]any{"tool": "resource_get", "call_id": id}
		if phase == "tool_call" {
			data["tool_args"] = map[string]any{"name": name}
		} else {
			data["status"] = "failed"
			data["result"] = result
		}
		entry := buildWorkflowEntry(Message{Phase: phase, Data: data}, 1000)
		block = appendOrMergeProjectionWorkflowEntry(block, *entry, 1000)
	}
	emit("tool_call", "a", "统计专题图", "")
	emit("tool_call", "b", "统计专题图指南", "")
	emit("tool_result", "a", "", "统计专题图未找到")
	emit("tool_result", "b", "", "统计专题图指南未找到")
	emit("tool_result", "a", "", "统计专题图未找到") // replay does not make a third card
	if len(block.Entries) != 2 {
		t.Fatalf("cards=%d", len(block.Entries))
	}
	for i, want := range []string{"统计专题图", "统计专题图指南"} {
		card := block.Entries[i].ToolCard
		if card.Args["name"] != want || card.Result != want+"未找到" {
			t.Fatalf("mismatched card: %+v", card)
		}
	}
	if block.Entries[0].ID == block.Entries[1].ID {
		t.Fatal("same-millisecond calls must have distinct identities")
	}
}

func TestProjectionDoesNotGuessAmbiguousLegacyToolResults(t *testing.T) {
	var block workflowBlock
	for _, name := range []string{"甲", "乙"} {
		entry := buildWorkflowEntry(Message{Phase: "tool_call", Data: map[string]any{"tool": "resource_get", "tool_args": map[string]any{"name": name}}}, 1000)
		block = appendOrMergeProjectionWorkflowEntry(block, *entry, 1000)
	}
	result := buildWorkflowEntry(Message{Phase: "tool_result", Data: map[string]any{"tool": "resource_get", "result": "未找到", "status": "failed"}}, 2000)
	block = appendOrMergeProjectionWorkflowEntry(block, *result, 2000)
	if len(block.Entries) != 3 || block.Entries[0].ToolCard.Status != "running" || block.Entries[1].ToolCard.Status != "running" {
		t.Fatal("ambiguous legacy result was attached to a guessed call")
	}
}
