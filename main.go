// Copyright Programmfabrik GmbH
// All Rights Reserved
package main

import (
	"fmt"
	"github.com/programmfabrik/fylr-apitest/lib/api"

	log "github.com/sirupsen/logrus"

	"github.com/programmfabrik/fylr-apitest/lib/report"
	"io/ioutil"

	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
)

var (
	reportFormat, reportFile    string
	verbosity                   int
	rootDirectorys, singleTests []string
)

func init() {
	//Configure all the flags that fylr-apitest offers
	TestCMD.PersistentFlags().StringVarP(&cfgFile, "config", "c", "./fylr.yml", "config file")

	TestCMD.PersistentFlags().StringSliceVarP(
		&rootDirectorys, "directory", "d", []string{"."},
		"path to directory containing the tests.")

	TestCMD.PersistentFlags().Bool(
		"no-requirements", false,
		"don't run requirements for the testsuite.")

	TestCMD.PersistentFlags().StringSliceVarP(
		&singleTests, "single", "s", []string{},
		"path to a single manifest. Runs only that specified testsuite")

	TestCMD.PersistentFlags().IntVarP(
		&verbosity, "verbosity", "v", -1,
		`in [-1, 0, 1, 2], defines logging of requests and responses of the programm
-1: Only normal test ouput
0: All from '-1' & failed test responses
1: All from '-1' & all responses
2: All from '1' & all requests`)

	TestCMD.PersistentFlags().StringVar(
		&reportFile, "report-file", "",
		"Defines where the log statements should be saved.")

	TestCMD.PersistentFlags().StringVar(
		&reportFormat, "report-format", "",
		"Defines how the report statements should be saved. [junit/json]")

	//Bind the flags to overwrite the yml config if they are set
	viper.BindPFlag("apitest.report.file", TestCMD.PersistentFlags().Lookup("report-file"))
	viper.BindPFlag("apitest.report.format", TestCMD.PersistentFlags().Lookup("report-format"))
}

var TestCMD = &cobra.Command{
	Args:             cobra.MaximumNArgs(0),
	PersistentPreRun: setup,
	Use:              "fylr apitest",
	Short:            "flyr Apitester lets you define API tests on the go",
	Long:             `A fast and flexible API testing tool. Helping you to define API tests on the go`,
	Run:              runApiTests,
}

func main() {
	err := TestCMD.Execute()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

var cfgFile string

func setup(ccmd *cobra.Command, args []string) {
	//Load yml config
	LoadConfig(cfgFile)

	//Set log verbosity
	FylrConfig.SetLogVerbosity(verbosity)
}

func runApiTests(cmd *cobra.Command, args []string) {

	//Check if paths are valid

	for _, rootDirectory := range rootDirectorys {
		if _, err := os.Stat(rootDirectory); rootDirectory != "." && os.IsNotExist(err) {
			log.Fatalf("The path '%s' for the test folders is not valid", rootDirectory)
		}
	}
	for _, singleTest := range singleTests {
		if _, err := os.Stat(singleTest); singleTest != "" && os.IsNotExist(err) {
			log.Fatalf("The path '%s' for the single test is not valid", singleTest)
		}
	}

	serverUrl := FylrConfig.Apitest.Server
	reportFormat = FylrConfig.Apitest.Report.Format
	reportFile = FylrConfig.Apitest.Report.File

	//Save the config into TestToolConfig
	testToolConfig, err := NewTestToolConfig(serverUrl, rootDirectorys)
	if err != nil {
		log.Fatal(err)
	}

	datastore := api.NewStore()
	for k, v := range FylrConfig.Apitest.StoreInit {
		datastore.Set(k, v)
		log.Debugf("Add Init value for datastore Key: '%s', Value: '%v'", k, v)
	}

	//Actually run the tests
	//Run test function
	runSingleTest := func(manifestPath string, r *report.Report) {
		suite, err := NewTestSuite(
			testToolConfig,
			manifestPath,
			r,
			datastore,
			0,
		)
		if err != nil {
			log.Fatal(err)
		}

		suite.Run()
	}

	r := report.NewReport()

	//Decide if run only one test
	if len(singleTests) > 0 {
		for _, singleTest := range singleTests {
			absManifestPath, _ := filepath.Abs(singleTest)
			runSingleTest(absManifestPath, r)
		}
	} else {
		for _, singlerootDirectory := range testToolConfig.TestDirectories {
			manifestPath := filepath.Join(singlerootDirectory, "manifest.json")
			absManifestPath, _ := filepath.Abs(manifestPath)
			runSingleTest(absManifestPath, r)
		}
	}

	//Create report
	if reportFile != "" {
		var parsingFunction func(baseResult *report.ReportElement) []byte
		switch reportFormat {
		case "junit":
			parsingFunction = report.ParseJUnitResult
		case "json":
			parsingFunction = report.ParseJSONResult
		default:
			log.Errorf(
				"Given report format '%s' not supported. Saving report '%s' as json",
				reportFormat,
				reportFile)

			parsingFunction = report.ParseJSONResult
		}

		err = ioutil.WriteFile(reportFile, r.GetTestResult(parsingFunction), 0644)
		if err != nil {
			fmt.Println("Could not save into file: ", err)
		}
	}

	if r.DidFail() {
		os.Exit(1)
	}
}
