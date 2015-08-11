// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ht

import (
	"bytes"
	"encoding/xml"
	"fmt"
	htmltemplate "html/template"
	"io"
	"io/ioutil"
	"os"
	"path"
	"text/template"
	"unicode/utf8"
)

// ----------------------------------------------------------------------------
// Status

// Status describes the status of a Test or a Check.
type Status int

const (
	NotRun  Status = iota // Not jet executed
	Skipped               // Omitted deliberately
	Pass                  // That's what we want
	Fail                  // One ore more checks failed
	Error                 // Request or body reading failed (not for checks).
	Bogus                 // Bogus test or check (malformd URL, bad regexp, etc.)
)

func (s Status) String() string {
	return []string{"NotRun", "Skipped", "Pass", "Fail", "Error", "Bogus"}[int(s)]
}

func (s Status) MarshalText() ([]byte, error) {
	if s < 0 || s > Bogus {
		return []byte(""), fmt.Errorf("no such status %d", s)
	}
	return []byte(s.String()), nil
}

// Stats counts the test results of sr.
func (s *Suite) Stats() (notRun int, skipped int, passed int, failed int, errored int, bogus int) {
	for _, tr := range s.AllTests() {
		switch tr.Status {
		case NotRun:
			notRun++
		case Skipped:
			skipped++
		case Pass:
			passed++
		case Fail:
			failed++
		case Error:
			errored++
		case Bogus:
			bogus++
		default:
			panic(fmt.Sprintf("No such Status %d in suite %q test %q",
				tr.Status, s.Name, tr.Name))
		}
	}
	return
}

// ----------------------------------------------------------------------------
// CheckResult

// CheckResult captures the outcom of a single check inside a test.
type CheckResult struct {
	Name     string   // Name of the check as registered.
	JSON     string   // JSON serialization of check.
	Status   Status   // Outcome of check. All status but Error
	Duration Duration // How long the check took.
	Error    error    // For a Status of Bogus or Fail.
}

// ----------------------------------------------------------------------------
// Templates to output

var defaultCheckTmpl = `{{define "CHECK"}}{{printf "%-7s %-15s %s" .Status .Name .JSON}}` +
	`{{if eq .Status 3 5}} {{.Error.Error}}{{end}}{{end}}`

var htmlCheckTmpl = `{{define "CHECK"}}
<div class="toggle{{if gt .Status 2}}Visible{{end}}2">
  <div class="collapsed2">
    <h3 class="toggleButton2">Check:
      <span class="{{ToUpper .Status.String}}">{{ToUpper .Status.String}}</span>
      <code>{{.Name}}</code> ▹
    </h3>
  </div>
  <div class="expanded2">
    <h3 class="toggleButton2">Check: 
      <span class="{{ToUpper .Status.String}}">{{ToUpper .Status.String}}</span>
      <code>{{.Name}}</code> ▾
    </h3>
    <div class="checkDetails">
      <div>Checking took {{.Duration}}</div>
      <div><code>{{.JSON}}</code></div>
      {{if eq .Status 3 5}}<pre class="description">{{.Error.Error}}</pre>{{end}}
    </div>
  </div>
</div>
{{end}}
`

var defaultTestTmpl = `{{define "TEST"}}{{ToUpper .Status.String}}: {{.Name}}{{if gt .Tries 1}}
  {{printf "(after %d tries)" .Tries}}{{end}}
  Started: {{.Started}}   Duration: {{.FullDuration}}   Request: {{.Duration}}{{if .Error}}
  Error: {{.Error}}{{end}}
{{if eq .Status 2 3 4 5}}  {{if .CheckResults}}Checks:
{{range $i, $c := .CheckResults}}{{printf "    %2d. " $i}}{{template "CHECK" .}}
{{end}}{{end}}{{end}}{{end}}`

var htmlTestTmpl = `{{define "TEST"}}
<div class="toggle{{if gt .Status 2}}Visible{{end}}">
  <div class="collapsed">
    <h2 class="toggleButton">{{.SeqNo}}:
      <span class="{{ToUpper .Status.String}}">{{ToUpper .Status.String}}</span> 
      "<code>{{.Name}}</code>"
      ({{.FullDuration}}) ▹
    </h2>
  </div>
  <div class="expanded">
    <h2 class="toggleButton">{{.SeqNo}}: 
      <span class="{{ToUpper .Status.String}}">{{ToUpper .Status.String}}</span> 
      "<code>{{.Name}}</code>"
      ({{.FullDuration}}) ▾
    </h2>
    <div class="testDetails">
      <div id="summary">
        <pre class="description">{{.Description}}</pre>
	Started: {{.Started}}<br/>
	Full Duration: {{.FullDuration}} <br/>
        Number of tries: {{.Tries}} <br/>
        Request Duration: {{.Duration}}
        {{if .Error}}</br>Error: {{.Error}}{{end}}
      </div>
      {{if eq .Status 2 3 4 5}}{{if .CheckResults}}
        <div class="checks">
          {{range $i, $c := .CheckResults}}{{template "CHECK" .}}{{end}}
        </div>
      {{end}}{{end}}
      {{if .Request.Request}}{{template "REQUEST" .}}{{end}}
      {{if .Response.Response}}{{template "RESPONSE" .}}{{end}}
    </div>
  </div>
</div>
{{end}}`

var htmlHeaderTmpl = `{{define "HEADER"}}
<div class="httpheader">
  {{range $h, $v := .}}
    {{range $v}}
      <code><strong>{{printf "%25s: " $h}}</strong> {{.}}</code></br>
    {{end}}
  {{end}}
</div>
{{end}}`

var htmlResponseTmpl = `{{define "RESPONSE"}}
<div class="toggle2">
  <div class="expanded2">
    <h3 class="toggleButton2">HTTP Response ▾</h3>
    <div class="responseDetails">
      {{if .Response.Response}}
        {{template "HEADER" .Response.Response.Header}}
      {{end}}
      {{if .Response.BodyErr}}Error reading body: {{.Response.BodyErr.Error}}
      {{else}} 
        <a href="{{.SeqNo}}.ResponseBody" target="_blank">Response Body</a>
      {{end}}
    </div>
  </div>
  <div class="collapsed2">
    <h3 class="toggleButton2">HTTP Response ▹</h3>
  </div>
</div>
{{end}}
`

var htmlRequestTmpl = `{{define "REQUEST"}}
<div class="toggle2">
  <div class="expanded2">
    <h3 class="toggleButton2">HTTP Request ▾</h3>
    <div class="requestDetails">
      <code><strong>{{.Request.Request.Method}}</strong> {{.Request.Request.URL.String}}</code>
      {{template "HEADER" .Request.Request.Header}}
<pre>{{.Request.SentBody}}</pre>
    </div>
  </div>
  <div class="collapsed2">
    <h3 class="toggleButton2">HTTP Request ▹</h3>
  </div>
</div>
{{end}}
`

var defaultSuiteTmpl = `{{Box (printf "%s: %s" (ToUpper .Status.String) .Name) ""}}{{if .Error}}
Error: {{.Error}}{{end}}
Started: {{.Started}}   Duration: {{.Duration}}

{{range .Setup}}{{template "TEST" .}}
{{end}}
{{range .Tests}}{{template "TEST" .}}
{{end}}
{{range .Teardown}}{{template "TEST" .}}
{{end}}
`

var htmlStyleTmpl = `{{define "STYLE"}}
<style>
.toggleButton { cursor: pointer; }
.toggleButton2 { cursor: pointer; }

.toggle .collapsed { display: block; }
.toggle .expanded { display: none; }
.toggleVisible .collapsed { display: none; }
.toggleVisible .expanded { display: block; }

.toggle2 .collapsed2 { display: block; }
.toggle2 .expanded2 { display: none; }
.toggleVisible2 .collapsed2 { display: none; }
.toggleVisible2 .expanded2 { display: block; }

h2 { 
  margin-top: 0.5em;
  margin-bottom: 0.2em;
}

h3 { 
  font-size: 1em;
  margin-top: 0.5em;
  margin-bottom: 0em;
}
.testDetails { margin-left: 1em; }
.checkDetails { margin-left: 2em; }
.requestDetails { margin-left: 2em; }
.responseDetails { margin-left: 2em; }

.PASS { color: green; }
.FAIL { color: red; }
.ERROR { color: magenta; }
.NOTRUN { color: grey; }

pre.description { font-family: serif; margin: 0px; }
</style>
{{end}}`

var htmlJavascriptTmpl = `{{define "JAVASCRIPT"}}
<script type="text/javascript" src="https://ajax.googleapis.com/ajax/libs/jquery/1.8.2/jquery.min.js"></script>
<script type="text/javascript">
(function() {
'use strict';

function bindToggle(el) {
  $('.toggleButton', el).click(function() {
    if ($(el).is('.toggle')) {
      $(el).addClass('toggleVisible').removeClass('toggle');
    } else {
      $(el).addClass('toggle').removeClass('toggleVisible');
    }
  });
}
function bindToggles(selector) {
  $(selector).each(function(i, el) {
    bindToggle(el);
  });
}

function bindToggle2(el) {
  console.log("bindToggle2 for " + el);
  $('.toggleButton2', el).click(function() {
    if ($(el).is('.toggle2')) {
      $(el).addClass('toggleVisible2').removeClass('toggle2');
    } else {
      $(el).addClass('toggle2').removeClass('toggleVisible2');
    }
  });
}

function bindToggles2(selector) {
console.log("bindToggles2("+selector+")");
  $(selector).each(function(i, el) {
    bindToggle2(el);
  });
}

$(document).ready(function() {
console.log("bindingstuff");
  bindToggles(".toggle");
  bindToggles(".toggleVisible");
  bindToggles2(".toggle2");
  bindToggles2(".toggleVisible2");
});

})();
</script>
{{end}}`

var htmlSuiteTmpl = `<!DOCTYPE html>
<html>
<head>
  <meta http-equiv="content-type" content="text/html; charset=UTF-8" />
  {{template "STYLE"}}
  <title>Suite {{.Name}}</title>
</head>
</body>
<h1>Results of Suite <code>{{.Name}}</code></h1>

{{.Description}}

<div id="summary">
  Status: <span class="{{ToUpper .Status.String}}">{{ToUpper .Status.String}}</span> <br/>
  Started: {{.Started}} <br/>
  Full Duration: {{.Duration}}
</div>

{{range .AllTests}}{{template "TEST" .}}{{end}}

{{template "JAVASCRIPT"}}
</body>
</html>
`

var (
	TestTmpl      *template.Template
	SuiteTmpl     *template.Template
	HtmlSuiteTmpl *htmltemplate.Template
)

func init() {
	fm := make(template.FuncMap)
	fm["Underline"] = Underline
	fm["Box"] = Box
	fm["ToUpper"] = ToUpper

	TestTmpl = template.New("TEST")
	TestTmpl.Funcs(fm)
	TestTmpl = template.Must(TestTmpl.Parse(defaultTestTmpl))
	TestTmpl = template.Must(TestTmpl.Parse(defaultCheckTmpl))

	SuiteTmpl = template.New("SUITE")
	SuiteTmpl.Funcs(fm)
	SuiteTmpl = template.Must(SuiteTmpl.Parse(defaultSuiteTmpl))
	SuiteTmpl = template.Must(SuiteTmpl.Parse(defaultTestTmpl))
	SuiteTmpl = template.Must(SuiteTmpl.Parse(defaultCheckTmpl))

	HtmlSuiteTmpl = htmltemplate.New("SUITE")
	HtmlSuiteTmpl.Funcs(htmltemplate.FuncMap{"ToUpper": ToUpper})
	HtmlSuiteTmpl = htmltemplate.Must(HtmlSuiteTmpl.Parse(htmlSuiteTmpl))
	HtmlSuiteTmpl = htmltemplate.Must(HtmlSuiteTmpl.Parse(htmlTestTmpl))
	HtmlSuiteTmpl = htmltemplate.Must(HtmlSuiteTmpl.Parse(htmlCheckTmpl))
	HtmlSuiteTmpl = htmltemplate.Must(HtmlSuiteTmpl.Parse(htmlResponseTmpl))
	HtmlSuiteTmpl = htmltemplate.Must(HtmlSuiteTmpl.Parse(htmlRequestTmpl))
	HtmlSuiteTmpl = htmltemplate.Must(HtmlSuiteTmpl.Parse(htmlHeaderTmpl))
	HtmlSuiteTmpl = htmltemplate.Must(HtmlSuiteTmpl.Parse(htmlStyleTmpl))
	HtmlSuiteTmpl = htmltemplate.Must(HtmlSuiteTmpl.Parse(htmlJavascriptTmpl))
}

func (t Test) PrintReport(w io.Writer) error {
	return TestTmpl.Execute(w, t)
}

func (r Suite) PrintReport(w io.Writer) error {
	return SuiteTmpl.Execute(w, r)
}

func (s Suite) HTMLReport(dir string) error {
	report, err := os.Create(path.Join(dir, "Report.html"))
	if err != nil {
		return err
	}
	err = HtmlSuiteTmpl.Execute(report, s)
	if err != nil {
		return err
	}
	for _, tr := range s.AllTests() {
		body := tr.Response.BodyBytes
		err = ioutil.WriteFile(path.Join(dir, tr.SeqNo+".ResponseBody"), body, 0666)
		if err != nil {
			return err
		}
	}
	return nil
}

// JUnit style output.
// ----------------------------------------------------------------------------

// JUnit4XML generates a JUnit 4 compatible XML result with each Check
// reported as an individual testcase.
// NotRun checks are reported as Skipped and Bogus checks are counted as
// Errored tests.
func (s *Suite) JUnit4XML(props map[string]string) (string, error) {
	// Local types used for XML encoding
	type SysOut struct {
		XMLName xml.Name `xml:"system-out"`
		Data    string   `xml:",innerxml"`
	}
	type ErrorMsg struct {
		Message string `xml:"message,attr"`
		Typ     string `xml:"type,attr"`
	}
	type Testcase struct {
		XMLName   xml.Name  `xml:"testcase"`
		Name      string    `xml:"name,attr"`
		Classname string    `xml:"classname,attr"`
		Time      float64   `xml:"time,attr"`
		Skipped   *struct{} `xml:"Skipped,omitempty"`
		Error     *ErrorMsg `xml:"error,omitempty"`
		Failure   *ErrorMsg `xml:"failure,omitempty"`
		SystemOut string    `xml:"system-out,omitempty"`
	}
	type Property struct {
		XMLName xml.Name `xml:"property"`
		Name    string   `xml:"name,attr"`
		Value   string   `xml:"value,attr"`
	}
	type Testsuite struct {
		XMLName xml.Name `xml:"testsuite"`
		Tests   int      `xml:"tests,attr"`
		// Disabled   int        `xml:"disabled,attr"`
		Errors     int        `xml:"errors,attr"`
		Failures   int        `xml:"failures,attr"`
		Skipped    int        `xml:"skipped,attr"`
		Time       float64    `xml:"time,attr"`
		Timestamp  string     `xml:"timestamp,attr"`
		Properties []Property `xml:"properties"`
		Testcase   []Testcase
		SystemOut  SysOut
	}

	// Unwind all Checks to their own testcase.
	skipped, passed, failed, errored := 0, 0, 0, 0
	testcases := []Testcase{}
	for _, test := range s.Tests {
		for _, cr := range test.CheckResults {
			tc := Testcase{
				Name:      cr.Name,
				Classname: test.Name,
				Time:      float64(cr.Duration) / 1e9,
				SystemOut: cr.JSON,
			}

			switch cr.Status {
			case NotRun, Skipped:
				tc.Skipped = &struct{}{}
				skipped++
			case Pass:
				passed++
			case Fail:
				tc.Failure = &ErrorMsg{
					Message: test.Error.Error(),
					Typ:     fmt.Sprintf("%T", test.Error),
				}
				failed++
			case Error, Bogus:
				tc.Error = &ErrorMsg{
					Message: test.Error.Error(),
					Typ:     fmt.Sprintf("%T", test.Error),
				}
				errored++
			default:
				panic(cr.Status)
			}

			testcases = append(testcases, tc)
		}
	}

	// Generate a standard text report which becomes the standard-out of
	// the generated JUnit report.
	buf := &bytes.Buffer{}
	var sysout string
	err := s.PrintReport(buf)
	if err != nil {
		sysout = err.Error()
	} else {
		sysout = xmlEscapeChars(buf.Bytes())
	}

	// Populate the Testsuite type for marshalling.
	ts := Testsuite{
		Tests:     skipped + passed + failed + errored,
		Errors:    errored,
		Failures:  failed,
		Skipped:   skipped,
		Time:      float64(s.Duration) / 1e9,
		Timestamp: s.Started.Format("2006-01-02T15:04:05"),
		Testcase:  testcases,
		SystemOut: SysOut{Data: "\n" + sysout},
	}
	for k, v := range props {
		ts.Properties = append(ts.Properties, Property{Name: k, Value: v})
	}

	data, err := xml.MarshalIndent(ts, "", "  ")
	if err != nil {
		return string(data), err
	}
	return xml.Header + string(data) + "\n", nil
}

// xmlEscapeChars escapes the reserved characters. TODO: \r ?
func xmlEscapeChars(s []byte) string {
	buf := &bytes.Buffer{}
	for i := 0; i < len(s); {
		rune, width := utf8.DecodeRune(s[i:])
		i += width
		switch rune {
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '&':
			buf.WriteString("&amp;")
		case '"':
			buf.WriteString("&quot;")
		case '\'':
			buf.WriteString("&apos;")
		case '\t':
			buf.WriteString("&#x9;")
		default:
			// TODO: not every rune is allowed in XML
			buf.WriteRune(rune)
		}
	}
	return buf.String()
}
