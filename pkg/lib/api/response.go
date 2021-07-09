package api

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/clbanning/mxj"
	"github.com/pkg/errors"

	"github.com/programmfabrik/apitest/pkg/lib/util"
	"github.com/programmfabrik/go-csvx"
)

type Response struct {
	statusCode  int
	headers     map[string][]string
	cookies     []*http.Cookie
	body        []byte
	bodyControl util.JsonObject
	Format      ResponseFormat
}

// Cookie definition
type Cookie struct {
	Name     string        `json:"name"`
	Value    string        `json:"value"`
	Path     string        `json:"path,omitempty"`
	Domain   string        `json:"domain,omitempty"`
	Expires  time.Time     `json:"expires,omitempty"`
	MaxAge   int           `json:"max_age,omitempty"`
	Secure   bool          `json:"secure,omitempty"`
	HttpOnly bool          `json:"http_only,omitempty"`
	SameSite http.SameSite `json:"same_site,omitempty"`
}

type ResponseSerialization struct {
	StatusCode  int                 `yaml:"statuscode" json:"statuscode"`
	Headers     map[string][]string `yaml:"header" json:"header,omitempty"`
	Cookies     map[string]Cookie   `yaml:"cookie" json:"cookie,omitempty"`
	Body        interface{}         `yaml:"body" json:"body,omitempty"`
	BodyControl util.JsonObject     `yaml:"body:control" json:"body:control,omitempty"`
	Format      ResponseFormat      `yaml:"format" json:"format,omitempty"`
}

type ResponseFormat struct {
	IgnoreBody bool   `json:"-"`    // if true, do not try to parse the body (since it is not expected in the response)
	Type       string `json:"type"` // default "json", allowed: "csv", "json", "xml", "binary"
	CSV        struct {
		Comma string `json:"comma,omitempty"`
	} `json:"csv,omitempty"`
	PreProcess *PreProcess `json:"pre_process,omitempty"`
}

func NewResponse(statusCode int, headers map[string][]string, cookies []*http.Cookie, body io.Reader, bodyControl util.JsonObject, bodyFormat ResponseFormat) (res Response, err error) {
	bodyBytes, err := ioutil.ReadAll(body)
	if err != nil {
		return res, err
	}
	return Response{statusCode: statusCode, headers: headers, cookies: cookies, body: bodyBytes, bodyControl: bodyControl, Format: bodyFormat}, nil
}

func NewResponseFromSpec(spec ResponseSerialization) (res Response, err error) {
	bodyBytes, err := json.Marshal(spec.Body)
	if err != nil {
		return res, err
	}
	// if statuscode is not explicitly set; we assume 200
	if spec.StatusCode == 0 {
		spec.StatusCode = 200
	}

	// Build standard cookies bag from spec map
	var cookies []*http.Cookie
	if len(spec.Cookies) > 0 {
		cookies = make([]*http.Cookie, 0)
		for _, rck := range spec.Cookies {
			cookies = append(cookies, &http.Cookie{
				Name:     rck.Name,
				Value:    rck.Value,
				Path:     rck.Path,
				Domain:   rck.Domain,
				Expires:  rck.Expires,
				MaxAge:   rck.MaxAge,
				Secure:   rck.Secure,
				HttpOnly: rck.HttpOnly,
				SameSite: rck.SameSite,
			})
		}
	}

	return NewResponse(spec.StatusCode, spec.Headers, cookies, bytes.NewReader(bodyBytes), spec.BodyControl, spec.Format)
}

// ServerResponseToGenericJSON parse response from server. convert xml, csv, binary to json if necessary
func (response Response) ServerResponseToGenericJSON(responseFormat ResponseFormat, bodyOnly bool) (interface{}, error) {
	var (
		res, bodyJSON interface{}
		bodyData      []byte
		err           error
		resp          Response
	)

	if responseFormat.PreProcess != nil {
		resp, err = responseFormat.PreProcess.RunPreProcess(response)
		if err != nil {
			return res, errors.Wrap(err, "Could not pre process response")
		}
	} else {
		resp = response
	}

	switch responseFormat.Type {
	case "xml":
		xmlDeclarationRegex := regexp.MustCompile(`<\?xml.*?\?>`)
		replacedXML := xmlDeclarationRegex.ReplaceAll(resp.Body(), []byte{})

		mv, err := mxj.NewMapXmlSeq(replacedXML)
		if err != nil {
			return res, errors.Wrap(err, "Could not parse xml")
		}

		bodyData, err = mv.JsonIndent("", " ")
		if err != nil {
			return res, errors.Wrap(err, "Could not marshal xml to json")
		}
	case "csv":
		csvData, err := csvx.NewCSV(',', '#', true).ToTypedMap(resp.Body())
		if err != nil {
			return res, errors.Wrap(err, "Could not parse csv data")
		}

		bodyData, err = json.Marshal(csvData)
		if err != nil {
			return res, errors.Wrap(err, "Could not marshal csv to json")
		}
	case "binary":
		// We have another file format (binary). We thereby take the md5 Hash of the body and compare that one
		hasher := md5.New()
		hasher.Write([]byte(resp.Body()))
		JsonObject := util.JsonObject{
			"md5sum": util.JsonString(hex.EncodeToString(hasher.Sum(nil))),
		}
		bodyData, err = json.Marshal(JsonObject)
		if err != nil {
			return res, errors.Wrap(err, "Could not marshal body with md5sum to json")
		}
	case "":
		// no specific format, we assume a json, and thereby try to unmarshal it into our body
		bodyData = resp.Body()
	default:
		return res, fmt.Errorf("Invalid response format '%s'", responseFormat.Type)
	}

	responseJSON := ResponseSerialization{
		StatusCode: resp.statusCode,
	}
	if len(resp.headers) > 0 {
		responseJSON.Headers = resp.headers
	}
	// Build cookies map from standard bag
	if len(resp.cookies) > 0 {
		responseJSON.Cookies = make(map[string]Cookie)
		for _, ck := range resp.cookies {
			if ck != nil {
				responseJSON.Cookies[ck.Name] = Cookie{
					Name:     ck.Name,
					Value:    ck.Value,
					Path:     ck.Path,
					Domain:   ck.Domain,
					Expires:  ck.Expires,
					MaxAge:   ck.MaxAge,
					Secure:   ck.Secure,
					HttpOnly: ck.HttpOnly,
					SameSite: ck.SameSite,
				}
			}
		}
	}

	// if the body should not be ignored, serialize the parsed/converted body
	if !responseFormat.IgnoreBody {

		if len(bodyData) > 0 {
			err = json.Unmarshal(bodyData, &bodyJSON)
			if err != nil {
				return res, err
			}
		}

		if bodyOnly {
			return bodyJSON, nil
		}

		responseJSON.Body = &bodyJSON
	}

	responseBytes, err := json.Marshal(responseJSON)
	if err != nil {
		return res, err
	}
	json.Unmarshal(responseBytes, &res)

	return res, nil
}

// ToGenericJSON parse expected response
func (response Response) ToGenericJSON() (interface{}, error) {
	var (
		bodyJSON, res interface{}
		err           error
	)

	// We have a json, and thereby try to unmarshal it into our body
	resBody := response.Body()
	if len(resBody) > 0 {
		err = json.Unmarshal(resBody, &bodyJSON)
		if err != nil {
			return res, err
		}
	}

	responseJSON := ResponseSerialization{
		StatusCode:  response.statusCode,
		BodyControl: response.bodyControl,
	}
	if len(response.headers) > 0 {
		responseJSON.Headers = response.headers
	}

	// Build cookies map from standard bag
	if len(response.cookies) > 0 {
		responseJSON.Cookies = make(map[string]Cookie)
		for _, ck := range response.cookies {
			if ck != nil {
				responseJSON.Cookies[ck.Name] = Cookie{
					Name:     ck.Name,
					Value:    ck.Value,
					Path:     ck.Path,
					Domain:   ck.Domain,
					Expires:  ck.Expires,
					MaxAge:   ck.MaxAge,
					Secure:   ck.Secure,
					HttpOnly: ck.HttpOnly,
					SameSite: ck.SameSite,
				}
			}
		}
	}

	// necessary because check for <nil> against missing body would fail, but must succeed
	if bodyJSON != nil {
		responseJSON.Body = &bodyJSON
	}

	responseBytes, err := json.Marshal(responseJSON)
	if err != nil {
		return res, err
	}
	json.Unmarshal(responseBytes, &res)

	return res, nil
}

func (response Response) ServerResponseToJsonString(bodyOnly bool) (string, error) {
	genericJSON, err := response.ServerResponseToGenericJSON(response.Format, bodyOnly)
	if err != nil {
		return "", fmt.Errorf("error formatting response: %s", err)
	}
	bytes, err := json.MarshalIndent(genericJSON, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error formatting response: %s", err)
	}
	return string(bytes), nil
}

func (response Response) Body() []byte {
	// some endpoints return empty strings;
	// since that is no valid json so we interpret it as the json null literal to
	// establish the invariant that api endpoints return json responses

	// hmmm, does not really make sense
	// shouldn't a byte array always be checked
	// before actually attempting to decode it?
	// if bytes.Compare(response.body, []byte("")) == 0 {
	// 	return []byte("null")
	// }
	return response.body
}

func (response *Response) marshalBodyInto(target interface{}) (err error) {
	bodyBytes := response.Body()
	if len(bodyBytes) > 0 {
		if err = json.Unmarshal(bodyBytes, target); err != nil {
			return fmt.Errorf("error unmarshaling response: %s", err)
		}
	}
	return nil
}

func (response Response) ToString() string {
	var (
		headersString string
		bodyString    string
		err           error
		resp          Response
	)

	for k, v := range response.headers {
		value := ""
		for _, iv := range v {
			value = fmt.Sprintf("%s %s", value, iv)
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		headersString = fmt.Sprintf("%s\n%s:%s", headersString, k, value)
	}

	if response.Format.PreProcess != nil {
		resp, err = response.Format.PreProcess.RunPreProcess(response)
		if err != nil {
			resp = response
		}
	} else {
		resp = response
	}

	// for logging, always show the body
	resp.Format.IgnoreBody = false

	body := resp.Body()
	switch resp.Format.Type {
	case "xml", "csv":
		if utf8.Valid(body) {
			bodyString, err = resp.ServerResponseToJsonString(true)
			if err != nil {
				bodyString = string(body)
			}
		} else {
			bodyString = fmt.Sprintf("[BINARY DATA NOT DISPLAYED]\n\n")
		}
	case "binary":
		resp.Format.IgnoreBody = false
		bodyString, err = resp.ServerResponseToJsonString(true)
		if err != nil {
			bodyString = string(body)
		}
	default:
		if utf8.Valid(body) {
			bodyString = string(body)
		} else {
			bodyString = fmt.Sprintf("[BINARY DATA NOT DISPLAYED]\n\n")
		}
	}

	return fmt.Sprintf("%d\n%s\n\n%s", resp.statusCode, headersString, bodyString)
}
