// Copyright 2015 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ht

import (
	"fmt"
	"testing"
)

func TestModify(t *testing.T) {
	r := Resilience{}
	for _, tc := range [][]string{
		{"hello"},
		{"12"},
		{"98.95"},
		{"foo@test.org"},
		{"H"},
		{""},
		{"foo", "bar"},
	} {
		// TODO: some proper automated test?
		modvals := r.modify(tc, modAll)
		if *verboseTest {
			fmt.Printf("Original: %v\n", tc)
			for _, mv := range modvals {
				fmt.Printf("  %#v\n", mv)
			}
		} else if n := len(modvals); n < 10 {
			t.Errorf("Got only %d modified values for 'all'", n)
		}
	}
}

func TestParseModification(t *testing.T) {
	for i, tc := range []struct {
		in   string
		want modification
		ok   bool
	}{
		{"none", modNone, true},
		{"drop", modDrop, true},
		{"nonsense", modNonsense, true},
		{"tiny", modTiny, true},
		{"twice delete large", modTwice | modDelete | modLarge, true},
		{"negative type change", modNegative | modType | modChange, true},
		{"all", modAll, true},
		{"drop xxx", modDrop, false},
	} {
		got, err := parseModifications(tc.in)
		if err != nil {
			if tc.ok {
				t.Errorf("%d. Unexpected error %s on %q", i, err, tc.in)
			}
		} else {
			if !tc.ok {
				t.Errorf("%d. Missing error on %q", i, tc.in)
			}
			if got != tc.want {
				t.Errorf("%d. parseModification(%q)=%d want %d",
					i, tc.in, got, tc.want)
			}

		}
	}
}
