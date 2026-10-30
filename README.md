# JXScout

JXScout collects and analyzes JavaScript for security research. This fork adds an
agent-friendly CLI, local project workspaces, recursive discovery of hidden hashed
chunks, and automatic webcrack processing to the original Burp/Caido workflow.

## Features

- **Local workspaces:** initialize a target directory and keep its settings and output in `./jxscout/`.
- **Standalone scans:** start from a page, a JavaScript URL, or a list of known chunk URLs without running a proxy.
- **Hidden chunk discovery:** reconstruct filenames from ID/name/hash tables, runtime builders, encoded constant tables, and observed filename templates.
- **Recursive coverage:** follow imports, HTML scripts, preload hints, dependency tables, and newly discovered chunks.
- **Webcrack integration:** deobfuscate and unpack JavaScript, then analyze recovered files for findings and additional chunks.
- **Agent output:** query assets, candidates, findings, and runs using JSON or JSONL.
- **Proxy capture:** retain Burp/Caido ingestion, source-map recovery, beautification, overrides, and the VSCode integration.

## Install

Building requires Go 1.23.2 or newer and Bun. Bun is also required at runtime.
Proxy beautification requires Prettier. Webcrack is optional by default and requires
a supported Node.js version; the current release supports Node.js 22 or 24.

```sh
git clone https://github.com/r1pu5u/jxscout.git
cd jxscout
bun install --frozen-lockfile
bun run scripts/build-agent-cli.ts
go install ./cmd/jxscout

# Install processing tools separately if needed.
bun install -g prettier
npm install -g webcrack

jxscout doctor --format json
```

`go install` puts the executable in `GOBIN`, or `$(go env GOPATH)/bin` when `GOBIN`
is unset. Add that directory to your PATH. To build a repository-local executable,
use `go build -o jxscout ./cmd/jxscout` instead.

Use `make build` when rebuilding all embedded JavaScript processors after changing
their source. The install commands above rebuild the standalone CLI and chunk
processor and use the checked-in AST and source-map processors.

## Initialize a target

Run this from the directory where you want to keep your target's files:

```sh
cd ~/target/foo.com
jxscout init --project foo.com --scope '*foo.com*'
```

Output goes directly into `~/target/foo.com/jxscout/`, with no additional
project-name directory. Settings are saved in `jxscout/config.json` and reused by
commands run from the target directory or any of its subdirectories.

Repeat `--scope` to allow several patterns. A URL may match any one of them:

```sh
jxscout init --project foo.com \
  --scope '*foo.com*' \
  --scope '*bar.com*' \
  --scope '*cdn.example.net*'
```

Use `init --force` with the complete desired scope list to update an existing
workspace. Re-running `init` without changes preserves its settings. Explicit
`--project` and `--scope` flags override saved defaults for an individual command.
Quote patterns so your shell does not expand `*`.

## Scan and analyze

```sh
# Scan a page and recursively follow its JavaScript dependencies.
jxscout scan https://foo.com --format json

# Seed discovery with known bundle and chunk URLs, one per line.
jxscout scan --list chunks.txt --format jsonl

# Analyze a local file or directory without changing the inputs.
jxscout analyze ./downloaded-js --webcrack required --format json

# Resolve and fetch remote dependencies referenced by downloaded files.
jxscout analyze ./downloaded-js --fetch --base-url https://foo.com/assets/ --format json

# Inspect saved results.
jxscout assets --format json
jxscout chunks --format json
jxscout findings --search fetch --format json
jxscout runs --format json
```

Local analysis follows local dependencies without applying network scope patterns
to local input files. Remote fetching requires `--fetch`; configured URL scope
patterns still apply to remote requests.

### Finding hidden hashed chunks

A bundle may contain an ID/hash table without a complete filename or recognizable
loader. Given a known URL such as `https://foo.com/assets/4821.a8bc39de.js`, JXScout
can infer an `assets/{id}.{hash}.js` template, extract additional pairs from the
JavaScript, fetch matching files, and repeat discovery on those files.

Supply several known URLs with `--list`, or provide a template explicitly:

```sh
jxscout scan --list chunks.txt \
  --chunk-template 'https://foo.com/assets/{id}.{hash}.js' \
  --format json
```

Known mapping keys are considered even when their IDs exceed the numeric
brute-force limit. `--bruteforce-limit 0` disables numeric guessing while retaining
known-key discovery. Multiple runtime builders, Vite dependency tables, static and
dynamic imports, and supported constant decoders are processed together.

Candidates retain their discovery method and evidence. Downloaded files, failed
candidates, and unresolved runtime expressions remain distinguishable in `chunks`.
Discovery uses static analysis; it does not execute the target application in a browser.

### Webcrack

- `--webcrack auto` is the default: run webcrack when installed.
- `--webcrack off` skips recovery.
- `--webcrack required` fails when webcrack is unavailable.

Recovered JavaScript receives AST analysis and chunk discovery. Captured bytes are
preserved separately from derived files. Successful standalone results are cached
using input content, processor revision, and webcrack version; failed partial
webcrack output is retried.

## Proxy capture and terminal interface

```sh
# Run capture without a terminal UI; stop with Ctrl-C.
jxscout serve

# Open the compact interactive terminal interface.
jxscout tui

# Override the workspace project or server port.
jxscout serve --project another-project -port 3333
```

Configure your proxy plugin to forward traffic to the listener, which defaults to
`localhost:3333`:

- [Burp plugin](https://github.com/francisconeves97/jxscout-burp)
- [Caido plugin](https://github.com/francisconeves97/jxscout-caido)
- [VSCode extension](https://github.com/francisconeves97/jxscout-vscode)

Disable browser caching when relying on proxy capture so requests pass through the
proxy. Capture also follows extracted script references and discovered chunks.
Webcrack runs after beautification; recovered files feed into AST analysis and
chunk discovery. Existing terminal commands, including `config`, `logs`, and Caido
overrides, remain available inside `tui`.

Running `jxscout` without arguments displays help. Legacy launch flags still open
the interactive capture interface. Use `jxscout serve -h` for listener and proxy
processing flags, or `jxscout scan --help` for standalone commands.

## Output and automation

Inside an initialized workspace:

```text
jxscout/
  config.json                 Project name and scope
  artifacts/                  Standalone captured and recovered code
  original/                   Proxy working files
  raw/                        Immutable proxy capture snapshots
  .state/PROJECT/              SQLite databases, capture settings, and logs
```

Additional proxy source-map and webcrack output is written as those stages run.
Standalone source-map recovery supports explicit source-map comments and inline
maps with `sourcesContent`; proxy capture retains its additional map probes.

Without a workspace, output defaults to `~/jxscout/PROJECT/`, with state under
`~/.jxscout/PROJECT/`. `--data-dir` overrides standalone storage;
`JXSCOUT_DATA_DIR` provides a storage override shared with proxy capture.

| Option | Behavior |
| --- | --- |
| `--format text` | Human-readable output; the default |
| `--format json` | One versioned summary or query result |
| `--format jsonl` | Versioned progress records followed by a summary |
| `--run ID` | Filter saved results by run |
| `--page N --page-size N` | Paginate query results |
| `--header 'Name: value'` | Repeatable headers restricted to the first seed's origin |
| `--public-path URL` | Override the base for discovered references |
| `--max-assets N` | Limit visited inputs; defaults to 10000 |

Diagnostics go to stderr. Exit codes are `0` for completion, `1` for invalid input
or fatal errors, `2` for partial results, and `130` for interruption. HTTP and chunk
discovery timeouts are 30 seconds; webcrack has a 120-second timeout. Partial results
remain queryable. Supplied credentials are not forwarded to other origins, and
scope is checked on redirect targets.

Query commands also include existing proxy assets and analysis when no run filter
is supplied; proxy rows are labeled `source: proxy`.

## Development

```sh
bun install --frozen-lockfile
make build
go build -o jxscout ./cmd/jxscout
go test ./...

# Include compiled CLI and proxy workspace integration tests.
JXSCOUT_TEST_BINARY="$PWD/jxscout" bun run vitest run
```

See [the agent CLI guide](docs/agent-cli.md) for additional details and
[the analysis tree reference](docs/tree.md) for the VSCode result hierarchy.

## Upstream

This fork is based on [francisconeves97/jxscout](https://github.com/francisconeves97/jxscout).
The original open-source version is no longer maintained by its original author;
[JXScout Pro](https://jxscout.app/) is a separate product. The JS Deep-style
filename-template approach informed the additional hidden chunk discovery in this fork.

See [COPYING](COPYING) for the license.
