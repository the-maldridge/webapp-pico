package pico

import "github.com/the-maldridge/webapp"

// Required are the templates the framework renders itself, which this theme
// therefore has to supply in full.
//
// They come from the framework rather than from a list kept here, because which
// templates those are is the framework's decision: a theme replaces the
// framework's pages rather than adding optional ones, so the set is fixed by what
// the framework renders. A copy maintained in this repository could only lag it,
// and lagging quietly is the bad outcome, because a theme missing a page the
// framework later grew has no way to say so except by not having that page.
var Required = webapp.RequiredTemplates()

// init registers this theme with the framework.
//
// Registration lives here rather than in the file holding the templates so that
// the choice to depend on the framework is visible as the package's only such
// dependency: everything else in the theme is data, and only this file knows it
// is plugging into anything.
//
// An application selects the theme by importing it for its effect, after which
// <PREFIX>_THEME=pico or webapp.WithTheme("pico") resolves to it. Without the
// import nothing happens, which is the point: a theme an application never
// mentioned is not a theme it should be able to be switched into by an
// environment variable, because nobody checked that it was deployed with one.
func init() {
	webapp.RegisterTheme(webapp.ThemeInfo{
		Name:     "pico",
		FS:       FS,
		Static:   Static,
		Required: Required,
	})
}
