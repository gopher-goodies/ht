// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package response provides a type for captuiring a HTTP response. Its main
// purpose is breaking an import cycle between ht and ht/check.
package ht

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Duration is a time.Duration but has nicer JSON encoding.
type Duration time.Duration

func (d Duration) String() string {
	n := int64(d)
	if n <= 180*1e9 {
		return si(n)
	} else {
		return clock(n / 1e9)
	}
}

var siUnits = map[int64]string{1e3: "µs", 1e6: "ms", 1e9: "s"}

// si formats n nanoseconds to three significant digits. N must be <= 999 seconds.
// BUG: rounding up can produce 4 significant digits
// TODO: negativ duration are most likely broken
func si(n int64) string {
	neg := ""
	if n < 0 {
		neg = "-"
		n = -n
	}
	if n <= 999 {
		return fmt.Sprintf("%s%dns", neg, n)
	}
	scale := int64(1000)
	for n/scale > 999 {
		scale *= 1000
	}
	f := float64(n/(scale/1000)) / 1000
	n /= scale
	prec := 2
	if n > 99 {
		prec = 0
	} else if n > 9 {
		prec = 1
	}
	return fmt.Sprintf("%s%.*f%s", neg, prec, f, siUnits[scale])
}

// clock formats sec as a minutes and seconds
func clock(sec int64) string {
	min := sec / 60
	sec -= min * 60
	return fmt.Sprintf("%dm%02ds", min, sec)
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	s := string(data)
	scale := int64(1e9)
	if strings.HasPrefix(s, `"`) {
		s = s[1 : len(s)-1]
		if strings.HasSuffix(s, "ns") {
			scale = 1
			s = s[:len(s)-2]
		} else if strings.HasSuffix(s, "µs") {
			scale = 1e3
			s = s[:len(s)-3]
		} else if strings.HasSuffix(s, "ms") {
			scale = 1e6
			s = s[:len(s)-2]
		} else if strings.HasSuffix(s, "s") {
			scale = 1e9
			s = s[:len(s)-1]
			if i := strings.Index(s, "m"); i != -1 {
				m, err := strconv.Atoi(s[:i])
				if err != nil {
					return err
				}
				sec, err := strconv.Atoi(s[i+1:])
				if err != nil {
					return err
				}
				sec += 60 * m
				*d = Duration(int64(sec) * 1e9)
				return nil
			}
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return err
	}
	*d = Duration(f * float64(scale))

	return nil
}