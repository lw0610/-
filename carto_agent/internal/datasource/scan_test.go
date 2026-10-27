package datasource

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestV4ScanLocalDraftUsesFolderCSVAndXLSX(t *testing.T) {
	c := builtinCatalog(t)
	root := t.TempDir()
	geo := filepath.Join(root, "铁路.geojson")
	if err := os.WriteFile(geo, []byte(`{"type":"FeatureCollection","features":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	draft, err := c.ScanLocalDraft(context.Background(), root)
	if err != nil || len(draft.Entries) != 1 || draft.Entries[0].Category != "transport.railway" {
		t.Fatalf("%+v %v", draft, err)
	}
	csv := filepath.Join(root, "清单.csv")
	if err := os.WriteFile(csv, []byte("名称,路径,类别,字段\n铁路,铁路.geojson,transport.railway,NAME=name\n"), 0600); err != nil {
		t.Fatal(err)
	}
	draft, err = c.ScanLocalDraft(context.Background(), csv)
	if err != nil || len(draft.Entries) != 1 || len(draft.Entries[0].Fields["name"]) != 1 || draft.Entries[0].Fields["name"][0] != "NAME" {
		t.Fatalf("%+v %v", draft, err)
	}
	xlsx := filepath.Join(root, "清单.xlsx")
	file, err := os.Create(xlsx)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, body := range map[string]string{"xl/workbook.xml": `<workbook xmlns:r="urn:r"><sheets><sheet name="资源表" r:id="sheet"/></sheets></workbook>`, "xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="sheet" Type="urn:r/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`, "xl/worksheets/sheet1.xml": `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>名称</t></is></c><c r="B1" t="inlineStr"><is><t>路径</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>铁路</t></is></c><c r="B2" t="inlineStr"><is><t>铁路.geojson</t></is></c></row></sheetData></worksheet>`} {
		w, e := archive.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write([]byte(body)); e != nil {
			t.Fatal(e)
		}
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	draft, err = c.ScanLocalDraft(context.Background(), xlsx)
	if err != nil || len(draft.Entries) != 1 || draft.Entries[0].Path != "铁路.geojson" {
		t.Fatalf("xlsx: %+v %v", draft, err)
	}
	if _, err = c.ScanLocalDraft(context.Background(), "relative"); err == nil {
		t.Fatal("accepted relative path")
	}
	missing := filepath.Join(root, "缺失清单.csv")
	if err := os.WriteFile(missing, []byte("name,path,required\n铁路,missing.geojson,true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	missingDraft, err := c.ScanLocalDraft(context.Background(), missing)
	if err != nil || len(missingDraft.Entries) != 1 || missingDraft.Entries[0].Exists || len(missingDraft.Messages) == 0 {
		t.Fatalf("missing input must remain an unselected draft: %+v %v", missingDraft, err)
	}
	if len(draft.Source.Provides) != 0 {
		t.Fatal("scan saved or auto confirmed category")
	}
}
func TestV4SourceAvailabilityIsConfigurationOnly(t *testing.T) {
	c := builtinCatalog(t)
	env := PlanningEnvironment{Catalog: c, Integrations: map[string]bool{}, Secrets: map[string]bool{}}
	source := c.Source("source.osm.overpass")
	if source == nil {
		t.Fatal("fixture missing")
	}
	if ready, reasons := env.Availability(*source); ready || len(reasons) == 0 {
		t.Fatal("missing integration presented as available")
	}
	env.Integrations["osm"] = true
	if ready, reasons := env.Availability(*source); !ready {
		t.Fatalf("configured integration: %v", reasons)
	}
}

func TestV4ScanLocalDraftPreservesGeoPackageLayer(t *testing.T) {
	c := builtinCatalog(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "base.gpkg"), []byte("synthetic, inspected only at run time"), 0600); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(root, "list.csv")
	if err := os.WriteFile(list, []byte("name,path,category\n铁路,base.gpkg|layername=railway,transport.railway\n"), 0600); err != nil {
		t.Fatal(err)
	}
	draft, err := c.ScanLocalDraft(context.Background(), list)
	if err != nil || len(draft.Entries) != 1 || draft.Entries[0].Path != "base.gpkg" || draft.Entries[0].Layer != "railway" || !draft.Entries[0].Exists {
		t.Fatalf("%+v %v", draft, err)
	}
}
