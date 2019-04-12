package loadtest_test

import (
	"testing"
	"time"

	"github.com/interchainio/tm-load-test/pkg/loadtest"
	"github.com/interchainio/tm-load-test/pkg/loadtest/messages"
)

func TestSummaryStatsMerging(t *testing.T) {
	testCases := []struct {
		dest     *messages.SummaryStats
		src      *messages.SummaryStats
		expected *messages.SummaryStats
	}{
		{
			dest:     loadtest.NewSummaryStats(1*time.Second, 1),
			src:      loadtest.NewSummaryStats(1*time.Second, 1),
			expected: loadtest.NewSummaryStats(1*time.Second, 1),
		},
		{
			dest: &messages.SummaryStats{
				Count:     100,
				Errors:    5,
				TotalTime: 100000,
				MinTime:   100,
				MaxTime:   1000,
				ErrorsByType: map[string]int64{
					"error1": 5,
					"error2": 10,
				},
				ResponseTimes: &messages.ResponseTimeHistogram{
					Timeout:  1000,
					BinSize:  200,
					BinCount: 6,
					TimeBins: map[int64]int64{
						0:    1,
						200:  2,
						400:  3,
						600:  4,
						800:  5,
						1000: 6,
					},
				},
			},
			src: &messages.SummaryStats{
				Count:     120,
				Errors:    3,
				TotalTime: 100000,
				MinTime:   90,
				MaxTime:   900,
				ErrorsByType: map[string]int64{
					"error1": 3,
					"error3": 15,
				},
				ResponseTimes: &messages.ResponseTimeHistogram{
					Timeout:  1000,
					BinSize:  200,
					BinCount: 6,
					TimeBins: map[int64]int64{
						0:    6,
						200:  5,
						400:  4,
						600:  3,
						800:  2,
						1000: 1,
					},
				},
			},
			expected: &messages.SummaryStats{
				Count:     220,
				Errors:    8,
				TotalTime: 200000,
				MinTime:   90,
				MaxTime:   1000,
				ErrorsByType: map[string]int64{
					"error1": 8,
					"error2": 10,
					"error3": 15,
				},
				ResponseTimes: &messages.ResponseTimeHistogram{
					Timeout:  1000,
					BinSize:  200,
					BinCount: 6,
					TimeBins: map[int64]int64{
						0:    7,
						200:  7,
						400:  7,
						600:  7,
						800:  7,
						1000: 7,
					},
				},
			},
		},
	}
	for i, tc := range testCases {
		t.Logf("Executing test case %d", i)
		loadtest.MergeSummaryStats(tc.dest, tc.src)
		assertSummaryStatsEqual(t, tc.expected, tc.dest)
	}
}

func assertSummaryStatsEqual(t *testing.T, expected, actual *messages.SummaryStats) {
	if expected.Count != actual.Count {
		t.Errorf("Expected Count field to be %d, but was %d", expected.Count, actual.Count)
	}
	if expected.Errors != actual.Errors {
		t.Errorf("Expected Errors field to be %d, but was %d", expected.Errors, actual.Errors)
	}
	if expected.TotalTime != actual.TotalTime {
		t.Errorf("Expected TotalTime field to be %d, but was %d", expected.TotalTime, actual.TotalTime)
	}
	if expected.MinTime != actual.MinTime {
		t.Errorf("Expected MinTime field to be %d, but was %d", expected.MinTime, actual.MinTime)
	}
	if expected.MaxTime != actual.MaxTime {
		t.Errorf("Expected MaxTime field to be %d, but was %d", expected.MaxTime, actual.MaxTime)
	}
	for k, expectedCount := range expected.ErrorsByType {
		actualCount, ok := actual.ErrorsByType[k]
		if !ok {
			t.Errorf("Expected ErrorsByType field %s to be present, but was not", k)
		} else {
			if expectedCount != actualCount {
				t.Errorf("Expected count for ErrorsByType field %s to be %d, but was %d", k, expectedCount, actualCount)
			}
		}
	}
	for bin, expectedCount := range expected.ResponseTimes.TimeBins {
		actualCount, ok := actual.ResponseTimes.TimeBins[bin]
		if !ok {
			t.Errorf("Expected ResponseTimes.TimeBins field %d to be present, but was not", bin)
		} else {
			if expectedCount != actualCount {
				t.Errorf("Expected count for ResponseTimes.TimeBins[%d] to be %d, but was %d", bin, expectedCount, actualCount)
			}
		}
	}
}

type summaryStatsAddition struct {
	timeTaken time.Duration
	err       error
}

type summaryStatsTestCase struct {
	timeout      time.Duration
	totalClients int64
	additions    []summaryStatsAddition
}

type combinedStatsTestCase struct {
	interactions summaryStatsTestCase
	requests     map[string]summaryStatsTestCase
}

func (tc *summaryStatsTestCase) buildSummaryStats() *messages.SummaryStats {
	stats := loadtest.NewSummaryStats(tc.timeout, tc.totalClients)
	for _, add := range tc.additions {
		loadtest.AddStatistic(stats, add.timeTaken, add.err)
	}
	return stats
}

func (tc *combinedStatsTestCase) buildCombinedStats() *messages.CombinedStats {
	stats := &messages.CombinedStats{
		Interactions: tc.interactions.buildSummaryStats(),
		Requests:     make(map[string]*messages.SummaryStats),
	}
	for reqName, stc := range tc.requests {
		stats.Requests[reqName] = stc.buildSummaryStats()
	}
	return stats
}

func TestStatsReadingAndWriting(t *testing.T) {
	testCases := []combinedStatsTestCase{
		combinedStatsTestCase{
			interactions: summaryStatsTestCase{
				timeout:      time.Duration(5 * time.Second),
				totalClients: 1,
				additions: []summaryStatsAddition{
					summaryStatsAddition{
						timeTaken: time.Duration(400 * time.Millisecond),
						err:       nil,
					},
				},
			},
			requests: map[string]summaryStatsTestCase{
				"req1": summaryStatsTestCase{
					timeout:      time.Duration(2 * time.Second),
					totalClients: 1,
					additions: []summaryStatsAddition{
						summaryStatsAddition{
							timeTaken: time.Duration(100 * time.Millisecond),
							err:       nil,
						},
					},
				},
				"req2": summaryStatsTestCase{
					timeout:      time.Duration(2 * time.Second),
					totalClients: 1,
					additions: []summaryStatsAddition{
						summaryStatsAddition{
							timeTaken: time.Duration(300 * time.Millisecond),
							err:       nil,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		origCombinedStats := tc.buildCombinedStats()
		// write the stats to a CSV file
		tempFile := createTempFile(t)
		if err := loadtest.WriteCombinedStatsToFile(tempFile, origCombinedStats); err != nil {
			t.Fatalf("Failed to write combined stats to temporary file: %s (err=%s)", tempFile, err)
		}
		// now read the stats back
		readCombinedStats, err := loadtest.ReadCombinedStatsFromFile(tempFile)
		if err != nil {
			t.Fatalf("Failed to read combined stats from temporary file: %s (err=%s)", tempFile, err)
		}

		if !origCombinedStats.Equal(readCombinedStats) {
			t.Error("Original stats and stats read back from temp file do not match")
			printJSONDiff(origCombinedStats, readCombinedStats)
		}
	}
}
