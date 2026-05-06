# CLAUDE.md

This file provides guidance to a Coding Agent when working with code in this repository.

## Build & run

- `make build` — `go mod tidy` then build to `bin/syncai`.
- `make run` — builds and then runs `go run ./cmd -config syncai.json`.
- `make clean` — removes `bin/`.

There is no test suite in this repo (no `*_test.go` files). Verify changes with `make build` plus a manual smoke test against a temp working directory:

```bash
./bin/syncai -workdir /tmp/syncai-smoke -config /tmp/syncai-smoke/syncai.json -no-watch
```

CLI flags: `-config`, `-workdir`, `-no-watch` (one-shot for CI), `-self-update`, `-version`, `-help`.

## Architecture

SyncAI is a polling watcher that propagates AI-assistant config files between agents (Cursor, Copilot, Claude Code, Codex, OpenCode). The whole program is a single Go binary; the moving parts split cleanly along five "sync kinds" defined in `internal/model/model.go`:

| Kind     | Source config field      | Source unit            | Destination logic |
|----------|--------------------------|------------------------|-------------------|
| context  | `Agent.Context.Path`     | single file            | verbatim copy |
| ignore   | `Agent.Ignore.Path`      | single file            | verbatim copy |
| rules    | `Agent.Rules.Pattern`    | files matched by glob  | content rewritten by per-agent rules generator (frontmatter shape differs between cursor's `globs/alwaysApply` and copilot's `applyTo`) |
| skills   | `Agent.Skills.Pattern`   | **whole directory** matched by glob | every file inside the matched dir copied verbatim, preserving the in-skill relative path |
| mcp      | `Agent.MCP.Path`         | single file (may be shared with other agent settings) | source agent's adapter parses servers into a normalized `mcp.Server` map; each peer's adapter rewrites only the MCP section of its file (preserving non-MCP content for `.codex/config.toml` and `opencode.json`) |

### Lifecycle

1. `cmd/main.go` loads config, runs `initialSync` (dedup key `kind|stem|rel`, picks the newest mtime per logical file across agents), then enters a ticker loop at `cfg.Interval()` (default 5s).
2. Each tick rebuilds the watched-file list via `(config.Agent).Files()` and diffs sha256 hashes against the in-memory `filesState`. Changes → `(*SyncAI).Sync`. Missing entries → `(*SyncAI).Delete`.
3. `Files()` uses two distinct collectors: `globPattern` (file glob, used for rules) and `walkSkillDirs` (recursive walk of every matched directory, used for skills). Don't conflate them.

### `Identify` is the routing decision

`(*SyncAI).Identify(path)` returns `(*config.Agent, model.Kind, stem, rel)`:
- `stem` is the wildcard capture (skill folder name, or rule file's wildcard portion).
- `rel` is non-empty only for skills — it's the file's path relative to its skill folder.
- `Identify` walks the agents in config order; the first match wins. Rules are checked before skills, so don't configure overlapping patterns.

### Pattern matching helpers in `internal/syncai/util.go`

- `matchPattern(pattern, path)` — for file patterns (rules). `*` may appear in any path component. Falls back to basename-without-extension as stem when the pattern has no `*`.
- `matchSkillsPattern(pattern, path)` — for directory patterns (skills). The pattern is a prefix of the path; the remainder is the rel.
- `generatePatternPath(pattern, stem)` — substitutes `stem` into every `*` component. For skills, callers join `rel` afterwards in `generatePath`.
- `skillsBaseDir(pattern)` — the literal prefix of the pattern before its first wildcard component. Used to bound the empty-dir cleanup walk.

### Three sync paths in `(*SyncAI).Sync`

- **Skills** → `syncSkill`: read source bytes once, `util.WriteFile` them to every peer's substituted destination. No frontmatter parsing — skill folders may contain scripts/data that aren't markdown.
- **MCP** → `syncMCP`: parse source via `mcp.GetAdapter(srcAgent.Name).Parse(path)` into a `map[string]mcp.Server`, then for each peer call its adapter's `Write(dstPath, servers)`. Adapters live in `internal/mcp/`: `dedicated.go` covers cursor/claude/copilot (JSON, differing only in top-level key and whether `type` is emitted for stdio); `codex.go` does surgical text-level rewriting of `[mcp_servers.*]` blocks in `.codex/config.toml` to preserve other Codex settings (and skips remote servers since Codex is stdio-only); `opencode.go` round-trips through `map[string]json.RawMessage` so unrelated top-level keys in `opencode.json` survive.
- **Everything else** → document-stack path: parse all peers' versions of the logical file, sort with the changed path forced last (so it wins regardless of mtime), pass through `generate()` which dispatches to a per-agent `RulesGenerator` only when `kind == KindRules`. Context/ignore fall through verbatim.

### Deletes and empty-dir pruning

`(*SyncAI).Delete` only propagates deletions for `KindRules` and `KindSkills` (context, ignore, and MCP deletions are intentionally non-propagating to avoid accidents — to remove an MCP server, edit its entry out of the source file rather than deleting the whole file). After each successful peer-side skill file removal, `util.PruneEmptyDirs(dirOf(dst), skillsBaseDir(dstAgent.Skills.Pattern))` walks upward removing empty dirs, stopping at the configured base — so `.codex/skills/` survives even when its last skill is deleted.

### Atomic file writes

`util.WriteFile` is the sole writer: temp file in the same dir → fsync → rename → fsync the dir. It returns early if destination bytes already match, which is what prevents ping-pong syncs.

## Frontmatter parsing

`util.ParseFile` extracts YAML frontmatter from markdown. Used by the document-stack path to merge metadata across agents in `generator.ExtractRulesMetadata`. Non-string YAML values are coerced via `cleanYAMLValue`; sequences are joined with commas. Skills bypass this entirely.

## CI

`.github/workflows/syncai.yml` runs syncai against the repo's own `syncai.json` on every push (except `main` and `release/*`). It builds from source (`make build`) — do **not** reintroduce the cross-repo binary download; it was removed because unauth'd `curl` to `api.github.com` from the runner hit IP rate limits and broke the workflow.
