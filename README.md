# Automated Certificate Management Environment (ACME) Scoped DNS Challenges

This is the working area for the individual Internet-Draft, "Automated Certificate Management Environment (ACME) Scoped DNS Challenges".

* [Editor's Copy](https://aaomidi.github.io/draft-ietf-acme-scoped-dns-challenges/)
<!-- * [Datatracker Page](https://datatracker.ietf.org/doc/draft-todo-chariton-dns-account01) -->
<!-- * [Individual Draft](https://datatracker.ietf.org/doc/html/draft-todo-chariton-dns-account01) -->
<!-- * [Compare Editor's Copy to Individual Draft](https://daknob.github.io/draft-todo-chariton-dns-account-01/#go.draft-todo-chariton-dns-account.diff) -->


## Building

The draft is written in [kramdown-rfc](https://github.com/cabo/kramdown-rfc)
flavoured Markdown, in `draft-ietf-acme-dns-account-challenge.mkd`. It is
built with three small Go programs that live in this repository; the only
thing to install is [Go](https://go.dev/dl/).

```sh
mkdir -p docs
go run ./cmd/mkd2xml  -o docs/draft.xml  draft-ietf-acme-dns-account-challenge.mkd
go run ./cmd/xml2txt  -o docs/draft.txt  docs/draft.xml
go run ./cmd/xml2html -o docs/index.html docs/draft.xml
```

* `mkd2xml` turns the Markdown into RFCXML v3. `docs/draft.xml` is what is
  submitted to the [datatracker](https://datatracker.ietf.org/submit/).
* `xml2txt` and `xml2html` render that XML as the text and the HTML page,
  laid out as [xml2rfc](https://github.com/ietf-tools/xml2rfc) lays them out.

The tools support what this draft uses and stop with an error, and a line
number, on anything else. In particular `mkd2xml` refuses Markdown that
kramdown-rfc would read differently from it, and says how to rewrite it.

The first build fetches the cited RFCs and Internet-Drafts and keeps them in
`.refcache`; delete that directory to pick up a newer revision of a cited
draft.

Every pull request is built the same way, and a comment on it links to a
preview of the result.

## Contributing

See the
[guidelines for contributions](https://github.com/daknob/draft-todo-chariton-dns-account-01/blob/main/docs/Code-Of-Conduct.md).

Contributions can be made by creating pull requests.
The GitHub interface supports creating pull requests using the Edit (✏) button.

Once a change is approved and merged, GitHub actions will rebuild the editor's copy.
