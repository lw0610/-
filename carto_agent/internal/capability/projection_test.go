package capability

import (
	"testing"

	"github.com/autocarto/carto_agent/internal/toolcatalog"
)

func TestEmptySnapshotProjectionMethodsReturnEmptyViews(t *testing.T) {
	snapshot := &CapabilitySnapshot{
		ConfigName: "default",
		Tools:      map[string]ToolCapability{},
		ToolOrder:  []string{},
		Packages:   map[string]PackageCapability{},
		Agents:     []AgentCapabilityView{},
	}

	runtimeItems := snapshot.AsRuntimeCatalogItems()
	if runtimeItems == nil {
		t.Fatal("expected runtime projection slice to be initialized")
	}
	if len(runtimeItems) != 0 {
		t.Fatalf("expected no runtime items, got %d", len(runtimeItems))
	}

	toolCatalog := snapshot.ProjectToolCatalog(nil, ToolCatalogConfigured)
	if toolCatalog == nil {
		t.Fatal("expected tool catalog projection slice to be initialized")
	}
	if len(toolCatalog) != 0 {
		t.Fatalf("expected no tool catalog items, got %d", len(toolCatalog))
	}

	payload := snapshot.BuildCapabilityPayload("", "", nil)
	if payload == nil {
		t.Fatal("expected capability payload to be initialized")
	}
	if got := payload["config_name"]; got != "default" {
		t.Fatalf("expected config_name default, got %#v", got)
	}
	if got := payload["total_tools"]; got != 0 {
		t.Fatalf("expected total_tools 0, got %#v", got)
	}
	agents, ok := payload["agents"].([]map[string]any)
	if !ok {
		t.Fatalf("expected agents slice payload, got %T", payload["agents"])
	}
	if len(agents) != 0 {
		t.Fatalf("expected empty agents payload, got %d", len(agents))
	}

	diff := snapshot.DiffRuntimeCatalog(nil)
	if diff.Changed {
		t.Fatalf("expected empty diff when comparing against nil snapshot: %#v", diff)
	}
}

func TestSnapshotProjectionMethodsMapToolAndAgentViews(t *testing.T) {
	snapshot := &CapabilitySnapshot{
		ConfigName: "default",
		Tools: map[string]ToolCapability{
			"buffer": {
				Name:        "buffer",
				RawName:     "buffer",
				Description: "缓冲区分析",
				Category:    "vector",
				Backend:     "cli",
				ProviderID:  "cli",
				Source:      "cli",
				InputSchema: map[string]any{"type": "object"},
				Available:   true,
				Package: PackageRef{
					Key:       "global::analysis-global",
					PackageID: "analysis-global",
				},
				PackageKind:    "category",
				PackageEnabled: true,
			},
			"flash_feature": {
				Name:              "flash_feature",
				RawName:           "flash_feature",
				Description:       "高亮要素",
				Category:          "vector",
				Backend:           "bridge",
				ProviderID:        "qgis_bridge",
				Source:            "interactive",
				Available:         false,
				UnavailableReason: "interactive_instance_disconnected",
				Package: PackageRef{
					Key:       "global::interactive-global",
					PackageID: "interactive-global",
				},
				PackageKind:    "category",
				PackageEnabled: true,
			},
		},
		ToolOrder: []string{"buffer", "flash_feature"},
		Agents: []AgentCapabilityView{{
			AgentName:     "planner",
			DisplayName:   "Planner",
			Description:   "生成计划",
			Enabled:       true,
			SkillPackages: []string{"analysis-global"},
			Tools: []ToolRef{{
				Name:        "buffer",
				Description: "缓冲区分析",
				Package:     PackageRef{PackageID: "analysis-global"},
			}},
		}},
	}

	runtimeItems := snapshot.AsRuntimeCatalogItems()
	if len(runtimeItems) != 2 {
		t.Fatalf("expected 2 runtime items, got %d", len(runtimeItems))
	}
	if runtimeItems[0].Name != "buffer" || runtimeItems[1].Name != "flash_feature" {
		t.Fatalf("unexpected runtime item order: %#v", runtimeItems)
	}
	if !runtimeItems[0].Available || runtimeItems[1].UnavailableReason != "interactive_instance_disconnected" {
		t.Fatalf("unexpected runtime availability projection: %#v", runtimeItems)
	}
	if runtimeItems[0].Domain != "vector" || runtimeItems[1].Domain != "vector" {
		t.Fatalf("expected runtime projection to preserve domain via category fallback, got %#v", runtimeItems)
	}

	configuredCatalog := snapshot.ProjectToolCatalog([]string{"buffer", "flash_feature"}, ToolCatalogConfigured)
	if len(configuredCatalog) != 2 {
		t.Fatalf("expected 2 configured catalog items, got %d", len(configuredCatalog))
	}
	if configuredCatalog[0].PackageID != "analysis-global" || configuredCatalog[1].MissingReason != "interactive_instance_disconnected" {
		t.Fatalf("unexpected configured catalog projection: %#v", configuredCatalog)
	}
	if configuredCatalog[0].Domain != "vector" || configuredCatalog[1].Domain != "vector" {
		t.Fatalf("expected tool catalog projection to preserve domain via category fallback, got %#v", configuredCatalog)
	}

	effectiveCatalog := snapshot.ProjectToolCatalog([]string{"buffer", "flash_feature"}, ToolCatalogEffective)
	if len(effectiveCatalog) != 1 || effectiveCatalog[0].Name != "buffer" {
		t.Fatalf("expected only available tool in effective projection, got %#v", effectiveCatalog)
	}

	payload := snapshot.BuildCapabilityPayload("planner", "buffer", nil)
	agents, ok := payload["agents"].([]map[string]any)
	if !ok || len(agents) != 1 {
		t.Fatalf("unexpected capability payload agents: %#v", payload["agents"])
	}
	tools, ok := agents[0]["tools"].([]map[string]any)
	if !ok || len(tools) != 1 || tools[0]["package_id"] != "analysis-global" {
		t.Fatalf("unexpected capability payload tools: %#v", agents)
	}
}

func TestCapabilityPayloadAndToolCatalogProjectionStayAligned(t *testing.T) {
	snapshot := &CapabilitySnapshot{
		ConfigName: "default",
		Tools: map[string]ToolCapability{
			"buffer": {
				Name:        "buffer",
				Description: "缓冲区分析",
				Backend:     "cli",
				Source:      "cli",
				Available:   true,
				Package:     PackageRef{PackageID: "analysis-global"},
			},
			"flash_feature": {
				Name:        "flash_feature",
				Description: "高亮要素",
				Backend:     "qgis",
				Source:      "qgis",
				Available:   true,
				Package:     PackageRef{PackageID: "interactive-global"},
			},
		},
		ToolOrder: []string{"buffer", "flash_feature"},
		Agents: []AgentCapabilityView{{
			AgentName:     "planner",
			Enabled:       true,
			Tools:         []ToolRef{{Name: "buffer", Description: "缓冲区分析", Package: PackageRef{PackageID: "analysis-global"}}, {Name: "flash_feature", Description: "高亮要素", Package: PackageRef{PackageID: "interactive-global"}}},
			SkillPackages: []string{"analysis-global", "interactive-global"},
		}},
	}

	projected := snapshot.ProjectToolCatalog([]string{"buffer", "flash_feature"}, ToolCatalogConfigured)
	payload := snapshot.BuildCapabilityPayload("planner", "", nil)
	agents, ok := payload["agents"].([]map[string]any)
	if !ok || len(agents) != 1 {
		t.Fatalf("unexpected capability payload agents: %#v", payload["agents"])
	}
	payloadTools, ok := agents[0]["tools"].([]map[string]any)
	if !ok || len(payloadTools) != 2 {
		t.Fatalf("unexpected capability payload tools: %#v", agents[0]["tools"])
	}

	byName := map[string]toolcatalog.Item{}
	for _, item := range projected {
		byName[item.Name] = item
	}
	for _, payloadTool := range payloadTools {
		name, _ := payloadTool["name"].(string)
		packageID, _ := payloadTool["package_id"].(string)
		projectedItem, exists := byName[name]
		if !exists {
			t.Fatalf("planner payload tool missing from projected catalog: %s", name)
		}
		if projectedItem.PackageID != packageID {
			t.Fatalf("package mismatch for %s: payload=%s projected=%s", name, packageID, projectedItem.PackageID)
		}
	}
}
