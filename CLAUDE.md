# CLAUDE.md

Notes for working in this repo. Read `CONTRIBUTING.md` first: its ground rules
and package layering apply here too.

## Tests

- Styled output in `internal/render` (tables, checks, marks) only runs when
  `isTerminal(w)` sees a terminal, and its layout depends on
  `terminalWidth(w)`. Tests reach it only by swapping those func variables (see
  `styledTerminal` in `internal/render/styled_test.go`). Strip ANSI codes before
  asserting, and don't call `t.Parallel()` in these tests: render state is
  package-global.

## Shared output

- `pluginInstalled` (`internal/cli/plugins.go`) builds the result line for both
  `kemory connect` and `kemory doctor --fix`. Changing its text changes both
  commands' output and the tests that pin them.

## Restricted networks

- Where go.dev, dl.google.com, proxy.golang.org and golang.org don't resolve,
  `mise install go` fails, and so do `make test`, `make lint` and other
  `go run` tools. Go 1.27 can be installed from PyPI instead
  (`pip install go-bin` in a venv). Then build with `GOPROXY=direct`,
  `GOSUMDB=off` and a copy of `go.mod` passed with `-modfile` that replaces
  the modules hosted there (`golang.org/x/term`, `sys`, `text`; also
  `go.yaml.in` and `gopkg.in` modules) with their github.com mirrors, e.g.
  `golang.org/x/term` with `github.com/golang/term`. If that isn't possible,
  verify the change through the pull request's CI checks.
