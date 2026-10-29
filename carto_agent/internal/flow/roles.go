package flow

import (
	"regexp"
	"strings"
)

var packageParentheses = regexp.MustCompile(`（[^）]*）|\([^)]*\)`)

func resolvePackages(names []string, env Environment) ([]string, string) {
	ids := []string{}
	for _, name := range names {
		matches := []string{}
		for stage := 0; stage < 3; stage++ {
			for _, p := range env.Packages {
				match := p.ID == name
				if stage == 1 {
					match = p.DisplayName == name
				}
				if stage == 2 {
					match = strings.TrimSpace(packageParentheses.ReplaceAllString(p.DisplayName, "")) == name
				}
				if match {
					matches = append(matches, p.ID)
				}
			}
			if len(matches) > 0 {
				break
			}
		}
		if len(matches) > 1 {
			return ids, "FLOW_ROLE_PACKAGE_AMBIGUOUS"
		}
		if len(matches) == 0 {
			return ids, "FLOW_ROLE_PACKAGE_UNKNOWN"
		}
		ids = append(ids, matches[0])
	}
	return uniqueStrings(ids), ""
}
func missingPackages(required, owned []string) []string {
	have := map[string]bool{}
	for _, id := range owned {
		have[id] = true
	}
	result := []string{}
	for _, id := range required {
		if !have[id] {
			result = append(result, id)
		}
	}
	return uniqueStrings(result)
}
func roleByID(id string, roles []RoleSpec) *RoleSpec {
	for _, r := range roles {
		if r.ID == id {
			copy := r
			return &copy
		}
	}
	return nil
}
