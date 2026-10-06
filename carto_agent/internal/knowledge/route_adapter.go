package knowledge

import "fmt"

// NormalizeRouteAttachments removes a secondary path's repeated junction node
// only when the graph identifies exactly one possible junction on its main path.
// The source-backed movement and every other node remain unchanged. It mutates
// g and returns the number of paths normalized.
func NormalizeRouteAttachments(g Graph) int {
	index, err := indexGraph(g)
	if err != nil {
		return 0
	}
	return normalizeRouteAttachments(g, index)
}

func normalizeRouteAttachments(g Graph, index map[string]graphObject) int {
	changed := 0
	for _, raw := range array(g["routes"]) {
		route, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		var main map[string]any
		mainCount := 0
		for _, value := range array(route["paths"]) {
			path, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if path["role"] == "main" {
				main, mainCount = path, mainCount+1
			}
		}
		if mainCount != 1 {
			continue
		}
		mainPlaces := map[string]int{}
		for _, node := range array(main["nodes"]) {
			id := resolvedNode(g, node)
			if index[id].kind == "spatial_entities" {
				mainPlaces[id]++
			}
		}
		mainID := stringValue(main["path_id"])
		for _, value := range array(route["paths"]) {
			path, ok := value.(map[string]any)
			if !ok {
				continue
			}
			role := stringValue(path["role"])
			if role != "branch" && role != "incoming" {
				continue
			}
			nodes := array(path["nodes"])
			if len(nodes) < 2 {
				continue
			}
			endpointIndex := 0
			if role == "incoming" {
				endpointIndex = len(nodes) - 1
			}
			endpoint, ok := nodes[endpointIndex].(map[string]any)
			if !ok || endpoint["ref_type"] != "spatial_entity" {
				continue
			}
			placeID := stringValue(endpoint["ref_id"])
			if index[placeID].kind != "spatial_entities" || mainPlaces[placeID] != 1 {
				continue
			}
			attach := stringValue(path["attach_ref"])
			if attach != mainID && attach != placeID {
				continue
			}
			shared := 0
			for _, node := range nodes {
				if mainPlaces[resolvedNode(g, node)] > 0 {
					shared++
				}
			}
			if shared != 1 {
				continue
			}
			path["attach_ref"] = placeID
			path["nodes"] = append(append([]any{}, nodes[:endpointIndex]...), nodes[endpointIndex+1:]...)
			changed++
		}
	}
	return changed
}

// Renderer limits are presented as editable route questions. The extracted
// facts and semantic review remain intact while the user corrects topology.
func routeAdapterIssues(g Graph) []Issue {
	evidence := map[string]string{}
	for _, raw := range array(g["evidence"]) {
		row := raw.(map[string]any)
		evidence[stringValue(row["evidence_id"])] = stringValue(row["quote"])
	}
	issues := []Issue{}
	for _, raw := range array(g["routes"]) {
		route := raw.(map[string]any)
		if err := checkRouteStructure(g, route, true); err != nil {
			quotes := []string{}
			for _, ref := range array(route["evidence_refs"]) {
				if quote := evidence[stringValue(ref)]; quote != "" {
					quotes = append(quotes, quote)
				}
			}
			issues = append(issues, Issue{
				ID: "adapter:" + stringValue(route["route_id"]), Kind: "route", Reason: "context", Field: "paths",
				SubjectRefs: []string{stringValue(route["route_id"])}, EvidenceQuotes: quotes,
				Message: "六表暂不能编译这条路线：" + err.Error() + "。请核对路径结构，保留原文中的行动。",
			})
		}
	}
	return issues
}

func checkRouteStructure(g Graph, route map[string]any, final bool) error {
	for _, diagnostic := range InspectGraphConsistency(g).Semantic {
		if diagnostic.RouteID == stringValue(route["route_id"]) {
			return fmt.Errorf("%s", diagnostic.Message)
		}
	}
	return checkRouteGeometry(g, route, final)
}

// The accepted compiler copy checks geometry, not earlier semantic opinions.
func checkRouteGeometry(g Graph, route map[string]any, final bool) error {
	index, err := indexGraph(g)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	mainNodes := map[string]bool{}
	for _, value := range array(route["paths"]) {
		path, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("路径必须为对象")
		}
		role := stringValue(path["role"])
		if role != "main" && role != "branch" && role != "incoming" {
			return fmt.Errorf("未知路径角色 %q", role)
		}
		counts[role]++
		nodes := array(path["nodes"])
		if (len(nodes) == 0 || (final && role == "main" && len(nodes) < 2)) && !hasApproachConstraint(g, path) {
			return fmt.Errorf("路径 %s 节点不足", path["path_id"])
		}
		previous := ""
		for _, node := range nodes {
			id := resolvedNode(g, node)
			if id == "" || index[id].kind != "spatial_entities" || id == previous {
				return fmt.Errorf("路径 %s 节点无效或相邻重复", path["path_id"])
			}
			previous = id
			if role == "main" {
				mainNodes[id] = true
			}
		}
		if role == "main" && stringValue(path["attach_ref"]) != "" {
			return fmt.Errorf("主路径不应设置分兵/会合连接点")
		}
	}
	if counts["main"] == 0 || (final && counts["main"] != 1) {
		return fmt.Errorf("路线草稿至少一条 main，最终制图须有且仅有一条 main；只能依据已确认的 continuation 接续，不能删除路径或将每日主路改成支路规避")
	}
	if final && (counts["branch"] > 1 || (counts["branch"] > 0 && counts["incoming"] > 0)) {
		return fmt.Errorf("six-table 路线结构要求一条 main、至多一条 branch，且 branch/incoming 不混用；不得删减真实路径绕过限制")
	}
	for _, value := range array(route["paths"]) {
		path := value.(map[string]any)
		if path["role"] == "main" {
			continue
		}
		attach := stringValue(path["attach_ref"])
		if attach == "" || !mainNodes[attach] {
			return fmt.Errorf("路径 %s 分兵/会合地点不在主路径上", path["path_id"])
		}
		for _, node := range array(path["nodes"]) {
			if resolvedNode(g, node) == attach {
				return fmt.Errorf("路径 %s 连接地点仍出现在支路节点中，需要按原文核对位置及顺序", path["path_id"])
			}
		}
	}
	return nil
}

// Derived parameters live only on the compiler copy. Python owns the style
// catalogue; omitting style_ref lets it select the matching topology/side preset.
func deriveRouteRendering(g Graph) error {
	NormalizeRouteAttachments(g)
	for _, value := range array(g["routes"]) {
		route := value.(map[string]any)
		if err := checkRouteStructure(g, route, true); err != nil {
			return fmt.Errorf("路线 %s: %w", route["route_id"], err)
		}
		if err := normalizeTopology(route); err != nil {
			return err
		}
		delete(route, "style_ref")
		for _, value := range array(route["paths"]) {
			path := value.(map[string]any)
			path["direction"] = map[string]string{"main": "forward", "branch": "away_from_attach", "incoming": "toward_attach"}[stringValue(path["role"])]
		}
	}
	return nil
}

func normalizeTopology(route map[string]any) error {
	counts := map[string]int{}
	for _, item := range array(route["paths"]) {
		counts[stringValue(item.(map[string]any)["role"])]++
	}
	if counts["main"] != 1 || counts["branch"] > 1 || (counts["branch"] > 0 && counts["incoming"] > 0) {
		return fmt.Errorf("joined topology exceeds the six-table adapter: require one main, at most one branch and no branch/incoming mix")
	}
	topology := "standard"
	if counts["branch"] > 0 {
		topology = "branch"
	} else if counts["incoming"] > 0 {
		topology = "multi_start"
	}
	if route["topology"] != topology {
		delete(route, "style_ref") // Let the existing compiler select its matching preset.
	}
	route["topology"] = topology
	return nil
}
