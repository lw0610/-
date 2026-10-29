package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autocarto/carto_agent/internal/config"
	"github.com/autocarto/carto_agent/internal/flow"
	"github.com/autocarto/carto_agent/internal/flowrun"
	"github.com/autocarto/carto_agent/internal/layers"
	"github.com/autocarto/carto_agent/internal/plan"
	"github.com/autocarto/carto_agent/internal/stats"
)

func TestV7UserStatisticsIdentityExclusionAndRetrieveReuse(t *testing.T) {
	s, factory, session, p := cartoRuntimeFixture(t)
	s.configService = config.NewConfigService(nil, &config.Config{Acquisition: config.AcquisitionConfig{NetworkPolicy: "deny", FallbackPolicy: "deny"}}, "", filepath.Join(t.TempDir(), "config.yaml"))
	factory.invoke = func(context.Context, string, map[string]any) (any, error) {
		t.Fatal("statistics import/reuse must not call external tools")
		return nil, nil
	}
	assets, err := config.ResolveAssetsRoot()
	if err != nil {
		t.Fatal(err)
	}
	home, err := config.ResolveAutoCartoHome()
	if err != nil {
		t.Fatal(err)
	}
	store := stats.New(filepath.Join(home, "data", "stats"))
	state := &flowrun.RunState{SessionID: session.SessionID, ProjectPath: session.ProjectPath, Scheme: session.Scheme, CartoPlan: p, Plan: &flow.FlowPlan{}}
	paths := map[string]string{}
	for _, tc := range []struct {
		region, code string
		exclude      []string
	}{{"河南省", "410000", []string{"济源市"}}, {"湖南省", "430000", nil}} {
		units, err := layers.AdministrativeUnits(assets, tc.region, "市")
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		text.WriteString("region_name,value,year,unit\n")
		count := 0
		for _, unit := range units {
			if len(tc.exclude) > 0 && unit.Name == tc.exclude[0] {
				continue
			}
			fmt.Fprintf(&text, "%s,%d,2023,万人\n", unit.Name, 100+count)
			count++
		}
		if tc.region == "河南省" && count != 17 {
			t.Fatalf("fixture must exercise excluding Jiyuan from 18 units: %d", count)
		}
		path := filepath.Join(t.TempDir(), "user-population.csv")
		if err := os.WriteFile(path, []byte(text.String()), 0600); err != nil {
			t.Fatal(err)
		}
		p.Data = []plan.Data{{ID: "population", Name: "用户人口表", Source: "user", Ref: path, Kind: "table", Need: &plan.Need{Region: tc.region, Level: "市", Year: 2023, Indicator: "常住人口", Caliber: "常住", Unit: "万人", Exclude: tc.exclude}}}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := plan.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		data := decoded.Data[0]
		imported, err := s.getPlanDataV2(context.Background(), state, data)
		if err != nil || imported.Preparation != nil {
			t.Fatalf("user statistics import failed: %+v %v", imported, err)
		}
		query := stats.Need{RegionCode: tc.code, Region: tc.region, Level: "市", Year: 2023, Indicator: "常住人口", Caliber: "常住", Unit: "万人", Exclude: tc.exclude}
		cached, found, err := store.Lookup(context.Background(), query)
		if err != nil || !found || len(cached.Observations) != count || imported.Value != cached.Path {
			t.Fatalf("canonical region/exclusion identity not stored: %+v found=%v err=%v", cached, found, err)
		}
		paths[tc.code] = cached.Path
		data.Source, data.Ref, data.Name = "retrieve", "", "换名后自动获取的人口表"
		reused, err := s.getPlanDataV2(context.Background(), state, data)
		if err != nil || reused.Preparation != nil || reused.Value != cached.Path || !strings.Contains(strings.Join(reused.Notes, " "), "统计库复用") {
			t.Fatalf("retrieve failed to reuse uploaded table by region/exclusion: %+v %v", reused, err)
		}
		if len(tc.exclude) > 0 {
			query.Exclude = nil
			if _, found, err := store.Lookup(context.Background(), query); err != nil || found {
				t.Fatalf("17-unit table must not serve a full 18-unit request: found=%v err=%v", found, err)
			}
		}
		if after, err := os.ReadFile(path); err != nil || string(after) != text.String() {
			t.Fatalf("user input changed: %v", err)
		}
	}
	if paths["410000"] == paths["430000"] {
		t.Fatal("different regions collided in the statistics store")
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("earlier regional dataset was replaced", err)
		}
	}
	if len(factory.roles) != 0 {
		t.Fatal("statistics import/reuse must not invoke an agent")
	}
}
