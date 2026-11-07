# CLAUDE.md

fujiwatch: a daily price watcher for Fujifilm X-mount gear in Vietnamese camera shops (Go, GitHub Actions, Svelte history site). Start with the [README](README.md); details are in [docs/usage.md](docs/usage.md), [docs/architecture.md](docs/architecture.md) and [docs/shops.md](docs/shops.md). Don't repeat their content here.

## Keep the docs current

The project is pre-release: the docs describe what the code does **now**, not the original design. After every change, update the doc that describes it, in the same commit:

| Change | Update |
|---|---|
| Product added, removed or renamed in `config/watchlist.yaml` | README products table |
| Shop added | README shops table, a `## <shop>` section in `docs/shops.md`, the adding-a-shop steps if they changed |
| Something learnt about a shop (URL forms, robots rules, API fields, quirks) | its section in `docs/shops.md` |
| Source pages added or removed | nothing, unless it changes what we know about the shop (`docs/shops.md`) |
| CLI command or flag | `docs/usage.md` CLI section (and the `usage` string in `cmd/fujiwatch/main.go`) |
| Config field, notifier variable, workflow or secret | `docs/usage.md` |
| Package, interface, event type, pipeline or state format | `docs/architecture.md` |
| A milestone or open item done or added | README |

`cmd/fujiwatch/docs_test.go` fails when the README tables, `docs/shops.md` or the CLI flags fall behind the code, or a relative link breaks. Code comments refer to docs by file and section name (`docs/architecture.md, State, history and events`), not by number.

## Verify against the real sites

- Before adding a page: check the shop's `robots.txt`, and whether the listing spans several pages.
- After changing a product, source or adapter: `go run ./cmd/fujiwatch run --dry-run --no-notify --only <shop>`, and look at the offer counts, matches and the unmatched list.
- Check a matcher change with `go run ./cmd/fujiwatch match "<real shop title>"`, including kits and other mounts that must **not** match.
- **Never** run `fujiwatch run` without `--dry-run` locally: it writes `data/` and sends real notifications. `data/` is written by CI only.
- Keep live requests few and spaced (a few seconds per host); save pages with `fujiwatch snapshot` and iterate on the files.

## Code and commits

- `gofmt`, `go vet ./...` and `go test ./...` must pass before a commit. Tests that run over saved snapshots count matches, so watchlist changes can shift expected numbers in `internal/runner/runner_test.go`; update the numbers and their comments together.
- Adapter changes: refresh the golden files with `go test ./internal/adapters -update` and review the diff.
- Work on a branch, merge into `main` with `--ff-only` when asked. Don't push unless asked.
