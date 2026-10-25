package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

func testOptions() Options {
	return Options{
		Date: time.Date(2026, time.October, 25, 0, 0, 0, 0, time.UTC),
		Refs: &CachedFetcher{Dir: filepath.Join("testdata", "refcache"), Offline: true},
	}
}

// TestConvertGolden converts a sample draft that exercises every supported
// construct and compares the result with the checked-in XML. Run
// "go test ./cmd/mkd2xml -update" to regenerate it after an intended change.
func TestConvertGolden(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "sample.mkd"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Convert(src, testOptions())
	if err != nil {
		t.Fatal(err)
	}

	dec := xml.NewDecoder(bytes.NewReader(got))
	for {
		if _, err := dec.Token(); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("output is not well-formed XML: %v", err)
		}
	}

	golden := filepath.Join("testdata", "sample.xml")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output differs from %s; run with -update if the change is intended.\ngot:\n%s", golden, got)
	}
}

// minimal wraps a middle part in the smallest front matter that converts.
func minimal(middle string) []byte {
	return []byte(`---
title: T
docname: draft-example-00
category: info
ipr: trust200902
author:
 -
    fullname: A
--- abstract

Abstract.

--- middle

` + middle)
}

func TestConvertErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"no front matter", "# Title\n", "line 1: expected the front matter"},
		{"missing abstract", "---\ntitle: T\n--- middle\n\n# A\n", `missing "--- abstract" part`},
		{"unknown part", "---\ntitle: T\n--- abstract\n\nA.\n\n--- appendix\n", `line 7: unsupported part "appendix"`},
		{"unknown front matter key", "---\ntitle: T\ncolour: blue\n--- abstract\n\nA.\n\n--- middle\n", "field colour not found"},
		{"missing docname", "---\ntitle: T\n--- abstract\n\nA.\n\n--- middle\n", `missing "docname"`},
		{"content before heading", string(minimal("Text.\n")), "line 15: content before the first heading"},
		{"heading level jump", string(minimal("# A\n\n### B\n")), "line 17: heading level jumps from 1 to 3"},
		{"dangling xref", string(minimal("# A\n\nSee {{nowhere}}.\n")), "line 17: {{nowhere}} refers to nothing"},
		{"bad xref", string(minimal("# A\n\nSee {{RFC8555, page 3}}.\n")), "line 17: unsupported cross-reference"},
		{"unfetchable reference", string(minimal("# A\n\nSee {{!BCP14}}.\n")), `cannot fetch reference "BCP14"`},
		{"reference not cached", string(minimal("# A\n\nSee {{!RFC9999}}.\n")), "reference RFC9999 is not in"},
		{"blockquote", string(minimal("# A\n\n> quoted\n")), "line 17: unsupported Markdown construct (Blockquote)"},
		{"raw html", string(minimal("# A\n\nSome <b>bold</b> text.\n")), "line 17: unsupported Markdown construct (RawHTML)"},
		{"unknown directive", string(minimal("# A\n\n{::include other.md}\n")), "line 17: unsupported kramdown directive"},
		{"unknown section attribute", string(minimal("# A\n{:colour=\"blue\"}\n")), `line 16: unsupported section attribute "colour"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Convert([]byte(tt.src), testOptions())
			if err == nil {
				t.Fatalf("no error, want one containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestParseXref(t *testing.T) {
	tests := []struct {
		raw  string
		want xref
	}{
		{"RFC8555", xref{target: "RFC8555"}},
		{"!RFC8555", xref{target: "RFC8555", cite: '!'}},
		{"?I-D.ietf-acme-ari", xref{target: "I-D.ietf-acme-ari", cite: '?'}},
		{"FIPS180-4", xref{target: "FIPS180-4"}},
		{"!RFC8555, Section 7.3", xref{target: "RFC8555", cite: '!', section: "7.3", format: "comma"}},
		{"!RFC9846, Appendix C.1", xref{target: "RFC9846", cite: '!', section: "C.1", format: "comma"}},
		{"Section 8.1 of !RFC8555", xref{target: "RFC8555", cite: '!', section: "8.1", format: "of"}},
		{"RFC8555 (Section 8)", xref{target: "RFC8555", section: "8", format: "parens"}},
	}
	for _, tt := range tests {
		got, err := parseXref(tt.raw)
		if err != nil {
			t.Errorf("parseXref(%q): %v", tt.raw, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseXref(%q) = %+v, want %+v", tt.raw, got, tt.want)
		}
	}
	for _, raw := range []string{"", "RFC8555, page 3", "two words", "RFC8555, Section"} {
		if _, err := parseXref(raw); err == nil {
			t.Errorf("parseXref(%q): no error", raw)
		}
	}
}

func TestAutoAnchor(t *testing.T) {
	tests := map[string]string{
		"Introduction":                "introduction",
		"DNS-ACCOUNT-01 Challenge":    "dns-account-01-challenge",
		"Conventions and Definitions": "conventions-and-definitions",
		"Requirements & Lists":        "requirements-lists",
		"3.1 What's New?":             "whats-new",
		"123":                         "section",
	}
	for title, want := range tests {
		if got := autoAnchor(title); got != want {
			t.Errorf("autoAnchor(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestRefLocation(t *testing.T) {
	tests := []struct {
		key, file, url string
	}{
		{"RFC8555", "reference.RFC.8555.xml", "https://bib.ietf.org/public/rfc/bibxml/reference.RFC.8555.xml"},
		{"RFC791", "reference.RFC.0791.xml", "https://bib.ietf.org/public/rfc/bibxml/reference.RFC.0791.xml"},
		{"I-D.ietf-acme-ari", "reference.I-D.ietf-acme-ari.xml", "https://datatracker.ietf.org/doc/bibxml3/draft-ietf-acme-ari.xml"},
		{"I-D.draft-ietf-acme-ari", "reference.I-D.ietf-acme-ari.xml", "https://datatracker.ietf.org/doc/bibxml3/draft-ietf-acme-ari.xml"},
	}
	for _, tt := range tests {
		file, url, err := refLocation(tt.key)
		if err != nil {
			t.Errorf("refLocation(%q): %v", tt.key, err)
			continue
		}
		if file != tt.file || url != tt.url {
			t.Errorf("refLocation(%q) = %q, %q; want %q, %q", tt.key, file, url, tt.file, tt.url)
		}
	}
	for _, key := range []string{"BCP14", "FIPS180-4", "RFC", "I-D."} {
		if _, _, err := refLocation(key); err == nil {
			t.Errorf("refLocation(%q): no error", key)
		}
	}
}

func TestNormalizeReference(t *testing.T) {
	in := `<?xml version="1.0" encoding="UTF-8"?>
<reference anchor="I-D.ietf-x" target="https://example.com/x">
  <front><title>X</title></front>
</reference>
`
	got, err := normalizeReference("I-D.draft-ietf-x", []byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := `<reference anchor="I-D.draft-ietf-x">
  <front><title>X</title></front>
</reference>`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}

	for _, bad := range []string{"<html><body>Not found</body></html>", `<reference anchor="x"><front>`, ""} {
		if _, err := normalizeReference("X", []byte(bad)); err == nil {
			t.Errorf("normalizeReference(%q): no error", bad)
		}
	}
}

func TestParseDate(t *testing.T) {
	tests := map[string]xmlDate{
		"2015":       {Year: "2015"},
		"2015-08":    {Year: "2015", Month: "August"},
		"2026-10-05": {Year: "2026", Month: "October", Day: "5"},
	}
	for in, want := range tests {
		got, err := parseDate(in)
		if err != nil {
			t.Errorf("parseDate(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseDate(%q) = %+v, want %+v", in, got, want)
		}
	}
	for _, in := range []string{"", "August 2015", "2015-13", "15-08", "2015-08-01-02"} {
		if _, err := parseDate(in); err == nil {
			t.Errorf("parseDate(%q): no error", in)
		}
	}
}
