package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/programmfabrik/apitest/pkg/lib/cjson"
	"github.com/programmfabrik/apitest/pkg/lib/datastore"
	"github.com/programmfabrik/apitest/pkg/lib/filesystem"
	"github.com/programmfabrik/apitest/pkg/lib/report"
	"github.com/programmfabrik/apitest/pkg/lib/template"
	"github.com/programmfabrik/apitest/pkg/lib/util"
)

// Suite defines the structure of our apitest. We do read this in with the config loader
type Suite struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	HttpServer  *struct {
		Addr     string          `json:"addr"`
		Dir      string          `json:"dir"`
		Testmode bool            `json:"testmode"`
		Proxy    HTTPServerProxy `json:"proxy"`
	} `json:"http_server,omitempty"`
	SMTPServer *SMTPServer            `json:"smtp_server,omitempty"`
	Tests      []interface{}          `json:"tests"`
	Store      map[string]interface{} `json:"store"`

	StandardHeader          map[string]*string `yaml:"header" json:"header"`
	StandardHeaderFromStore map[string]string  `yaml:"header_from_store" json:"header_from_store"`

	Config          TestToolConfig
	datastore       *datastore.Datastore
	manifestDir     string
	manifestPath    string
	reporterRoot    *report.ReportElement
	index           int
	serverURL       string
	httpServer      http.Server
	httpServerProxy *HTTPServerProxy
	httpServerDir   string
	idleConnsClosed chan struct{}
	HTTPServerHost  string
	smtpServer      *SMTPServer
}

// NewTestSuite creates a new suite on which we execute our tests on. Normally this only gets call from within the apitest main command
func NewTestSuite(config TestToolConfig, manifestPath string, r *report.ReportElement, datastore *datastore.Datastore, index int) (*Suite, error) {
	suite := Suite{
		Config:       config,
		manifestDir:  filepath.Dir(manifestPath),
		manifestPath: manifestPath,
		reporterRoot: r,
		datastore:    datastore,
		index:        index,
	}
	// Here we create this additional struct in order to preload the suite manifest
	// It is needed, for example, for getting the suite HTTP server address
	// Then preloaded values are used to load again the manifest with relevant replacements
	suitePreload := Suite{
		Config:       config,
		manifestDir:  filepath.Dir(manifestPath),
		manifestPath: manifestPath,
		reporterRoot: r,
		datastore:    datastore,
		index:        index,
	}

	manifest, err := suitePreload.loadManifest()
	if err != nil {
		err = fmt.Errorf("error loading manifest: %s", err)
		suitePreload.reporterRoot.Failure = fmt.Sprintf("%s", err)
		return &suitePreload, err
	}

	err = cjson.Unmarshal(manifest, &suitePreload)
	if err != nil {
		err = fmt.Errorf("error unmarshaling manifest '%s': %s", manifestPath, err)
		suitePreload.reporterRoot.Failure = fmt.Sprintf("%s", err)
		return &suitePreload, err
	}

	// Add external http server url here, as only after this point the http_server.addr may be available
	hsru := new(url.URL)
	shsu := new(url.URL)
	if httpServerReplaceHost != "" {
		hsru, err = url.Parse("//" + httpServerReplaceHost)
		if err != nil {
			return nil, errors.Wrap(err, "set http_server_host failed (command argument)")
		}
	}
	if suitePreload.HttpServer != nil {
		preloadHTTPAddrStr := suitePreload.HttpServer.Addr
		if preloadHTTPAddrStr == "" {
			preloadHTTPAddrStr = ":80"
		}
		// We need to append it as the golang URL parser is not smart enough to differenciate between hostname and protocol
		shsu, err = url.Parse("//" + preloadHTTPAddrStr)
		if err != nil {
			return nil, errors.Wrap(err, "set http_server_host failed (manifesr addr)")
		}
	}
	suitePreload.HTTPServerHost = ""
	if hsru.Hostname() != "" {
		suitePreload.HTTPServerHost = hsru.Hostname()
	} else if shsu.Hostname() != "" {
		suitePreload.HTTPServerHost = shsu.Hostname()
	} else {
		suitePreload.HTTPServerHost = "localhost"
	}
	if suite.HTTPServerHost == "0.0.0.0" {
		suitePreload.HTTPServerHost = "localhost"
	}
	if hsru.Port() != "" {
		suitePreload.HTTPServerHost += ":" + hsru.Port()
	} else if shsu.Port() != "" {
		suitePreload.HTTPServerHost += ":" + shsu.Port()
	}

	// Here we load the usable manifest, now that we can do all potential replacements
	manifest, err = suitePreload.loadManifest()
	if err != nil {
		err = fmt.Errorf("error loading manifest: %s", err)
		suite.reporterRoot.Failure = fmt.Sprintf("%s", err)
		return &suite, err
	}
	// fmt.Printf("%s", string(manifest))
	// We unmarshall the final manifest into the final working suite
	err = cjson.Unmarshal(manifest, &suite)
	if err != nil {
		err = fmt.Errorf("error unmarshaling manifest '%s': %s", manifestPath, err)
		suite.reporterRoot.Failure = fmt.Sprintf("%s", err)
		return &suite, err
	}
	suite.HTTPServerHost = suitePreload.HTTPServerHost

	//Append suite manifest path to name, so we know in an automatic setup where the test is loaded from
	suite.Name = fmt.Sprintf("%s (%s)", suite.Name, manifestPath)

	// init store
	err = suite.datastore.SetMap(suite.Store)
	if err != nil {
		err = fmt.Errorf("error setting datastore map:%s", err)
		suite.reporterRoot.Failure = fmt.Sprintf("%s", err)
		return &suite, err
	}

	return &suite, nil
}

// Run run the given testsuite
func (ats *Suite) Run() bool {
	r := ats.reporterRoot
	logrus.Infof("[%2d] '%s'", ats.index, ats.Name)

	// Important!!! this one needs to start before, as we may need to assign http routes to its proxy
	ats.StartSMTPServer()

	ats.StartHttpServer()

	start := time.Now()

	success := true
	for k, v := range ats.Tests {
		child := r.NewChild(strconv.Itoa(k))
		sTestSuccess := ats.parseAndRunTest(v, ats.manifestDir, ats.manifestPath, k, false, child)
		child.Leave(sTestSuccess)
		if !sTestSuccess {
			success = false
			break
		}
	}

	elapsed := time.Since(start)
	r.Leave(success)
	if success {
		logrus.WithFields(logrus.Fields{"elapsed": elapsed.Seconds()}).Infof("[%2d] success", ats.index)
	} else {
		logrus.WithFields(logrus.Fields{"elapsed": elapsed.Seconds()}).Warnf("[%2d] failure", ats.index)
	}

	ats.StopSMTPServer()

	ats.StopHttpServer()

	return success
}

type TestContainer struct {
	CaseByte json.RawMessage
	Path     string
}

func (ats *Suite) parseAndRunTest(v interface{}, manifestDir, testFilePath string, k int, runParallel bool, r *report.ReportElement) bool {
	//Init variables
	loader := template.NewLoader(ats.datastore)

	loader.HTTPServerHost = ats.HTTPServerHost
	serverURL, err := url.Parse(ats.Config.ServerURL)
	if err != nil {
		logrus.Error(fmt.Errorf("can not load server url into test (%s): %s", testFilePath, err))
		return false
	}
	loader.ServerURL = serverURL

	isParallelPathSpec := false
	switch t := v.(type) {
	case string:
		isParallelPathSpec = util.IsParallelPathSpec([]byte(t))
	}

	//Get the Manifest with @ logic
	fileh, testObj, err := template.LoadManifestDataAsRawJson(v, manifestDir)
	dir := filepath.Dir(fileh)
	if fileh != "" {
		testFilePath = filepath.Join(filepath.Dir(testFilePath), fileh)
	}
	if err != nil {
		r.SaveToReportLog(err.Error())
		logrus.Error(fmt.Errorf("can not LoadManifestDataAsRawJson (%s): %s", testFilePath, err))
		return false
	}

	//Try to directly unmarshal the manifest into testcase array
	var testCases []json.RawMessage
	err = cjson.Unmarshal(testObj, &testCases)
	if err == nil {
		d := 1
		if isParallelPathSpec || runParallel {
			d = len(testCases)
		}

		waitCh := make(chan bool, d)
		succCh := make(chan bool, len(testCases))

		go func() {
			for ki, v := range testCases {
				waitCh <- true
				go testGoRoutine(k, ki, v, ats, testFilePath, manifestDir, dir, r, loader, waitCh, succCh, isParallelPathSpec || runParallel)
			}
		}()

		for i := 0; i < len(testCases); i++ {
			select {
			case succ := <-succCh:
				if succ == false {
					return false
				}
			}
		}
	} else {
		//We were not able unmarshal into array, so we try to unmarshal into raw message
		var singleTest json.RawMessage
		err = cjson.Unmarshal(testObj, &singleTest)
		if err == nil {
			//Did work to unmarshal -> no go template

			//Check if is @ and if so load the test
			if util.IsPathSpec(testObj) {
				var sS string

				err := cjson.Unmarshal(testObj, &sS)
				if err != nil {
					r.SaveToReportLog(err.Error())
					logrus.Error(fmt.Errorf("can not unmarshal (%s): %s", testFilePath, err))
					return false
				}

				return ats.parseAndRunTest(sS, filepath.Join(manifestDir, dir), testFilePath, k, isParallelPathSpec, r)
			} else {
				return ats.runSingleTest(TestContainer{CaseByte: testObj, Path: filepath.Join(manifestDir, dir)}, r, testFilePath, loader, k, runParallel)
			}
		} else {
			//Did not work -> Could be go template or a mallformed json
			requestBytes, lErr := loader.Render(testObj, filepath.Join(manifestDir, dir), nil)
			if lErr != nil {
				r.SaveToReportLog(lErr.Error())
				logrus.Error(fmt.Errorf("can not render template (%s): %s", testFilePath, lErr))
				return false
			}

			//If the both objects are the same we did not have a template, but a mallformed json -> Call error
			if string(requestBytes) == string(testObj) {
				r.SaveToReportLog(err.Error())
				logrus.Error(fmt.Errorf("can not unmarshal (%s): %s", testFilePath, err))
				return false
			}

			//We have a template -> One level deeper with rendered bytes
			return ats.parseAndRunTest([]byte(requestBytes), filepath.Join(manifestDir, dir),
				testFilePath, k, isParallelPathSpec, r)

		}
	}

	return true
}

func (ats *Suite) runSingleTest(tc TestContainer, r *report.ReportElement, testFilePath string, loader template.Loader, k int, isParallel bool) bool {
	r.SetName(testFilePath)

	var test Case
	jErr := cjson.Unmarshal(tc.CaseByte, &test)
	if jErr != nil {

		r.SaveToReportLog(jErr.Error())
		logrus.Error(fmt.Errorf("can not unmarshal single test (%s): %s", testFilePath, jErr))

		return false
	}

	test.loader = loader
	test.manifestDir = tc.Path
	test.suiteIndex = ats.index
	test.index = k
	test.dataStore = ats.datastore
	test.standardHeader = ats.StandardHeader
	test.standardHeaderFromStore = ats.StandardHeaderFromStore
	if isParallel {
		test.ContinueOnFailure = true
	}
	if test.LogNetwork == nil {
		test.LogNetwork = &ats.Config.LogNetwork
	}
	if test.LogVerbose == nil {
		test.LogVerbose = &ats.Config.LogVerbose
	}

	if test.ServerURL == "" {
		test.ServerURL = ats.Config.ServerURL
	}
	success := test.runAPITestCase(r)

	if !success && !test.ContinueOnFailure {
		return false
	}

	return true
}

func (ats *Suite) loadManifest() ([]byte, error) {
	var res []byte
	logrus.Tracef("Loading manifest: %s", ats.manifestPath)
	loader := template.NewLoader(ats.datastore)
	loader.HTTPServerHost = ats.HTTPServerHost
	serverURL, err := url.Parse(ats.Config.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("can not load server url into manifest (%s): %s", ats.manifestPath, err)
	}
	loader.ServerURL = serverURL
	manifestFile, err := filesystem.Fs.Open(ats.manifestPath)
	if err != nil {
		return res, fmt.Errorf("error opening manifestPath (%s): %s", ats.manifestPath, err)
	}
	defer manifestFile.Close()

	manifestTmpl, err := ioutil.ReadAll(manifestFile)
	if err != nil {
		return res, fmt.Errorf("error loading manifest (%s): %s", ats.manifestPath, err)
	}
	return loader.Render(manifestTmpl, ats.manifestDir, nil)
}

func testGoRoutine(k, ki int, v json.RawMessage, ats *Suite, testFilePath, manifestDir, dir string, r *report.ReportElement, loader template.Loader, waitCh, succCh chan bool, runParallel bool) {
	success := false

	//Check if is @ and if so load the test
	switch util.IsPathSpec(v) {
	case true:
		var sS string
		err := cjson.Unmarshal(v, &sS)
		if err != nil {
			r.SaveToReportLog(err.Error())
			logrus.Error(fmt.Errorf("can not unmarshal (%s): %s", testFilePath, err))
			success = false
			break
		}
		success = ats.parseAndRunTest(sS, filepath.Join(manifestDir, dir), testFilePath, k+ki, runParallel, r)
	default:
		success = ats.runSingleTest(TestContainer{CaseByte: v, Path: filepath.Join(manifestDir, dir)},
			r, testFilePath, loader, ki, runParallel)
	}

	succCh <- success
	if success {
		<-waitCh
	}
}
