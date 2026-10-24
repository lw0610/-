package resource

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLegacyBundledManifestOffersApplicableRepair(t *testing.T) {
	root, library := resourceProject(t)
	dir := filepath.Join(root, "flow")
	path := resourceWrite(t, filepath.Join(dir, "resources/_deps/legacy.doc.0123456789/guide.md"), "legacy guide")
	lock := &Lock{Resources: []LockEntry{{Type: "doc", Name: "指南", ID: "legacy.doc.0123456789", Source: "bundled", Bundled: "resources/_deps/legacy.doc.0123456789/"}}}
	resolved := library.Resolve(context.Background(), Ref{Type: "doc", Name: "指南", Location: "公共资源库"}, Scope{FlowDir: dir, Lock: lock})
	if resolved.Root != path || HasErrors(resolved.Problems) {
		t.Fatal(resolved)
	}
	found := false
	for _, problem := range resolved.Problems {
		if problem.Code == "FLOW_RESOURCE_MANIFEST_MISSING" {
			found = true
			if problem.Fix["action"] != "install_resource" {
				t.Fatalf("inapplicable bundled repair: %+v", problem)
			}
		}
	}
	if !found {
		t.Fatal("missing legacy diagnostic")
	}
}
