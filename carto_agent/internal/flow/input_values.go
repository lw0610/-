package flow

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

func InputProvided(value any) bool {
	if value == nil {
		return false
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]any:
		if v["$ref"] == "current_user_message" {
			b, _ := json.Marshal(v["length"])
			return string(b) != "0" && string(b) != "null"
		}
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		return v.Len() > 0
	}
	return true
}
func InputTruthy(value any) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	if value == nil {
		return false
	}
	switch fmt.Sprint(value) {
	case "false", "否", "0", "":
		return false
	}
	return InputProvided(value)
}
