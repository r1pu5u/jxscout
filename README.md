# JXScout

JXScout captures and analyzes JavaScript for security research through Burp or
Caido. This fork adds local project workspaces, recursive discovery of hidden
hashed chunks, and automatic webcrack processing to the proxy workflow.

## Features

- **Local workspaces:** initialize a target directory and keep its settings and output in `./jxscout/`.
- **Automatic chunk discovery:** reconstruct filenames from ID/name/hash tables, runtime builders, encoded constant tables, and observed filename templates in captured JavaScript.
- **Recursive coverage:** follow imports, HTML scripts, preload hints, dependency tables, and newly discovered chunks from proxy traffic.
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
their source. The install commands above rebuild the agent CLI and chunk
processor and use the checked-in AST and source-map processors.

## Capture with Burp

Initialize the project from the target directory. Give it a project name and one
or more JXScout scope patterns:

```sh
cd ~/target/lippo
jxscout init --project lippo \
  --scope '*lippo*' \
  --scope '*cdn.example.net*'
```

`init` is setup-only: it creates `~/target/lippo/jxscout/` and saves project
settings there. Commands run from the target directory or any subdirectory reuse
those settings. Repeat `--scope` for additional patterns; use `init --force` to
update an existing workspace. Quote wildcard patterns so your shell does not
expand `*`.

Start the capture listener from the target directory or any subdirectory and
leave it running while browsing through Burp:

```sh
jxscout serve
```

The listener defaults to `localhost:3333`. In the JXScout Burp extension, set
**Server Host** to `localhost` and **Server Port** to `3333`, click **Save**, and
check **Filter In-Scope**. Configure the target scope separately in Burp's
Target scope settings; JXScout's local scope patterns do not configure Burp's
scope for you.

As Burp forwards traffic, `serve` captures responses, stores the original files,
and automatically analyzes JavaScript and follows discovered chunks. Webcrack
output and source-map recovery feed into analysis and further chunk discovery.
Use Ctrl-C to stop the listener.

Inspect the captured results from the workspace:

```sh
jxscout assets --format json
jxscout chunks --format json
jxscout findings --search fetch --format json
jxscout runs --format json
```

JXScout's scope patterns control its own capture processing. They are separate
from Burp's Target scope and the extension's **Filter In-Scope** setting.

## Optional local analysis

`analyze` processes JavaScript, HTML, or directories already on disk. It is useful
for reviewing saved files; it does not replace proxy capture or capture traffic.

```sh
jxscout analyze ./downloaded-js --webcrack required --format json
```

Local analysis follows local dependencies. Fetching remote references requires
`--fetch`, and configured JXScout scope patterns still apply to remote requests.
Source-map comments and inline maps with `sourcesContent` are supported. Use
`jxscout analyze --help` for options.

## Proxy capture and terminal interface

```sh
# Run the same capture listener without a terminal UI; stop with Ctrl-C.
jxscout serve

# Open the compact interactive terminal interface.
jxscout tui

# Override the project or server port for this invocation.
jxscout serve --project another-project -port 3333
```

The Burp extension setup above forwards traffic to the listener, which defaults to
`localhost:3333`. JXScout also supports these integrations:

- [Burp plugin](https://github.com/francisconeves97/jxscout-burp)
- [Caido plugin](https://github.com/francisconeves97/jxscout-caido)
- [VSCode extension](https://github.com/francisconeves97/jxscout-vscode)

Disable browser caching when relying on proxy capture so requests pass through the
proxy. Captured JavaScript feeds chunk discovery automatically. Webcrack runs after
beautification; recovered files feed into AST analysis and chunk discovery.
Existing terminal commands, including `config`, `logs`, and Caido overrides,
remain available inside `tui`.

Running `jxscout` without arguments displays help. Legacy launch flags still open
the interactive capture interface. Use `jxscout serve -h` for listener and proxy
processing flags, or `jxscout analyze --help` for local-file analysis options.

## Output and automation

Inside an initialized workspace:

```text
jxscout/
  config.json                 Project name and scope
  original/                   Proxy working files
  raw/                        Immutable proxy capture snapshots
  original/path/to/file.js.webcrack/  Recovered JavaScript beside its source
  .state/PROJECT/              SQLite databases, capture settings, and logs
```

Proxy source-map recovery and webcrack output are written as those stages run.
Local `analyze` also supports explicit source-map comments and inline maps with
`sourcesContent`; proxy capture retains its additional map probes.

Without a workspace, output defaults to `~/jxscout/PROJECT/`, with state under
`~/.jxscout/PROJECT/`. `--data-dir` overrides local analysis storage;
`JXSCOUT_DATA_DIR` provides a storage override shared with proxy capture.

| Option                   | Behavior                                         |
| ------------------------ | ------------------------------------------------ |
| `--format text`          | Human-readable output; the default               |
| `--format json`          | One versioned summary or query result            |
| `--format jsonl`         | Versioned progress records followed by a summary |
| `--run ID`               | Filter saved results by run                      |
| `--page N --page-size N` | Paginate query results                           |
| `--header 'Name: value'` | Repeatable headers for optional remote fetches   |
| `--public-path URL`      | Override the base for local analysis references  |
| `--max-assets N`         | Limit local analysis inputs; defaults to 10000   |

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
