// Command mkd2xml converts the kramdown-rfc flavoured Markdown source of this
// Internet-Draft into RFCXML v3, the format accepted by the IETF datatracker.
//
// It is a deliberately small replacement for kramdown-rfc: it supports the
// constructs this draft uses and fails with a line number on anything else,
// rather than guessing.
//
// Usage:
//
//	go run ./cmd/mkd2xml -o docs/draft.xml draft-ietf-acme-dns-account-challenge.mkd
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func main() {
	out := flag.String("o", "", "output file (default: standard output)")
	date := flag.String("date", "", "document date as YYYY-MM-DD (default: the front matter date, else today)")
	cache := flag.String("refcache", ".refcache", "directory used to cache fetched references")
	offline := flag.Bool("offline", false, "fail instead of fetching references that are not cached")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: mkd2xml [flags] draft.mkd\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(flag.Arg(0), *out, *date, *cache, *offline); err != nil {
		fmt.Fprintf(os.Stderr, "mkd2xml: %v\n", err)
		os.Exit(1)
	}
}

func run(in, out, date, cache string, offline bool) error {
	src, err := os.ReadFile(in)
	if err != nil {
		return err
	}

	opts := Options{Refs: &CachedFetcher{Dir: cache, Offline: offline}}
	if date != "" {
		opts.Date, err = time.Parse("2006-01-02", date)
		if err != nil {
			return fmt.Errorf("-date: %v", err)
		}
	}

	xml, err := Convert(src, opts)
	if err != nil {
		return fmt.Errorf("%s: %v", in, err)
	}
	if out == "" {
		_, err = os.Stdout.Write(xml)
		return err
	}
	return os.WriteFile(out, xml, 0o644)
}
