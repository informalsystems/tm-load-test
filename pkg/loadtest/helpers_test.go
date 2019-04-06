package loadtest_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"
)

// Creates a temporary file and returns its name. Fails the test if it can't
// create the file.
func createTempFile(t *testing.T) string {
	tf, err := ioutil.TempFile("", "temp*")
	if err != nil {
		t.Fatal("Failed to write to temporary file", err)
	}
	defer tf.Close()
	return tf.Name()
}

func findLongestRow(rows []string) int {
	longestRow := 0
	for _, row := range rows {
		if len(row) > longestRow {
			longestRow = len(row)
		}
	}
	return longestRow
}

func padRight(s string, maxLen int) string {
	result := s
	if len(result) < maxLen {
		result = result + strings.Repeat(" ", maxLen-len(result))
	}
	return result
}

func printJSONDiff(a, b interface{}) {
	abytes, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		panic(err)
	}
	arows := strings.Split(string(abytes), "\n")
	bbytes, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		panic(err)
	}
	brows := strings.Split(string(bbytes), "\n")

	longestA, longestB := findLongestRow(arows), findLongestRow(brows)
	longest := longestA
	if longestB > longest {
		longest = longestB
	}
	maxRows := len(arows)
	if len(brows) > maxRows {
		maxRows = len(brows)
	}

	// join the rows, padding the left side
	for i := 0; i < maxRows; i++ {
		rowA, rowB := "", ""
		if i < len(arows) {
			rowA = padRight(arows[i], longest)
		}
		if i < len(brows) {
			rowB = brows[i]
		}
		fmt.Println(rowA + "    |  " + rowB)
	}
}
