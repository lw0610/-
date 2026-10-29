package datasource

import (
	"strings"
	"testing"
)

func TestBuiltinSourceHandbooksAndMissingWarning(t *testing.T) {
	catalog, err := Load("../../../autocarto/tools/system/data-acquisition/assets", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Sources) != 9 {
		t.Fatal(len(catalog.Sources))
	}
	for _, source := range catalog.Sources {
		if source.Handbook.GoodFor == "" || source.Handbook.Summary() == "" || !strings.Contains(catalog.HandbookIndex(), source.ID) {
			t.Fatal(source.ID)
		}
	}
	_, diagnostics := catalog.ParseSource([]byte("apiVersion: data_source/v1\nid: user.no-guide\nname: 本机数据\nkind: local_folder\nroot: /missing\nprovides: []\n"), false)
	found := false
	for _, d := range diagnostics {
		if d.Code == "DS_HANDBOOK_MISSING" && d.Level == "warning" {
			found = true
		}
	}
	if !found {
		t.Fatal(diagnostics)
	}
}
