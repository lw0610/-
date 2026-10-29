package flow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"strings"
)

func uniqueStrings(values []string) []string {
	result := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func writeHashJSON(digest hash.Hash, value any) {
	encoded, _ := json.Marshal(value)
	fmt.Fprintf(digest, "%d:", len(encoded))
	_, _ = digest.Write(encoded)
}

func hashKey(value any) string {
	digest := sha256.New()
	writeHashJSON(digest, value)
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}
