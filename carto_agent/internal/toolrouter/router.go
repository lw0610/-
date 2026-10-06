package toolrouter

type Router struct {
    classifier *ClassificationRegistry
}

func NewRouter(classifier *ClassificationRegistry) *Router {
    return &Router{classifier: classifier}
}

func (r *Router) IsInteractive(toolName string) bool {
    if r == nil || r.classifier == nil {
        return false
    }
    return r.classifier.IsInteractive(toolName)
}
