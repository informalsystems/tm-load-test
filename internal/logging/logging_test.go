package logging

import (
	"reflect"
	"testing"
)

func TestKVPairSerialization(t *testing.T) {
	testCases := []struct {
		kvpairs  []interface{}
		expected map[string]interface{}
	}{
		{
			[]interface{}{"a", 1, "b", "v"},
			map[string]interface{}{
				"a": 1,
				"b": "v",
			},
		},
		{
			[]interface{}{"a"},
			map[string]interface{}{},
		},
		{
			[]interface{}{"a", 1, "b"},
			map[string]interface{}{},
		},
	}

	for i, tc := range testCases {
		actual := serializeKVPairs(tc.kvpairs...)
		if !reflect.DeepEqual(actual, tc.expected) {
			t.Errorf("Test case %d: Expected result %v, but got %v", i, tc.expected, actual)
		}
	}
}
