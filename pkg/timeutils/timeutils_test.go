package timeutils_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/interchainio/tm-load-test/pkg/timeutils"
)

func TestBasicParsing(t *testing.T) {
	testCases := []struct {
		jsonObj  string
		expected time.Duration
	}{
		{`{"value": "10s"}`, 10 * time.Second},
		{`{"value": "3h"}`, 3 * time.Hour},
		{`{"value": "5m"}`, 5 * time.Minute},
	}

	for i, tc := range testCases {
		actual := struct {
			Value timeutils.ParseableDuration `json:"value"`
		}{}
		if err := json.Unmarshal([]byte(tc.jsonObj), &actual); err != nil {
			t.Errorf("failed to unmarshal test case JSON for case %d: %v", i, err)
			continue
		}
		if tc.expected != actual.Value.Duration() {
			t.Errorf("expected duration %s but got %s", tc.expected.String(), actual.Value.Duration().String())
		}
	}
}
