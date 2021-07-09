package compare

import (
	"fmt"
	"testing"

	"github.com/programmfabrik/apitest/pkg/lib/util"
	go_test_utils "github.com/programmfabrik/go-test-utils"
)

func TestComparison(t *testing.T) {
	testData := []struct {
		name      string
		left      util.JsonObject
		right     util.JsonObject
		eEqual    bool
		eFailures []CompareFailure
	}{
		{
			name: "Should be equal",
			left: util.JsonObject{
				"array": util.JsonArray{
					"val2",
					"val3",
				},
			},
			right: util.JsonObject{
				"array": util.JsonArray{
					"val2",
					"val3",
				},
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "There should be any string",
			left: util.JsonObject{
				"stringerino:control": util.JsonObject{
					"must_exist": true,
					"is_string":  true,
				},
			},

			right: util.JsonObject{
				"stringerino": "not equal",
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "String matches regex",
			left: util.JsonObject{
				"stringerino:control": util.JsonObject{
					"match":     "\\d+\\..+",
					"is_string": true,
				},
			},
			right: util.JsonObject{
				"stringerino": "123.abc",
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "String does not match regex",
			left: util.JsonObject{
				"stringerino:control": util.JsonObject{
					"match":     "\\d+\\.\\d+",
					"is_string": true,
				},
			},
			right: util.JsonObject{
				"stringerino": "xyz-456",
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "stringerino",
					Message: "does not match regex '\\d+\\.\\d+'",
				},
			},
		},
		{
			name: "String match with invalid regex (must fail)",
			left: util.JsonObject{
				"stringerino:control": util.JsonObject{
					"match":     ".+[",
					"is_string": true,
				},
			},
			right: util.JsonObject{
				"stringerino": "",
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "stringerino",
					Message: "could not match regex '.+[': 'error parsing regexp: missing closing ]: `[`'",
				},
			},
		},
		{
			name: "String match tried on integer (must fail)",
			left: util.JsonObject{
				"numberino:control": util.JsonObject{
					"match": ".+",
				},
			},
			right: util.JsonObject{
				"numberino": util.JsonNumber(123),
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "numberino",
					Message: "should be 'String' for regex match but is 'Number'",
				},
			},
		},
		{
			name: "String starts with another string",
			left: util.JsonObject{
				"stringerino:control": util.JsonObject{
					"starts_with": "123.",
					"is_string":   true,
				},
			},
			right: util.JsonObject{
				"stringerino": "123.abc",
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "String does not start with another string",
			left: util.JsonObject{
				"stringerino:control": util.JsonObject{
					"starts_with": "789.",
					"is_string":   true,
				},
			},
			right: util.JsonObject{
				"stringerino": "123.abc",
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "stringerino",
					Message: "does not start with '789.'",
				},
			},
		},
		{
			name: "String ends with another string",
			left: util.JsonObject{
				"stringerino:control": util.JsonObject{
					"ends_with": ".abc",
					"is_string": true,
				},
			},
			right: util.JsonObject{
				"stringerino": "123.abc",
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "String does not end with another string",
			left: util.JsonObject{
				"stringerino:control": util.JsonObject{
					"ends_with": ".xyz",
					"is_string": true,
				},
			},
			right: util.JsonObject{
				"stringerino": "123.abc",
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "stringerino",
					Message: "does not end with '.xyz'",
				},
			},
		},
		{
			name: "There should be any number",
			left: util.JsonObject{
				"numberino:control": util.JsonObject{
					"must_exist": true,
					"is_number":  true,
				},
			},
			right: util.JsonObject{
				"numberino": util.JsonNumber(99999999),
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "There should be any bool",
			left: util.JsonObject{
				"boolerino:control": util.JsonObject{
					"must_exist": true,
					"is_bool":    true,
				},
			},
			right: util.JsonObject{
				"boolerino": util.JsonBool(false),
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "There should be any array",
			left: util.JsonObject{
				"arrayerino:control": util.JsonObject{
					"must_exist": true,
					"is_array":   true,
				},
			},
			right: util.JsonObject{
				"arrayerino": util.JsonArray(nil),
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "There should be an empty object",
			left: util.JsonObject{
				"objecterino": util.JsonObject{},
				"objecterino:control": util.JsonObject{
					"no_extra": true,
				},
			},
			right: util.JsonObject{
				"objecterino": util.JsonObject{"1": 1, "2": 2, "3": 3},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{"objecterino", `extra elements found in object`},
			},
		},
		{
			name: "There should be empty array",
			left: util.JsonObject{
				"arrayerino": util.JsonArray{},
				"arrayerino:control": util.JsonObject{
					"no_extra": true,
				},
			},
			right: util.JsonObject{
				"arrayerino": util.JsonArray{"1", "2", "3"},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{"arrayerino", `extra elements found in array`},
			},
		},
		{
			name: "There should be any object",
			left: util.JsonObject{
				"objecterino:control": util.JsonObject{
					"must_exist": true,
					"is_object":  true,
				},
			},
			right: util.JsonObject{
				"objecterino": util.JsonObject(nil),
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "Token match with wrong order",
			left: util.JsonObject{
				"tokens": util.JsonArray{
					util.JsonObject{
						"suggest": "<b>a</b>",
					},
					util.JsonObject{
						"suggest": "<b>a</b>b",
					},
					util.JsonObject{
						"suggest": "<b>a</b>bc",
					},
				},
			},
			right: util.JsonObject{
				"tokens": util.JsonArray{
					util.JsonObject{
						"suggest": "<b>a</b>",
					},
					util.JsonObject{
						"suggest": "<b>a</b>bc",
					},
					util.JsonObject{
						"suggest": "<b>a</b>b",
					},
				},
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "There should be no object",
			left: util.JsonObject{
				"objecterino:control": util.JsonObject{
					"must_not_exist": true,
				},
			},
			right:     util.JsonObject{},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "There should be no object but it exists",
			left: util.JsonObject{
				"objecterino:control": util.JsonObject{
					"must_not_exist": true,
				},
			},
			right: util.JsonObject{
				"objecterino": util.JsonObject(nil),
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "objecterino",
					Message: "was found, but should NOT exist",
				},
			},
		},
		{
			name: "There should be no deeper object but it exists",
			left: util.JsonObject{
				"it": util.JsonArray{
					util.JsonObject{
						"objecterino:control": util.JsonObject{
							"must_not_exist": true,
						},
					},
				},
			},
			right: util.JsonObject{
				"it": util.JsonArray{
					util.JsonObject{
						"objecterino": util.JsonString("I AM HERE"),
					},
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "it[0].objecterino",
					Message: "was found, but should NOT exist",
				},
			},
		},
		{
			name: "There should be no deeper object but it exists2",
			left: util.JsonObject{
				"it": util.JsonArray{
					util.JsonObject{
						"objecterino:control": util.JsonObject{
							"must_not_exist": true,
						},
					},
				},
			},
			right: util.JsonObject{
				"it": util.JsonArray{
					util.JsonObject{
						"objecterino": util.JsonString("I AM HERE"),
					},
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "it[0].objecterino",
					Message: "was found, but should NOT exist",
				},
			},
		},
		{
			name: "There should be a exact object match",
			left: util.JsonObject{
				"objecterino": util.JsonObject{
					"1": util.JsonNumber(1),
					"2": util.JsonNumber(2),
					"3": util.JsonNumber(3),
				},
				"objecterino:control": util.JsonObject{
					"no_extra": true,
				},
			},
			right: util.JsonObject{
				"objecterino": util.JsonObject{
					"1": util.JsonNumber(1),
					"3": util.JsonNumber(3),
					"2": util.JsonNumber(2),
				},
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "There should be a exact object match even if order is mixed",
			left: util.JsonObject{
				"objecterino": util.JsonObject{
					"1": util.JsonNumber(1),
					"2": util.JsonNumber(2),
					"3": util.JsonNumber(3),
				},
				"objecterino:control": util.JsonObject{
					"no_extra": true,
				},
			},
			right: util.JsonObject{
				"objecterino": util.JsonObject{
					"2": util.JsonNumber(2),
					"3": util.JsonNumber(3),
					"1": util.JsonNumber(1),
				},
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "Exact match is not present",
			left: util.JsonObject{
				"MYobjecterino": util.JsonObject{
					"1": util.JsonNumber(1),
					"2": util.JsonNumber(2),
					"3": util.JsonNumber(3),
				},
				"MYobjecterino:control": util.JsonObject{
					"no_extra": true,
				},
			},
			right: util.JsonObject{
				"MYobjecterino": util.JsonObject{
					"2": util.JsonNumber(2),
					"4": util.JsonNumber(4),
					"1": util.JsonNumber(1),
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "MYobjecterino.3",
					Message: "was not found, but should exist",
				},
				{
					Key:     "MYobjecterino",
					Message: "extra elements found in object",
				},
			},
		},
		{
			name: "Not all contained",
			left: util.JsonObject{
				"array": util.JsonArray{
					"val2",
					"val3",
				},
			},
			right: util.JsonObject{
				"array": util.JsonArray{
					"val1",
					"val2",
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "array[1]",
					Message: "Got 'val1', expected 'val3'",
				},
			},
		},
		{
			name: "Wrong order",
			left: util.JsonObject{
				"array": util.JsonArray{
					"val3",
					"val2",
				},
				"array:control": util.JsonObject{
					"order_matters": true,
					"no_extra":      true,
				},
			},
			right: util.JsonObject{
				"array": util.JsonArray{
					"val2",
					"val3",
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "array[1]",
					Message: "element \"val2\" not found in array in proper order",
				},
				{
					Key:     "array",
					Message: "extra elements found in array",
				},
			},
		},
		{
			name: "Wrong order deeper with map",
			left: util.JsonObject{
				"array": util.JsonObject{
					"inner": util.JsonObject{
						"deeper": util.JsonArray{
							"val4",
							"val5",
						},
						"deeper:control": util.JsonObject{
							"order_matters": true,
						},
					},
				},
			},
			right: util.JsonObject{
				"array": util.JsonObject{
					"inner": util.JsonObject{
						"deeper": util.JsonArray{
							"val5",
							"val4",
						},
					},
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "array.inner.deeper[1]",
					Message: "element \"val5\" not found in array in proper order",
				},
			},
		},
		{
			name: "Right error message for array",
			left: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{
						"henk": "denk",
					},
				},
			},
			right: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{},
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "body[0].henk",
					Message: "was not found, but should exist",
				},
			},
		},
		/*	{
			name: "Wrong order deeper with arrays",
			left: util.JsonObject{
				"array": util.JsonArray{
					util.JsonArray{
						util.JsonArray{
							"val9",
							"val10",
						},
					},
				},
				"array:control": util.JsonObject{
					"order_matters": true,
				},
			},
			right: util.JsonObject{
				"array": util.JsonArray{
					util.JsonArray{
						util.JsonArray{
							"val10",
							"val9",
						},
					},
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "array",
					Message: "[0][0][0]Expected 'val9' != 'val10' Got",
				},
				{
					Key:     "array",
					Message: "[0][0][1]Expected 'val10' != 'val9' Got",
				},
			},
		},*/
		{
			name: "All fine deeper with arrays",
			left: util.JsonObject{
				"array": util.JsonArray{
					util.JsonArray{
						util.JsonArray{
							"val9",
							"val10",
						},
					},
				},
			},
			right: util.JsonObject{
				"array": util.JsonArray{
					util.JsonArray{
						util.JsonArray{
							"val9",
							"val10",
						},
					},
				},
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "Check array length",
			left: util.JsonObject{
				"array:control": util.JsonObject{
					"element_count": 3,
				},
			},
			right: util.JsonObject{
				"array": util.JsonArray{
					util.JsonArray{
						"val9",
						"val10",
					},
					util.JsonArray{
						"val9",
						"val10",
					},
					util.JsonArray{
						"val9",
						"val10",
					},
				},
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "Check array length and fail",
			left: util.JsonObject{
				"array:control": util.JsonObject{
					"element_count": 2,
				},
				"array": util.JsonArray{
					util.JsonArray{},
				},
			},
			right: util.JsonObject{
				"array": util.JsonArray{
					util.JsonArray{
						util.JsonArray{
							"val9",
							"val10",
						},
					},
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "array",
					Message: "length of the actual response array '1' != '2' expected length",
				},
			},
		},
		{
			name: "Check controls in array (1 element)",
			left: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{
						"pool": util.JsonObject{
							"reference:control": util.JsonObject{
								"is_number": true,
							},
						},
					},
				},
			},
			right: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{
						"pool": util.JsonObject{
							"reference": "system:root",
						},
					},
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "body[0].pool.reference",
					Message: "should be 'Number' but is 'String'",
				},
			},
		},
		{
			name: "Check controls in array (more elements)",
			left: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{
						"pool": util.JsonObject{
							"reference:control": util.JsonObject{
								"is_number": true,
							},
						},
					},
					util.JsonObject{
						"pool": util.JsonObject{
							"reference:control": util.JsonObject{
								"is_number": true,
							},
						},
					},
				},
			},
			right: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{
						"pool": util.JsonObject{
							"reference": "system:root",
						},
					},
					util.JsonObject{
						"pool": util.JsonObject{
							"reference": 123,
						},
					},
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "body[1].pool.reference",
					Message: "should be 'Number' but is 'String'",
				},
			},
		},
		{
			name: "Check controls in array (more elements, different order)",
			left: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{
						"pool": util.JsonObject{
							"reference:control": util.JsonObject{
								"is_number": true,
							},
						},
					},
					util.JsonObject{
						"pool": util.JsonObject{
							"reference:control": util.JsonObject{
								"is_string": true,
							},
						},
					},
				},
			},
			right: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{
						"pool": util.JsonObject{
							"reference": "system:root",
						},
					},
					util.JsonObject{
						"pool": util.JsonObject{
							"reference": 123,
						},
					},
				},
			},
			eEqual:    true,
			eFailures: nil,
		},
		{
			name: "Check body no extra",
			left: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{
						"pool": util.JsonObject{
							"reference": "system:root",
						},
					},
				},
				"body:control": util.JsonObject{
					"no_extra": true,
				},
			},
			right: util.JsonObject{
				"body": util.JsonArray{
					util.JsonObject{
						"pool": util.JsonObject{
							"reference":  "system:root",
							"reference2": "system:root",
						},
					},
					util.JsonObject{
						"pool": util.JsonObject{
							"reference": "system:root",
						},
					},
				},
			},
			eEqual: false,
			eFailures: []CompareFailure{
				{
					Key:     "body",
					Message: "extra elements found in array",
				},
			},
		},
	}

	for _, data := range testData {
		t.Run(data.name, func(t *testing.T) {
			equal, err := JsonEqual(data.left, data.right, ComparisonContext{})
			if err != nil {
				t.Fatal(err)
			}

			if equal.Equal != data.eEqual {
				t.Errorf("Expected equal '%t' != '%t' Got equal", data.eEqual, equal.Equal)
			}

			if (equal.Failures != nil && data.eFailures == nil) || (equal.Failures == nil && data.eFailures != nil) {
				t.Errorf("Expected Failure '%v' != '%v' Got Failure", data.eFailures, equal.Failures)
				return
			}

			wantFailures := []string{}
			for _, v := range data.eFailures {
				wantFailures = append(wantFailures, fmt.Sprintf("%s", v))
			}
			haveFailures := []string{}
			for _, v := range equal.Failures {
				haveFailures = append(haveFailures, fmt.Sprintf("%s", v))
			}

			go_test_utils.AssertStringArraysEqualNoOrder(t, wantFailures, haveFailures)

			if t.Failed() {
				t.Log("EXPECTED ", wantFailures)
				t.Log("GOT      ", haveFailures)
			}
		})
	}
}
