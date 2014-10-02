// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// ht generates HTTP requests and checks the received responses.
//
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/vdobler/ht"
)

// A Command is one of the subcommands of ht.
type Command struct {
	// Run the command.
	// The args are the arguments after the command name.
	Run func(cmd *Command, suites []*ht.Suite)

	Usage string       // must start with command name
	Help  string       // the output of 'ht help'
	Flag  flag.FlagSet // special flags for this command
}

// Name returns the command's name: the first word in the usage line.
func (c *Command) Name() string {
	name := c.Usage
	i := strings.Index(name, " ")
	if i >= 0 {
		name = name[:i]
	}
	return name
}

func (c *Command) usage() {
	fmt.Fprintf(os.Stderr, "usage: %s\n\n", c.Usage)
	fmt.Fprintf(os.Stderr, "%s\n", c.Help)
	os.Exit(2)
}

// Commands lists the available commands and help topics.
// The order here is the order in which they are printed by 'go help'.
var commands = []*Command{
	cmdList,
	cmdExec,
	cmdPerf,
	cmdBench,
}

func usage() {
	fmt.Println(`ht is a tool to generate http request and test the response.

Usage:

    ht <command> [flags] <suite.ht>...

The commands are
    * help  Print help command
    * list  List the tests found in suite.ht
    * exec  Execute the tests found in suite.ht
    * perf  Run a load test

Flags:
    -D <name>=<value>  set parameter exapnsion of <name> to <value>  
    -only <id>,...   run only test with the given ids
    -skip <id>,...   skip tests wih the given ids ...
    -

Tests IDs have the following format <SuiteNo>.<Type><TestNo> with
<SuiteNo> and <TestNo> the sequential numbers of the suite and the
test inside the suite. Type is either empty, "u" for setUp test or
"d" for tearDown tests.
`)
	os.Exit(2)
}

// Variables which can be set via the command line. Statisfied flag.Value interface.
type cmdlVar map[string]string

func (c cmdlVar) String() string { return "" }
func (c cmdlVar) Set(s string) error {
	part := strings.SplitN(s, "=", 2)
	if len(part) != 2 {
		return fmt.Errorf("Bad argument '%s' to -D commandline parameter", s)
	}
	c[part[0]] = part[1]
	return nil
}

// Most of the flags.
var variablesFlag cmdlVar = make(cmdlVar) // flag -D
var onlyFlag = flag.String("only", "", "run only these tests, e.g. -only 3,4,9")
var skipFlag = flag.String("skip", "", "skip these tests, e.g. -skip 2,5")

func main() {
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		usage()
	}

	if args[0] == "help" {
		help(args[1:])
		return
	}
	for _, cmd := range commands {
		if cmd.Name() == args[0] {
			cmd.Flag.Usage = func() { cmd.usage() }
			cmd.Flag.Parse(args[1:])
			args = cmd.Flag.Args()
			suites := loadSuites(args)
			cmd.Run(cmd, suites)
			return
		}
	}

	fmt.Fprintf(os.Stderr, "go: unknown subcommand %q\nRun 'go help' for usage.\n",
		args[0])
	os.Exit(2)
}

// The help command.
func help(args []string) {
	if len(args) == 0 {
		usage() // TODO: this is not a failure
	}
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "usage: ht help <command>\n\nToo many arguments given.\n")
		os.Exit(2)
	}

	arg := args[0]

	for _, cmd := range commands {
		if cmd.Name() == arg {
			fmt.Printf("ht %s\n%s\n", cmd.Usage, cmd.Help)
			cmd.Flag.PrintDefaults()
			return
		}
	}

	fmt.Fprintf(os.Stderr, "Unknown help topic %#q.  Run 'ht help'.\n", arg)
	os.Exit(2) // failed at 'go help cmd'
}

func loadSuites(args []string) []*ht.Suite {
	var suites []*ht.Suite

	// Construct search paths to scan for suite files.
	var searchPaths []string
	for _, s := range args {
		searchPaths = append(searchPaths, path.Dir(s))
	}
	searchPaths = append(searchPaths, ".")
	logger := log.New(os.Stdout, "", log.LstdFlags)

	// Handle -only and -skip flags.
	only, skip := splitTestIDs(*onlyFlag), splitTestIDs(*skipFlag)

	// Input and setup suites from command line arguments.
	for _, s := range args {
		// Prepend "current" dir. Ugly, inefficent, works.
		thisSearchPath := append([]string{path.Dir(s)}, searchPaths...)
		filename := path.Base(s)
		suite, err := ht.LoadSuite(filename, thisSearchPath)
		if err != nil {
			log.Fatalf("Cannot read suite %q: %s", s, err)
		}
		for varName, varVal := range variablesFlag {
			suite.Variables[varName] = varVal
		}
		suite.Log = logger
		suite.Init()
		err = suite.Compile()
		if err != nil {
			log.Fatal(err.Error())
		}
		suites = append(suites, suite)
	}

	// Disable tests based on the -only and -skip flags.
	for sNo, suite := range suites {
		for tNo, test := range suite.Setup {
			shouldRun(test, fmt.Sprintf("%d.u%d", sNo+1, tNo+1), only, skip)
		}
		for tNo, test := range suite.Tests {
			shouldRun(test, fmt.Sprintf("%d.%d", sNo+1, tNo+1), only, skip)
		}
		for tNo, test := range suite.Teardown {
			shouldRun(test, fmt.Sprintf("%d.d%d", sNo+1, tNo+1), only, skip)
		}
	}

	return suites
}

// shouldRun disables t if needed.
func shouldRun(t *ht.Test, id string, only, skip map[string]struct{}) {
	if _, ok := skip[id]; ok {
		t.Poll.Max = -1
		log.Printf("Skipping test %s %q", id, t.Name)
		return
	}
	if _, ok := only[id]; !ok && len(only) > 0 {
		t.Poll.Max = -1
		log.Printf("Not running test %s %q", id, t.Name)
		return
	}
}

func splitTestIDs(f string) (ids map[string]struct{}) {
	ids = make(map[string]struct{})
	if len(f) == 0 {
		return
	}
	fp := strings.Split(f, ",")
	for _, x := range fp {
		xp := strings.SplitN(x, ".", 2)
		s, t := "1", xp[0]
		if len(xp) == 2 {
			s, t = xp[0], xp[1]
		}
		typ := ""
		switch t[0] {
		case 'U', 'u':
			typ = "U"
			t = t[1:]
		case 'D', 'd':
			typ = "D"
			t = t[1:]
		default:
			typ = ""
		}
		// TODO: support ranges like "3.1-5"
		sNo, tNo := mustAtoi(s), mustAtoi(t)
		id := fmt.Sprintf("%d.%s%d", sNo, typ, tNo)
		ids[id] = struct{}{}
	}
	return ids
}

func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		log.Fatalf("%s", err.Error())
	}
	return n
}
