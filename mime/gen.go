// Copyright 2016 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io/ioutil"
	"net/http"
)

var MimeTypeExtension = map[string]string{}

type entry struct {
	Extensions []string `json:"extensions"`
}

func main() {
	resp, err := http.Get("https://cdn.rawgit.com/jshttp/mime-db/master/db.json")
	if err != nil {
		panic(err)
	}

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	list := make(map[string]entry)
	err = json.Unmarshal(data, &list)
	if err != nil {
		panic(err)
	}

	for mimetype, e := range list {
		if len(e.Extensions) == 0 {
			continue
		}
		MimeTypeExtension[mimetype] = e.Extensions[0]
	}

	buf := &bytes.Buffer{}
	fmt.Fprintln(buf, `// Generated by "go run gen.go". DO NOT EDIT.

// Package mime provides a map of mime types to file extensions.
package mime

// MimeTypeExtension maps mime type to filename extension.
//
// The content is derived from github.com/jshttp/mime-db published
// under the MIT license.
var MimeTypeExtension = map[string]string{`)

	for mt, ext := range MimeTypeExtension {
		fmt.Fprintf(buf, "\t%q: %q,\n", mt, ext)
	}
	fmt.Fprintln(buf, "}")

	b, err := format.Source(buf.Bytes())
	if err != nil {
		panic(err)
	}

	err = ioutil.WriteFile("mime.go", b, 0666)
	if err != nil {
		panic(err)
	}

}