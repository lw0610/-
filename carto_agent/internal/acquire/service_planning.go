package acquire

import (
	"context"
	"github.com/autocarto/carto_agent/internal/datasource"
)

// ResolveExtent exposes the local metadata resolver independently of legacy flows.
// BBox uses west,south,east,north; callers translate their own input notation.
func ResolveExtent(ctx context.Context, value string, catalog *datasource.Catalog, resolver ExtentResolver) (*datasource.ExtentPlan, error) {
	extent, err := resolveExtent(ctx, value, catalog, resolver)
	if err == nil {
		err = prepareExtent(extent)
	}
	return extent, err
}

func PrepareExtent(extent *datasource.ExtentPlan) error { return prepareExtent(extent) }

// PlanningWithStore never downloads or changes acquisition data.
func PlanningWithStore(ctx context.Context, env datasource.PlanningEnvironment, store *Store) datasource.PlanningEnvironment {
	if store == nil || env.Catalog == nil {
		return env
	}
	env.CacheLookup = func(item datasource.AcquisitionItem, choice datasource.SourceChoice) (string, bool) {
		source := env.Catalog.Source(choice.SourceID)
		if source == nil {
			return "", false
		}
		spec, err := cacheSpecification(env.Catalog, item, choice, *source)
		if err != nil {
			return "", false
		}
		_, ok, err := store.Lookup(ctx, spec.Key())
		return spec.Key(), ok && err == nil
	}
	return env
}
