package template

import (
	"bytes"
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"text/template"

	"github.com/pkg/errors"
	"github.com/programmfabrik/apitest/pkg/lib/datastore"

	"github.com/programmfabrik/apitest/pkg/lib/cjson"
	"github.com/programmfabrik/apitest/pkg/lib/csv"
	"github.com/programmfabrik/apitest/pkg/lib/util"

	"io/ioutil"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"github.com/tidwall/gjson"
)

/*
Hack to dynamically pass parameters as context to a nested template load call alá
{{ load_file path Param1 Param2 .... }} with a file at path that loads a template like "something = {{ .Param1 }}"
*/
type templateParams0 struct{}

type templateParams1 struct {
	Param1 interface{}
}
type templateParams2 struct {
	Param1 interface{}
	Param2 interface{}
}
type templateParams3 struct {
	Param1 interface{}
	Param2 interface{}
	Param3 interface{}
}

type templateParams4 struct {
	Param1 interface{}
	Param2 interface{}
	Param3 interface{}
	Param4 interface{}
}

func newTemplateParams(params []interface{}) (interface{}, error) {
	switch len(params) {
	case 0:
		return templateParams0{}, nil
	case 1:
		return templateParams1{Param1: params[0]}, nil
	case 2:
		return templateParams2{Param1: params[0], Param2: params[1]}, nil
	case 3:
		return templateParams3{Param1: params[0], Param2: params[1], Param3: params[2]}, nil
	case 4:
		return templateParams4{Param1: params[0], Param2: params[1], Param3: params[2], Param4: params[3]}, nil
	default:
		return templateParams0{}, fmt.Errorf("newParams only supports up to 4 parameters")
	}
}

type Loader struct {
	datastore      *datastore.Datastore
	HTTPServerHost string
	ServerURL      *url.URL
	OAuthClient    util.OAuthClientsConfig
}

func NewLoader(datastore *datastore.Datastore) Loader {
	return Loader{datastore: datastore}
}

func (loader *Loader) Render(
	tmplBytes []byte,
	rootDir string,
	ctx interface{}) (res []byte, err error) {

	//Remove comments from template

	var re = regexp.MustCompile(`(?m)^[\t ]*(#|//).*$`)
	tmplBytes = []byte(re.ReplaceAllString(string(tmplBytes), `$1`))

	var funcMap template.FuncMap

	funcMap = template.FuncMap{
		"qjson": func(path string, json string) (result string, err error) {
			if json == "" {
				err = fmt.Errorf("The given json was empty")
				return
			}

			result = gjson.Get(json, path).Raw
			if len(result) == 0 {
				err = fmt.Errorf("'%s' was not found or was empty string. Qjson Input: %s", path, json)
			}
			return
		},
		"split": func(s, sep string) []string {
			return strings.Split(s, sep)
		},
		"md5sum": func(path string) (string, error) {
			_, file, err := util.OpenFileOrUrl(path, rootDir)
			if err != nil {
				return "", err
			}

			fileBytes, err := ioutil.ReadAll(file)
			if err != nil {
				return "", err
			}

			hasher := md5.New()
			hasher.Write([]byte(fileBytes))
			return hex.EncodeToString(hasher.Sum(nil)), nil
		},
		"file": func(path string, params ...interface{}) (string, error) {
			tmplParams, err := newTemplateParams(params)
			if err != nil {
				return "", err
			}

			_, file, err := util.OpenFileOrUrl(path, rootDir)
			if err != nil {
				return "", err
			}

			fileBytes, err := ioutil.ReadAll(file)
			if err != nil {
				return "", err
			}

			absPath := filepath.Join(rootDir, path)
			tmplBytes, err := loader.Render(fileBytes, filepath.Dir(absPath), tmplParams)
			if err != nil {
				return "", err
			}
			return string(tmplBytes), nil
		},
		"file_csv": func(path string, delimiter rune) ([]map[string]interface{}, error) {

			_, file, err := util.OpenFileOrUrl(path, rootDir)
			if err != nil {
				return nil, err
			}
			fileBytes, err := ioutil.ReadAll(file)
			if err != nil {
				return nil, err
			}
			data, err := csv.CSVToMap(fileBytes, delimiter)
			if err != nil {
				return data, fmt.Errorf("'%s' %s", path, err)
			}
			return data, err
		},
		"parse_csv": func(path string, delimiter rune) ([]map[string]interface{}, error) {
			_, file, err := util.OpenFileOrUrl(path, rootDir)
			if err != nil {
				return nil, err
			}
			fileBytes, err := ioutil.ReadAll(file)
			if err != nil {
				return nil, err
			}
			data, err := csv.GenericCSVToMap(fileBytes, delimiter)
			if err != nil {
				return data, fmt.Errorf("'%s' %s", path, err)
			}
			return data, err
		},
		"file_sqlite": func(path, statement string) ([]map[string]interface{}, error) {
			sqliteFile := filepath.Join(rootDir, path)
			database, err := sql.Open("sqlite3", sqliteFile)
			if err != nil {
				return nil, err
			}
			defer database.Close()

			rows, err := database.Query(statement)
			if err != nil {
				return nil, err
			}
			defer rows.Close()

			columns, err := rows.ColumnTypes()
			if err != nil {
				return nil, err
			}
			row := make([]interface{}, len(columns))

			data := []map[string]interface{}{}

			for rows.Next() {
				dataEntry := map[string]interface{}{}
				for idx, col := range columns {
					dataEntry[col.Name()] = new(interface{})
					row[idx] = dataEntry[col.Name()]
				}

				err = rows.Scan(row...)
				if err != nil {
					return nil, err
				}

				for idx, d := range row {
					dataEntry[columns[idx].Name()] = reflect.ValueOf(d).Elem().Interface()
				}

				data = append(data, dataEntry)
			}

			return data, nil
		},
		"file_path": func(path string) string {
			return util.LocalPath(path, rootDir)
		},
		"datastore": func(index interface{}) (interface{}, error) {
			var key string

			switch index.(type) {
			case int:
				key = fmt.Sprintf("%d", (index.(int)))
			case int64:
				key = fmt.Sprintf("%d", (index.(int64)))
			case string:
				key = index.(string)
				// all good
			default:
				return "", fmt.Errorf("datastore needs string, int, or int64 as parameter")
			}

			return loader.datastore.Get(key)
		},
		"unmarshal": func(s string) (interface{}, error) {
			var gj interface{}
			err := cjson.Unmarshal([]byte(s), &gj)
			if err != nil {
				return nil, err
			}
			return gj, nil
		},
		"N": N,
		"marshal": func(data interface{}) (string, error) {
			bytes, err := json.Marshal(data)
			if err != nil {
				return "", err
			}
			return string(bytes), nil
		},
		// return json escape string
		"str_escape": func(s string) (string, error) {
			s = strings.Replace(s, "\\", "\\\\", -1)
			return strings.Replace(s, "\"", "\\\"", -1), nil
		},
		// return json escape string
		"url_path_escape": func(s string) (string, error) {
			return url.PathEscape(s), nil
		},
		// add a + b
		"add": add,
		// subtract a - b
		"subtract": subtract,
		// multiply a * b
		"multiply": multiply,
		// divide a / b
		"divide": divide,
		// create a slice
		"slice": func(args ...interface{}) []interface{} {
			return args
		},
		"rows_to_map": func(keyColumn, valueColumn string, rowsInput interface{}) (map[string]interface{}, error) {
			return rowsToMap(keyColumn, valueColumn, getRowsFromInput(rowsInput))
		},
		"group_map_rows": func(groupColumn string, rowsInput interface{}) (map[string][]map[string]interface{}, error) {
			grouped_rows := make(map[string][]map[string]interface{}, 1000)
			rows := getRowsFromInput(rowsInput)
			for _, row := range rows {
				group_key, ok := row[groupColumn]
				if !ok {
					return nil, fmt.Errorf("Group column \"%s\" does not exist in row.", groupColumn)
				}
				switch idx := group_key.(type) {
				case string:
					_, ok := grouped_rows[idx]
					if !ok {
						grouped_rows[idx] = make([]map[string]interface{}, 0)
					}
					grouped_rows[idx] = append(grouped_rows[idx], row)
				default:
					return nil, fmt.Errorf("Group column \"%s\" needs to be int64 but is %t.", groupColumn, idx)
				}
			}
			return grouped_rows, nil
		},
		"group_rows": func(groupColumn string, rowsInput interface{}) ([][]map[string]interface{}, error) {
			grouped_rows := make([][]map[string]interface{}, 1000)
			rows := getRowsFromInput(rowsInput)

			for _, row := range rows {
				group_key, ok := row[groupColumn]
				if !ok {
					return nil, fmt.Errorf("Group column \"%s\" does not exist in row.", groupColumn)
				}
				switch idx := group_key.(type) {
				case int64:
					if idx <= 0 {
						return nil, fmt.Errorf("Group column \"%s\" needs to be >= 0 and < 1000 but is %d.", groupColumn, idx)
					}
					rows2 := grouped_rows[idx]
					if rows2 == nil {
						grouped_rows[idx] = make([]map[string]interface{}, 0)
					}
					grouped_rows[idx] = append(grouped_rows[idx], row)
				default:
					return nil, fmt.Errorf("Group column \"%s\" needs to be int64 but is %t.", groupColumn, idx)
				}
			}
			// remove empty rows
			g_rows := make([][]map[string]interface{}, 0)
			for _, row := range grouped_rows {
				if row == nil {
					continue
				}
				g_rows = append(g_rows, row)
			}
			return g_rows, nil
		},
		"match": func(regex, text string) (bool, error) {
			return regexp.Match(regex, []byte(text))
		},
		"replace_host": func(srcURL string) (string, error) {
			// If no override provided, return original one
			if loader.HTTPServerHost == "" {
				return srcURL, nil
			}
			// Parse source URL or fail
			parsedURL, err := url.Parse(srcURL)
			if err != nil {
				return "", err
			}
			parsedURL.Host = loader.HTTPServerHost
			return parsedURL.String(), nil
		},
		"server_url": func() url.URL {
			return *loader.ServerURL
		},
		"server_url_no_user": func() *url.URL {
			u := new(url.URL)
			*u = *loader.ServerURL
			u.User = nil
			return u
		},
		"is_zero": func(v interface{}) bool {
			if v == nil {
				return true
			}
			return reflect.ValueOf(v).IsZero()
		},
		"oauth2_password_token": func(client string, login string, password string) (tE oAuth2TokenExtended, err error) {
			oAuthClient, ok := loader.OAuthClient[client]
			if !ok {
				return tE, errors.Errorf("OAuth client %s not configured", client)
			}
			oAuthClient.Client = client
			return readOAuthReturnValue(oAuthClient.GetPasswordCredentialsAuthToken(login, password))

		},
		"oauth2_client_token": func(client string) (tE oAuth2TokenExtended, err error) {
			oAuthClient, ok := loader.OAuthClient[client]
			if !ok {
				return tE, errors.Errorf("OAuth client %s not configured", client)
			}
			oAuthClient.Client = client
			return readOAuthReturnValue(oAuthClient.GetClientCredentialsAuthToken())
		},
		"oauth2_code_token": func(client string, params ...string) (tE oAuth2TokenExtended, err error) {
			oAuthClient, ok := loader.OAuthClient[client]
			if !ok {
				return tE, errors.Errorf("OAuth client %s not configured", client)
			}
			oAuthClient.Client = client
			return readOAuthReturnValue(oAuthClient.GetCodeAuthToken(params...))
		},
		"oauth2_implicit_token": func(client string, params ...string) (tE oAuth2TokenExtended, err error) {
			oAuthClient, ok := loader.OAuthClient[client]
			if !ok {
				return tE, errors.Errorf("OAuth client %s not configured", client)
			}
			oAuthClient.Client = client
			return readOAuthReturnValue(oAuthClient.GetAuthToken(params...))
		},
		"oauth2_client": func(client string) (c *util.OAuthClientConfig, err error) {
			oAuthClient, ok := loader.OAuthClient[client]
			if !ok {
				return nil, errors.Errorf("OAuth client %s not configured", client)
			}
			oAuthClient.Client = client
			return &oAuthClient, nil
		},
		"oauth2_basic_auth": func(client string) (string, error) {
			oAuthClient, ok := loader.OAuthClient[client]
			if !ok {
				return "", errors.Errorf("OAuth client %s not configured", client)
			}
			oAuthClient.Client = client
			return "Basic " + base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", oAuthClient.Client, oAuthClient.Secret))), nil
		},
		"query_escape": func(in string) string {
			return url.QueryEscape(in)
		},
		"query_unescape": func(in string) string {
			out, err := url.QueryUnescape(in)
			if err != nil {
				return err.Error()
			}
			return out
		},
		"base64_encode": func(in string) string {
			return base64.StdEncoding.EncodeToString([]byte(in))
		},
		"base64_decode": func(in string) string {
			b, err := base64.StdEncoding.DecodeString(in)
			if err != nil {
				return err.Error()
			}
			return string(b)
		},
	}
	tmpl, err := template.New("tmpl").Funcs(funcMap).Parse(string(tmplBytes))
	if err != nil {
		return nil, fmt.Errorf("error loading template: %s", err)
	}

	var b []byte
	buf := bytes.NewBuffer(b)
	if err = tmpl.Execute(buf, ctx); err != nil {
		return nil, fmt.Errorf("error executing template: %s", err)
	}
	return buf.Bytes(), nil
}

func getRowsFromInput(rowsInput interface{}) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0)
	switch t := rowsInput.(type) {
	case []map[string]interface{}:
		rows = t
	case []interface{}:
		for _, v := range t {
			rows = append(rows, v.(map[string]interface{}))
		}
	}
	return rows
}
