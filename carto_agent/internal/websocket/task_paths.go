package websocket

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var quotedUserPath = regexp.MustCompile("[`\"'“‘]([^`\"'”’]+)[`\"'”’]")
var unquotedUserPath = regexp.MustCompile(`(?:^|[\s（(:：\p{Han}])((?:/|\./|[A-Za-z]:[/\\])[^\s，,；;。！？!?\)）\]】<>]+)`)

// Only literal paths in a human message grant access. Never scan assistant text,
// never infer a parent folder from a basename, and never expand a home wildcard.
func explicitUserPaths(content, root string) []string {
	var values []string
	for _, m := range quotedUserPath.FindAllStringSubmatch(content, -1) {
		values = append(values, m[1])
	}
	// Remove quoted spans first: a quoted path containing spaces must not also
	// grant access to an unrelated prefix directory.
	unquoted := quotedUserPath.ReplaceAllString(content, " ")
	for _, m := range unquotedUserPath.FindAllStringSubmatch(unquoted, -1) {
		values = append(values, m[1])
	}
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(strings.TrimRight(value, "`\"'”’"))
		if !filepath.IsAbs(value) && !strings.HasPrefix(value, "./") {
			continue
		}
		path := value
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		path = strings.Split(path, "|layername=")[0]
		info, err := os.Stat(path)
		if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			continue
		}
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}
