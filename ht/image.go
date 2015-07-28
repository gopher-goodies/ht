// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// image.go contains checks against image data.

package ht

import (
	"fmt"

	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/vdobler/ht/fingerprint"
)

func init() {
	RegisterCheck(Image{})
}

// ----------------------------------------------------------------------------
// Image

// Image checks image format, size and fingerprint. As usual a zero value of
// a field skipps the check of that property.
// Image fingerprinting is done via github.com/vdobler/ht/fingerprint.
// Only one of BMV or ColorHist should be used as there is just one threshold.
type Image struct {
	// Format is the format of the image as registered in package image.
	Format string `json:",omitempty"`

	// If > 0 check width or height of image.
	Width, Height int `json:",omitempty"`

	// Fingerprint is either the 16 hex digit long Block Mean Value hash or
	// the 24 hex digit long Color Histogram hash of the image.
	Fingerprint string `json:",omitempty"`

	// Threshold is the limit up to which the received image may differ
	// from the given BMV or ColorHist fingerprint.
	Threshold float64 `json:",omitempty"`
}

func (c Image) Execute(t *Test) error {
	img, format, err := image.Decode(t.Response.Body())
	if err != nil {
		fmt.Printf("Image.Okay resp.BodyReader=%#v\n", t.Response.Body())
		return CantCheck{err}
	}
	// TODO: Do not abort on first failure.
	if c.Format != "" && format != c.Format {
		return fmt.Errorf("Got %s image, want %s", format, c.Format)
	}

	bounds := img.Bounds()
	if c.Width > 0 && c.Width != bounds.Dx() {
		return fmt.Errorf("Got %d px wide image, want %d",
			bounds.Dx(), c.Width)

	}
	if c.Height > 0 && c.Height != bounds.Dy() {
		return fmt.Errorf("Got %d px heigh image, want %d",
			bounds.Dy(), c.Height)

	}

	if len(c.Fingerprint) == 16 {
		targetBMV, err := fingerprint.BMVHashFromString(c.Fingerprint)
		if err != nil {
			return CantCheck{fmt.Errorf("bad BMV hash: %s", err)}
		}
		imgBMV := fingerprint.NewBMVHash(img)
		if d := fingerprint.BMVDelta(targetBMV, imgBMV); d > c.Threshold {
			return fmt.Errorf("Got BMV of %s, want %s (delta=%.4f)",
				imgBMV.String(), targetBMV.String(), d)
		}

	} else if len(c.Fingerprint) == 24 {
		targetCH, err := fingerprint.ColorHistFromString(c.Fingerprint)
		if err != nil {
			return CantCheck{fmt.Errorf("bad ColorHist hash: %s", err)}
		}
		imgCH := fingerprint.NewColorHist(img)
		if d := fingerprint.ColorHistDelta(targetCH, imgCH); d > c.Threshold {
			return fmt.Errorf("Got ColorHist of %s, want %s (delta=%.4f)",
				imgCH.String(), targetCH.String(), d)
		}
	}

	return nil
}

func (i Image) Prepare() error {
	switch len(i.Fingerprint) {
	case 0:
		return nil
	case 16:
		_, err := fingerprint.BMVHashFromString(i.Fingerprint)
		if err != nil {
			return MalformedCheck{err}
		}
	case 24:
		_, err := fingerprint.ColorHistFromString(i.Fingerprint)
		if err != nil {
			return MalformedCheck{err}
		}
	default:
		return fmt.Errorf("ht: image fingerprint has illegal length %d", len(i.Fingerprint))
	}
	return nil
}