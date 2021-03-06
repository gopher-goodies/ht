// Copyright 2015 Volker Dobler.  All rights reserved.
// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package recorder allows to capture request/response pairs via a
// reverse proxy and generate tests for these pairs.
package recorder

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/andybalholm/cascadia"
	"github.com/vdobler/ht/fingerprint"
	"github.com/vdobler/ht/ht"
	"github.com/vdobler/ht/sanitize"
)

// Events is the global list of recorded events.
var Events []Event

// Event is a request/response pair.
type Event struct {
	Request      *http.Request              // The request.
	Response     *httptest.ResponseRecorder // The recorded response.
	RequestBody  string                     // The captured body.
	ResponseBody string
	Timestamp    time.Time // Timestamp when caputred.
	Name         string    // Used during dumping.
}

// extractName tries to come up with a useful and representative name for
// the event.
func (e Event) extractName() string {
	doc, err := html.Parse(bytes.NewBufferString(e.ResponseBody))
	if err != nil {
		return ""
	}

	// Try title first.
	if node := cascadia.MustCompile("head title").MatchFirst(doc); node != nil {
		title := ht.TextContent(node, false)
		if title != "" {
			return title
		}
	}

	// First h1 is a good second choice.
	if node := cascadia.MustCompile("h1").MatchFirst(doc); node != nil {
		title := ht.TextContent(node, false)
		if title != "" {
			return title
		}
	}

	// Last part of the URL without extension is my last idea.
	p := e.Request.URL.Path
	p = p[strings.LastIndex(p, "/")+1:]
	if i := strings.Index(p, "."); i != -1 {
		p = p[:i]
		if p != "" {
			return p
		}
	}

	return ""
}

// Options determining which and how events should be captured.
type Options struct {
	// Disarm is the time span after a captured request/response pair
	// in which the capturing is disarmed.
	Disarm time.Duration

	// IgnoredContentType allows to skip capturing a request whose
	// Content-Type header matches.
	IgnoredContentType *regexp.Regexp

	// IgnoredPath allows to skip capturing events based on the
	// requested path,
	IgnoredPath *regexp.Regexp

	// Rewrite determines what is rewritten.
	Rewrite Rewriter
}

func (o Options) ignore(e Event) bool {
	if o.IgnoredPath != nil && o.IgnoredPath.MatchString(e.Request.URL.Path) {
		log.Println("Ignoring path", e.Request.URL.Path)
		return true
	}
	if o.IgnoredContentType != nil &&
		o.IgnoredContentType.MatchString(e.Response.HeaderMap.Get("Content-Type")) {
		log.Println("Ignoring content type ", e.Response.HeaderMap.Get("Content-Type"))
		return true
	}
	return false
}

var (
	remoteHost string
)

// Rewriter from remote to local host.
type Rewriter struct {
	local  string
	remote string

	remoteRe  *regexp.Regexp
	remoteSub string

	localRe  *regexp.Regexp
	localSub string

	what uint32
}

const (
	// Different things to rewrite.
	RewriteNothing        uint32 = 0
	RewriteResponseHeader uint32 = 1 << (iota - 1)
	RewriteResponseBody
	RewriteRequestHeader
	RewriteRequestBody
)

// NewRewriter for what between local and remote.
func NewRewriter(local, remote string, what uint32) Rewriter {
	r := Rewriter{
		local:  local,
		remote: remote,
		what:   what,
	}

	r.remoteRe = regexp.MustCompile(
		`(^|[^a-zAZ0-9])` + regexp.QuoteMeta(remote) + `([^a-zAZ0-9]|$)`)
	r.remoteSub = "${1}" + local + "${2}"

	r.localRe = regexp.MustCompile(
		`(^|[^a-zAZ0-9])` + regexp.QuoteMeta(local) + `([^a-zAZ0-9]|$)`)
	r.localSub = "${1}" + remote + "${2}"

	return r
}

// Response rewrites the header and body of a HTTP response.
func (r Rewriter) Response(header http.Header, body []byte) (http.Header, []byte) {
	rheader := r.header(header, r.remoteRe, r.remoteSub, r.what&RewriteResponseHeader != 0)
	rbody := r.body(body, r.remoteRe, r.remoteSub, r.what&RewriteResponseBody != 0)
	return rheader, rbody
}

// Request rewrites the header and body of a HTTP response.
func (r Rewriter) Request(header http.Header, body []byte) (http.Header, []byte) {
	rheader := r.header(header, r.localRe, r.localSub, r.what&RewriteRequestHeader != 0)
	rbody := r.body(body, r.localRe, r.localSub, r.what&RewriteRequestBody != 0)
	return rheader, rbody
}

func (r Rewriter) header(header http.Header, re *regexp.Regexp, sub string, do bool) http.Header {
	// Header first, allways "rewritten"
	rheader := http.Header{}
	for h, vv := range header {
		if h == "Content-Length" {
			// Any gzip content will be rezipped and rewrting the
			// body might change the content length too.
			// So drop the header and let net/http recalculate it.
			continue
		}
		if do {
			for i, v := range vv {
				w := r.remoteRe.ReplaceAllString(v, r.remoteSub)
				if w != v {
					fmt.Printf("Rewrite Response Header %q\n    from: %q\n    to:   %q\n",
						h, v, w)
				}
				vv[i] = w
			}
		}
		rheader[h] = vv
	}
	return rheader
}

func (r Rewriter) body(body []byte, re *regexp.Regexp, sub string, do bool) []byte {
	if !do {
		return body
	}
	rbody := r.remoteRe.ReplaceAll(body, []byte(r.remoteSub))
	if !bytes.Equal(body, rbody) {
		n := len(r.remoteRe.FindAllIndex(body, -1))
		fmt.Printf("Rewrite Response Body: %d occurrences\n", n)
	}
	return rbody
}

// StartReverseProxy listens on the local port and forwards request to remote
// while capturing the request/response pairs selected by opts.
func StartReverseProxy(port string, remoteURL *url.URL, opts Options) error {
	remoteHost = remoteURL.Host
	requests := make(chan Event, 10)
	go process(requests, opts)

	remote := remoteURL.Host

	proxy := newSingleHostReverseProxy(remoteURL)
	http.HandleFunc("/", handler(proxy, requests, opts.Rewrite))
	log.Println("Staring reverse proxy")
	log.Printf("Proxying from http://recorder.ht%s to %s", port, remote)
	return http.ListenAndServe(port, nil)
}

func newSingleHostReverseProxy(target *url.URL) *httputil.ReverseProxy {
	targetQuery := target.RawQuery
	director := func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
		if targetQuery == "" || req.URL.RawQuery == "" {
			req.URL.RawQuery = targetQuery + req.URL.RawQuery
		} else {
			req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
		}
		// Additional to httputil.NewSingleHostReverseProxy:
		// Fake host and disable caching.
		req.Host = target.Host
		// TODO: the next 3 are definitively useful as response headers,
		// but check if the can be used for requests too.
		req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		req.Header.Set("Pragma", "no-cache")
		req.Header.Del("If-Modified-Since")
		req.Header.Del("If-None-Match")
	}
	return &httputil.ReverseProxy{Director: director}
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

// handler produces a http.HandlerFunc which routes the request via the
// reverse proxy p, records the request and the response and sends these
// to events.
func handler(p *httputil.ReverseProxy, events chan Event, rewrite Rewriter) func(http.ResponseWriter, *http.Request) {

	log.Printf("Rewriting %d\n", rewrite.what)
	log.Printf("   %s  -->  %s\n", rewrite.remoteRe.String(), rewrite.remoteSub)
	log.Printf("   %s  -->  %s\n", rewrite.localRe.String(), rewrite.localSub)
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Handling", r.URL.String())
		rr := httptest.NewRecorder()
		requestBody, err := ioutil.ReadAll(r.Body)
		if err != nil {
			panic(err.Error()) // Harsh but what else?
		}

		fheader, fbody := rewrite.Request(r.Header, requestBody)
		r.Header = fheader
		r.Body = ioutil.NopCloser(bytes.NewBuffer(fbody))

		p.ServeHTTP(rr, r)

		// Read response body, transparently unzip if needed
		var respBodyReader io.Reader
		gzipped := false
		if rr.HeaderMap.Get("Content-Encoding") == "gzip" {
			respBodyReader, err = gzip.NewReader(rr.Body)
			if err != nil {
				panic(err) // TODO
			}
			gzipped = true
		} else {
			respBodyReader = rr.Body
		}
		body, err := ioutil.ReadAll(respBodyReader)
		if err != nil {
			panic(err) // TODO
		}

		events <- Event{
			Request:      r,
			RequestBody:  string(requestBody),
			Response:     rr,
			ResponseBody: string(body),
			Timestamp:    time.Now(),
		}

		rheader, rbody := rewrite.Response(rr.HeaderMap, body)
		for h, vv := range rheader {
			w.Header()[h] = vv
		}
		w.WriteHeader(rr.Code)
		if gzipped {
			gz := gzip.NewWriter(w)
			gz.Write(rbody)
			gz.Close()
		} else {
			w.Write(rbody)
		}
	}
}

// process drains events and decides whether too keep (i.e append to Events)
// or ignore it.
func process(events chan Event, opts Options) {
	log.Println("Started processing")
	last := time.Now()
	for e := range events {
		delta := e.Timestamp.Sub(last)
		if delta < opts.Disarm {
			continue
		}
		if opts.ignore(e) {
			continue
		}
		name := e.extractName()
		last = e.Timestamp
		e.Name = fmt.Sprintf("Event %d: %s", len(Events)+1, name)
		Events = append(Events, e)
		log.Println("Recorded", e.Request.Method, e.Request.URL, " --> ",
			e.Response.Code, e.Response.HeaderMap.Get("Content-Type"))
	}
}

func printHeader(which string, header http.Header) {
	hs := []string{}
	for h := range header {
		hs = append(hs, h)
	}
	sort.Strings(hs)
	fmt.Println(which)
	for _, h := range hs {
		fmt.Printf("%20s :  %v\n", h, header[h])
	}
	fmt.Println()
}

// Test is a reduced version of ht.Test suitable for serialization to JSON.
type Test struct {
	Name        string
	Description string   `json:",omitempty"`
	BasedOn     []string `json:",omitempty"`
	Request     ht.Request
	Checks      ht.CheckList `json:",omitempty"`
}

// Suite is a reduced version of ht.Suite suitable to serialization to JSON.
type Suite struct {
	Name        string
	Description string `json:",omitempty"`
	Tests       []string
	Variables   map[string]string
}

// DumpEvents writes events to directory, it extracts common request headers.
func DumpEvents(events []Event, directory string, suitename string) error {
	err := os.MkdirAll(directory, 0777)
	if err != nil {
		return err
	}

	// extract all common headers into mixin
	commonHeaders := ExtractCommonRequestHeaders(events)
	commonHeadersName := "common-headers.mixin"
	test := &Test{
		Name: fmt.Sprintf("Common Header of %s", suitename),
		Request: ht.Request{
			Header: commonHeaders,
		},
	}

	commonFilename := path.Join(directory, commonHeadersName)
	err = writeTest(test, commonFilename)
	if err != nil {
		return err
	}

	suite := Suite{
		Name:        suitename,
		Description: fmt.Sprintf("Generated at %s", time.Now()),
		Variables: map[string]string{
			"HOSTNAME": remoteHost,
		},
	}

	for _, e := range events {
		host := e.Request.URL.Host
		e.Request.URL.Host = "H.O.S.T.N.A.M.E"
		cookies := []ht.Cookie{}
		for _, c := range e.Request.Cookies() {
			cookies = append(cookies, ht.Cookie{Name: c.Name, Value: c.Value})
		}
		e.Request.Header.Del("Cookie")

		// Inspect body and extract parameters if appropriate.
		queryParams := e.Request.URL.Query()
		rawQuery := e.Request.URL.RawQuery
		e.Request.URL.RawQuery = "" // clear to prevent reparsing when body is analyzed
		body, bodyParams, paramsAs := scanRequestBody(&e)

		var params url.Values
		if len(queryParams) > 0 && len(bodyParams) > 0 {
			// Parameters in URL _and_ body: Must keep both
			e.Request.URL.RawQuery = rawQuery
			params = bodyParams
		} else {
			// Just one "type" of parameters.
			if len(queryParams) > 0 {
				params = queryParams
				paramsAs = ""
			} else {
				params = bodyParams
			}
		}

		urlString := e.Request.URL.String()
		urlString = strings.Replace(urlString, "H.O.S.T.N.A.M.E", "{{HOSTNAME}}", 1)

		dropUnnecessaryHeaders(e.Request.Header)

		checks := extractChecks(e)

		test := &Test{
			Name:        e.Name,
			Description: fmt.Sprintf("Recorded from %s on %s", host, time.Now()),
			BasedOn:     []string{commonHeadersName},
			Request: ht.Request{
				Method:   e.Request.Method,
				URL:      urlString,
				Cookies:  cookies,
				Header:   e.Request.Header,
				Params:   params,
				ParamsAs: paramsAs,
				Body:     body,
			},
			Checks: checks,
		}

		name := sanitize.Filename(e.Name) + ".ht"
		suite.Tests = append(suite.Tests, name)
		filename := path.Join(directory, name)
		err = writeTest(test, filename)
		if err != nil {
			return err
		}

		e.Request.URL.Host = host
		log.Println("Generate test for ", e.Request.Method, e.Request.URL, " --> ", filename)
	}

	name := strings.ToLower(strings.Replace(suitename, " ", "_", -1))
	if !strings.HasSuffix(name, ".suite") {
		name += ".suite"
	}
	filename := path.Join(directory, name)
	err = writeSuite(suite, filename)
	if err != nil {
		return err
	}
	log.Println("Generate suite ", filename)

	return nil
}

func scanRequestBody(e *Event) (body string, params url.Values, as string) {
	if len(e.RequestBody) == 0 {
		return "", nil, ""
	}

	if e.Request.Method != "POST" {
		log.Printf("Ooops: Don't know how to treat %s-Request with non-empty body.",
			e.Request.Method)
		return e.RequestBody, nil, ""
	}

	// Repopulate the request body with an "unconsumed" writer (the original
	// request has been forwarded to the proxy which drained the body).
	e.Request.Body = ioutil.NopCloser(bytes.NewBufferString(e.RequestBody))

	contentType := e.Request.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(contentType, "application/x-www-form-urlencoded"):
		if err := e.Request.ParseForm(); err != nil {
			log.Printf("Error parsing form: %s", err)
		}
		as = "body"
	case strings.HasPrefix(contentType, "multipart/form-data"):
		if err := e.Request.ParseMultipartForm(1 << 26); err != nil {
			log.Printf("Error parsing multipart form: %s", err)
		}
		as = "multipart"
	default:
		log.Printf("Ooops: Don't know how to treat Content-Type %s with non-empty body.",
			contentType)
		return e.RequestBody, nil, ""
	}

	return "", e.Request.Form, as
}

func writeTest(test *Test, filename string) error {
	data, err := json.MarshalIndent(test, "", "    ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filename, data, 0666)
	if err != nil {
		return err
	}
	return nil
}

// TODO: combine with writeTest
func writeSuite(suite Suite, filename string) error {
	data, err := json.MarshalIndent(suite, "", "    ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filename, data, 0666)
	if err != nil {
		return err
	}
	return nil
}

// ----------------------------------------------------------------------------
// Extract Checks

// extractChecks tries to generate checks based on the given
// request/response pair in e.
func extractChecks(e Event) ht.CheckList {
	list := ht.CheckList{}

	isRedirect := e.Response.Code/100 == 3 //  Uaaahhrg!

	// Allways add StatusCode check.
	list = append(list, ht.StatusCode{Expect: e.Response.Code})

	// Check for Content-Type header.
	contentType := e.Response.Header().Get("Content-Type")
	contentTypeParts := []string{"??", "??"}
	if contentType != "" {
		contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
		if i := strings.Index(contentType, "/"); i != -1 && !isRedirect {
			contentTypeParts = strings.SplitN(contentType, "/", 2)
			list = append(list, ht.ContentType{Is: contentTypeParts[1]})
		}
	}

	// Checks for Set-Cookie headers:
	dummy := http.Response{Header: e.Response.Header()}
	now := e.Timestamp
	for _, c := range dummy.Cookies() {
		path := cookiePath(c, e.Request.URL)
		if c.MaxAge < 0 || (!c.Expires.IsZero() && c.Expires.Before(now)) {
			dc := &ht.DeleteCookie{Name: c.Name, Path: path}
			list = append(list, dc)
		} else {
			sc := createSetCookieCheck(c, now)
			sc.Path = ht.Condition{Equals: path}
			list = append(list, sc)
		}
	}

	// Check redirections:
	if loc := e.Response.HeaderMap.Get("Location"); loc != "" && isRedirect {
		red := &ht.Redirect{To: loc, StatusCode: e.Response.Code}
		list = append(list, red)
	}

	// Based on content type but ignore responses without body (e.g. 301)
	if len(e.ResponseBody) > 0 && !isRedirect {
		switch {
		case contentTypeParts[1] == "html", contentTypeParts[1] == "xhtml":
			list = append(list, extractHTMLChecks(e)...)
		case contentTypeParts[0] == "image":
			list = append(list, extractImageChecks(e)...)
		case contentTypeParts[1] == "pdf":
			list = append(list, identityCheck(e))
		}
	}

	return list
}

// ----------------------------------------------------------------------------
// Content based checks

func identityCheck(e Event) ht.Check {
	hash := sha1.Sum([]byte(e.ResponseBody))
	return ht.Identity{SHA1: fmt.Sprintf("%02x", hash)}
}

func extractHTMLChecks(e Event) ht.CheckList {
	list := ht.CheckList{}

	// Anything else than UTF-8 is bad.
	list = append(list, ht.UTF8Encoded{})

	// Allways add Links check.
	list = append(list, &ht.Links{
		Head:        true,
		Which:       "a img link script",
		Concurrency: 4,
		Timeout:     20 * time.Second,
		IgnoredLinks: []ht.Condition{
			{Contains: "www.facebook.com/"},
			{Contains: "www.twitter.com/"},
		},
	})

	// Allways add Screenshot check.
	list = append(list, &ht.Screenshot{
		Browser:           ht.Browser{Geometry: "256x144+0+0*20%"}, // 256x144 at 20% zoom is 1280x720 at 100%
		Expected:          "{{TEST_DIR}}/screenshot-XYZ.png",
		Actual:            "{{TEST_DIR}}/screenshot-XYZ-_actual.png",
		AllowedDifference: 12,
		IgnoreRegion:      []string{"2x3+1+1"},
	})

	doc, err := html.Parse(bytes.NewBufferString(e.ResponseBody))
	if err != nil {
		log.Println(err)
		return list
	}

	htmlTitleSel := cascadia.MustCompile("head title")
	htmlH1Sel := cascadia.MustCompile("body h1")

	// Title
	if node := htmlTitleSel.MatchFirst(doc); node != nil {
		title := ht.TextContent(node, false)
		list = append(list, &ht.HTMLContains{
			Selector: "head title",
			Text:     []string{title},
			Complete: true,
		})
	}

	// All h1
	if nodes := htmlH1Sel.MatchAll(doc); len(nodes) != 0 {
		h1s := []string{}
		for _, node := range nodes {
			h1s = append(h1s, ht.TextContent(node, false))
		}
		list = append(list, &ht.HTMLContains{
			Selector: "body h1",
			Text:     h1s,
			Complete: true,
			InOrder:  true,
		})
	}

	return list
}

func extractImageChecks(e Event) ht.CheckList {
	list := ht.CheckList{}

	image, format, err := image.Decode(bytes.NewBufferString(e.ResponseBody))
	if err != nil {
		return list
	}

	list = append(list, ht.Image{
		Format: format,
		Width:  image.Bounds().Dx(),
		Height: image.Bounds().Dy(),
	})

	BMV := fingerprint.NewBMVHash(image)
	list = append(list, ht.Image{
		Fingerprint: BMV.String(),
		Threshold:   0.01,
	})

	ch := fingerprint.NewColorHist(image)
	list = append(list, ht.Image{
		Fingerprint: ch.String(),
		Threshold:   0.01,
	})

	return list
}

// ----------------------------------------------------------------------------
// Cookie handling

func cookiePath(c *http.Cookie, u *url.URL) string {
	if c.Path != "" {
		return c.Path // assume this is well-formed
	}

	p := u.Path
	i := strings.LastIndex(p, "/")
	if i == 0 {
		return "/" // p ~ "/XYZ"
	}
	return p[:i] // Either p ~ "/XYZ/ABC" or p ~ "/XYZ/ABC/"
}

func createSetCookieCheck(c *http.Cookie, now time.Time) *ht.SetCookie {
	sc := &ht.SetCookie{Name: c.Name, Value: ht.Condition{Equals: c.Value}}

	lt := time.Duration(0)
	if c.MaxAge > 0 {
		lt = time.Second * time.Duration(c.MaxAge)
	} else if !c.Expires.IsZero() && c.Expires.After(now) {
		lt = c.Expires.Sub(now)
	}

	flags := []string{}
	if c.HttpOnly {
		flags = append(flags, "httpOnly")
	} else {
		flags = append(flags, "exposed")
	}
	if c.Secure {
		flags = append(flags, "secure")
	} else {
		flags = append(flags, "unsafe")
	}
	if lt > 0 {
		flags = append(flags, "persistent")
		if lt > 10*time.Second {
			lt -= 10 * time.Second
		}
		sc.MinLifetime = lt
	} else {
		flags = append(flags, "session")
	}
	sc.Type = strings.Join(flags, " ")

	return sc
}

// ----------------------------------------------------------------------------
// Handling of headers

// ExtractCommonResponseHeaders from events.
func ExtractCommonResponseHeaders(events []Event) http.Header {
	headers := make([]http.Header, len(events))
	for i := range events {
		headers[i] = events[i].Response.HeaderMap
	}
	return extractCommonHeaders(headers)
}

// ExtractCommonRequestHeaders from events.
func ExtractCommonRequestHeaders(events []Event) http.Header {
	headers := make([]http.Header, len(events))
	for i := range events {
		headers[i] = events[i].Request.Header
	}
	return extractCommonHeaders(headers)
}

// extractCommonHeaders collects and returns all common header entries in
// headers an deletes the common one from headers.
func extractCommonHeaders(headers []http.Header) http.Header {
	common := http.Header{}
	for h, v := range headers[0] {
		vs := fmt.Sprintf("%v", v)
		identical := true
		for j := 2; j < len(headers); j++ {
			if vs != fmt.Sprintf("%v", headers[j][h]) {
				identical = false
				break
			}
		}
		if identical {
			common[h] = v
			for i := range headers {
				headers[i].Del(h)
			}
		}
	}
	return common
}

func dropUnnecessaryHeaders(h http.Header) {
	h.Del("Content-Length") // Automatically set by package http
	h.Del("Origin")         // This one is "http://localhost:8080"
}
