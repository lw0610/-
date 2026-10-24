package main

// Clear the environment before queueing the debounced catalog refresh. A new
// conversation can start immediately after a successful settings mutation.
type flowEnvironmentRefreshRequester struct {
	invalidate func()
	request    func(string)
}

func (r flowEnvironmentRefreshRequester) RequestRefresh(reason string) {
	if r.invalidate != nil {
		r.invalidate()
	}
	if r.request != nil {
		r.request(reason)
	}
}
