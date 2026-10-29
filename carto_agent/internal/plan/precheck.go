package plan

// Precheck is local evidence recorded once at proposal/edit time. The resolver
// refreshes the actual files and bounds when the consumer is about to run.
type Precheck struct {
	Pack             string   `json:"pack,omitempty"`
	Selection        string   `json:"selection,omitempty"`
	EstimatedSeconds int      `json:"estimated_seconds,omitempty"`
	DataID           string   `json:"data_id"`
	Status           string   `json:"status"`
	Reason           string   `json:"reason"`
	Suggestion       string   `json:"suggestion,omitempty"`
	Source           string   `json:"source,omitempty"`
	Ref              string   `json:"ref,omitempty"`
	Methods          []string `json:"methods,omitempty"`
}
type PrecheckResolver func(*Data) Precheck

func CheckData(p *Plan, resolve PrecheckResolver) []Precheck {
	results := make([]Precheck, 0, len(p.Data))
	for i := range p.Data {
		item := resolve(&p.Data[i])
		item.DataID = p.Data[i].ID
		if item.Source == "" {
			item.Source = p.Data[i].Source
		}
		if item.Ref == "" {
			item.Ref = p.Data[i].Ref
		}
		results = append(results, item)
	}
	return results
}
