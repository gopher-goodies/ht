// Copyright 2015 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ht

import (
	"math/rand"
	"strings"
	"testing"
)

func TestSetRandomVariable(t *testing.T) {
	for i, tc := range []struct {
		r, want, err string
	}{
		{r: "RANDOM NUMBER 80", want: "67"},
		{r: "RANDOM NUMBER 80 %d", want: "67"},
		{r: "RANDOM NUMBER 80 %04d", want: "0067"},
		{r: "RANDOM NUMBER 70 %x", want: "2f"},
		{r: "RANDOM NUMBER 70 %04X", want: "002F"},
		{r: "RANDOM NUMBER 70-80", want: "80"},
		{r: "RANDOM NUMBER 1000-9999", want: "5786"},
		{r: "RANDOM NUMBER 40-30", err: "invalid range"},
		{r: "RANDOM TEXT 8", want: "jour de gloire est arrivé!"},
		{r: "RANDOM TEXT 1-2", want: "jour"},
		{r: "RANDOM TEXT de 8", want: "fromme Seele ahnt Gott im"},
		{r: "RANDOM TEXT de 10-20",
			want: "fromme Seele ahnt Gott im hehren Vaterland, " +
				"Gott, den Herrn, im hehren Vaterland Trittst " +
				"im Morgenrot daher, Seh'ich dich im"},
		{r: "RANDOM TEXT 4-2", err: "invalid range"},
		{r: "RANDOM TEXT 0-0", want: ""},
		{r: "RANDOM TEXT tlh 1-2", want: "\uf8d1\uf8dd\uf8e1\uf8e3\uf8d0\uf8e2"},
		{r: "RANDOM EMAIL", want: "Graf.M.Laura@gmail.com"},
		{r: "RANDOM EMAIL web.de", want: "Graf.M.Laura@web.de"},
	} {
		vars := map[string]string{}
		Random = rand.New(rand.NewSource(2))
		err := setRandomVariable(vars, tc.r)
		if tc.err == "" {
			if err != nil {
				t.Errorf("%d: %q unexpected error %s", i, tc.r, err)
			} else {
				if got, ok := vars[tc.r]; !ok {
					t.Errorf("%d: %q missing value; vars=%v", i, tc.r, vars)
				} else if got != tc.want {
					t.Errorf("%d: %q got %q, want %q", i, tc.r, got, tc.want)
				}

			}
		} else {
			if err == nil {
				t.Errorf("%d: %q missing error, want %s", i, tc.r, tc.err)
			} else if !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%d: %q wrong error, got %s, want %s", i, tc.r, err, tc.err)
			}
		}
	}
}
