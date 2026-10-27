package testutil

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestV4ProfilesAndOfflineFixtures(t *testing.T) {
	for _, raw := range []string{V4ZttztProfileYAML, V4MinimalProfileYAML} {
		var profile map[string]any
		if err := yaml.Unmarshal([]byte(raw), &profile); err != nil {
			t.Fatal(err)
		}
		if profile["version"] != "2.0" {
			t.Fatal(profile)
		}
	}
	archive, err := zip.NewReader(bytes.NewReader(V4ShapefileZIP(t)), int64(len(V4ShapefileZIP(t))))
	if err != nil || len(archive.File) != 6 {
		t.Fatalf("invalid shapefile fixture: %v", err)
	}
	server := V4HTTPServer(t, map[string]V4HTTPResponse{"/fixture.osm": {Body: []byte(V4OSMFixture)}, "/limited": {Status: 429}})
	response, err := http.Get(server.URL + "/fixture.osm")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != V4OSMFixture {
		t.Fatal("fixture response differs", err)
	}
	response, err = http.Get(server.URL + "/limited")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 429 {
		t.Fatal(response.StatusCode)
	}
}
