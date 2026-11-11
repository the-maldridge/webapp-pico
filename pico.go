/*
Package pico is an alternative theme for the webapp framework: the base layout,
login form, navigation fragment, and error pages written in Pico CSS's markup
instead of the default theme's.

It is a rewrite rather than a wrapper, and with Pico the difference is sharper
than with a class-based framework. Pico styles elements: an `<article>` is a
card, a `<nav>` holding two `<ul>`s is a navigation bar, a `<label>` followed by
a block `<input>` is a form field, and `aria-current="page"` on a link is the
active state. There is almost nothing to opt into, which means markup borrowed
from another framework's idiom does not merely look wrong, it selects nothing at
all. So this package owns its templates outright, writes them the way Pico's own
documentation writes them, and vendors only the stylesheet.

The stylesheet is committed rather than fetched at build time. The framework's
default alternative theme grew that way the hard way: it vendored its bundle with
`go generate`, which made it unusable from any application that imported it as a
library, because go generate refuses to run over a package belonging to a
dependency, and that left the bundle missing from the consumer's build with every
page rendering unstyled. A theme in its own module has exactly the same failure
available to it, so the bundle ships with the source and its licence ships
beside it: the templates are this module's own work and the stylesheet is not.

A release is advanced by editing pico.lock and running `go generate`, which
fetches that release and refuses to write anything whose digest the lock does not
record. `fetchpico -check` answers, without the network, whether what is on disk
is what the lock says, and the tests assert the digests directly so that a
committed bundle which does not match its own lock is a test failure rather than
something shipped.

Importing this package registers the theme, after which WithTheme("pico") or
<PREFIX>_THEME=pico selects it.
*/
package pico

import (
	"embed"
	"io/fs"
)

//go:generate go run ./internal/fetchpico

//go:embed p2
var embedded embed.FS

//go:embed static
var embeddedStatic embed.FS

// FS returns the theme's templates with the p2 directory as its root.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "p2")
	if err != nil {
		// Unreachable: the embed directive above references the directory
		// literally, so a build in which "p2" is absent does not compile.
		panic("pico: embedded template directory is missing: " + err.Error())
	}
	return sub
}

// Static returns the vendored Pico bundle, served by the core under its static
// root so the base layout can link it the way any theme's base layout does.
func Static() fs.FS {
	sub, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		panic("pico: embedded static directory is missing: " + err.Error())
	}
	return sub
}
