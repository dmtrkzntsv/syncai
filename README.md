<p align="center">
    <picture>
        <img src="https://github.com/flowmitry/syncai/raw/main/doc/assets/syncai_github.png" width="194">
    </picture>
</p>

---

**SyncAI** is a lightweight and cross-platform utility that keeps AI-assistant guidelines, rules and ignored files in sync across multiple
agents:

* Cursor
* GitHub Copilot
* Cline
* Claude Code
* OpenAI Codex

It watches the files you specify in a JSON configuration and propagates every change to the corresponding locations for
the other agents.


## Supported sync types

SyncAI supports four kinds of synced items in agent configurations:

- **context** — a single file with general AI guidelines or assistant context (for example `AGENTS.md` or `CLAUDE.md`). Configure with `context.path`. The file is copied verbatim to the target location.

- **rules** — a pattern that matches multiple rule files (for example Cursor rules or Copilot instructions). Configure with `rules.pattern`. If the pattern contains a `*` wildcard, the wildcard is replaced with the source file's base name when copying.

- **ignore** — a single file with instructions telling the assistant what to ignore (for example `.copilotignore`). Configure with `ignore.path`. The file is copied verbatim.

- **skills** — a directory pattern matching skill folders (for example `.claude/skills/*`). Each match is treated as a whole skill: every file inside the directory (including nested subfolders, scripts, data, etc.) is mirrored to the equivalent location at every other agent that has a `skills` pattern. The `*` wildcard captures the skill's folder name and is substituted into the target pattern. Skill files are copied verbatim. Deletions are propagated and now-empty target directories are cleaned up automatically down to the configured base directory, which is preserved.

These sections can be used together for each agent to keep context, rule files, ignore files, and skill folders in sync across different assistants.

## Quick start

1. Download a suitable binary from the [GitHub Releases](https://github.com/flowmitry/syncai/releases).
2. Copy [syncai.json](syncai.json) to your project and adjust the configuration for your agents.
3. Launch the binary in the project dir `./syncai`.

## Configuration

### Launching with arguments

1. Use `./syncai -config {path_to_syncai.json}` to start SyncAI with your custom configuration file.
2. Use `./syncai -workdir {path_to_working_directory}` to specify a different working directory.
3. Use `./syncai -no-watch` to sync your files only once, without watching for changes  (useful for CI).
4. Use `./syncai -self-update` to update SyncAI to the latest version.

### Configuration File

The default configuration is a simple JSON map (for more details check [syncai.json](syncai.json)):

```json
{
  "config": {
    // sync interval in seconds
    "interval": 5,
    // working directory (optional, default is current directory)
    "workdir": ""
  },
  "agents": [
    {
      // agent name
      "name": "<AGENT_NAME>",
      // optional "rules" section
      // GitHub Copilot calls it "instructions", Cursor and Cline "rules"
      "rules": {
        "pattern": ".<AGENT>/rules/*.md"
      },
      // optional "context" section
      "context": {
        "path": "/path/to/your/guidelines.md"
      },
      // optional "ignore" section
      "ignore": {
        "path": "/path/to/your/ignorefile"
      },
      // optional "skills" section: directory pattern matching skill folders.
      // The `*` is captured as the skill folder name and substituted on the
      // other agents. Every file inside the matched directory (recursively)
      // is mirrored.
      "skills": {
        "pattern": ".<AGENT>/skills/*"
      }
    }
  ]
}
```

## How it works

1. SyncAI loads the configuration file and builds a watch-list of directories and files derived from all sections.
2. It periodically scans those files for new or updated modification times.
3. When a rule file changes, its contents are copied to every other agent’s rule directory.

The copying logic is intentionally simple and conservative:

* The filename is preserved exactly, unless the target pattern contains a `*` wildcard—in that case, the wildcard is
  replaced with the source file’s base name.
* Destination directories are created as needed.


## How to build

To build SyncAI manually, follow the next steps:

```bash
cd syncai

make build
```
