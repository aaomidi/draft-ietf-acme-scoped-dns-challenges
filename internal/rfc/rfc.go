// Package rfc renders an Internet-Draft written in RFCXML v3 as paginated
// text and as HTML, laid out the way xml2rfc lays it out.
//
// It is deliberately small: it supports the elements that mkd2xml produces
// for this draft, for an IETF stream Internet-Draft, and fails on anything
// else rather than guessing.
package rfc

import (
	"fmt"
	"io"
	"time"
)

// load reads and prepares a document. The build date supplies the copyright
// year and any part of the document date that is missing.
func load(r io.Reader, today time.Time) (*document, error) {
	root, err := parse(r)
	if err != nil {
		return nil, err
	}
	return prepare(root, today)
}

// render runs fn on the prepared document. The renderers panic on a
// combination of elements they have no rule for; that is reported as an
// error like any other unsupported input.
func render(r io.Reader, today time.Time, fn func(*document) string) (out string, warnings []string, err error) {
	defer func() {
		if p := recover(); p != nil {
			out, warnings, err = "", nil, fmt.Errorf("cannot render this document: %v", p)
		}
	}()
	doc, err := load(r, today)
	if err != nil {
		return "", nil, err
	}
	out = fn(doc)
	return out, doc.warnings, nil
}

// Text renders an RFCXML document as paginated plain text, and returns any
// warnings about the result.
func Text(r io.Reader, today time.Time) (text string, warnings []string, err error) {
	return render(r, today, (*document).text)
}

// HTMLOptions adjusts the HTML output.
type HTMLOptions struct {
	// XMLLink is the URL of the XML source, for the page's "alternate"
	// link. It is left out when empty.
	XMLLink string
}

// HTML renders an RFCXML document as a single HTML page, and returns any
// warnings about the result.
func HTML(r io.Reader, today time.Time, opts HTMLOptions) (html string, warnings []string, err error) {
	return render(r, today, func(d *document) string { return d.html(opts) })
}
