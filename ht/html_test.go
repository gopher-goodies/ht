// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ht

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

var sampleHTML = `<!doctype html>
<html>
<link href="/css/base.css">
<head><title>CSS Selectors</title></head>
<body>
<h1 id="mt">FooBar</h1>
<p class="X">Hello <span class="X">World</span><p>
<p class="X" id="end">Thanks!</p>
<a href="#">Link1</a>
<a href="/foo/bar">Link2</a>
<a href="../waz#top">Link3</a>
<a href="http://www.google.com">Link4</a>
<img src="pic.jpg"><img src="http://www.google.com/logo.png">
<script src="/js/common.js"></script>
<script>blob="aaa"</script>
<div class="WS">
  <p class="em">Inter<em>word</em>emphasis</p>
  <p class="strong">
	Some
	<strong> important </strong>
	things.
  </p>
  <ul class="items"><li>Foo</li><li>Bar</li><li>Waz</li></ul>
  <ul class="fancy"><li>One</li><li>S<strong>econ</strong>d</li><li> Three </li></ul>
</div>
<p>Large 24&#034; Monitor</p>
<p>Small 12" Monitor</p>
</body>
</html>
`

var hcr = Response{
	BodyStr: sampleHTML}

var htmlTagTests = []TC{
	{hcr, &HTMLTag{Selector: "h1"}, nil},
	{hcr, &HTMLTag{Selector: "p.X", Count: 2}, nil},
	{hcr, &HTMLTag{Selector: "#mt", Count: 1}, nil},
	{hcr, &HTMLTag{Selector: "h2"}, ErrNotFound},
	{hcr, &HTMLTag{Selector: "h1", Count: 2}, someError},
	{hcr, &HTMLTag{Selector: "h1", Count: -1}, ErrFoundForbidden},
	{hcr, &HTMLTag{Selector: "p.z"}, ErrNotFound},
	{hcr, &HTMLTag{Selector: "#nil"}, ErrNotFound},
}

func TestHTMLTag(t *testing.T) {
	for i, tc := range htmlTagTests {
		runTest(t, i, tc)
	}
}

var htmlContainsTests = []TC{
	{hcr, &HTMLContains{Selector: "p.X",
		Text: []string{"Hello World", "Thanks!"}}, nil},
	{hcr, &HTMLContains{Selector: "#mt",
		Text: []string{"FooBar"}, Complete: true}, nil},
	{hcr, &HTMLContains{Selector: "span",
		Text: []string{"World"}}, nil},
	{hcr, &HTMLContains{Selector: "span",
		Text: []string{"World"}, Complete: true}, nil},
	{hcr, &HTMLContains{Selector: "p.X",
		Text: []string{"Hello World", "FooBar"}}, someError},
	{hcr, &HTMLContains{Selector: "p.X",
		Text: []string{"Hello World"}, Complete: true}, someError},
	{hcr, &HTMLContains{Selector: "p.X",
		Text: []string{"Hello World", "Thanks!", "ZZZ"}}, someError},
	{hcr, &HTMLContains{Selector: "div.WS p.em",
		Text: []string{"Interwordemphasis"}}, nil},
	{hcr, &HTMLContains{Selector: "div.WS p.strong",
		Text: []string{"Some important things."}}, nil},
	{hcr, &HTMLContains{Selector: "ul.items",
		Text: []string{"Foo Bar Waz"}}, nil},
	{hcr, &HTMLContains{Selector: "ul.fancy",
		Text: []string{"One Second Three"}}, nil},
	{hcr, &HTMLContains{Selector: "li",
		Text: []string{"Foo", "Bar", "Waz"}}, nil},
	{hcr, &HTMLContains{Selector: "li",
		Text: []string{"Waz", "Bar", "Foo"}}, nil},
	{hcr, &HTMLContains{Selector: "li", InOrder: true,
		Text: []string{"Waz", "Bar", "Foo"}}, someError},
	{hcr, &HTMLContains{Selector: "li", Complete: true,
		Text: []string{"One", "Waz", "Bar", "Foo", "Three", "Second"}}, nil},
	{hcr, &HTMLContains{Selector: "li", Complete: true, InOrder: true,
		Text: []string{"One", "Waz", "Bar", "Foo", "Three", "Second"}}, someError},
	{hcr, &HTMLContains{Selector: "li", Complete: true, InOrder: true,
		Text: []string{"Foo", "Bar", "Waz", "One", "Second", "Three"}}, nil},
	{hcr, &HTMLContains{Selector: "p",
		Text: []string{`Large 24" Monitor`}}, nil},
	{hcr, &HTMLContains{Selector: "p",
		Text: []string{`Small 12" Monitor`}}, nil},
	// Nice error messages
	{hcr, &HTMLContains{Selector: "p.X span.X",
		Text: []string{"Foo"}}, fmt.Errorf(`missing "Foo", have ["World"]`)},
	{hcr, &HTMLContains{Selector: "p.Y span.Y",
		Text: []string{"Foo"}}, errTagNotFound},
	{hcr, &HTMLContains{Selector: "li", InOrder: true,
		Text: []string{"Foo", "Bar", "Waz", "One", "missing", "Second"}},
		fmt.Errorf(`missing "missing", have ["Second" "Three"]`)},
	{hcr, &HTMLContains{Selector: "p.X",
		Text: []string{"missing"}},
		fmt.Errorf(`missing "missing", have ["Hello World" "Thanks!"]`)},
}

func TestHTMLContains(t *testing.T) {
	for i, tc := range htmlContainsTests {
		runTest(t, i, tc)
	}
}

func TestW3CValidatorHTML(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping W3C Validator based checks in short mode.")
	}

	body := `<!DOCTYPE html>
<html>
<head>
  <meta http-equiv="content-type" content="text/html; charset=UTF-8" />
  <title>This is okay</title>
</head>
<body>
  <h1>Here all good &amp; nice</h1>
</body>`

	rr := Response{BodyStr: body}
	check := W3CValidHTML{
		AllowedErrors: 0,
	}
	runTest(t, 0, TC{rr, check, nil})

	body2 := `<!DOCTYPE html>
<html>
<head>
  <meta http-equiv="content-type" content="text/html; charset=UTF-8" />
  <title>This is okay</title>
</head>
<body>
  <h1 title="K&K">Here some issues problems</h1>
  <button role="presentation">Button</button>
  <span><div>Strangly nested</div></span>
</body>`

	rr2 := Response{BodyStr: body2}
	check2 := W3CValidHTML{
		AllowedErrors: 1,
		IgnoredErrors: []Condition{
			{Prefix: "& did not start a character reference"},
		},
	}
	runTest(t, 1, TC{rr2, check2, someError})

	check3 := W3CValidHTML{
		AllowedErrors: 3,
	}
	runTest(t, 1, TC{rr2, check3, nil})

}

func htmlLinksHandler(w http.ResponseWriter, r *http.Request) {
	status := 200
	if strings.Index(r.URL.Path, "/404/") != -1 {
		status = 404
	} else if strings.Index(r.URL.Path, "/302/") != -1 {
		status = 302
	}
	linksHandlerCalls <- r.Host + r.URL.String()
	time.Sleep(2 * time.Microsecond)
	http.Error(w, "Link Handler", status)
}

var linksHandlerCalls chan string

func TestHTMLLinksExtraction(t *testing.T) {
	body := `<!doctype html>
<html>
<head>
  <title>CSS Selectors</title>
  <link rel="copyright" title="Copyright" href="/impressum.html#top" />
  <script type="text/javascript" src="/js/jquery.js"></script>
</head>
<body>
  <a href="/path/link4">Link4</a>
  <img src="/some/image.gif">
  <a href="/path/link4#nav">Link4</a>
  <a href="http://www.google.com">Google</a>
  <a href="rel/path">Page</a>
  <img src="http://www.amazon.com/logo.png" />
  <iframe src="http://i.frame"></iframe>
  <video src="/video/greet.mpg">
    <source src='/video/greet.ogv' type='video/ogg'>
  </video>
  <audio src="/audio/sound.wav"> </audio>
</body>
</html>`

	baseURL, err := url.Parse("http://www.example.org/foo/bar.html")
	if err != nil {
		t.Fatalf("Unexpected error: %#v", err)
	}

	test := &Test{
		Request: Request{
			Request: &http.Request{URL: baseURL},
		},
		Response: Response{BodyStr: body},
	}

	for i, tc := range []struct{ which, want string }{
		{"img", "http://www.example.org/some/image.gif http://www.amazon.com/logo.png"},
		{"link", "http://www.example.org/impressum.html"},
		{"a", "http://www.example.org/path/link4 http://www.google.com http://www.example.org/foo/rel/path"},
		{"script", "http://www.example.org/js/jquery.js"},
		{"iframe", "http://i.frame"},
		{"video", "http://www.example.org/video/greet.mpg"},
		{"audio", "http://www.example.org/audio/sound.wav"},
		{"source", "http://www.example.org/video/greet.ogv"},
	} {

		check := Links{Which: tc.which}
		err = check.Prepare()
		if err != nil {
			t.Fatalf("%d: unexpected error: %#v", i, err)
		}
		urls, err := check.collectURLs(test)
		if err != nil {
			t.Fatalf("%d: Unexpected error: %#v", i, err)
		}
		expectedURLs := strings.Split(tc.want, " ")
		for _, expected := range expectedURLs {
			if _, ok := urls[expected]; !ok {
				t.Errorf("%d: Missing expected URL %q", i, expected)
			}
		}
		if len(urls) > len(expectedURLs) {
			t.Errorf("%d: Extracted too many URLs: Want %d, got %v",
				i, len(expectedURLs), urls)
		}
	}
}

func TestHTMLLinkFiltering(t *testing.T) {
	body := `<!doctype html>
<html><body>
  <a href="/C/abc"></a>
  <a href="/C/123/not"></a>
  <a href="/C/xyz/skip"></a>
  <a href="/A/abc"></a>
  <a href="/A/123/not"></a>
  <a href="/A/xyz/skip"></a>
  <a href="/B/abc"></a>
  <a href="/B/123/not"></a>
  <a href="/B/xyz/skip"></a>
</body></html>`
	baseURL, err := url.Parse("http://www.example.org/")
	if err != nil {
		t.Fatalf("Unexpected error: %#v", err)
	}

	test := &Test{
		Request: Request{
			Request: &http.Request{URL: baseURL},
		},
		Response: Response{BodyStr: body},
	}

	check := Links{
		Which: "a",
		OnlyLinks: []Condition{
			{Contains: "/A/"},
			{Contains: "/C/"},
		},
		IgnoredLinks: []Condition{
			{Contains: "not"},
			{Contains: "skip"},
		},
	}
	err = check.Prepare()
	if err != nil {
		t.Fatalf("Unexpected error: %#v", err)
	}
	urls, err := check.collectURLs(test)
	if err != nil {
		t.Fatalf("Unexpected error: %#v", err)
	}
	if len(urls) != 2 {
		t.Errorf("Got %v", urls)
	}
	if _, ok := urls["http://www.example.org/A/abc"]; !ok {
		t.Errorf("Missing http://www.example.org/A/abc")
	}
	if _, ok := urls["http://www.example.org/C/abc"]; !ok {
		t.Errorf("Missing http://www.example.org/C/abc")
	}
}

func TestHTMLLinksNone(t *testing.T) {
	body := `<!doctype html>
<html><body>
  <a href="/C/abc"></a>
  <a href="/C/123/not"></a>
</body></html>`
	baseURL, err := url.Parse("http://www.example.org/")
	if err != nil {
		t.Fatalf("Unexpected error: %#v", err)
	}

	test := &Test{
		Request: Request{
			Request: &http.Request{URL: baseURL},
		},
		Response: Response{BodyStr: body},
	}

	check := Links{Which: "-none-"}
	err = check.Prepare()
	if err != nil {
		t.Fatalf("Unexpected error: %#v", err)
	}
	err = check.Execute(test)
	if err != nil {
		t.Fatalf("Unexpected error: %#v", err)
	}
}

func testHTMLLinks(t *testing.T, urls []string, max time.Duration) (called []string, err error) {
	ts1 := httptest.NewServer(http.HandlerFunc(htmlLinksHandler))
	defer ts1.Close()
	ts2 := httptest.NewServer(http.HandlerFunc(htmlLinksHandler))
	defer ts2.Close()

	body := fmt.Sprintf(`<!doctype html>
<html>
<head>
  <title>CSS Selectors</title>
  <link rel="copyright" title="Copyright" href="%s#top" />
  <script type="text/javascript" src="%s"></script>
</head>
<body>
  <a href="%s">Link4</a>
  <img src="%s">
  <a href="%s#nav">Link5</a>
  <a href="%s">LinkWAZ</a>
</body>
</html>`, urls[0], urls[1], ts1.URL+urls[2], ts1.URL+urls[3], ts1.URL+urls[4], ts2.URL+urls[5])

	baseURL, err := url.Parse(ts1.URL)
	if err != nil {
		t.Fatalf("Unexpected error: %#v", err)
	}

	test := &Test{
		Request:   Request{Request: &http.Request{URL: baseURL}},
		Response:  Response{BodyStr: body},
		Execution: Execution{Verbosity: 1},
	}

	check := Links{Which: "a img link script -none-", Concurrency: 2, MaxTime: max}
	err = check.Prepare()
	if err != nil {
		t.Fatalf("Unexpected error: %#v", err)
	}

	linksHandlerCalls = make(chan string, 10)
	err = check.Execute(test)
	close(linksHandlerCalls)

	for c := range linksHandlerCalls {
		called = append(called, c)
	}

	return called, err
}

func TestHTMLLinksOkay(t *testing.T) {
	urls := []string{
		"/impressum.html",
		"/js/jquery.js",
		"/foo",
		"/supertoll/bild.gif",
		"/foo",
		"/waz",
	}
	called, err := testHTMLLinks(t, urls, 20*time.Millisecond)
	if err != nil {
		t.Errorf("Unexpected error: %#v %s", err, err)
	}
	if len(called) != 5 {
		t.Errorf("Unexpected error: %v", called)
	}
}

func TestHTMLLinksTooSlow(t *testing.T) {
	// 20 URLs each at least 2musec at 2 concurrent request take at least 10 musec.
	urls := []string{
		"/impressum.html",
		"/js/jquery.js",
		"/foo",
		"/supertoll/bild.gif",
		"/foo",
		"/waz",
		"/foo/bar",
		"/waz/bar",
		"/foo/123",
		"/waz/123",
		"/foo/123/bar",
		"/waz/123/bar",
	}
	_, err := testHTMLLinks(t, urls, 10*time.Microsecond)
	if err == nil {
		t.Fatalf("Missing error")
	}
}

func TestHTMLLinksBroken(t *testing.T) {
	urls := []string{
		"/404/impressum.html",
		"/404/js/jquery.js",
		"/404/foo",
		"/404/supertoll/bild.gif",
		"/404/foo",
		"/404/waz",
	}
	called, err := testHTMLLinks(t, urls, 20*time.Millisecond)
	if err == nil {
		t.Fatalf("Missing error: %#v", err)
	}
	if len(called) != 5 {
		t.Errorf("Unexpected error: %v", called)
	}
}

var mixedContentBody = `<!doctype html>
<html><body>
  <img src="/absolute">
  <img src="./relative">
  <img src="http://%s/unsecure">
  <img src="https://%s/secure">
  <a href="http://%s/unsec-a"></a>
</body></html>`

func htmlDummyLinksHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Dummy Link Handler", http.StatusOK)
}

func TestLinksMixedContent(t *testing.T) {
	ts1 := httptest.NewServer(http.HandlerFunc(htmlDummyLinksHandler))
	defer ts1.Close()
	ts2 := httptest.NewTLSServer(http.HandlerFunc(htmlDummyLinksHandler))
	defer ts2.Close()
	Transport.TLSClientConfig.InsecureSkipVerify = true
	defer func() { Transport.TLSClientConfig.InsecureSkipVerify = false }()
	u1, _ := url.Parse(ts1.URL + "/foo")
	u2, _ := url.Parse(ts2.URL + "/foo")
	body := fmt.Sprintf(mixedContentBody, u1.Host, u2.Host, u1.Host)

	for i, tc := range []struct {
		origin *url.URL
		policy string
		mixed  bool
		want   string
	}{
		// HTML page is from http.
		{u1, "blabla", false, ""},
		{u1, "blabla", true, ""},
		{u1, "upgrade-insecure-requests", false, ""},
		{u1, "upgrade-insecure-requests", true, ""},

		// HTML page via https, but dont fail on mixed content.
		{u2, "blabla", false, ""},
		{u2, "upgrade-insecure-requests", false, ""},

		// HTML page via https, and fail on mixed content.
		{u2, "blabla", true, "/unsecure  -->  un-upgraded"},
		// The following is hard to test: Links upgrades
		// http://localhost:<portOfHttp> to http://localhost:<portOfHttp>
		// as upgrading just involes URL scheme changes (not the port).
		// thus the error. But this error is expected.
		{u2, "upgrade-insecure-requests", true, "/unsec-a: http: server gave HTTP response"},
	} {
		test := &Test{
			Request: Request{
				URL: tc.origin.String(),
				Request: &http.Request{
					URL: tc.origin,
				},
			},
			Response: Response{
				BodyStr: body,
				Response: &http.Response{
					Header: http.Header{
						"Content-Security-Policy": []string{tc.policy},
					},
				},
			},
		}

		check := &Links{Which: "img a", FailMixedContent: tc.mixed}
		err := check.Prepare()
		if err != nil {
			t.Fatalf("%d: unexpected error: %#v", i, err)
		}

		err = check.Execute(test)
		if err == nil {
			if tc.want != "" {
				t.Errorf("%d: missing error, want %s", i, tc.want)
			}
		} else {
			if strings.Contains(err.Error(), "/unsec-a  -->  un-upgraded") {
				t.Errorf("%d: anchor tag treated as mixed content: %s", i, err)
			}
			if tc.want == "" {
				t.Errorf("%d: unexpected error %s", i, err)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%d: wrong error %s, expecting %s in it", i, err, tc.want)
			}
		}

	}
}
