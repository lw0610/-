package flowrun

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autocarto/carto_agent/internal/hitl"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type ConfirmationRequest struct {
	Title       string
	Description string
	Options     []hitl.RequestOption
	Default     string
}
type confirmationState struct{ Request ConfirmationRequest }
type ConfirmAgent struct{ request ConfirmationRequest }

func init() { gob.Register(confirmationState{}) }
func NewConfirmAgent(request ConfirmationRequest) *ConfirmAgent {
	return &ConfirmAgent{request: request}
}
func (a *ConfirmAgent) Name(context.Context) string { return "flow_confirmation" }
func (a *ConfirmAgent) Description(context.Context) string {
	return "等待用户决定流程的下一步"
}
func (a *ConfirmAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() { defer gen.Close(); gen.Send(a.interrupt(ctx)) }()
	return iter
}
func (a *ConfirmAgent) interrupt(ctx context.Context) *adk.AgentEvent {
	return adk.StatefulInterrupt(ctx, hitl.InterruptPayload{Type: "decision", Title: a.request.Title, Description: a.request.Description, Options: a.request.Options, Default: a.request.Default, SelectMode: "single", Required: true, Category: "flow_confirmation"}, confirmationState{Request: a.request})
}
func (a *ConfirmAgent) Resume(ctx context.Context, info *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer gen.Close()
		if state, ok := info.InterruptState.(confirmationState); ok {
			a = NewConfirmAgent(state.Request)
		}
		if !info.IsResumeTarget {
			gen.Send(a.interrupt(ctx))
			return
		}
		choice, err := parseChoice(info.ResumeData)
		if err == nil {
			valid := false
			for _, option := range a.request.Options {
				valid = valid || option.Value == choice.Choice
			}
			if !valid {
				err = fmt.Errorf("未选择有效的流程选项")
			}
		}
		if err != nil {
			gen.Send(&adk.AgentEvent{AgentName: a.Name(ctx), Err: err})
			return
		}
		raw, _ := json.Marshal(choice)
		gen.Send(adk.EventFromMessage(schema.AssistantMessage(string(raw), nil), nil, schema.Assistant, ""))
	}()
	return iter
}

func parseChoice(data any) (Choice, error) {
	if text, ok := data.(string); ok {
		if strings.HasPrefix(strings.TrimSpace(text), "{") {
			var decoded any
			if json.Unmarshal([]byte(text), &decoded) == nil {
				return parseChoice(decoded)
			}
		}
		if text != "" {
			return Choice{Choice: text}, nil
		}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return Choice{}, err
	}
	var decoded map[string]any
	if json.Unmarshal(raw, &decoded) != nil {
		return Choice{}, fmt.Errorf("用户选择格式无效")
	}
	choice := Choice{}
	choice.Choice, _ = decoded["choice"].(string)
	choice.Guidance, _ = decoded["guidance"].(string)
	if choice.Guidance == "" {
		choice.Guidance, _ = decoded["note"].(string)
	}
	if choice.Guidance == "" {
		choice.Guidance, _ = decoded["text"].(string)
	}
	if choice.Choice == "" {
		for _, key := range []string{"selected", "value", "option", "options"} {
			switch v := decoded[key].(type) {
			case string:
				choice.Choice = v
			case []any:
				if len(v) > 0 {
					choice.Choice, _ = v[0].(string)
				}
			}
			if choice.Choice != "" {
				break
			}
		}
	}
	if choice.Choice == "" {
		if value, ok := decoded["confirmed"].(bool); ok {
			if value {
				choice.Choice = "confirm"
			} else {
				choice.Choice = "reject"
			}
		}
	}
	if choice.Choice == "" {
		for _, key := range []string{"user_data", "decisions"} {
			if nested, exists := decoded[key]; exists {
				if nestedChoice, err := parseChoice(nested); err == nil {
					return nestedChoice, nil
				}
			}
		}
		for _, nested := range decoded {
			if object, ok := nested.(map[string]any); ok {
				if nestedChoice, err := parseChoice(object); err == nil {
					return nestedChoice, nil
				}
			}
		}
	}
	if choice.Choice == "" {
		return choice, fmt.Errorf("缺少用户选择")
	}
	return choice, nil
}

func confirmationForStep(instructions, title string, failure bool) ConfirmationRequest {
	if failure {
		return ConfirmationRequest{Title: "本步骤未完成", Description: instructions, Default: "retry", Options: []hitl.RequestOption{{Label: "重试本步", Value: "retry"}, {Label: "跳过本步", Value: "skip"}, {Label: "终止流程", Value: "stop"}}}
	}
	options := []hitl.RequestOption{}
	for _, line := range strings.Split(instructions, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			label := strings.TrimSpace(line[2:])
			if label != "" {
				options = append(options, hitl.RequestOption{Label: label, Value: label})
			}
		}
	}
	if len(options) == 0 {
		options = []hitl.RequestOption{{Label: "确认", Value: "confirm"}, {Label: "不确认", Value: "reject"}}
	}
	return ConfirmationRequest{Title: title, Description: instructions, Options: options, Default: options[0].Value}
}
