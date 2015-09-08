// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ht

import (
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
)

// Random is the source for all randmoness used in ht.
var Random *rand.Rand

func init() {
	Random = rand.New(rand.NewSource(34)) // Seed choosen truely random by Sabine.
}

// randomFunc is one of the random functions.
type randomFunc struct {
	name string         // for diagnostics
	re   *regexp.Regexp // submatches yield arguments
	args []string       // defaults and int parsing
	fn   func(args []interface{}) (string, error)
}

var randomFuncs = []randomFunc{
	{
		name: "NUMBER",
		re:   regexp.MustCompile(`^((\d+)-)?(\d+)( +(%.+))?$`),
		args: []string{"", "#1", "#", "", "%d"},
		fn:   randomNumber,
	},
	{
		name: "TEXT",
		re:   regexp.MustCompile(`^(([a-z][a-z][a-z]?) +)?((\d+)-)?(\d+)$`),
		args: []string{"", "fr", "", "#4", "#"},
		fn:   randomText,
	},
	{
		name: "IMAGE",
		re:   regexp.MustCompile(`^((\d+)-)?(\d+)$`),
		args: []string{"", "any", "#180", "#120"},
		// fn:   randomImage,
	},
}

func randomNumber(args []interface{}) (string, error) {
	from, to, format := args[1].(int), args[2].(int), args[4].(string)
	if span := (to - from + 1); span > 0 {
		return fmt.Sprintf(format, from+Random.Intn(span)), nil
	}
	return "", fmt.Errorf("ht: invalid range [%d,%d] for random number", from, to)
}

var textCorpus = map[string]string{
	"fr": "Allons enfants de la Patrie Le jour de gloire est arrivé! " +
		"Contre nous de la tyrannie L'étendard sanglant est levé " +
		"Entendez-vous dans nos campagnes. Mugir ces féroces soldats? " +
		"Ils viennent jusque dans vos bras. Égorger vos fils, vos " +
		"compagnes! Aux armes, citoyens! Formez vos bataillons! " +
		"Marchons! Marchons! Qu'un sang impur Abreuve nos sillons! " +
		"Amour sacré de la patrie, Conduis, soutiens nos bras vengeurs! " +
		"Liberté, Liberté cherie, Combats avec tes défenseurs! " +
		"Sous nos drapeaux, que la victoire Accoure à tes mâles accents! " +
		"Que tes ennemis expirants Voient ton triomphe et notre gloire!",
	"de": "Trittst im Morgenrot daher, Seh'ich dich im Strahlenmeer, " +
		"Dich, du Hocherhabener, Herrlicher! Wenn der Alpenfirn sich " +
		"rötet, Betet, freie Schweizer, betet! Eure fromme Seele ahnt " +
		"Eure fromme Seele ahnt Gott im hehren Vaterland, Gott, den " +
		"Herrn, im hehren Vaterland. Kommst im Abendglühn daher, " +
		"Find'ich dich im Sternenheer, Dich, du Menschenfreundlicher, " +
		"Liebender! In des Himmels lichten Räumen Kann ich froh und " +
		"selig träumen! Denn die fromme Seele ahnt Denn die fromme " +
		"Seele ahnt Gott im hehren Vaterland, Gott, den Herrn, " +
		"im hehren Vaterland",
	"en": "God save our gracious Queen, Long live our noble Queen, " +
		"God save the Queen! Send her victorious, Happy and glorious, " +
		"Long to reign over us; God save the Queen! O Lord, our God arise, " +
		"Scatter her enemies And make them fall; Confound their politics, " +
		"Frustrate their knavish tricks, On Thee our hopes we fix, " +
		"God save us all! Thy choicest gifts in store " +
		"On her be pleased to pour; Long may she reign; " +
		"May she defend our laws, And ever give us cause " +
		"To sing with heart and voice, God save the Queen!",
	"tlh": "      " +
		"      " +
		"       " +
		"       ",
}

func randomText(args []interface{}) (string, error) {
	lang, min, max := args[1].(string), args[3].(int), args[4].(int)
	corpus, ok := textCorpus[lang]
	if !ok {
		return "", fmt.Errorf("ht: no %s corpus of random text", lang)
	}
	span := max - min + 1
	if span <= 0 {
		return "", fmt.Errorf("ht: invalid range [%d,%d] for random text", min, max)
	}
	n := min + Random.Intn(span)
	if n == 0 {
		return "", nil
	}
	words := strings.Split(corpus, " ")
	w := len(words)
	begin := Random.Intn(w - 1)
	if begin+n <= w {
		return strings.Join(words[begin:begin+n], " "), nil
	}
	text := []string{}
	for len(text) < n {
		text = append(text, words[begin:]...)
		begin = 0
	}
	return strings.Join(text[:n], " "), nil
}

// randomValue interpretes a r of the form "RANDOM <what> [parameters]".
func setRandomVariable(vars map[string]string, r string) error {
	if _, ok := vars[r]; ok {
		return nil // This one was not a new one.
	}
	what := strings.TrimLeft(r[7:], " ")

	for _, rf := range randomFuncs {
		if !strings.HasPrefix(what, rf.name) {
			continue
		}
		args := strings.TrimLeft(what[len(rf.name):], " ")
		arglist, err := parseRandomArgs(args, rf)
		if err != nil {
			return err
		}
		value, err := rf.fn(arglist)
		if err != nil {
			return err
		}
		vars[r] = value
		return nil
	}
	return fmt.Errorf("ht: no such random type %q", r)
}

// parseRandomArgs produces an argument list for rf based on s.
// Default values are set and integer parsing is done.
func parseRandomArgs(s string, rf randomFunc) ([]interface{}, error) {
	matches := rf.re.FindStringSubmatch(s)
	if matches == nil {
		return nil, fmt.Errorf("ht: cannot parse argument %q to %s as %q",
			s, rf.name, rf.re)
	}
	matches = matches[1:]
	if len(matches) != len(rf.args) {
		panic(fmt.Sprintf("ht: random function %s needs %d arguments but got %d submatches",
			rf.name, len(rf.args), len(matches)))
	}

	vals := []interface{}{}
	for i, a := range rf.args {
		number := false
		if strings.HasPrefix(a, "#") {
			number = true
			a = a[1:]
		}
		if matches[i] == "" {
			matches[i] = a // Set default value if empty.
		}
		if number {
			n, err := strconv.Atoi(matches[i])
			if err != nil {
				return nil, fmt.Errorf("ht: argument %d to random %s: %s",
					i+1, rf.name, err.Error())
			}
			vals = append(vals, n)
		} else {
			vals = append(vals, matches[i])
		}
	}
	return vals, nil

}
