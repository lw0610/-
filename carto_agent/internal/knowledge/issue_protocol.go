package knowledge

import "strings"

// Keep the model instructions and validation vocabulary in one place. Fields
// name semantic review concerns, not arbitrary paths through the graph JSON.
type issueKindProtocol struct {
	kind         string
	fields       []string
	subjectKinds []string
}

var issueProtocols = []issueKindProtocol{
	{kind: "actor", fields: []string{"identity", "actor_refs"}, subjectKinds: []string{"actors", "events", "routes", "paths"}},
	{kind: "time", fields: []string{"time_text"}, subjectKinds: []string{"events"}},
	{kind: "place", fields: []string{"identity", "location_ref", "map_role"}, subjectKinds: []string{"spatial_entities", "events"}},
	{kind: "route", fields: []string{"identity", "nodes", "attach_ref", "continuation", "paths", "after_last_known"}, subjectKinds: []string{"routes", "paths", "events", "spatial_entities"}},
}

func issueProtocolForKind(kind string) (issueKindProtocol, bool) {
	for _, protocol := range issueProtocols {
		if protocol.kind == kind {
			return protocol, true
		}
	}
	return issueKindProtocol{}, false
}

func allowedIssueFields(kind string) []string {
	protocol, _ := issueProtocolForKind(kind)
	return append([]string(nil), protocol.fields...)
}

// Only the model-response boundary accepts these lossless spelling aliases.
// Do not strip arbitrary prefixes: an unknown field must still fail validation.
// This changes neither the issue's reason nor its evidence or subject binding.
func canonicalModelIssueField(kind, field string) string {
	field = strings.TrimSpace(field)
	switch kind {
	case "route":
		switch field {
		case "paths.nodes", "routes.paths.nodes":
			return "nodes"
		case "paths.attach_ref", "routes.paths.attach_ref":
			return "attach_ref"
		case "paths.continuation", "routes.paths.continuation":
			return "continuation"
		}
	case "actor":
		switch field {
		case "name", "actors.name":
			return "identity"
		case "paths.actor_refs", "routes.paths.actor_refs", "routes.actor_refs", "events.actor_refs":
			return "actor_refs"
		}
	}
	return field
}

func issueProtocolInstructions() string {
	var out strings.Builder
	out.WriteString("\nunresolved 协议：每项必须含非空 id、message 和 source_refs；对象疑问的 subject_refs 只引用本次 graph 中已存在且符合以下类别的对象ID，source_refs 只复制 source_evidence.ref。原文缺少可抽取内容等整体问题使用 kind=source、field=content、subject_refs=[]，保留具体原因，不虚构对象来绑定问题。\n")
	for _, protocol := range issueProtocols {
		out.WriteString("kind=")
		out.WriteString(protocol.kind)
		out.WriteString("：field 只能是 ")
		out.WriteString(strings.Join(protocol.fields, "、"))
		out.WriteString("；subject_refs 对象只能来自 ")
		out.WriteString(strings.Join(protocol.subjectKinds, "、"))
		out.WriteString("。\n")
	}
	out.WriteString("field 使用上面的短字段名，不写 paths.nodes、routes.paths.nodes 等 JSON 路径；主体名称或身份不明用 actor/identity。reason 只能是 context 或 not_stated：context 表示给定原文范围内尚有歧义或需更多上下文；not_stated 只描述原文确实未记载的信息。route/not_stated 的 field 只能是 after_last_known、paths 或 continuation；已有路线的节点或连接歧义必须用 route/context/nodes、attach_ref 或 continuation，不能改成 not_stated 规避核对。place 不能使用 not_stated。路径 actor_refs 为空时，必须有 actor/context/actor_refs 或 actor/context/identity 问题并在 subject_refs 绑定该路径ID。只记录具体疑问，不补列原文未写的所有信息。\n")
	return out.String()
}
