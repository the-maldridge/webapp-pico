# webapp-pico

The [Pico CSS](https://picocss.com) theme for
[`github.com/the-maldridge/webapp`](https://github.com/the-maldridge/webapp), as
a separate module. It is the second worked example of a theme that lives outside
the framework: templates, a vendored Pico bundle, and a `register.go`, and nothing
else. The framework never imports it; an application imports it.

## Use it

```go
import (
	"github.com/the-maldridge/webapp"
	_ "github.com/the-maldridge/webapp-pico"
)

app, err := webapp.New(
	webapp.WithTheme("pico"),
	// ...
)
```

Blank-importing the package registers the theme under the name `pico` during
`init`. `WEBAPP_THEME=pico` (whatever the application's environment prefix is)
then selects it at boot, without a rebuild.

The theme owns the markup of every page the framework renders: the base layout,
the login page, the error pages. Application templates that extend
`@core/base.p2` are untouched by the switch, which is the point.

## What is in here

| Path | The thing it is |
|---|---|
| `p2/` | The theme's templates, embedded and mounted as `@core/…` |
| `static/css/pico.min.css` | The vendored Pico bundle, committed |
| `static/css/LICENSE.md` | Pico's MIT licence, so it travels with the binary |
| `pico.lock` | The Pico release this vendors, with the digest of every file |
| `internal/fetchpico` | The generator that advances the bundle, `-check` to verify offline |
| `register.go` | Registration: the one file here that imports the framework |

There is no `static/js`, and that is a property of the theme rather than an
oversight. Pico's navigation is a flex row that never hides itself, its dropdown
is a `<details>` element, and its light and dark scheme comes from
`prefers-color-scheme`, so nothing on a page has to be made to move by a script.
An application can serve this theme under `Content-Security-Policy: script-src
'none'` and every part of it still works.

Pico styles elements rather than classes, so the templates are written the way
Pico's own documentation writes them: `<article>` for a card, a `<nav>` holding
two `<ul>` elements for the navigation bar, a `<label>` wrapping its `<input>` for
a field, `aria-current="page"` for the active link. The only class the layout
carries is `container`. A test asserts that every class the templates name is a
class the bundle actually defines, because there is no stylesheet in this module
for an invented one to live in.

The bundle is committed rather than generated at build time, and that is
deliberate: a library consumer's build must not need a network fetch or a
`go:generate` step it does not know about. `make test` checks the committed
files against the digests in `pico.lock`, so a bundle that silently rotted or
was hand-edited fails the suite.

## Development

A clone of this repository alone builds and tests, because the framework is an
ordinary published dependency with a `require` and a `go.sum` like anyone else's.

The workspace is for the one thing a version pin cannot do: developing a change
that spans both repositories. Check them out side by side and let a gitignored
`go.work` point this module at the local framework.

```
parent/
├── webapp/       # github.com/the-maldridge/webapp
└── webapp-pico/  # this repository
```

```sh
go work init .
go work use ../webapp
```

While the workspace exists it supersedes the require, so framework edits are
picked up without waiting for a release. It stays gitignored because it names a
directory that only exists in a checkout that cloned both, and somebody who
cloned only the theme has to keep resolving the framework from its published
version; committing the workspace would turn that into "directory not found".

## Advancing the Pico release

1. Edit `pico.lock`: point `version` at the new release and update every
   `sha256:` digest to what the upstream assets have.
   `go run ./internal/fetchpico -version X.Y.Z -digest` prints them.
2. `go generate .` — the fetcher downloads the named release, refuses anything
   whose digests do not match the lock, and writes the bundle.
3. `make test` — including the contract suite the framework publishes
   (`webapp/themetest`), which is what says the templates still satisfy what
   the framework's Go code expects of them.

`go run ./internal/fetchpico -check` verifies the committed bundle against the
lock without any network, which is what CI would run if CI did not just run the
tests, which do the same check.
