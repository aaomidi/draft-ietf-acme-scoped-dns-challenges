package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// compatHead is the front matter that a "body" case in kramdown-compat.txt
// is appended to.
const compatHead = `---
title: "Matrix"
abbrev: "MATRIX"
category: std
submissiontype: IETF
docname: draft-example-matrix-00
v: 3
ipr: trust200902
date: 2026-10-25
author:
 -
    fullname: Alice Example
    organization: Example Org
    email: alice@example.com
--- abstract

Abstract.

--- middle

# Introduction

`

type compatCase struct {
	name, source string
	// want maps "error" or the name of an XML element to what is expected.
	want map[string]string
}

func readCompatCases(t *testing.T) []compatCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "kramdown-compat.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []compatCase
	var cur *compatCase
	var kind, section string
	var buf strings.Builder
	flush := func() {
		if cur == nil {
			return
		}
		text := buf.String()
		buf.Reset()
		if section != "source" {
			cur.want[section] = strings.TrimSpace(text)
			return
		}
		switch kind {
		case "body":
			cur.source = compatHead + text
		case "full":
			cur.source = text
		case "quoted":
			if err := json.Unmarshal([]byte(text), &cur.source); err != nil {
				t.Fatalf("case %s: %v", cur.name, err)
			}
		default:
			t.Fatalf("case %s: unknown kind %q", cur.name, kind)
		}
	}
	for _, line := range strings.SplitAfter(string(data), "\n") {
		fields := strings.Fields(line)
		switch {
		case cur == nil && !strings.HasPrefix(line, "%% case "):
			// The comment at the top of the file.
		case strings.HasPrefix(line, "%% case ") && len(fields) == 4:
			flush()
			cases = append(cases, compatCase{name: fields[2], want: map[string]string{}})
			cur, kind, section = &cases[len(cases)-1], fields[3], "source"
		case strings.HasPrefix(line, "%% want ") && len(fields) == 3:
			flush()
			section = fields[2]
		default:
			buf.WriteString(line)
		}
	}
	flush()
	return cases
}

var fetchedReference = regexp.MustCompile(`(?s)<reference anchor="((?:RFC|I-D)[^"]*)">.*?</reference>`)

// xmlPart cuts the named element out of a converted document. References
// that were fetched are reduced to their anchor, as they are in the file.
func xmlPart(doc, name string) string {
	if name == "rfc" {
		return regexp.MustCompile(`<rfc [^>]*>`).FindString(doc)
	}
	part := regexp.MustCompile(`(?s)<` + name + `>.*?</` + name + `>`).FindString(doc)
	return fetchedReference.ReplaceAllString(part, `<reference anchor="$1"/>`)
}

// TestKramdownCompat runs the cases in testdata/kramdown-compat.txt, which
// record where mkd2xml was checked to agree with kramdown-rfc and which
// input it refuses.
func TestKramdownCompat(t *testing.T) {
	cases := readCompatCases(t)
	if len(cases) < 150 {
		t.Fatalf("only %d cases read; the file was not parsed as expected", len(cases))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.want) == 0 {
				t.Fatal("case has no expectation")
			}
			// No date is passed: every case sets its own.
			got, err := Convert([]byte(c.source), Options{Refs: testOptions().Refs})
			if wantErr, ok := c.want["error"]; ok {
				if err == nil {
					t.Fatalf("no error, want one containing %q", wantErr)
				}
				if !strings.Contains(err.Error(), wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for part, want := range c.want {
				if have := strings.TrimSpace(xmlPart(string(got), part)); have != want {
					t.Errorf("<%s> differs\n got:\n%s\nwant:\n%s", part, have, want)
				}
			}
		})
	}
}

func TestResolveText(t *testing.T) {
	tests := map[string]string{
		`plain`:                    `plain`,
		`a \* b \_ c`:              `a * b _ c`,
		`back\\slash`:              `back\slash`,
		`\~ stays, \| goes`:        `\~ stays, | goes`,
		`A&nbsp;B`:                 "A B",
		`&mdash; &#x2014; &#8212;`: "— — —",
		`AT&T &bogus; &amp;amp;`:   `AT&T &bogus; &amp;`,
		`1 &lt; 2`:                 `1 < 2`,
	}
	for in, want := range tests {
		if got := resolveText(in); got != want {
			t.Errorf("resolveText(%q) = %q, want %q", in, got, want)
		}
	}
}
