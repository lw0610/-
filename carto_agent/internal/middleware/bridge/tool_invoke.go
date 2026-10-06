package bridge

type pendingRequest struct {
	ch chan pendingResponse
}

type pendingResponse struct {
	invoke *InvokeResult
	tools  []map[string]any
}
