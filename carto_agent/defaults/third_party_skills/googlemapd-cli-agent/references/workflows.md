# Workflow Reference

## Raster workflow

1. Run `googlemapd tiles info --json` with the user geometry and zoom.
2. Confirm tile count is acceptable.
3. Run `googlemapd tiles download --json`.
4. If interrupted, rerun with `googlemapd tiles resume --json`.
5. Optionally run `googlemapd tiles merge --json`.

Fallback compatibility entrypoints: `googlemapd info|download|resume|merge`

## Resume workflow

1. Locate the checkpoint file (`.checkpoint.json`).
2. Run `googlemapd tiles resume <checkpoint-file> --json`.
3. If the run fails, adjust `--workers`, `--retry`, `--timeout`, or proxy settings and retry.

Fallback compatibility entrypoint: `googlemapd resume <checkpoint-file>`

## MVT workflow

1. Validate bbox + zoom with `googlemapd tiles info --json` when useful.
2. Run `googlemapd vector mvt --json` with the proper source.
3. For auth-required sources, include `--api-key`.

Fallback compatibility entrypoint: `googlemapd mvt`

## OSM vector workflow

1. Run `googlemapd vector osm --json` with `--bbox` and desired `--features`.
2. If a large region times out, rerun with `--split`.
3. Save outputs as `geojson|osm|gpkg|shp` based on downstream use.

Fallback compatibility entrypoint: `googlemapd osm-data`

## Failure handling

- Invalid geometry or missing required flags: rerun with corrected CLI arguments.
- Network failures: retry with adjusted `--timeout`, `--retry`, or proxy settings.
- Interrupted raster downloads: resume from the checkpoint instead of restarting.
