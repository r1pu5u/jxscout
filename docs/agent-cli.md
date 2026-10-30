# Agent CLI

Build with `make build`, then `go build -o jxscout ./cmd/jxscout`.
Bun is required at runtime. Install webcrack separately (`npm install -g webcrack`)
for deobfuscation and module extraction; `jxscout doctor --format json` checks availability.

Initialize a target directory to keep all project output beside your target files:

```sh
cd ~/target/foo.com
jxscout init --project foo.com --scope '*foo.com*'
jxscout scan https://foo.com --format json
```

This creates `~/target/foo.com/jxscout/config.json` and writes artifacts directly
under `~/target/foo.com/jxscout/`. Project name and scope are reused automatically
from this directory or any subdirectory, including for `serve` and `tui`. Re-running
`init` preserves existing settings; use `init --force` to update them. Explicit
`--project` / `--scope` flags override saved defaults for an individual command.
Without an initialized directory, the home-directory layout below remains available.

```sh
jxscout scan https://example.test/ --format json
jxscout scan --list known-chunks.txt --project example --format jsonl
jxscout analyze ./bundles --webcrack required --format json
jxscout chunks --project example --page-size 100 --format json
jxscout findings --project example --search fetch --format json
jxscout runs --project example --format json
jxscout serve -project-name example -webcrack auto
jxscout tui -project-name example
```

With no arguments, JXScout prints help. Legacy launch flags still open the terminal
capture interface. `serve` runs capture until SIGINT/SIGTERM without a terminal.

`scan` follows scripts, module preloads, imports, runtime filename maps, and inferred
ID/hash templates. Supplying already-known hashed chunk URLs helps it reconstruct
URLs from tables that have no recognizable loader function:

```sh
jxscout scan --list chunks.txt --chunk-template 'https://cdn.example.test/js/{id}.{hash}.js'
```

Only the filename components in inferred templates are replaced. Numeric brute
force defaults to 3000; explicit map keys are considered regardless of that limit.
Downloaded chunks feed back into discovery. Candidates retain their method and
evidence, and failed candidates remain queryable. Unsupported runtime builders retain their
source expression as an `unresolved` chunk record. Discovery does not execute the
target application in a browser.

Use `--webcrack off` to skip recovery, or `required` to fail when it is unavailable.
Recovered files receive AST analysis and chunk discovery. Original standalone input
bytes remain in their artifact directory. Repeated identical analyses reuse findings
and webcrack output; the compiled processor revision and webcrack version are part
of the cache key.

Local analysis does not fetch remote references unless `--fetch` is supplied.
Use `--base-url` to resolve downloaded bundles against their original location.
`--public-path` overrides the resolution base. `--scope` accepts repeatable wildcard
patterns. `--header` accepts repeatable `Name: value` arguments, restricted to the
first seed's origin. Referenced hosts receive no supplied credentials.

Standalone results live under `~/jxscout/PROJECT/artifacts`; run metadata, candidates,
and findings use `~/.jxscout/PROJECT/agent.sqlite`. `--data-dir` overrides both roots. Set `JXSCOUT_DATA_DIR` to use the same
storage override for both proxy capture and standalone commands.
Query commands also include existing proxy assets and AST results when no run filter
is supplied. Proxy rows are marked with `source: proxy`.

Text is the default format. JSON produces one versioned summary/result record;
JSONL also emits progress records. Diagnostics use stderr. Exit codes are 0 for
completion, 1 for invalid input/fatal errors, 2 for partial results, and 130 for
interruption. The asset cap defaults to 10000, HTTP timeouts to 30 seconds, and
webcrack to 120 seconds. Partial results include errors and remain queryable.

Proxy capture uses the existing durable event pipeline. Automatic webcrack runs
after beautification and saves a separate `.webcrack` directory beside each asset;
its recovered JavaScript enters both AST and chunk processing. Standalone analysis also recovers embedded `sourcesContent` from explicit source-map
comments and inline data maps. Proxy capture retains its additional source-map probes.
