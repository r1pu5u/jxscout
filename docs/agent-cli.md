# JXScout CLI

Build with `make build`, then `go build -o jxscout ./cmd/jxscout`. Bun is
required at runtime. Install webcrack separately (`npm install -g webcrack`) for
deobfuscation and module extraction; `jxscout doctor --format json` checks
availability.

## Initialize and capture with Burp

From the directory where the target's files and JXScout output should live, run
`init` with a project name and the JXScout URL scope patterns:

```sh
cd ~/target/lippo
jxscout init --project lippo \
  --scope '*lippo*' \
  --scope '*cdn.example.net*'
```

`init` is setup-only: it writes `jxscout/config.json` and creates the workspace.
Settings are picked up by commands run from that directory or any subdirectory.
Use `init --force` with the desired project and complete scope list to update an
existing workspace. Explicit `--project` and `--scope` flags override saved
defaults for one invocation. Quote wildcard patterns so the shell does not
expand them.

Start the listener from the target directory or one of its subdirectories and
leave it running while browsing through Burp:

```sh
jxscout serve
```

The listener defaults to `localhost:3333`. In the JXScout Burp extension, set
**Server Host** to `localhost` and **Server Port** to `3333`, then click **Save**.
Check **Filter In-Scope** in the extension. Configure the target scope separately
in Burp's Target scope settings; JXScout's local scope patterns do not configure
Burp's scope.

`serve` receives the traffic forwarded by the extension, captures responses,
and automatically analyzes JavaScript and follows discovered chunks. Captured
files go to `jxscout/original/`, immutable proxy snapshots go to `jxscout/raw/`,
and webcrack outputs are written beside source files in `.webcrack` directories.
Recovered files also enter AST analysis and chunk discovery. Proxy capture keeps
its additional source-map probes. Stop the listener with Ctrl-C or SIGTERM.

Query saved results from the workspace:

```sh
jxscout assets --page-size 100 --format json
jxscout chunks --format json
jxscout findings --search fetch --format json
jxscout runs --format json
```

Query commands also include existing proxy assets and AST results when no run
filter is supplied. Proxy rows are marked with `source: proxy`.

## Optional local analysis

`analyze` processes JavaScript, HTML, or directories already on disk. It is an
optional workflow for reviewing local files; it does not replace proxy capture
or receive Burp traffic.

```sh
jxscout analyze ./bundles --webcrack required --format json
```

Local analysis follows local dependencies and does not fetch remote references
unless `--fetch` is supplied. Use `--base-url` to resolve local bundle references
against their original URL, or `--public-path` to override the resolution base.
Remote requests still follow configured JXScout scope patterns. `--header` accepts
repeatable `Name: value` arguments; supplied credentials are restricted to the
first seed's origin. Referenced hosts receive no supplied credentials. Local
analysis supports explicit source-map comments and inline maps containing
`sourcesContent`.

`--webcrack auto` is the default and runs webcrack when installed. Use
`--webcrack off` to skip recovery, or `--webcrack required` to fail if it is
unavailable. Original local inputs remain in their artifact directory. Repeated
identical analyses reuse findings and webcrack output, keyed by input content,
processor revision, and webcrack version.

## Other commands and output

With no arguments, JXScout prints help. `serve` runs capture without a terminal
UI; `tui` opens the interactive capture dashboard. Legacy launch flags still
open the terminal interface. Use `jxscout serve -h` for listener and proxy
processing flags, or `jxscout analyze --help` for local analysis options.

Text is the default output format. JSON emits one versioned summary or query
result; JSONL emits versioned progress records followed by a summary. Diagnostics
go to stderr. Exit codes are 0 for completion, 1 for invalid input or fatal
errors, 2 for partial results, and 130 for interruption. HTTP timeouts are 30
seconds and webcrack has a 120-second timeout. Partial results remain queryable.

Without a workspace, proxy output defaults to `~/jxscout/PROJECT/`, with state
under `~/.jxscout/PROJECT/`. `JXSCOUT_DATA_DIR` overrides storage for both proxy
capture and local analysis.
