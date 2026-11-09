# spondcli

A terminal client for [Spond](https://spond.com), built on the app's
unofficial API. Without arguments it starts an interactive UI (Home, Alerts,
Groups and Messages tabs); subcommands list and export a group's members and
search its posts.

The API is reverse engineered from the Android app and documented in
[docs/API.md](docs/API.md). It is not an official API and may change without
notice.

## Install

```
go install github.com/asbjorn/spondcli/cmd/spondcli@latest
```

From a checkout: `go install ./cmd/spondcli` or `make install`.

`make build` writes `bin/spondcli` with the version (from `git describe`)
stamped in.

## Usage

```
spondcli                                                  # interactive UI
spondcli members -group "Team name" [-subgroup U12] [-o members.csv]
spondcli search -group "Team name" [-subgroup U12] [-refresh] dugnad kiosk
spondcli logout                                           # forget the cached login
spondcli version
spondcli help [COMMAND]
```

Credentials come from `SPOND_EMAIL` and `SPOND_PASSWORD`, or are prompted
for. The access token is cached in the user config directory
(`~/.config/spondcli/session.json` on Linux, mode 0600); posts are cached for
search in the user cache directory (`~/.cache/spondcli/posts.db`).

Exit status: 0 on success, 1 on errors, 2 on a malformed command line.

Member exports contain personal data. `*.csv` is in `.gitignore` so they are
not committed by accident.

## Layout

```
cmd/spondcli/        main: signal handling and exit codes only
internal/cli/        command line: subcommands, flags, help, login prompts
internal/tui/        interactive UI (Bubble Tea)
internal/spondapi/   API client: auth, groups, members, arrangements, posts,
                     activity stream and chat
internal/store/      SQLite post cache and full-text search
internal/roster/     member rows and CSV export
internal/display/    text formatting shared by cli and tui
docs/API.md          reverse-engineered API reference
```

Dependencies point downwards: `cli` uses `tui`; both use `spondapi`,
`store`, `roster` and `display`; nothing imports `cli` or `tui`.

## Development

```
make check      # go vet, go test, gofmt check
```

## License

MIT, see [LICENSE](LICENSE).
