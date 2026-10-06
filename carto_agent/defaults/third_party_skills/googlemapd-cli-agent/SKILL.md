---
name: googlemapd-cli-agent
description: Use when operating GoogleMapD from a shell or CLI-capable agent for tile estimation, raster or vector downloads, checkpoint resume flows, merge workflows, or machine-readable JSON automation.
kind: workflow
backend: external
enabled: true
tags:
  - third-party
  - downloader
  - cli
  - tiles
---

# GoogleMapD CLI Agent

## Quick Start

1. Run `bash scripts/check_env.sh` to verify binary and config availability.
2. The binary is at `bin/googlemapd` within this skill directory (auto-embedded by `make build`).
3. Prefer V2 commands: `googlemapd tiles info|download|resume|merge` and `googlemapd vector mvt|osm`.
4. Prefer `--json`, explicit output paths, and explicit `--config` paths when another tool or agent will consume the result.
5. Example config available at `configs/config.example.yaml` within this skill directory.

## Workflow Rules

- Use exactly one area mode per request (`bbox`, `geojson`, `kml`, `wkt`, etc.).
- Estimate larger runs with `googlemapd tiles info --json` before starting downloads.
- Keep and reference `.checkpoint.json` explicitly for resumable downloads.
- Prefer explicit output directories so later merge/resume commands are deterministic.
- Prefer explicit `--config <path>` when repository or local config matters.
- Let default proxy detection work unless the user explicitly passes `--proxy` or `--proxy-file`.

## Command Selection

- Read-only estimation: `googlemapd tiles info --json` (fallback: `googlemapd info`)
- Raster download: `googlemapd tiles download --json` (fallback: `googlemapd download`)
- Resume previous run: `googlemapd tiles resume --json` (fallback: `googlemapd resume`)
- Vector tiles: `googlemapd vector mvt --json` (fallback: `googlemapd mvt`)
- OSM vector data: `googlemapd vector osm --json` (fallback: `googlemapd osm-data`)
- Merge output image: `googlemapd tiles merge --json` (fallback: `googlemapd merge`)
- Version check: `googlemapd version`

## References

- Tool argument summary and defaults: `references/tooling.md`
- End-to-end workflow patterns: `references/workflows.md`

## Scripts

- Environment preflight: `scripts/check_env.sh`
