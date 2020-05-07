package main

import (
	"context"
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"golang.org/x/xerrors"
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
	mux.Handle("/", http.FileServer(http.Dir(ats.httpServerDir)))

	// read the file at query param 'file' and return it as the response body
	mux.HandleFunc("/load-file", func(w http.ResponseWriter, r *http.Request) {
		loadFile(w, r, ats.httpServerDir)
	})

	// bounce json response
	mux.HandleFunc("/bounce-json", bounceJSON)

	// bounce binary response with information in headers
	mux.HandleFunc("/bounce", bounceBinary)

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
	Error string `json:"error"`
}

func errorResponse(w http.ResponseWriter, statuscode int, err error) {
	resp := ErrorResponse{
		Error: err.Error(),
	}

	b, err2 := json.MarshalIndent(resp, "", "  ")
	if err2 != nil {
		logrus.Debugf("Could not marshall error message %s: %s", err, err2)
		http.Error(w, err2.Error(), 500)
	}

	http.Error(w, string(b), statuscode)
}

// loadFile reads the file at query param 'path' and returns it as the response body
func loadFile(w http.ResponseWriter, r *http.Request, dir string) {
	fn := r.URL.Query().Get("file")
	if fn == "" {
		errorResponse(w, 400, xerrors.Errorf("file not found in query_params"))
		return
	}

	http.ServeFile(w, r, dir+"/"+fn)
}

type BounceResponse struct {
	Header      map[string][]string `json:"header"`
	QueryParams url.Values          `json:"query_params"`
	Body        interface{}         `json:"body"`
}

// bounceJSON builds a json response including the header, query params and body of the request
func bounceJSON(w http.ResponseWriter, r *http.Request) {

	var (
		err       error
		bodyBytes []byte
		bodyJSON  interface{}
	)

	bodyBytes, err = ioutil.ReadAll(r.Body)
	if err != nil {
		errorResponse(w, 500, xerrors.Errorf("bounce-json: could not read body: %s", err))
		return
	}

	err = json.Unmarshal(bodyBytes, &bodyJSON)
	if err != nil {
		errorResponse(w, 500, xerrors.Errorf("bounce-json: could not unmarshal body: %s", err))
		return
	}

	response := BounceResponse{
		Header:      r.Header,
		QueryParams: r.URL.Query(),
		Body:        bodyJSON,
	}

	responseData, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		errorResponse(w, 500, xerrors.Errorf("bounce-json: could not marshal response: %s", err))
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
