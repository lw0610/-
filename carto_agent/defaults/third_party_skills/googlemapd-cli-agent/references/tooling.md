# Tooling Reference

## Shared notes

- Duration arguments use Go duration format (`30s`, `5m`).
- Use exactly one geometry input mode at a time.
- Add `--json` for machine-readable output.
- Prefer V2 commands first; use V1 commands only when the caller explicitly needs the compatibility entrypoints.

## `googlemapd tiles info` (preferred)

- Required: `--zoom`
- Area args: one of `--bbox|--polygon|--geojson|--shapefile|--kml|--wkt|--center+--radius`
- Optional: `--type`, `--source`

Fallback: `googlemapd info`

## `googlemapd tiles download` (preferred)

- Required: `--zoom` plus one area mode
- Common optional args: `--output`, `--format`, `--scheme`, `--workers`, `--rate-limit`, `--retry`, `--timeout`, `--source`
- Proxy args: `--proxy`, `--proxy-file`
- Optional config: `--config`

Fallback: `googlemapd download`

## `googlemapd tiles resume` (preferred)

- Optional but recommended: checkpoint path argument
- Optional overrides: `--workers`, `--rate-limit`, `--retry`, `--timeout`, `--proxy`, `--proxy-file`

Fallback: `googlemapd resume`

## `googlemapd vector mvt` (preferred)

- Required: `--bbox`
- Optional: `--zoom`, `--source`, `--api-key`, `--output`, `--scheme`, `--rate-limit`, `--workers`, `--timeout`, `--proxy`

Fallback: `googlemapd mvt`

## `googlemapd vector osm` (preferred)

- Required: `--bbox`
- Optional: `--features`, `--format`, `--output`, `--timeout`, `--proxy`, `--endpoint`, `--split`

Fallback: `googlemapd osm-data`

## `googlemapd tiles merge` (preferred)

- Required: `--input`, `--output`, `--zoom`
- Optional: `--format`, `--quality`

Fallback: `googlemapd merge`
