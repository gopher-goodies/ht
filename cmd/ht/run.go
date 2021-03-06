// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"log"
	"os"

	"github.com/vdobler/ht/suite"
)

var cmdRun = &Command{
	RunTests:    runRun,
	Usage:       "run <test>...",
	Description: "run a single test",
	Flag:        flag.NewFlagSet("run", flag.ContinueOnError),
	Help: `
Run loads the single test, unrolls it and prepares it and executes the
test (or the first of the unrolled tests each).
Variables set with the -D flag overwrite variables read from file with -Dfile.
	`,
}

func init() {
	addOutputFlag(cmdRun.Flag)

	addTestFlags(cmdRun.Flag)
}

func runRun(cmd *Command, tests []*suite.RawTest) {
	s := &suite.RawSuite{
		File: &suite.File{
			Data: "---",
			Name: "<internal>",
		},
		Name:        "Autogenerated suite for " + cmd.Name(),
		KeepCookies: true,
		Variables:   variablesFlag,
	}
	s.AddRawTests(tests...)
	err := s.Validate(variablesFlag)
	if err != nil {
		log.Println(err.Error())
		os.Exit(3)
	}

	// Propagate verbosity from command line to suite/test.
	setVerbosity(s)

	runExecute(cmd, []*suite.RawSuite{s})
}
