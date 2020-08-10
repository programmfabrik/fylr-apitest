package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
	"github.com/programmfabrik/apitest/pkg/lib/datastore"

	"github.com/programmfabrik/apitest/pkg/lib/cjson"

	"github.com/programmfabrik/apitest/pkg/lib/api"
	"github.com/programmfabrik/apitest/pkg/lib/compare"
	"github.com/programmfabrik/apitest/pkg/lib/report"
	"github.com/programmfabrik/apitest/pkg/lib/template"
	"github.com/programmfabrik/apitest/pkg/lib/util"
	"github.com/sirupsen/logrus"
)

// Case defines the structure of our single testcase
// It gets read in by our config reader at the moment the mainfest.json gets parsed
type Case struct {
	Name              string                 `json:"name"`
	Description       string                 `json:"description"`
	RequestData       *interface{}           `json:"request"`
	ResponseData      interface{}            `json:"response"`
	ContinueOnFailure bool                   `json:"continue_on_failure"`
	Store             map[string]interface{} `json:"store"`                // init datastore before testrun
	StoreResponse     map[string]string      `json:"store_response_qjson"` // store qjson parsed response in datastore

	Timeout         int           `json:"timeout_ms"`
	WaitBefore      *int          `json:"wait_before_ms"`
	WaitAfter       *int          `json:"wait_after_ms"`
	Delay           *int          `json:"delay_ms"`
	BreakResponse   []interface{} `json:"break_response"`
	CollectResponse interface{}   `json:"collect_response"`

	SendEmailData *interface{} `json:"send_email"`

	LogNetwork *bool `json:"log_network"`
	LogVerbose *bool `json:"log_verbose"`

	loader      template.Loader
	manifestDir string
	ReportElem  *report.ReportElement
	suiteIndex  int
	index       int
	dataStore   *datastore.Datastore

	standardHeader          map[string]*string
	standardHeaderFromStore map[string]string

	ServerURL         string `json:"server_url"`
	ReverseTestResult bool   `json:"reverse_test_result"`
}

func (testCase Case) runAPITestCase(parentReportElem *report.ReportElement) bool {
	if testCase.Name == "" {
		testCase.Name = "<no name>"
	}
	if testCase.Description == "" {
		logrus.Infof("     [%2d] '%s'", testCase.index, testCase.Name)
	} else {
		logrus.Infof("     [%2d] '%s': '%s'", testCase.index, testCase.Name, testCase.Description)
	}

	testCase.ReportElem = parentReportElem.NewChild(testCase.Name)
	r := testCase.ReportElem

	start := time.Now()

	// Store standard data into datastore
	if testCase.dataStore == nil && len(testCase.Store) > 0 {
		err := fmt.Errorf("error setting datastore. Datastore is nil")
		r.SaveToReportLog(fmt.Sprintf("Error during execution: %s", err))
		logrus.Errorf("     [%2d] %s", testCase.index, err)

		return false
	}
	err := testCase.dataStore.SetMap(testCase.Store)
	if err != nil {
		err = fmt.Errorf("error setting datastore map:%s", err)
		r.SaveToReportLog(fmt.Sprintf("Error during execution: %s", err))
		logrus.Errorf("     [%2d] %s", testCase.index, err)

		return false
	}

	success := true
	if testCase.RequestData != nil {
		success, err = testCase.run()
	}

	if testCase.SendEmailData != nil {
		err = testCase.sendEmail()
		success = success && (err == nil)
	}

	elapsed := time.Since(start)
	if err != nil {
		r.SaveToReportLog(fmt.Sprintf("Error during execution: %s", err))
		logrus.Errorf("     [%2d] %s", testCase.index, err)
		success = false
	}

	// Reverse if needed
	if testCase.ReverseTestResult {
		success = !success
	}

	if !success {
		logrus.WithFields(logrus.Fields{"elapsed": elapsed.Seconds()}).Warnf("     [%2d] failure", testCase.index)
	} else {
		logrus.WithFields(logrus.Fields{"elapsed": elapsed.Seconds()}).Infof("     [%2d] success", testCase.index)
	}

	r.Leave(success)

	return success
}

// cheRckForBreak Response tests the given response for a so called break response.
// If this break response is present it returns a true
func (testCase Case) breakResponseIsPresent(request api.Request, response api.Response) (bool, error) {

	if testCase.BreakResponse != nil {
		for _, v := range testCase.BreakResponse {
			spec, err := testCase.loadResponseSerialization(v)
			if err != nil {
				return false, fmt.Errorf("error loading check response serilization: %s", err)
			}

			expectedResponse, err := api.NewResponseFromSpec(spec)
			if err != nil {
				return false, fmt.Errorf("error loading check response from spec: %s", err)
			}

			expectedResponse.Format = response.Format

			responsesMatch, err := testCase.responsesEqual(expectedResponse, response)
			if err != nil {
				return false, fmt.Errorf("error matching break responses: %s", err)
			}

			if testCase.LogVerbose != nil && *testCase.LogVerbose {
				logrus.Tracef("breakResponseIsPresent: %v", responsesMatch)
			}

			if responsesMatch.Equal {
				return true, nil
			}
		}

	}
	return false, nil
}

// checkCollectResponse loops over all given collect responses and than
// If this continue response is present it returns a true.
// If no continue response is set, it also returns true to keep the testsuite running
func (testCase *Case) checkCollectResponse(request api.Request, response api.Response) (int, error) {

	if testCase.CollectResponse != nil {
		_, loadedResponses, err := template.LoadManifestDataAsObject(testCase.CollectResponse, testCase.manifestDir, testCase.loader)
		if err != nil {
			return -1, fmt.Errorf("error loading check response: %s", err)
		}

		jsonRespArray := util.JsonArray{}
		switch t := loadedResponses.(type) {
		case util.JsonArray:
			jsonRespArray = t
		case util.JsonObject:
			jsonRespArray = util.JsonArray{t}
		default:
			return -1, fmt.Errorf("error loading check response no valid typew")

		}

		leftResponses := make(util.JsonArray, 0)
		for _, v := range jsonRespArray {
			spec, err := testCase.loadResponseSerialization(v)
			if err != nil {
				return -1, fmt.Errorf("error loading check response serilization: %s", err)
			}

			expectedResponse, err := api.NewResponseFromSpec(spec)
			if err != nil {
				return -1, fmt.Errorf("error loading check response from spec: %s", err)
			}

			expectedResponse.Format = response.Format

			responsesMatch, err := testCase.responsesEqual(expectedResponse, response)
			if err != nil {
				return -1, fmt.Errorf("error matching check responses: %s", err)
			}

			if !responsesMatch.Equal {
				leftResponses = append(leftResponses, v)
			}
		}

		testCase.CollectResponse = leftResponses

		if testCase.LogVerbose != nil && *testCase.LogVerbose {
			logrus.Tracef("Remaining CheckReponses: %s", testCase.CollectResponse)
		}

		return len(leftResponses), nil
	}

	return 0, nil
}

func (testCase Case) executeRequest(counter int) (compare.CompareResult, api.Request, api.Response, error) {
	var (
		responsesMatch    compare.CompareResult
		req               api.Request
		apiResp           api.Response
		apiRespJsonString string
		err               error
	)

	// Store datastore
	err = testCase.dataStore.SetMap(testCase.Store)
	if err != nil {
		err = fmt.Errorf("error setting datastore map:%s", err)
	}

	//Do Request
	req, err = testCase.loadRequest()
	if err != nil {
		err = fmt.Errorf("error loading request: %s", err)
		return responsesMatch, req, apiResp, err
	}

	//Log request on trace level (so only v2 will trigger this)
	if testCase.LogNetwork != nil && *testCase.LogNetwork {
		logrus.Tracef("[REQUEST]:\n%s\n\n", limitLines(req.ToString(logCurl), Config.Apitest.Limit.Request))
	}

	apiResp, err = req.Send()
	if err != nil {
		testCase.LogReq(req)
		err = fmt.Errorf("error sending request: %s", err)
		return responsesMatch, req, apiResp, err
	}

	expectedResponse, err := testCase.loadResponse()
	if err != nil {
		testCase.LogReq(req)
		err = fmt.Errorf("error loading response: %s", err)
		return responsesMatch, req, apiResp, err
	}
	apiResp.Format = expectedResponse.Format

	if testCase.ResponseData != nil || testCase.CollectResponse != nil ||
		len(testCase.BreakResponse) > 0 || len(testCase.StoreResponse) > 0 {
		apiRespJsonString, err = apiResp.ServerResponseToJsonString(false)
		if err != nil {
			testCase.LogReq(req)
			err = fmt.Errorf("error getting json from response: %s", err)
			return responsesMatch, req, apiResp, err
		}
	}

	// Store in custom store
	err = testCase.dataStore.SetWithQjson(apiRespJsonString, testCase.StoreResponse)
	if err != nil {
		testCase.LogReq(req)
		err = fmt.Errorf("error store response with qjson: %s", err)
		return responsesMatch, req, apiResp, err
	}

	if !req.DoNotStore {
		// Store in datastore -1 list
		if counter == 0 {
			testCase.dataStore.AppendResponse(apiRespJsonString)
		} else {
			testCase.dataStore.UpdateLastResponse(apiRespJsonString)
		}
	}

	// Compare Responses
	responsesMatch, err = testCase.responsesEqual(expectedResponse, apiResp)
	if err != nil {
		testCase.LogReq(req)
		err = fmt.Errorf("error matching responses: %s", err)
		return responsesMatch, req, apiResp, err
	}

	return responsesMatch, req, apiResp, nil
}

// LogResp print the response to the console
func (testCase Case) LogResp(response api.Response) {
	errString := fmt.Sprintf("[RESPONSE]:\n%s\n\n", limitLines(response.ToString(), Config.Apitest.Limit.Response))

	if !testCase.ReverseTestResult && testCase.LogNetwork != nil && !*testCase.LogNetwork && !testCase.ContinueOnFailure {
		testCase.ReportElem.SaveToReportLogF(errString)
		logrus.Debug(errString)
	}
}

// LogReq print the request to the console
func (testCase Case) LogReq(req api.Request) {
	errString := fmt.Sprintf("[REQUEST]:\n%s\n\n", limitLines(req.ToString(logCurl), Config.Apitest.Limit.Request))

	if !testCase.ReverseTestResult && !testCase.ContinueOnFailure && testCase.LogNetwork != nil && *testCase.LogNetwork == false {
		testCase.ReportElem.SaveToReportLogF(errString)
		logrus.Debug(errString)
	}
}

func (testCase Case) sendEmail() error {
	var (
		email Email
		err   error
	)

	// Load email
	email, err = testCase.loadEmail()
	if err != nil {
		return fmt.Errorf("error loading email: %s", err)
	}

	auth := sasl.NewPlainClient("", email.Auth.Username, email.Auth.Password)
	msg := strings.NewReader(email.Body)
	
	err = smtp.SendMail(util.PolyfillLocalhost(email.Addr), auth, email.Sender, email.Recipients, msg)
	if err != nil {
		return fmt.Errorf("error sending email: %s", err)
	}
	return nil
}

func limitLines(in string, limitCount int) string {
	if limitCount <= 0 {
		return in
	}
	out := ""
	scanner := bufio.NewScanner(strings.NewReader(in))
	k := 0
	for scanner.Scan() && k < limitCount {
		out += scanner.Text() + "\n"
		k++
	}
	if k >= limitCount {
		out += fmt.Sprintf("[Limited after %d lines]", limitCount)
	}
	return out
}

func (testCase Case) run() (bool, error) {
	var (
		responsesMatch compare.CompareResult
		request        api.Request
		apiResponse    api.Response
		timedOutFlag   bool
		err            error
	)

	startTime := time.Now()
	r := testCase.ReportElem
	requestCounter := 0
	collectPresent := testCase.CollectResponse != nil

	if testCase.WaitBefore != nil {
		logrus.Infof("wait_before_ms: %d", *testCase.WaitBefore)
		time.Sleep(time.Duration(*testCase.WaitBefore) * time.Millisecond)
	}

	//Poll repeats the request until the right response is found, or a timeout triggers
	for {
		// delay between repeating a request
		if testCase.Delay != nil {
			time.Sleep(time.Duration(*testCase.Delay) * time.Millisecond)
		}

		responsesMatch, request, apiResponse, err = testCase.executeRequest(requestCounter)
		if testCase.LogNetwork != nil && *testCase.LogNetwork {
			logrus.Debugf("[RESPONSE]:\n%s\n\n", limitLines(apiResponse.ToString(), Config.Apitest.Limit.Response))
		}
		if err != nil {
			testCase.LogResp(apiResponse)
			return false, err
		}

		if responsesMatch.Equal && !collectPresent {
			break
		}

		breakPresent, err := testCase.breakResponseIsPresent(request, apiResponse)
		if err != nil {
			testCase.LogReq(request)
			testCase.LogResp(apiResponse)
			return false, fmt.Errorf("error checking for break response: %s", err)
		}

		if breakPresent {
			testCase.LogReq(request)
			testCase.LogResp(apiResponse)
			return false, fmt.Errorf("Break response found")
		}

		collectLeft, err := testCase.checkCollectResponse(request, apiResponse)
		if err != nil {
			testCase.LogReq(request)
			testCase.LogResp(apiResponse)
			return false, fmt.Errorf("error checking for continue response: %s", err)
		}

		if collectPresent && collectLeft <= 0 {
			break

		}

		//break if timeout or we do not have a repeater
		if timedOut := time.Now().Sub(startTime) > (time.Duration(testCase.Timeout) * time.Millisecond); timedOut && testCase.Timeout != -1 {
			if timedOut && testCase.Timeout > 0 {
				logrus.Warnf("Pull Timeout '%dms' exceeded", testCase.Timeout)
				r.SaveToReportLogF("Pull Timeout '%dms' exceeded", testCase.Timeout)
				timedOutFlag = true
			}
			break
		}

		requestCounter++
	}

	if !responsesMatch.Equal || timedOutFlag {
		if !testCase.ReverseTestResult {
			for _, v := range responsesMatch.Failures {
				logrus.Errorf("[%s] %s", v.Key, v.Message)
				r.SaveToReportLog(fmt.Sprintf("[%s] %s", v.Key, v.Message))
			}
		}

		collectArray, ok := testCase.CollectResponse.(util.JsonArray)
		if ok {
			for _, v := range collectArray {
				jsonV, err := json.Marshal(v)
				if err != nil {
					testCase.LogReq(request)
					testCase.LogResp(apiResponse)
					return false, err
				}
				logrus.Errorf("Collect response not found: %s", jsonV)
				r.SaveToReportLog(fmt.Sprintf("Collect response not found: %s", jsonV))
			}
		}

		testCase.LogReq(request)
		testCase.LogResp(apiResponse)
		return false, nil
	}

	if testCase.WaitAfter != nil {
		logrus.Infof("wait_after_ms: %d", *testCase.WaitAfter)
		time.Sleep(time.Duration(*testCase.WaitAfter) * time.Millisecond)
	}

	return true, nil
}

func (testCase Case) loadRequest() (api.Request, error) {
	req, err := testCase.loadRequestSerialization()
	if err != nil {
		return req, fmt.Errorf("error loadRequestSerialization: %s", err)
	}

	return req, err
}

func (testCase Case) loadResponse() (api.Response, error) {
	var (
		res api.Response
		err error
	)

	// unspecified response is interpreted as status_code 200
	if testCase.ResponseData == nil {
		return api.NewResponse(200, nil, bytes.NewReader([]byte("")), nil, res.Format)
	}
	spec, err := testCase.loadResponseSerialization(testCase.ResponseData)
	if err != nil {
		return res, fmt.Errorf("error loading response spec: %s", err)
	}
	res, err = api.NewResponseFromSpec(spec)
	if err != nil {
		return res, fmt.Errorf("error creating response from spec: %s", err)
	}
	return res, nil
}

func (testCase Case) loadEmail() (Email, error) {
	req, err := testCase.loadEmailSerialization()
	if err != nil {
		return req, fmt.Errorf("error loadEmailSerialization: %s", err)
	}

	return req, err
}

func (testCase Case) responsesEqual(expected, got api.Response) (compare.CompareResult, error) {
	expectedJSON, err := expected.ToGenericJSON()
	if err != nil {
		return compare.CompareResult{}, fmt.Errorf("error loading expected generic json: %s", err)
	}
	if testCase.ResponseData == nil && testCase.CollectResponse == nil && len(testCase.BreakResponse) == 0 {
		expected.Format.IgnoreBody = true
	}
	gotJSON, err := got.ServerResponseToGenericJSON(expected.Format, false)
	if err != nil {
		return compare.CompareResult{}, fmt.Errorf("error loading response generic json: %s", err)
	}
	return compare.JsonEqual(expectedJSON, gotJSON, compare.ComparisonContext{})
}

func (testCase Case) loadRequestSerialization() (api.Request, error) {
	var (
		spec api.Request
	)

	_, requestData, err := template.LoadManifestDataAsObject(*testCase.RequestData, testCase.manifestDir, testCase.loader)
	if err != nil {
		return spec, fmt.Errorf("error loading request data: %s", err)
	}
	specBytes, err := json.Marshal(requestData)
	if err != nil {
		return spec, fmt.Errorf("error marshaling req: %s", err)
	}
	err = cjson.Unmarshal(specBytes, &spec)
	spec.ManifestDir = testCase.manifestDir
	spec.DataStore = testCase.dataStore

	if spec.ServerURL == "" {
		spec.ServerURL = testCase.ServerURL
	}
	if len(spec.Headers) == 0 {
		spec.Headers = make(map[string]*string, 0)
	}
	for k, v := range testCase.standardHeader {
		if spec.Headers[k] == nil {
			spec.Headers[k] = v
		}
	}

	if len(spec.HeaderFromStore) == 0 {
		spec.HeaderFromStore = make(map[string]string, 0)
	}
	for k, v := range testCase.standardHeaderFromStore {
		if spec.HeaderFromStore[k] == "" {
			spec.HeaderFromStore[k] = v
		}
	}

	return spec, nil
}

func (testCase Case) loadResponseSerialization(genJSON interface{}) (api.ResponseSerialization, error) {
	var (
		spec api.ResponseSerialization
	)

	_, responseData, err := template.LoadManifestDataAsObject(genJSON, testCase.manifestDir, testCase.loader)
	if err != nil {
		return spec, fmt.Errorf("error loading response data: %s", err)
	}

	specBytes, err := json.Marshal(responseData)
	if err != nil {
		return spec, fmt.Errorf("error marshaling res: %s", err)
	}
	err = cjson.Unmarshal(specBytes, &spec)
	if err != nil {
		return spec, fmt.Errorf("error unmarshaling res: %s", err)
	}

	// the body must not be parsed if it is not expected in the response, or should not be stored
	if spec.Body == nil && len(testCase.StoreResponse) < 1 {
		spec.Format.IgnoreBody = true
	}

	return spec, nil
}

func (testCase Case) loadEmailSerialization() (Email, error) {
	var (
		email Email
	)

	_, emailData, err := template.LoadManifestDataAsObject(*testCase.SendEmailData, testCase.manifestDir, testCase.loader)
	if err != nil {
		return email, fmt.Errorf("error loading send email data: %s", err)
	}
	emailBytes, err := json.Marshal(emailData)
	if err != nil {
		return email, fmt.Errorf("error marshaling send email data: %s", err)
	}
	err = cjson.Unmarshal(emailBytes, &email)
	if err != nil {
		return email, fmt.Errorf("error unmarshaling send email data: %s", err)
	}

	return email, nil
}
