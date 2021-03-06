// Copyright 2015 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"sort"

	"github.com/vdobler/ht/ht"
)

var cmdHelp = &Command{
	RunArgs:     runHelp,
	Usage:       "help [subcommand]",
	Description: "print help information",
	Flag:        flag.NewFlagSet("help", flag.ContinueOnError),
	Help: `
Help shows help for ht as well as for the different subcommands.
Running 'ht help checks' displays the list of builtin checks and
'ht help extractors' displays the builtin variable extractors.
Running 'ht help doc <type>' displays detail information of <type>.
`,
}

func runHelp(cmd *Command, args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(0)
	}

	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s\n", cmd.Usage)
		os.Exit(9)
	}

	arg := args[0]
	if arg == "check" || arg == "checks" {
		displayChecks()
	}
	if arg == "extractor" || arg == "extractors" {
		displayExtractors()
	}

	for _, cmd := range commands {
		if cmd.Name() == arg {
			fmt.Printf(`Usage:

    ht %s
%s
Flags:
`, cmd.Usage, cmd.Help)
			cmd.Flag.PrintDefaults()
			os.Exit(0)
		}
	}

	fmt.Fprintf(os.Stderr, "Unknown help topic %#q.  Run 'ht help'.\n", arg)
	os.Exit(9) // failed at 'go help cmd'

}

func displayChecks() {
	checkNames := []string{}
	for name := range ht.CheckRegistry {
		checkNames = append(checkNames, name)
	}
	sort.Strings(checkNames)
	for _, name := range checkNames {
		fmt.Printf("%s := {\n", name)
		typ := ht.CheckRegistry[name]
		displayTypeAsPseudoJSON(typ)
		fmt.Printf("}\n\n")
	}
	fmt.Printf("Condition := {\n")
	displayTypeAsPseudoJSON(reflect.TypeOf(ht.Condition{}))
	fmt.Printf("}\n\n")
	os.Exit(0)
}

func displayExtractors() {
	exNames := []string{}
	for name := range ht.ExtractorRegistry {
		exNames = append(exNames, name)
	}
	sort.Strings(exNames)
	for _, name := range exNames {
		fmt.Printf("%s := {\n", name)
		typ := ht.ExtractorRegistry[name]
		displayTypeAsPseudoJSON(typ)
		fmt.Printf("}\n\n")
	}
	os.Exit(0)
}

func displayTypeAsPseudoJSON(typ reflect.Type) {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	for f := 0; f < typ.NumField(); f++ {
		field := typ.Field(f)
		c := field.Name[0]
		if c < 'A' || c > 'Z' {
			continue
		}
		fmt.Printf("    %s: ", field.Name)
		switch field.Type.Kind() {
		case reflect.Slice:
			e := field.Type.Elem()
			fmt.Printf("[ %s... ],\n", e.Name())
		case reflect.Map:
			fmt.Printf("{ ... },\n")
		default:
			fmt.Printf("%s,\n", field.Type.Name())
		}

	}
}
