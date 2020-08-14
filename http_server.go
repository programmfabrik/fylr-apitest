package main

import (
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"
	"unicode/utf8"

	"github.com/sirupsen/logrus"
)

// StartHttpServer start a simple http server that can server local test resources during the testsuite is running
func (ats *Suite) StartHttpServer() {

	if ats.HttpServer == nil {
		return
	}

	ats.idleConnsClosed = make(chan struct{})
	mux := http.NewServeMux()

	if ats.HttpServer.Dir == "" {
		ats.httpServerDir = ats.manifestDir
	} else {
		ats.httpServerDir = filepath.Clean(ats.manifestDir + "/" + ats.HttpServer.Dir)
	}
	mux.Handle("/", customStaticHandler(http.FileServer(http.Dir(ats.httpServerDir))))

	// bounce json response
	mux.HandleFunc("/bounce-json", bounceJSON)

	// bounce binary response with information in headers
	mux.HandleFunc("/bounce", bounceBinary)

	// Start listening into proxy
	ats.httpServerProxy = NewProxy(ats.HttpServer.Proxy)
	ats.httpServerProxy.Listen(mux, "/proxy")

	// Add SMTP proxy if defined
	if ats.SMTPServer != nil {
		ats.SMTPServer.Proxy.Listen(mux, "/proxy")
	}

	ats.httpServer = http.Server{
		Addr:    ats.HttpServer.Addr,
		Handler: mux,
	}

	run := func() {
		logrus.Infof("Starting HTTP Server: %s: %s", ats.HttpServer.Addr, ats.httpServerDir)

		err := ats.httpServer.ListenAndServe()
		if err != http.ErrServerClosed {
			// Error starting or closing listener:
			logrus.Errorf("HTTP server ListenAndServe: %v", err)
			return
		}
	}

	if ats.HttpServer.Testmode {
		// Run in foreground to test
		logrus.Infof("Testmode for HTTP Server. Listening, not running tests...")
		run()
	} else {
		go run()
	}
}

// customStaticHandler can perform some operations before passing into final handler
func customStaticHandler(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qs := r.URL.Query()
		// We try not to include Content-Length header here
		// As ultimately the default FileServer implementation will override all of them
		// After diving into its code, the only way to avoid it is setting Content-Encoding header to some value
		// In this case, 'identity', as per RFC 7231 / RFC 2616, means no compression or modification
		noContentLengthHeader := qs.Get("no-content-length")
		if noContentLengthHeader == "1" || noContentLengthHeader == "true" {
			w.Header().Set("Content-Encoding", "identity")
		} 
		h.ServeHTTP(w, r)
	}
}

// StopHttpServer stop the http server that was started for this test suite
func (ats *Suite) StopHttpServer() {

	if ats.HttpServer == nil {
		return
	}

	err := ats.httpServer.Shutdown(context.Background())
	if err != nil {
		// Error from closing listeners, or context timeout:
		logrus.Errorf("HTTP server Shutdown: %v", err)
		close(ats.idleConnsClosed)
		<-ats.idleConnsClosed
	} else {
		logrus.Infof("Http Server stopped: %s", ats.httpServerDir)
	}
	return
}

type ErrorResponse struct {
	Error string      `json:"error"`
	Body  interface{} `json:"body,omitempty"`
}

func errorResponse(w http.ResponseWriter, statuscode int, err error, body interface{}) {
	resp := ErrorResponse{
		Error: err.Error(),
		Body:  body,
	}

	b, err2 := json.MarshalIndent(resp, "", "  ")
	if err2 != nil {
		logrus.Debugf("Could not marshall error message %s: %s", err, err2)
		http.Error(w, err2.Error(), 500)
	}

	http.Error(w, string(b), statuscode)
}

type BounceResponse struct {
	Header      http.Header `json:"header"`
	QueryParams url.Values  `json:"query_params"`
	Body        interface{} `json:"body"`
}

// bounceJSON builds a json response including the header, query params and body of the request
func bounceJSON(w http.ResponseWriter, r *http.Request) {

	var (
		err       error
		bodyBytes []byte
		bodyJSON  interface{}
		errorBody interface{}
	)

	bodyBytes, err = ioutil.ReadAll(r.Body)

	if utf8.Valid(bodyBytes) {
		if len(bodyBytes) > 0 {
			errorBody = string(bodyBytes)
		} else {
			errorBody = nil
		}
	} else {
		errorBody = bodyBytes
	}

	if err != nil {
		errorResponse(w, 500, err, errorBody)
		return
	}

	err = json.Unmarshal(bodyBytes, &bodyJSON)
	if err != nil {
		errorResponse(w, 500, err, errorBody)
		return
	}

	response := BounceResponse{
		Header:      r.Header,
		QueryParams: r.URL.Query(),
		Body:        bodyJSON,
	}

	responseData, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		errorResponse(w, 500, err, response)
		return
	}

	w.Write(responseData)
}

// bounceBinary returns the request in binary form
func bounceBinary(w http.ResponseWriter, r *http.Request) {

	for param, values := range r.URL.Query() {
		for _, value := range values {
			w.Header().Add("X-Req-Query-"+param, value)
		}
	}

	for param, values := range r.Header {
		for _, value := range values {
			w.Header().Add("X-Req-Header-"+param, value)
		}
	}

	io.Copy(w, r.Body)
}
