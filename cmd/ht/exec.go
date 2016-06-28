// Copyright 2014 Volker Dobler.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/vdobler/ht/ht"
	"github.com/vdobler/ht/sanitize"
)

var cmdExec = &Command{
	RunSuites:   runExecute,
	Usage:       "exec [options] <suite>...",
	Description: "generate request and test response",
	Flag:        flag.NewFlagSet("run", flag.ContinueOnError),
	Help: `
Exec loads the given suites, unrolls the tests, prepares the tests and
executes them. The flags -skip and -only allow to fine control which
tests in the suite(s) are executed. Variables set with the -D flag overwrite
variables read from file with -Dfile.
The exit code is 3 if bogus tests or checks are found, 2 if test errors
are present, 1 if only check failures occured and 0 if everything passed.
	`,
}

func init() {
	cmdExec.Flag.BoolVar(&serialFlag, "serial", false,
		"run suites one after the other instead of concurrently")
	addOnlyFlag(cmdExec.Flag)
	addSkipFlag(cmdExec.Flag)

	addTestFlags(cmdExec.Flag)
}

var (
	serialFlag bool
)

func runExecute(cmd *Command, suites []*ht.Suite) {
	prepareExecution()

	executeSuites(suites)

	total, totalPass, totalError, totalSkiped, totalFailed, totalBogus := 0, 0, 0, 0, 0, 0
	for s := range suites {
		suites[s].PrintReport(os.Stdout)
	}

	for s := range suites {
		suites[s].PrintShortReport(os.Stdout)
		fmt.Println()

		// Statistics
		for _, r := range suites[s].AllTests() {
			switch r.Status {
			case ht.Pass:
				totalPass++
			case ht.Error:
				totalError++
			case ht.Skipped:
				totalSkiped++
			case ht.Fail:
				totalFailed++
			case ht.Bogus:
				totalBogus++
			}
			total++
		}

		dirname := outputDir + "/" + sanitize.Filename(suites[s].Name)
		fmt.Printf("Saveing result of suite %q to folder %q.\n", suites[s].Name, dirname)
		err := os.MkdirAll(dirname, 0766)
		if err != nil {
			log.Panic(err)
		}
		err = suites[s].HTMLReport(dirname)
		if err != nil {
			log.Panic(err)
		}
		cwd, err := os.Getwd()
		if err == nil {
			reportURL := "file://" + path.Join(cwd, dirname, "Report.html")
			fmt.Printf("See %s\n", reportURL)
		}
		junit, err := suites[s].JUnit4XML()
		if err != nil {
			log.Panic(err)
		}
		err = ioutil.WriteFile(dirname+"/junit-report.xml", []byte(junit), 0666)
		if err != nil {
			log.Panic(err)
		}
	}
	fmt.Println()
	fmt.Printf("Total %d,  Passed %d, Skipped %d,  Errored %d,  Failed %d,  Bogus %d\n",
		total, totalPass, totalSkiped, totalError, totalFailed, totalBogus)

	if totalBogus > 0 {
		fmt.Println("BOGUS")
		os.Exit(3)
	} else if totalError > 0 {
		fmt.Println("ERROR")
		os.Exit(2)
	} else if totalFailed > 0 {
		fmt.Println("FAIL")
		os.Exit(1)
	}

	fmt.Println("PASS")
	os.Exit(0)
}

func prepareExecution() {
	if outputDir == "" {
		outputDir = time.Now().Format("2006-01-02_15h04m05s")
	}
	os.MkdirAll(outputDir, 0766)

	prepareHT()
}

func prepareHT() {
	// Set parameters of package ht.
	if randomSeed == 0 {
		randomSeed = time.Now().UnixNano()
	}
	log.Printf("Seeding random number generator with %d", randomSeed)
	ht.Random = rand.New(rand.NewSource(randomSeed))
	if skipTLSVerify {
		log.Println("Skipping verification of TLS certificates presented by any server.")
		ht.Transport.TLSClientConfig.InsecureSkipVerify = true
	}
	ht.PhantomJSExecutable = phantomjs
	log.Printf("Using %q as PhantomJS executable", phantomjs)

	// Log variables and values sorted by variable name.
	varnames := make([]string, 0, len(variablesFlag))
	for v := range variablesFlag {
		varnames = append(varnames, v)
	}
	sort.Strings(varnames)
	for _, v := range varnames {
		log.Printf("Variable %s = %q", v, variablesFlag[v])
	}

}

func executeSuites(suites []*ht.Suite) {
	var wg sync.WaitGroup
	for i := range suites {
		if serialFlag {
			suites[i].Execute()
			if suites[i].Status > ht.Pass {
				log.Printf("Suite %d %q failed: %s", i+1,
					suites[i].Name,
					suites[i].Error.Error())
			}
		} else {
			wg.Add(1)
			go func(i int) {
				suites[i].Execute()
				if suites[i].Status > ht.Pass {
					log.Printf("Suite %d %q failed: %s", i+1,
						suites[i].Name, suites[i].Error.Error())
				}
				wg.Done()
			}(i)
		}
	}
	wg.Wait()
}
