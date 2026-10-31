# JXScout CLI

Build with `make build`, then `go build -o jxscout ./cmd/jxscout`. Bun is
required at runtime. Install webcrack separately (`npm install -g webcrack`) for
in-place JavaScript formatting; `jxscout doctor --format json` checks
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

`init` registers a named project with its absolute output folder and scope.
For example, running `jxscout init --project lippo --scope '*.lippomalls.com/*'`
in `~/target/lippo` saves output to `~/target/lippo/jxscout`.
You can then run `jxscout serve --project lippo` from any directory; it uses
that same output folder. `-p` and `--project-name` are aliases for `--project`.
Explicit `--scope GLOB` flags override the saved scope for that invocation.

The dashboard has five views: **1 Capture**, **2 Projects**, **3 Scope**,
**4 Activity**, and **5 Findings**. Use number keys or Tab to navigate (F2–F5 also work).
The Capture view shows saved-file counts and recent JavaScript/chunks.
In Scope, press **a** to add a pattern, **Enter** to save it, or **Delete** to
remove the selected pattern. Press **:** for commands; command output scrolls
with the arrow keys and closes with Esc. Changes are saved with the
project. Switching projects restores its scope and output folder while keeping
the listener host and port, so Burp stays connected.
The commands `project NAME`, `scope add PATTERN`, and `scope remove PATTERN`
remain available. Project records live in `~/.config/jxscout/projects.json`;
output and database state stay in the initialized folder.

The listener defaults to `localhost:3333`. In the JXScout Burp extension, set
**Server Host** to `localhost` and **Server Port** to `3333`, then click **Save**.
Check **Filter In-Scope** in the extension. Configure the target scope separately
in Burp's Target scope settings; JXScout's local scope patterns do not configure
Burp's scope.

`serve` receives the traffic forwarded by the extension, captures responses,
and automatically analyzes JavaScript and follows discovered chunks. Captured
files go to `jxscout/original/` as `.js` files. Webcrack formats that same file
in place, without `raw/` copies, `.webcrack` folders, or extracted bundle files.
HTML is parsed in memory to find scripts; HTML pages, icons, and Cloudflare
challenge scripts are not saved as capture output. Proxy capture keeps
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
`--webcrack off` to use Prettier only, or `--webcrack required` to fail if it is
unavailable. Original local inputs remain in their artifact directory. Repeated
identical analyses reuse findings and the formatted JavaScript, keyed by input content,
processor revision, and webcrack version.

## Other commands and output

With no arguments, JXScout prints help. `serve` opens the interactive capture
dashboard. Use `serve --headless` for agents and scripts. Legacy launch flags
still open the terminal interface. Use `jxscout serve -h` for listener and proxy
processing flags, or `jxscout analyze --help` for local analysis options.

Text is the default output format. JSON emits one versioned summary or query
result; JSONL emits versioned progress records followed by a summary. Diagnostics
go to stderr. Exit codes are 0 for completion, 1 for invalid input or fatal
errors, 2 for partial results, and 130 for interruption. HTTP timeouts are 30
seconds and webcrack has a 120-second timeout. Partial results remain queryable.

Without a workspace, proxy output defaults to `~/jxscout/PROJECT/`, with state
under `~/.jxscout/PROJECT/`. `JXSCOUT_DATA_DIR` overrides storage for both proxy
capture and local analysis.

Chunk discovery reads explicit precache manifest URLs, imports, and chunk hash tables.
It skips CSS hash maps and ordinary application object values, deduplicates URLs
across source files, and stores download outcomes for `jxscout chunks --format json`.
The Findings view shows static analysis matches with source locations; these are
review leads, not confirmed vulnerabilities.

Discovery starts automatically when JavaScript is captured, independently of
Prettier/Webcrack completion. Starting `serve` also resumes discovery from saved
project scripts. Candidate responses must be successful HTTP responses and pass
a JavaScript parser before they are saved; HTML, JSON, CSS, and malformed scripts
are rejected. Downloaded chunks trigger the same discovery pipeline recursively.
