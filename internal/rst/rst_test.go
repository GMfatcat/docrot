package rst

import (
	"reflect"
	"strings"
	"testing"
)

const sample = `.. _api:

==================
Developer Interface
==================

.. module:: requests

This part covers :class:` + "`" + `Response <Response>` + "`" + ` and :func:` + "`" + `~requests.get` + "`" + `; see :ref:` + "`" + `install` + "`" + `
and :doc:` + "`" + `user/quickstart` + "`" + ` and :doc:` + "`" + `/topics/intro` + "`" + ` and :ref:` + "`" + `genindex` + "`" + ` and :ref:` + "`" + `howto/upgrade:Resolving Warnings` + "`" + `. Config lives in ` + "``" + `docs/conf.py` + "``" + ` and :file:` + "`" + `setup.cfg` + "`" + `.
Use :option:` + "`" + `--verbose` + "`" + ` or :envvar:` + "`" + `REQUESTS_CA_BUNDLE` + "`" + `. Version 2.31.0 works. Path src/requests/api.py too.

Main Interface
--------------

See ` + "`" + `Cool URIs don't change` + "`" + `_ and Requests_ and ` + "`" + `text <https://example.com/x>` + "`" + `_ and https://example.com/bare.

.. _Cool URIs don't change: https://www.w3.org/Provider/Style/URI
.. _Requests: https://requests.readthedocs.io

Run it::

    $ python -m requests
    make docs

.. code-block:: python
   :linenos:

   import requests
   requests.get("https://x")

.. literalinclude:: ../code/example.py
.. image:: _static/logo.png

.. toctree::
   :maxdepth: 2

   user/install
   Advanced <user/advanced>
   https://external.example.com/page

.. autofunction:: request
.. autoclass:: requests.Session
   :inherited-members:

>>> import requests
>>> r = requests.get('https://api.github.com')

.. docrot:ignore
Ignored ` + "``" + `nope/x.py` + "``" + ` here.

.. this is a comment
   spanning lines

Main Interface
--------------

+-------+------+
| a     | b    |
+=======+======+
| 1     | 2    |
+-------+------+
| 3     | 4    |
+-------+------+
`

func TestParse(t *testing.T) {
	d := Parse("docs/api.rst", []byte(sample))

	var heads []string
	for _, h := range d.Headings {
		heads = append(heads, h.Text+"|"+h.Slug+"|"+itoa(h.Level))
	}
	wantHeads := []string{"Developer Interface|developer-interface|1", "Main Interface|main-interface|2", "Main Interface|main-interface-1|2"}
	if !reflect.DeepEqual(heads, wantHeads) {
		t.Errorf("headings = %v, want %v", heads, wantHeads)
	}
	if !reflect.DeepEqual(d.Labels, []string{"api"}) {
		t.Errorf("labels = %v", d.Labels)
	}

	var spans []string
	for _, s := range d.Spans {
		spans = append(spans, s.Text)
	}
	wantSpans := []string{"requests", "Response", "requests.get", "docs/conf.py", "setup.cfg", "--verbose", "REQUESTS_CA_BUNDLE", "request", "requests.Session", "nope/x.py"}
	if !reflect.DeepEqual(spans, wantSpans) {
		t.Errorf("spans = %v, want %v", spans, wantSpans)
	}

	var links []string
	for _, l := range d.Links {
		links = append(links, l.Target)
	}
	wantLinks := []string{
		"#install", "user/quickstart.rst", "/topics/intro.rst", "/howto/upgrade.rst#resolving-warnings",
		"https://www.w3.org/Provider/Style/URI", "https://requests.readthedocs.io", "https://example.com/x", "https://example.com/bare",
		"../code/example.py", "user/install.rst", "user/advanced.rst", "https://external.example.com/page",
	}
	if !reflect.DeepEqual(links, wantLinks) {
		t.Errorf("links = %v, want %v", links, wantLinks)
	}
	if len(d.Images) != 1 || d.Images[0].Target != "_static/logo.png" {
		t.Errorf("images = %+v", d.Images)
	}
	if len(d.RefDefs) != 2 {
		t.Errorf("refdefs = %+v", d.RefDefs)
	}

	var fences []string
	for _, f := range d.Fences {
		fences = append(fences, f.Lang+":"+strings.Join(f.Content, "|"))
	}
	wantFences := []string{
		":$ python -m requests|make docs",
		"python:import requests|requests.get(\"https://x\")",
		"pycon:>>> import requests|>>> r = requests.get('https://api.github.com')",
	}
	if !reflect.DeepEqual(fences, wantFences) {
		t.Errorf("fences = %v, want %v", fences, wantFences)
	}
	if len(d.Tables) != 1 || d.Tables[0].Rows != 2 || d.Tables[0].Cols != 2 {
		t.Errorf("tables = %+v", d.Tables)
	}

	var bare []string
	for _, b := range d.BarePaths {
		bare = append(bare, b.Text)
	}
	if !reflect.DeepEqual(bare, []string{"src/requests/api.py"}) {
		t.Errorf("bare paths = %v", bare)
	}
	// the docrot:ignore comment covers the next line
	ignoredLine := 0
	for _, s := range d.Spans {
		if s.Text == "nope/x.py" {
			ignoredLine = s.Line
		}
	}
	if ignoredLine == 0 || !d.Ignored(ignoredLine) {
		t.Errorf("docrot:ignore did not cover line %d", ignoredLine)
	}
	if d.SectionAt(d.Spans[0].Line) != "Developer Interface" {
		t.Errorf("section of first span = %q", d.SectionAt(d.Spans[0].Line))
	}
	nums := d.Numbers()
	if len(nums) == 0 {
		t.Error("no numbers extracted")
	}
}

func TestLooks(t *testing.T) {
	if !Looks([]byte("Title\n=====\n\nText.\n")) || !Looks([]byte("Intro\n\n.. code-block:: go\n\n   x\n")) {
		t.Error("RST not recognised")
	}
	if Looks([]byte("# Title\n\nSome **markdown** text.\n")) || Looks([]byte("docrot\n\n- item\n- item\n")) {
		t.Error("non-RST recognised as RST")
	}
}

func TestSlug(t *testing.T) {
	p := &parser{slugs: map[string]int{}}
	cases := map[string]string{"How Django processes a request": "how-django-processes-a-request", "1. Intro": "intro", "foo_bar (x)": "foo-bar-x", "核心目標": "核心目標"}
	for in, want := range cases {
		if got := p.slug(in); got != want {
			t.Errorf("slug(%q) = %q, want %q", in, got, want)
		}
	}
}
