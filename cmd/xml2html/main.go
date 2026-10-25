// Command xml2html renders an Internet-Draft in RFCXML v3 as a single HTML
// page with the structure and styling of xml2rfc's HTML.
//
// It is a deliberately small replacement for "xml2rfc --html": it supports
// the elements that mkd2xml produces for this draft, for an IETF stream
// Internet-Draft, and fails on anything else rather than guessing.
//
// Usage:
//
//	go run ./cmd/xml2html -o draft.html draft.xml
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/aaomidi/draft-ietf-acme-scoped-dns-challenges/internal/rfc"
)

func main() {
	out := flag.String("o", "", "output file (default: standard output)")
	date := flag.String("date", "", "build date as YYYY-MM-DD, used for the copyright year (default: today)")
	xmlLink := flag.String("xml-link", "", "URL of the XML source to link from the page (default: the input file name)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: xml2html [flags] draft.xml\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(flag.Arg(0), *out, *date, *xmlLink, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "xml2html: %v\n", err)
		os.Exit(1)
	}
}

func run(in, out, date, xmlLink string, warnings io.Writer) error {
	if xmlLink == "" {
		xmlLink = filepath.Base(in)
	}
	today := time.Now()
	if date != "" {
		var err error
		if today, err = time.Parse("2006-01-02", date); err != nil {
			return fmt.Errorf("-date: %v", err)
		}
	}

	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()

	text, warns, err := rfc.HTML(f, today, rfc.HTMLOptions{XMLLink: xmlLink})
	if err != nil {
		return fmt.Errorf("%s: %v", in, err)
	}
	for _, w := range warns {
		fmt.Fprintf(warnings, "xml2html: warning: %s\n", w)
	}
	if out == "" {
		_, err = io.WriteString(os.Stdout, text)
		return err
	}
	return os.WriteFile(out, []byte(text), 0o644)
}
