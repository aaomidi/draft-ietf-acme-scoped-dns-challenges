// Command xml2txt renders an Internet-Draft in RFCXML v3 as paginated plain
// text, laid out the way xml2rfc lays it out.
//
// It is a deliberately small replacement for "xml2rfc --text": it supports
// the elements that mkd2xml produces for this draft, for an IETF stream
// Internet-Draft, and fails on anything else rather than guessing.
//
// Usage:
//
//	go run ./cmd/xml2txt -o draft.txt draft.xml
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aaomidi/draft-ietf-acme-scoped-dns-challenges/internal/rfc"
)

func main() {
	out := flag.String("o", "", "output file (default: standard output)")
	date := flag.String("date", "", "build date as YYYY-MM-DD, used for the copyright year (default: today)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: xml2txt [flags] draft.xml\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(flag.Arg(0), *out, *date, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "xml2txt: %v\n", err)
		os.Exit(1)
	}
}

func run(in, out, date string, warnings io.Writer) error {
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

	text, warns, err := rfc.Text(f, today)
	if err != nil {
		return fmt.Errorf("%s: %v", in, err)
	}
	for _, w := range warns {
		fmt.Fprintf(warnings, "xml2txt: warning: %s\n", w)
	}
	if out == "" {
		_, err = io.WriteString(os.Stdout, text)
		return err
	}
	return os.WriteFile(out, []byte(text), 0o644)
}
