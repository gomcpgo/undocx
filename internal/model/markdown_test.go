package model

import "testing"

func text(s string) Inline { return Inline{Text: s} }

func TestEscape(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain words", "plain words"},
		{"snake_case and file_name.go", "snake_case and file_name.go"},
		{"_emphasis_ and trailing_", `\_emphasis\_ and trailing\_`},
		{"2 * 3 * 4", `2 \* 3 \* 4`},
		{"a `tick` and a \\ slash", "a \\`tick\\` and a \\\\ slash"},
		{"[note] and [link](x) and [^1]", `[note] and [link\](x) and \[^1]`},
		{"a < b, x<y, <div> </p> <!-- c -->", `a < b, x\<y, \<div> \</p> \<!-- c -->`},
		{"AT&T, R&D; &amp; &#38;", `AT&T, R\&D; \&amp; \&#38;`},
		{"~~struck~~ and ~approx", `\~\~struck\~\~ and ~approx`},
		{"# not a heading", `\# not a heading`},
		{"#hashtag and C# code", "#hashtag and C# code"},
		{"> not a quote", `\> not a quote`},
		{"- not a list", `\- not a list`},
		{"-5 degrees", "-5 degrees"},
		{"+ plus", `\+ plus`},
		{"1. not a list", `1\. not a list`},
		{"12) not a list", `12\) not a list`},
		{"1.5 metres and 3.2.1", "1.5 metres and 3.2.1"},
		{"2026. A year", `2026\. A year`},
		{"---", `\---`},
		{"===", `\===`},
		{"a - b = c", "a - b = c"},
	}
	for _, tt := range tests {
		if got := (Block{Inlines: []Inline{text(tt.in)}}).Markdown(); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.in, got, tt.want)
		}
	}
	// Only the start of a line gets the line-start rules.
	b := Block{Inlines: []Inline{text("See "), text("# 5"), {Kind: Break}, text("# again")}}
	b.Inlines[1].Bold = true
	if got, want := b.Markdown(), "See **# 5**\\\n\\# again"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInlineMarks(t *testing.T) {
	b := func(s string) Inline { return Inline{Text: s, Bold: true} }
	i := func(s string) Inline { return Inline{Text: s, Italic: true} }
	bi := func(s string) Inline { return Inline{Text: s, Bold: true, Italic: true} }
	link := func(in Inline, url string) Inline { in.Link = url; return in }
	tests := []struct {
		name string
		in   []Inline
		want string
	}{
		{"space at the end of bold moves out", []Inline{b("bold "), text("text")}, "**bold** text"},
		{"space at the start of bold moves out", []Inline{text("some"), b(" bold")}, "some **bold**"},
		{"bold then bold italic", []Inline{b("a "), bi("b"), b(" c")}, "**a *b* c**"},
		{"italic then bold italic", []Inline{i("a"), bi("b")}, "*a**b***"},
		{"space between two bold runs stays inside", []Inline{b("a"), text(" "), b("b")}, "**a b**"},
		{"marks inside a word", []Inline{text("un"), b("believ"), text("able")}, "un**believ**able"},
		{"code is not escaped", []Inline{{Text: "a*b_c", Code: true}}, "`a*b_c`"},
		{"code with a backtick", []Inline{{Text: "a`b", Code: true}}, "``a`b``"},
		{"bold code", []Inline{{Text: "x", Code: true, Bold: true}}, "**`x`**"},
		{"superscript and subscript", []Inline{text("x"), {Text: "2", Superscript: true}, text(" H"), {Text: "2", Subscript: true}, text("O")}, "x<sup>2</sup> H<sub>2</sub>O"},
		{"link", []Inline{text("see "), link(text("the site"), "https://example.com/a b(c)"), text(".")}, "see [the site](https://example.com/a%20b%28c%29)."},
		{"link over two formats", []Inline{link(text("plain "), "https://e.com"), link(b("bold"), "https://e.com")}, "[plain **bold**](https://e.com)"},
		{"two links in a row", []Inline{link(text("a"), "https://a.com"), text(" "), link(text("b"), "https://b.com")}, "[a](https://a.com) [b](https://b.com)"},
		{"link text that is only space", []Inline{text("a"), link(text(" "), "https://a.com"), text("b")}, "a b"},
		{"line break", []Inline{text("one "), {Kind: Break}, text(" two")}, "one\\\ntwo"},
		{"bold over a line break", []Inline{b("one"), {Kind: Break}, b("two")}, "**one**\\\n**two**"},
	}
	for _, tt := range tests {
		if got := (Block{Inlines: tt.in}).Markdown(); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}

	h := Block{Kind: Heading, Level: 2, Inlines: []Inline{b("Bold "), i("title"), {Kind: Break}, text("C#")}}
	if got, want := h.Markdown(), `## Bold *title* C\#`; got != want {
		t.Errorf("heading: got %q, want %q", got, want)
	}
	if got := (Block{Kind: Heading, Level: 9, Inlines: []Inline{text("Deep")}}).Markdown(); got != "###### Deep" {
		t.Errorf("level 9: got %q", got)
	}
	if got := (Block{Kind: Title, Inlines: []Inline{text("The Title")}}).Markdown(); got != "# The Title" {
		t.Errorf("title: got %q", got)
	}
	if got := (Block{Inlines: []Inline{b("one"), {Kind: Break}, text("two")}}).Text(); got != "one\ntwo" {
		t.Errorf("text: got %q", got)
	}
}

func TestAppend(t *testing.T) {
	var list []Inline
	list = Append(list, Inline{Text: "bo", Bold: true})
	list = Append(list, Inline{Text: "ld", Bold: true})
	list = Append(list, Inline{Text: ""})
	list = Append(list, Inline{Text: " plain"})
	list = Append(list, Inline{Kind: Break})
	list = Append(list, Inline{Kind: Break})
	if len(list) != 4 || list[0].Text != "bold" || list[1].Text != " plain" {
		t.Errorf("%+v", list)
	}
	if !Empty([]Inline{{Text: "  "}, {Kind: Break}}) || Empty(list) {
		t.Errorf("Empty is wrong")
	}
}
