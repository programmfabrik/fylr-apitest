package template

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"text/template"

	"github.com/programmfabrik/apitest/pkg/lib/datastore"

	"github.com/programmfabrik/apitest/pkg/lib/cjson"
	"github.com/programmfabrik/apitest/pkg/lib/csv"
	"github.com/programmfabrik/apitest/pkg/lib/util"

	"io/ioutil"
	"path/filepath"

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
		"server_url": func() *url.URL {
			return loader.ServerURL
		},
		"int_range": func(start, end int64) []int64 {
			n := end - start
			result := make([]int64, n)
			var i int64
			for i = 0; i < n; i++ {
				result[i] = start + i
			}
			return result
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
