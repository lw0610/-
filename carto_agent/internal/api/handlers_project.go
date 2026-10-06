package api

import "net/http"

// handleListProjects GET /api/projects
func (a *API) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if a.sessionService == nil {
		Success(w, map[string]any{"projects": []any{}})
		return
	}

	projects, err := a.sessionService.ListProjects()
	if err != nil {
		Error(w, http.StatusInternalServerError, "读取项目索引失败: "+err.Error())
		return
	}

	if projects == nil {
		projects = []map[string]any{}
	}
	Success(w, map[string]any{"projects": projects})
}
