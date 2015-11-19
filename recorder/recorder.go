// Copyright 2015 Volker Dobler.  All rights reserved.
// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package recorder allows to capture request/response pairs via a
// reverse proxy and generate tests for these pairs.
package recorder

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"image"
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
	"github.com/vdobler/ht/internal/json5"
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

// Options determining which events should be captured.
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

// StartReverseProxy listens on the local port and forwards request to remote
// while capturing the request/response pairs selected by opts.
func StartReverseProxy(port string, remote *url.URL, opts Options) error {
	remoteHost = remote.Host
	requests := make(chan Event, 10)
	go process(requests, opts)

	proxy := newSingleHostReverseProxy(remote)
	http.HandleFunc("/", handler(proxy, requests))
	log.Printf("Staring reverse proxying from localhost:%s to %s", port, remote.String())
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
		// TODO: the next 3 are definitively usefull as response headers,
		// but check if the can be used for requests too.
		req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
		req.Header.Set("Pragma", "no-cache")
		req.Header.Set("Expires", "0")
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
func handler(p *httputil.ReverseProxy, events chan Event) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		rr := httptest.NewRecorder()
		requestBody, err := ioutil.ReadAll(r.Body)
		if err != nil {
			panic(err.Error()) // Harsh but what else?
		}
		r.Body = ioutil.NopCloser(bytes.NewBuffer(requestBody))
		p.ServeHTTP(rr, r)

		events <- Event{
			Request:      r,
			RequestBody:  string(requestBody),
			Response:     rr,
			ResponseBody: rr.Body.String(),
			Timestamp:    time.Now()}
		for h, v := range rr.HeaderMap {
			w.Header()[h] = v
		}
		w.WriteHeader(rr.Code)
		w.Write(rr.Body.Bytes())
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
		last = e.Timestamp
		e.Name = fmt.Sprintf("Event %d", len(Events)+1)
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

// Test is a reduced version of ht.Test suitable for serialization to JSON5.
type Test struct {
	Name        string
	Description string   `json:",omitempty"`
	BasedOn     []string `json:",omitempty"`
	Request     ht.Request
	Checks      ht.CheckList `json:",omitempty"`
}

// Suite is a reduced version of ht.Suite suitable to serialization to JSON5.
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
	test := &Test{
		Name: fmt.Sprintf("Common Header of %s", suitename),
		Request: ht.Request{
			Header: commonHeaders,
		},
	}

	commonFilename := path.Join(directory, "common-headers.mixin")
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

		params := e.Request.URL.Query()
		e.Request.URL.RawQuery = ""
		urlString := e.Request.URL.String()
		urlString = strings.Replace(urlString, "H.O.S.T.N.A.M.E", "{{HOSTNAME}}", 1)

		// TODO: scan body for parameters and set ParamsAs
		body := e.RequestBody

		checks := extractChecks(e)

		test := &Test{
			Name:        e.Name,
			Description: fmt.Sprintf("Recorded from %s on %s", host, time.Now()),
			BasedOn:     []string{commonFilename},
			Request: ht.Request{
				Method:  e.Request.Method,
				URL:     urlString,
				Cookies: cookies,
				Header:  e.Request.Header,
				Params:  ht.URLValues(params),
				Body:    string(body),
			},
			Checks: checks,
		}

		name := strings.ToLower(strings.Replace(e.Name, " ", "_", -1)) + ".ht"
		suite.Tests = append(suite.Tests, name)
		filename := path.Join(directory, name)
		err = writeTest(test, filename)
		if err != nil {
			return err
		}

		e.Request.URL.Host = host
		log.Println("Generate test for ", e.Request.Method, e.Request.URL, " --> ", filename)
	}

	name := strings.ToLower(strings.Replace(suitename, " ", "_", -1)) + ".suite"
	filename := path.Join(directory, name)
	err = writeSuite(suite, filename)
	if err != nil {
		return err
	}
	log.Println("Generate suite ", filename)

	return nil
}

func writeTest(test *Test, filename string) error {
	data, err := json5.MarshalIndent(test, "", "    ")
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
	data, err := json5.MarshalIndent(suite, "", "    ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filename, data, 0666)
	if err != nil {
		return err
	}
	return nil
}

// extractChecks tries to generate checks based on the given
// request/response pair in e.
func extractChecks(e Event) ht.CheckList {
	list := ht.CheckList{}

	// Allways add StatusCode check.
	list = append(list, ht.StatusCode{Expect: e.Response.Code})

	// Check for Content-Type header.
	contentType := e.Response.Header().Get("Content-Type")
	contentTypeParts := []string{"??", "??"}
	if contentType != "" {
		contentType = strings.TrimSpace(strings.Split(contentType, ";")[0])
		if i := strings.Index(contentType, "/"); i != -1 {
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
	if loc := e.Response.HeaderMap.Get("Location"); loc != "" && e.Response.Code/100 == 3 { //  Uaaahhrg!
		red := &ht.Redirect{To: loc, StatusCode: e.Response.Code}
		list = append(list, red)
	}

	// Based on content type but ignore responses without body (e.g. 301)
	if len(e.ResponseBody) > 0 {
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
		Timeout:     ht.Duration(20 * time.Second),
		IgnoredLinks: []ht.Condition{
			ht.Condition{Contains: "www.facebook.com/"},
			ht.Condition{Contains: "www.twitter.com/"},
		},
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
		log.Printf("Title matched")
		title := ht.TextContent(node, false)
		list = append(list, &ht.HTMLContains{
			Selector: "head title",
			Text:     []string{title},
			Complete: true,
		})
	}

	// All h1
	if nodes := htmlH1Sel.MatchAll(doc); len(nodes) != 0 {
		log.Printf("H1 matched")
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
		sc.MinLifetime = ht.Duration(lt)
	} else {
		flags = append(flags, "session")
	}
	sc.Type = strings.Join(flags, " ")

	return sc
}

// ----------------------------------------------------------------------------
// Extraction of common header fields

func ExtractCommonResponseHeaders(events []Event) http.Header {
	headers := make([]http.Header, len(events))
	for i := range events {
		headers[i] = events[i].Response.HeaderMap
	}
	return extractCommonHeaders(headers)
}

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