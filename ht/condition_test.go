// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ht

import (
	"regexp"
	"testing"
)

var float12_3 float64 = 12.3
var float456 float64 = 456

var conditionTests = []struct {
	s string
	c Condition
	w string
}{
	// Equals
	{"foobar", Condition{Equals: "foobar"}, ``},
	{"foobar", Condition{Equals: "barfoo"}, `Unequal, was "foobar"`},
	{"foobarX", Condition{Equals: "foobar"}, `Unequal, was "foobarX"`},
	{"foobarXY", Condition{Equals: "foobar"}, `Unequal, was "foobarXY"`},
	{"foobarXYZ", Condition{Equals: "foobar"}, `Unequal, was "foobarXYZ"`},
	{"foobarbazwazturpot", Condition{Equals: "foobar"}, `Unequal, was "foobarbazwazturp"...`},
	// Corner cases of Equals
	{"A", Condition{Equals: "A"}, ``},
	{"", Condition{Equals: "A"}, `Unequal, was ""`},
	{"B", Condition{Equals: "A"}, `Unequal, was "B"`},
	{"BB", Condition{Equals: "A"}, `Unequal, was "BB"`},

	// Prefix and Suffix
	{"foobar", Condition{Prefix: "foo"}, ``},
	{"foobar", Condition{Prefix: "waz"}, `Bad prefix, got "foo"`},
	{"foobar", Condition{Prefix: "wazwazwaz"}, `Bad prefix, got "foobar"`},
	{"foobar", Condition{Prefix: "foobarbar"}, `Bad prefix, got "foobar"`},
	{"foobar", Condition{Suffix: "bar"}, ``},
	{"foobar", Condition{Suffix: "waz"}, `Bad suffix, got "bar"`},
	{"foobar", Condition{Suffix: "wazwazwaz"}, `Bad suffix, got "foobar"`},
	{"foobar", Condition{Suffix: "foofoobar"}, `Bad suffix, got "foobar"`},
	{"foobar", Condition{Prefix: "foo", Suffix: "bar"}, ``},
	{"foobar", Condition{Prefix: "waz", Suffix: "bar"}, `Bad prefix, got "foo"`},
	{"foobar", Condition{Prefix: "foo", Suffix: "waz"}, `Bad suffix, got "bar"`},
	{"foobar", Condition{Prefix: "waz", Suffix: "waz"}, `Bad prefix, got "foo"`},
	// Contains
	{"foobarfoobar", Condition{Contains: "oo"}, ``},
	{"foobarfoobar", Condition{Contains: "waz"}, `not found`},
	{"foobarfoobar", Condition{Contains: "waz", Count: -1}, ``},
	{"foobarfoobar", Condition{Contains: "oo", Count: -1}, `found forbidden`},
	{"foobarfoobar", Condition{Contains: "oo", Count: 2}, ``},
	{"foobarfoobar", Condition{Contains: "obarf", Count: 1}, ``},
	{"foobarfoobar", Condition{Contains: "o", Count: 4}, ``},
	{"foobarfoobar", Condition{Contains: "foo", Count: 1}, `found 2, want 1`},
	{"foobarfoobar", Condition{Contains: "foo", Count: 3}, `found 2, want 3`},
	// Regexp
	{"foobarwu", Condition{Regexp: "[aeiou]."}, ``},
	{"foobarwu", Condition{Regexp: "[aeiou].", Count: 2}, ``},
	{"foobarwu", Condition{Regexp: "[aeiou].", Count: 3}, `found 2, want 3`},
	{"foobarwu", Condition{Regexp: "[aeiou].", Count: -1}, `found forbidden`},
	{"frtgbwu", Condition{Regexp: "[aeiou]."}, `not found`},
	// Min and Max
	{"foobar", Condition{Min: 2}, ``},
	{"foobar", Condition{Min: 20}, `Too short, was 6`},
	{"foobar", Condition{Max: 30}, ``},
	{"foobar", Condition{Max: 3}, `Too long, was 6`},
	// GreaterThan and LessThan
	{"3", Condition{LessThan: &float12_3}, ``},
	{"3", Condition{GreaterThan: &float12_3}, `not greater than 12.3, was 3`},
	{" \t3\n\r", Condition{LessThan: &float12_3}, ``},
	{"'3'", Condition{GreaterThan: &float12_3}, `not greater than 12.3, was 3`},
	{"800", Condition{LessThan: &float456}, `not less than 456, was 800`},
	{"'-8.8e1'", Condition{LessThan: &float456}, ``},
	{"XYZ", Condition{LessThan: &float456}, `strconv.ParseFloat: parsing "XYZ": invalid syntax`},
	{"200", Condition{GreaterThan: &float12_3, LessThan: &float456}, ``},
}

func TestCondition(t *testing.T) {
	for i, tc := range conditionTests {
		if tc.c.Regexp != "" {
			tc.c.re = regexp.MustCompile(tc.c.Regexp)
		}
		err := tc.c.Fulfilled(tc.s)
		switch {
		case tc.w == "" && err != nil:
			t.Errorf("%d. %s, unexpected error %s", i, tc.s, err)
		case tc.w != "" && err == nil:
			t.Errorf("%d. %s, missing error", i, tc.s)
		case tc.w != "" && err != nil && err.Error() != tc.w:
			t.Errorf("%d. %s, wrong error %q, want %q", i, tc.s, err, tc.w)
		}

	}
}
