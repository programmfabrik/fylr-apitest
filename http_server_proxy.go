package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
)

// HTTPServerProxyStoreMode definition
type HTTPServerProxyStoreMode string
const (
	// HTTPServerProxyStoreModePassthrough mode
	HTTPServerProxyStoreModePassthrough HTTPServerProxyStoreMode = "passthru"
)

// HTTPServerProxyStoreRequestData definition
type HTTPServerProxyStoreRequestData struct {
	Method string `json:"method"`
	Headers http.Header `json:"header"`
	Query url.Values `json:"query"`
	Body interface{} `json:"body"`
}

// HTTPServerProxyStoreResponseData definition
type HTTPServerProxyStoreResponseData struct {
	StatusCode int `json:"statuscode"`
	Headers http.Header `json:"header"`
	Body interface{} `json:"body"`
}

// HTTPServerProxyStoreDataEntry definition
type HTTPServerProxyStoreDataEntry struct {
	Offset int `json:"offset"`
	Request HTTPServerProxyStoreRequestData `json:"request"`
	Response HTTPServerProxyStoreResponseData `json:"response"`
}

// HTTPServerProxyStore definition
type HTTPServerProxyStore struct {
	Name string `json:"name"`
	Mode HTTPServerProxyStoreMode `json:"mode"`
	Data []HTTPServerProxyStoreDataEntry
}

// HTTPServerProxyStoreResponse definition
type HTTPServerProxyStoreResponse struct {
	Mode HTTPServerProxyStoreMode `json:"mode"`
	Offset int `json:"offset"`
	NextOffset int `json:"next_offset"`
	Limit int `json:"limit"`
	Count int `json:"count"`
	Store []HTTPServerProxyStoreDataEntry `json:"store"`
}

// HTTPServerProxy definition
type HTTPServerProxy map[string]HTTPServerProxyStore

// Listen for the proxy store/retrieve
func (proxy *HTTPServerProxy) Listen(mux *http.ServeMux, prefix string) {
	for p, s := range *proxy {
		store := HTTPServerProxyStore{Name: p, Mode: s.Mode, Data: []HTTPServerProxyStoreDataEntry{}}
		mux.HandleFunc(fmt.Sprintf("%s/%s", prefix, p), store.write)
		mux.HandleFunc(fmt.Sprintf("%sstore/%s", prefix, p), store.read)
	}
}

// write stores incoming request data
func (store *HTTPServerProxyStore) write(w http.ResponseWriter, r *http.Request) {

	reqData := HTTPServerProxyStoreRequestData{
		Method: r.Method,
		Headers: r.Header,
		Query: r.URL.Query(),
	}

	if r.Body != nil {
		bin, err := ioutil.ReadAll(r.Body)
		if err == nil {
			json.Unmarshal(bin, &reqData.Body)
		}	
	}

	resData := HTTPServerProxyStoreResponseData{
		StatusCode: http.StatusOK,
	}

	offset := len(store.Data)

	store.Data = append(store.Data, HTTPServerProxyStoreDataEntry{offset, reqData, resData})

	w.WriteHeader(resData.StatusCode)
	if resData.Body != nil {
		json.NewEncoder(w).Encode(resData.Body)
	}
}

// read reads existing requests stored data
func (store *HTTPServerProxyStore) read(w http.ResponseWriter, r *http.Request) {

	var (
		offset, limit int
	)
	q := r.URL.Query()
	
	offsetStr := q.Get("offset")
	if offsetStr != "" {
		off, err := strconv.Atoi(offsetStr)
		if err == nil {
			offset = off
		}
	}

	limit = 10
	limitStr := q.Get("limit")
	if limitStr != "" {
		lim, err := strconv.Atoi(limitStr)
		if err == nil {
			limit = lim
		}
	}
	
	count := len(store.Data)
	if limit == 0 {
		limit = count
	}
	max := offset + limit
	nextOffset := max
	if max > count {
		max = count
		nextOffset = 0
	}

	res := HTTPServerProxyStoreResponse{
		Mode: store.Mode,
		Offset: offset,
		NextOffset: nextOffset,
		Limit: limit,
		Count: count,
	}

	data := []HTTPServerProxyStoreDataEntry{}
	if offset < count {
		data = store.Data[offset:max]
	}
	res.Store = data

	json.NewEncoder(w).Encode(res)
}
