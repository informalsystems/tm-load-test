package loadtest

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/common/expfmt"

	pdto "github.com/prometheus/client_model/go"
	"github.com/interchainio/tm-load-test/internal/logging"
	"github.com/interchainio/tm-load-test/pkg/loadtest/messages"
)

// DefaultHistogramBinCount indicates the maximum number of bins in a histogram.
// A value of 100 will give us percentiles.
const DefaultHistogramBinCount int64 = 100

// FlattenedError is the key/value pair from
// `messages.SummaryStats.ErrorsByType` flattened into a structure for easy
// sorting.
type FlattenedError struct {
	ErrorType string
	Count     int64
}

// CalculatedStats includes a number of parameters that can be calculated from a
// `messages.SummaryStats` object.
type CalculatedStats struct {
	AvgTotalClientTime float64           // Average total time that we were waiting for a single client's interactions/requests to complete.
	CountPerClient     float64           // Number of interactions/requests per client.
	AvgTimePerClient   float64           // Average time per interaction/request per client.
	PerSecPerClient    float64           // Interactions/requests per second per client.
	PerSec             float64           // Interactions/requests per second overall.
	AbsPerSec          float64           // Interactions/requests per second overall, including wait times.
	FailureRate        float64           // The % of interactions/requests that failed.
	TopErrors          []*FlattenedError // The top 10 errors by error count.
}

// PrometheusStats encapsulates all of the statistics we retrieved from the
// Prometheus endpoints for our target nodes.
type PrometheusStats struct {
	StartTime                time.Time                      // When was the load test started?
	MetricFamilyDescriptions map[string]string              // Mapping of metric family ID -> description
	TargetNodesStats         map[string]NodePrometheusStats // Target node stats organized by hostname.

	logger     logging.Logger
	collectors []*prometheusCollector   // The collectors that're responsible for fetching the Prometheus stats from each target node.
	statsc     chan nodePrometheusStats // The channel through which the primary Prometheus goroutine receives stats from collectors
	wg         *sync.WaitGroup
}

// NodePrometheusStats represents a single test network node's stats from its
// Prometheus endpoint. The mapping goes "Metric family ID -> Seconds since test start -> Data point"
type NodePrometheusStats map[string]map[int64]float64

// NewSummaryStats instantiates an empty, configured SummaryStats object.
func NewSummaryStats(timeout time.Duration, totalClients int64) *messages.SummaryStats {
	return &messages.SummaryStats{
		TotalClients:  totalClients,
		ErrorsByType:  make(map[string]int64),
		ResponseTimes: NewResponseTimeHistogram(timeout),
	}
}

// AddStatistic adds a single statistic to the given SummaryStats object.
func AddStatistic(stats *messages.SummaryStats, timeTaken time.Duration, err error) {
	timeTakenNs := int64(timeTaken) / int64(time.Nanosecond)
	// we cap the time taken at the response timeout
	if timeTakenNs > stats.ResponseTimes.Timeout {
		timeTakenNs = stats.ResponseTimes.Timeout
	}

	if stats.Count == 0 {
		stats.MinTime = timeTakenNs
		stats.MaxTime = timeTakenNs
	} else {
		if timeTakenNs < stats.MinTime {
			stats.MinTime = timeTakenNs
		}
		if timeTakenNs > stats.MaxTime {
			stats.MaxTime = timeTakenNs
		}
	}

	stats.Count++
	stats.TotalTime += timeTakenNs

	if err != nil {
		stats.Errors++
		errType := fmt.Sprintf("%s", err)
		if _, ok := stats.ErrorsByType[errType]; !ok {
			stats.ErrorsByType[errType] = 1
		} else {
			stats.ErrorsByType[errType]++
		}
	}

	bin := int64(0)
	if timeTakenNs > 0 {
		bin = stats.ResponseTimes.BinSize * (timeTakenNs / stats.ResponseTimes.BinSize)
	}
	stats.ResponseTimes.TimeBins[bin]++
}

// MergeSummaryStats will merge the given source stats into the destination
// stats, modifying the destination stats object.
func MergeSummaryStats(dest, src *messages.SummaryStats) {
	if src.Count == 0 {
		return
	}

	dest.Count += src.Count
	dest.Errors += src.Errors
	dest.TotalTime += src.TotalTime
	dest.TotalClients += src.TotalClients

	if src.MinTime < dest.MinTime {
		dest.MinTime = src.MinTime
	}
	if src.MaxTime > dest.MaxTime {
		dest.MaxTime = src.MaxTime
	}

	// merge ErrorsByType
	for errType, srcCount := range src.ErrorsByType {
		_, ok := dest.ErrorsByType[errType]
		if ok {
			dest.ErrorsByType[errType] += srcCount
		} else {
			dest.ErrorsByType[errType] = srcCount
		}
	}

	// merge response times histogram (assuming all bins are precisely the same
	// for the stats)
	for bin, srcCount := range src.ResponseTimes.TimeBins {
		dest.ResponseTimes.TimeBins[bin] += srcCount
	}
}

// MergeCombinedStats will merge the given src CombinedStats object's contents
// into the dest object.
func MergeCombinedStats(dest, src *messages.CombinedStats) {
	if src.TotalTestTime > dest.TotalTestTime {
		dest.TotalTestTime = src.TotalTestTime
	}
	MergeSummaryStats(dest.Interactions, src.Interactions)
	for srcReqName, srcReqStats := range src.Requests {
		MergeSummaryStats(dest.Requests[srcReqName], srcReqStats)
	}
}

func FlattenedSortedErrors(stats *messages.SummaryStats) []*FlattenedError {
	errorsByType := make([]*FlattenedError, 0)
	for errStr, count := range stats.ErrorsByType {
		errorsByType = append(errorsByType, &FlattenedError{ErrorType: errStr, Count: count})
	}
	sort.SliceStable(errorsByType[:], func(i, j int) bool {
		return errorsByType[i].Count > errorsByType[j].Count
	})
	return errorsByType
}

func CalculateStats(stats *messages.SummaryStats, totalTestTime int64) *CalculatedStats {
	// average total time for all cumulative interactions/requests per client
	avgTotalClientTime := math.Round(float64(stats.TotalTime) / float64(stats.TotalClients))
	// number of interactions/requests per client
	countPerClient := math.Round(float64(stats.Count) / float64(stats.TotalClients))
	// average time per interaction/request per client
	avgTimePerClient := float64(0)
	if countPerClient > 0 {
		avgTimePerClient = avgTotalClientTime / countPerClient
	}
	// interactions/requests per second per client
	perSecPerClient := float64(0)
	if avgTotalClientTime > 0 {
		perSecPerClient = 1 / (avgTimePerClient / float64(time.Second))
	}
	// interactions/requests per second overall
	perSec := perSecPerClient * float64(stats.TotalClients)
	absPerSec := float64(0)
	if totalTestTime > 0 {
		absPerSec = float64(stats.Count) / time.Duration(totalTestTime).Seconds()
	}
	return &CalculatedStats{
		AvgTotalClientTime: avgTotalClientTime,
		CountPerClient:     countPerClient,
		AvgTimePerClient:   avgTimePerClient,
		PerSecPerClient:    perSecPerClient,
		PerSec:             perSec,
		AbsPerSec:          absPerSec,
		FailureRate:        float64(100) * (float64(stats.Errors) / float64(stats.Count)),
		TopErrors:          FlattenedSortedErrors(stats),
	}
}

// LogSummaryStats is a utility function for displaying summary statistics
// through the logging interface. This is useful when working with tm-load-test
// from the command line.
func LogSummaryStats(logger logging.Logger, logPrefix string, stats *messages.SummaryStats, totalTestTime int64) {
	cs := CalculateStats(stats, totalTestTime)

	logger.Info(logPrefix+" total clients", "value", stats.TotalClients)
	logger.Info(logPrefix+" per second per client", "value", fmt.Sprintf("%.2f", cs.PerSecPerClient))
	logger.Info(logPrefix+" per second overall", "value", fmt.Sprintf("%.2f", cs.PerSec))
	logger.Info(logPrefix+" per second overall (absolute)", "value", fmt.Sprintf("%.2f", cs.AbsPerSec))
	logger.Info(logPrefix+" count", "value", stats.Count)
	logger.Info(logPrefix+" errors", "value", stats.Errors)
	logger.Info(logPrefix+" failure rate (%)", "value", fmt.Sprintf("%.2f", cs.FailureRate))
	logger.Info(logPrefix+" min time", "value", time.Duration(stats.MinTime)*time.Nanosecond)
	logger.Info(logPrefix+" max time", "value", time.Duration(stats.MaxTime)*time.Nanosecond)

	for i := 0; i < 10 && i < len(cs.TopErrors); i++ {
		logger.Info(logPrefix+fmt.Sprintf(" top error %d", i+1), cs.TopErrors[i].ErrorType, cs.TopErrors[i].Count)
	}
}

// LogStats is a utility function to log the given statistics for easy reading,
// especially from the command line.
func LogStats(logger logging.Logger, stats *messages.CombinedStats) {
	logger.Info("")
	logger.Info("INTERACTION STATISTICS")
	LogSummaryStats(logger, "Interactions", stats.Interactions, stats.TotalTestTime)

	logger.Info("")
	logger.Info("REQUEST STATISTICS")
	for reqName, reqStats := range stats.Requests {
		logger.Info(reqName)
		LogSummaryStats(logger, "  Requests", reqStats, stats.TotalTestTime)
		logger.Info("")
	}
}

// NewResponseTimeHistogram instantiates an empty response time histogram with
// the given timeout as an upper bound on the size of the histogram.
func NewResponseTimeHistogram(timeout time.Duration) *messages.ResponseTimeHistogram {
	timeoutNs := int64(timeout) / int64(time.Nanosecond)
	binSize := int64(math.Ceil(float64(timeoutNs) / float64(DefaultHistogramBinCount)))
	timeBins := make(map[int64]int64)
	for i := int64(0); i <= DefaultHistogramBinCount; i++ {
		timeBins[i*binSize] = 0
	}
	return &messages.ResponseTimeHistogram{
		Timeout:  timeoutNs,
		BinSize:  binSize,
		BinCount: DefaultHistogramBinCount + 1,
		TimeBins: timeBins,
	}
}

// FlattenResponseTimeHistogram will take the given histogram object and convert
// it into two flattened arrays, where the first returned array will be the bins
// and the second will be the counts.
func FlattenResponseTimeHistogram(h *messages.ResponseTimeHistogram, indent string) (string, string) {
	bins, counts := indent, indent
	for i := int64(0); i < h.BinCount; i++ {
		bin := h.BinSize * i
		bins += fmt.Sprintf("%d", time.Duration(bin)/time.Millisecond)
		counts += fmt.Sprintf("%d", h.TimeBins[bin])

		if i < (h.BinCount - 1) {
			bins += ", "
			counts += ", "
		}

		if (i+1)%10 == 0 {
			bins += "\n" + indent
			counts += "\n" + indent
		}
	}
	return bins, counts
}

// WriteSummaryStats will write the given summary statistics using the specified
// CSV writer.
func WriteSummaryStats(writer *csv.Writer, indentCount int, linePrefix string, stats *messages.SummaryStats, totalTestTime int64) error {
	cs := CalculateStats(stats, totalTestTime)
	indent := strings.Repeat(" ", indentCount)
	prefix := indent + linePrefix
	if err := writer.Write([]string{prefix + " total time", (time.Duration(stats.TotalTime) * time.Nanosecond).String()}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " total time (absolute)", (time.Duration(totalTestTime) * time.Nanosecond).String()}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " total clients", fmt.Sprintf("%d", stats.TotalClients)}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " per second per client", fmt.Sprintf("%.2f", cs.PerSecPerClient)}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " per second overall", fmt.Sprintf("%.2f", cs.PerSec)}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " per second overall (absolute)", fmt.Sprintf("%.2f", cs.AbsPerSec)}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " count", fmt.Sprintf("%d", stats.Count)}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " errors", fmt.Sprintf("%d", stats.Errors)}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " failure rate (%)", fmt.Sprintf("%.2f", cs.FailureRate)}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " min time", (time.Duration(stats.MinTime) * time.Nanosecond).String()}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " max time", (time.Duration(stats.MaxTime) * time.Nanosecond).String()}); err != nil {
		return err
	}
	if err := writer.Write([]string{prefix + " top errors:", ""}); err != nil {
		return err
	}
	for i := 0; i < len(cs.TopErrors) && i < 10; i++ {
		if err := writer.Write([]string{indent + "  " + cs.TopErrors[i].ErrorType, fmt.Sprintf("%d", cs.TopErrors[i].Count)}); err != nil {
			return err
		}
	}
	if err := writer.Write([]string{prefix + " response time histogram (milliseconds/count):", ""}); err != nil {
		return err
	}
	for bin := int64(0); bin <= stats.ResponseTimes.Timeout; bin += stats.ResponseTimes.BinSize {
		if err := writer.Write([]string{
			indent + "  " + fmt.Sprintf("%d", time.Duration(bin)/time.Millisecond),
			fmt.Sprintf("%d", stats.ResponseTimes.TimeBins[bin]),
		}); err != nil {
			return err
		}
	}
	return nil
}

// ParseSummaryStats will attempt to read a `*messages.SummaryStats` object from
// the given set of rows/columns (assuming they're from a CSV file), and on
// success will return that object and the number of lines read from the CSV
// reader. Otherwise an error will be returned.
//
// NOTE: Only a single `*messages.SummaryStats` object will be parsed from the
// given set of rows. The moment one full object has been parsed, the function
// will return.
func ParseSummaryStats(rows [][]string) (*messages.SummaryStats, int, int64, error) {
	var totalTestTime int64
	curRow, rowCount := 0, len(rows)
	parseNextVal := func() (string, string, error) {
		defer func() { curRow++ }()
		if curRow >= rowCount {
			return "", "", fmt.Errorf("Missing rows in CSV data")
		}
		if len(rows[curRow]) < 2 {
			return "", "", fmt.Errorf("Missing column in CSV data")
		}
		return rows[curRow][0], rows[curRow][1], nil
	}
	parseInt := func() (string, int64, error) {
		desc, v, err := parseNextVal()
		if err != nil {
			return "", 0, err
		}
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return "", 0, fmt.Errorf("%s -> %s", desc, err)
		}
		return desc, i, err
	}
	parseDuration := func() (string, int64, error) {
		desc, v, err := parseNextVal()
		if err != nil {
			return "", 0, err
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return "", 0, err
		}
		return desc, d.Nanoseconds(), nil
	}

	stats := &messages.SummaryStats{}
	var err error

	_, stats.TotalTime, err = parseDuration()
	if err != nil {
		return nil, curRow, 0, err
	}
	// skip the absolute total test time field here
	_, totalTestTime, err = parseDuration()
	if err != nil {
		return nil, curRow, 0, err
	}
	_, stats.TotalClients, err = parseInt()
	if err != nil {
		return nil, curRow, 0, err
	}

	// skip the computed rows
	curRow += 3

	_, stats.Count, err = parseInt()
	if err != nil {
		return nil, curRow, 0, err
	}
	_, stats.Errors, err = parseInt()
	if err != nil {
		return nil, curRow, 0, err
	}

	// skip the failure rate % row
	curRow++

	_, stats.MinTime, err = parseDuration()
	if err != nil {
		return nil, curRow, 0, fmt.Errorf("Failed to parse min time field: %s", err)
	}
	_, stats.MaxTime, err = parseDuration()
	if err != nil {
		return nil, curRow, 0, fmt.Errorf("Failed to parse max time field: %s", err)
	}

	// read the top errors
	stats.ErrorsByType = make(map[string]int64)
	// skip the "top errors:" line
	curRow++
	for curRow < rowCount {
		if len(rows[curRow]) == 0 {
			return nil, curRow, 0, fmt.Errorf("Missing rows in CSV data")
		}
		if strings.Contains(rows[curRow][0], "response time histogram") {
			break
		}
		desc, count, err := parseInt()
		if err != nil {
			return nil, curRow, 0, err
		}
		stats.ErrorsByType[desc] = count
	}

	// read the response time histogram
	stats.ResponseTimes = &messages.ResponseTimeHistogram{}
	stats.ResponseTimes.TimeBins = make(map[int64]int64)
	curBin, prevBin := int64(0), int64(0)
	curRow++
	for curRow < rowCount {
		// it's a sign we've hit the end of the histogram
		if isEmptyCSVRow(rows[curRow]) {
			break
		}
		binLabel, count, err := parseInt()
		if err != nil {
			return nil, curRow, 0, err
		}
		prevBin = curBin
		curBin, err = strconv.ParseInt(strings.Trim(binLabel, " "), 10, 64)
		if err != nil {
			return nil, curRow, 0, err
		}
		// convert to nanoseconds
		curBin = (time.Duration(curBin) * time.Millisecond).Nanoseconds()
		stats.ResponseTimes.TimeBins[curBin] = count
	}
	// we assume the final bin is our timeout
	stats.ResponseTimes.Timeout = curBin
	stats.ResponseTimes.BinCount = int64(len(stats.ResponseTimes.TimeBins))
	stats.ResponseTimes.BinSize = curBin - prevBin

	return stats, curRow, totalTestTime, nil
}

// WriteCombinedStats will write the given combined statistics using the
// specified writer.
func WriteCombinedStats(writer io.Writer, stats *messages.CombinedStats) error {
	cw := csv.NewWriter(writer)
	defer cw.Flush()

	if err := cw.Write([]string{"INTERACTION STATISTICS", ""}); err != nil {
		return err
	}
	if err := WriteSummaryStats(cw, 0, "Interactions", stats.Interactions, stats.TotalTestTime); err != nil {
		return err
	}

	if err := cw.Write([]string{"", ""}); err != nil {
		return err
	}
	if err := cw.Write([]string{"REQUEST STATISTICS", ""}); err != nil {
		return err
	}
	i := 0
	for reqName, reqStats := range stats.Requests {
		if i > 0 {
			if err := cw.Write([]string{"", ""}); err != nil {
				return err
			}
		}
		if err := cw.Write([]string{reqName, ""}); err != nil {
			return err
		}
		if err := WriteSummaryStats(cw, 2, "Requests", reqStats, stats.TotalTestTime); err != nil {
			return err
		}
		i++
	}
	return nil
}

func isEmptyCSVRow(row []string) bool {
	for _, col := range row {
		if len(col) > 0 {
			return false
		}
	}
	return true
}

// ReadCombinedStats will attempt to read a CombinedStats object from the given
// reader, assuming it's in CSV format, or return an error.
func ReadCombinedStats(r io.Reader) (*messages.CombinedStats, error) {
	var totalTestTime int64
	cr := csv.NewReader(r)
	curRow := 0
	// read all the data
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	// skip the "INTERACTION STATISTICS" heading
	curRow++
	istats, rowsRead, totalTestTime, err := ParseSummaryStats(rows[curRow:])
	if err != nil {
		return nil, err
	}
	curRow += rowsRead

	// skip the empty row and the "REQUEST STATISTICS" line
	curRow += 2

	// read the requests' statistics
	rstats := make(map[string]*messages.SummaryStats)
	for curRow < len(rows) {
		// read the request name from this row
		reqName := rows[curRow][0]
		curRow++
		curStats, rowsRead, _, err := ParseSummaryStats(rows[curRow:])
		if err != nil {
			return nil, err
		}
		curRow += rowsRead
		rstats[reqName] = curStats
		// skip any subsequent empty rows
		for curRow < len(rows) && isEmptyCSVRow(rows[curRow]) {
			curRow++
		}
	}
	return &messages.CombinedStats{
		TotalTestTime: totalTestTime,
		Interactions:  istats,
		Requests:      rstats,
	}, nil
}

// WriteCombinedStatsToFile will write the given stats object to the specified
// output CSV file. If the given output path does not exist, it will be created.
func WriteCombinedStatsToFile(outputFile string, stats *messages.CombinedStats) error {
	outputPath := path.Dir(outputFile)
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return err
	}
	f, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteCombinedStats(f, stats)
}

// ReadCombinedStatsFromFile will read the combined stats from the given CSV
// file on success, or return an error.
func ReadCombinedStatsFromFile(csvFile string) (*messages.CombinedStats, error) {
	f, err := os.Open(csvFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadCombinedStats(f)
}

// sortedErrorsByType returns a slice containing the keys of the given map in
// order from highest value to lowest.
func sortedErrorsByType(errorsByType map[string]int64) []string {
	types := make([]string, 0)
	for errType := range errorsByType {
		types = append(types, errType)
	}
	sort.SliceStable(types[:], func(i, j int) bool {
		return errorsByType[types[i]] > errorsByType[types[j]]
	})
	return types
}

func SummarizeSummaryStats(stats *messages.SummaryStats) *messages.SummaryStats {
	topErrors := make(map[string]int64)
	errorsByType := sortedErrorsByType(stats.ErrorsByType)
	for i := 0; i < 10 && i < len(errorsByType); i++ {
		topErrors[errorsByType[i]] = stats.ErrorsByType[errorsByType[i]]
	}
	return &messages.SummaryStats{
		Count:         stats.Count,
		Errors:        stats.Errors,
		TotalTime:     stats.TotalTime,
		MinTime:       stats.MinTime,
		MaxTime:       stats.MaxTime,
		TotalClients:  stats.TotalClients,
		ErrorsByType:  topErrors,
		ResponseTimes: &(*stats.ResponseTimes),
	}
}

// SummarizeCombinedStats will summarize the error reporting such that only a
// maximum of 10 kinds of errors are returned. This is to help avoid hitting the
// GRPC message size limits when sending data back to the master. It creates a
// new `CombinedStats` object without modifying the original one.
func SummarizeCombinedStats(stats *messages.CombinedStats) *messages.CombinedStats {
	summarizedRequests := make(map[string]*messages.SummaryStats)
	for reqName, reqStats := range stats.Requests {
		summarizedRequests[reqName] = SummarizeSummaryStats(reqStats)
	}
	return &messages.CombinedStats{
		TotalTestTime: stats.TotalTestTime,
		Interactions:  SummarizeSummaryStats(stats.Interactions),
		Requests:      summarizedRequests,
	}
}

//
// NodePrometheusStats
//

// GetNodePrometheusStats will perform a blocking GET request to the given URL
// to fetch the text-formatted Prometheus metrics for a particular node, parse
// those metrics, and then either return the parsed metrics or an error.
func GetNodePrometheusStats(c *http.Client, endpoint *PrometheusEndpoint, ts int64) (NodePrometheusStats, map[string]string, error) {
	stats := make(NodePrometheusStats)
	descriptions := make(map[string]string)
	resp, err := c.Get(endpoint.URL)
	if err != nil {
		return nil, nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, nil, fmt.Errorf("Non-OK response from URL: %s (%d)", resp.Status, resp.StatusCode)
	}

	parser := &expfmt.TextParser{}
	metricFamilies, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	for mfID, mf := range metricFamilies {
		fullID := fmt.Sprintf("%s:%s", endpoint.ID, mfID)
		stats[fullID] = make(map[int64]float64)
		switch mf.GetType() {
		case pdto.MetricType_COUNTER:
			stats[fullID][ts] = mf.GetMetric()[0].GetCounter().GetValue()
			descriptions[mfID] = mf.GetHelp()

		case pdto.MetricType_GAUGE:
			stats[fullID][ts] = mf.GetMetric()[0].GetGauge().GetValue()
			descriptions[mfID] = mf.GetHelp()
		}
	}

	return stats, descriptions, nil
}

// Merge will incorporate the given `src` object's metric families' stats into
// this one.
func (s NodePrometheusStats) Merge(src NodePrometheusStats) {
	if src == nil || len(src) == 0 {
		return
	}
	for srcName, srcMF := range src {
		if _, ok := s[srcName]; !ok {
			s[srcName] = make(map[int64]float64)
		}
		for ts, val := range srcMF {
			s[srcName][ts] = val
		}
	}
}

// ExtractTimestamps will extract the timestamps from the given
// NodePrometheusStats map.
func (s NodePrometheusStats) ExtractTimestamps() []int64 {
	timestamps := make([]int64, 0)
	uniqueTimestamps := make(map[int64]interface{})
	for _, mf := range s {
		for ts := range mf {
			uniqueTimestamps[ts] = nil
		}
	}
	for ts := range uniqueTimestamps {
		timestamps = append(timestamps, ts)
	}
	sort.SliceStable(timestamps[:], func(i, j int) bool {
		return timestamps[i] < timestamps[j]
	})
	return timestamps
}

//
// PrometheusStats
//

type nodePrometheusStats struct {
	hostID       string
	descriptions map[string]string // Metric family description
	stats        NodePrometheusStats
}

type prometheusCollector struct {
	logger    logging.Logger
	startTime time.Time
	cfg       TestNetworkTargetConfig
	endpoints []*PrometheusEndpoint // The endpoints for which this collector is responsible
	client    *http.Client
	ticker    *time.Ticker
	shutdownc chan bool
	wg        *sync.WaitGroup
}

func spawnCollector(cfg *Config, nodeCfg TestNetworkTargetConfig, startTime time.Time, statsc chan nodePrometheusStats, wg *sync.WaitGroup, logger logging.Logger) *prometheusCollector {
	col := &prometheusCollector{
		logger:    logger,
		startTime: startTime,
		cfg:       nodeCfg,
		endpoints: nodeCfg.GetPrometheusEndpoints(),
		client: &http.Client{
			Timeout: time.Duration(cfg.TestNetwork.PrometheusPollTimeout),
		},
		ticker:    time.NewTicker(time.Duration(cfg.TestNetwork.PrometheusPollInterval)),
		shutdownc: make(chan bool, 1),
		wg:        wg,
	}
	wg.Add(1)
	// spawn the goroutine for the collector
	go col.collectorLoop(statsc)
	return col
}

func (col *prometheusCollector) collectorLoop(statsc chan nodePrometheusStats) {
	col.logger.Debug("Goroutine created for Prometheus collector", "hostID", col.cfg.ID, "nodeURLs", col.endpoints)
	// fire off an initial request for stats (prior to first tick)
	col.collect(statsc)
loop:
	for {
		select {
		case <-col.ticker.C:
			col.collect(statsc)

		case <-col.shutdownc:
			col.logger.Debug("Shutting down collector goroutine", "hostID", col.cfg.ID)
			break loop
		}
	}
	col.wg.Done()
}

// collect will run through all of the Prometheus endpoints and actually fetch
// the stats from that endpoint. Stats are then collected and passed through to
// the given `statsc` channel.
func (col *prometheusCollector) collect(statsc chan nodePrometheusStats) {
	resultStats := make(NodePrometheusStats)
	descriptions := make(map[string]string)
	ts := time.Since(col.startTime)
	timestamp := int64(math.Round(ts.Seconds()))

	for _, endpoint := range col.endpoints {
		stats, desc, err := GetNodePrometheusStats(col.client, endpoint, timestamp)
		if err == nil {
			resultStats.Merge(stats)
			for mfID, mfDesc := range desc {
				descriptions[mfID] = mfDesc
			}
		} else {
			col.logger.Error("Failed to retrieve Prometheus stats", "hostID", col.cfg.ID, "err", err)
		}
	}

	statsc <- nodePrometheusStats{
		hostID:       col.cfg.ID,
		descriptions: descriptions,
		stats:        resultStats,
	}
}

// NewPrometheusStats creates a new, ready-to-use PrometheusStats object.
func NewPrometheusStats(logger logging.Logger) *PrometheusStats {
	return &PrometheusStats{
		MetricFamilyDescriptions: make(map[string]string),
		TargetNodesStats:         make(map[string]NodePrometheusStats),
		logger:                   logger,
	}
}

// Merge will take the given `nodePrometheusStats` object and merge its contents
// with what `ps` currently has.
func (ps *PrometheusStats) Merge(s nodePrometheusStats) {
	if _, ok := ps.TargetNodesStats[s.hostID]; !ok {
		ps.TargetNodesStats[s.hostID] = make(NodePrometheusStats)
	}
	ps.TargetNodesStats[s.hostID].Merge(s.stats)
	for mfID, mfDesc := range s.descriptions {
		ps.MetricFamilyDescriptions[mfID] = mfDesc
	}
}

// RunCollectors will kick off one goroutine per Tendermint node from which
// we're collecting Prometheus stats.
func (ps *PrometheusStats) RunCollectors(cfg *Config, shutdownc, donec chan bool, logger logging.Logger) {
	// provide enough room in the channel for all of our collectors to send
	// updates at once
	ps.statsc = make(chan nodePrometheusStats, len(cfg.TestNetwork.Targets))
	ps.wg = &sync.WaitGroup{}
	ps.collectors = make([]*prometheusCollector, 0)

	logger.Debug("Starting up Prometheus collectors", "count", len(cfg.TestNetwork.Targets))
	ps.StartTime = time.Now()
	for _, node := range cfg.TestNetwork.Targets {
		ps.collectors = append(ps.collectors, spawnCollector(
			cfg,
			node,
			ps.StartTime,
			ps.statsc,
			ps.wg,
			logger,
		))
	}
	logger.Debug("Prometheus collectors started")
	// wait for all the collectors to finish their collection
	ps.waitForCollectors(shutdownc)

	// collect one last batch of stats once the network's settled a bit
	ps.collectFinalStats(time.Duration(cfg.TestNetwork.PrometheusPollTimeout))

	logger.Debug("Prometheus collector loop shut down")
	donec <- true
}

func (ps *PrometheusStats) waitForCollectors(shutdownc chan bool) {
loop:
	for {
		select {
		case nodeStats := <-ps.statsc:
			ps.Merge(nodeStats)

		case <-shutdownc:
			ps.logger.Debug("Shutdown signal received by Prometheus collector loop")
			break loop
		}
	}
	for _, col := range ps.collectors {
		col.ticker.Stop()
		col.shutdownc <- true
	}
	// wait for all of the collector loops to shut down
	ps.wg.Wait()
}

func (ps *PrometheusStats) collectFinalStats(timeout time.Duration) {
	ps.logger.Info("Waiting 10 seconds for network to settle post-test...")
	time.Sleep(10 * time.Second)

	ps.logger.Debug("Doing one final Prometheus stats collection from each node")
	// do one final collection from each node
	for _, col := range ps.collectors {
		finalStatsc := make(chan nodePrometheusStats, 1)
		col.collect(finalStatsc)
		select {
		case nodeStats := <-finalStatsc:
			ps.Merge(nodeStats)

		case <-time.After(timeout):
			ps.logger.Error("Timed out waiting for final Prometheus polling operation to complete", "hostID", col.cfg.ID)
		}
	}
}

func writeTimeSeriesTargetNodeStats(w io.Writer, startTime time.Time, familyDescriptions map[string]string, nodeStats NodePrometheusStats) error {
	itimestamps := nodeStats.ExtractTimestamps()
	timestamps := make([]string, 0)
	for _, ts := range itimestamps {
		timestamps = append(timestamps, fmt.Sprintf("%d", ts))
	}

	familyIDsSorted := make([]string, 0)
	for familyID := range nodeStats {
		familyIDsSorted = append(familyIDsSorted, familyID)
	}
	sort.SliceStable(familyIDsSorted[:], func(i, j int) bool {
		return strings.Compare(familyIDsSorted[i], familyIDsSorted[j]) < 0
	})

	// then we write the data to the given file as time series CSV data
	cw := csv.NewWriter(w)
	defer cw.Flush()

	header := []string{"Metric Family", "Description"}
	header = append(header, timestamps...)
	if err := cw.Write(header); err != nil {
		return err
	}

	// now we write the metric family samples
	for _, name := range familyIDsSorted {
		nameParts := strings.Split(name, ":")
		mfID := name
		if len(nameParts) > 1 {
			mfID = strings.Join(nameParts[1:], ":")
		}
		row := []string{name, familyDescriptions[mfID]}
		for _, ts := range itimestamps {
			if sample, ok := nodeStats[name][ts]; ok {
				row = append(row, fmt.Sprintf("%.2f", sample))
			} else {
				// empty/missing value
				row = append(row, "0.0")
			}
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// Dump will write each node's stats to a separate file, named according to the
// host ID, in the given output directory.
func (ps *PrometheusStats) Dump(outputPath string) error {
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return err
	}
	for hostID, nodeStats := range ps.TargetNodesStats {
		outputFile := path.Join(outputPath, fmt.Sprintf("%s.csv", hostID))
		f, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := writeTimeSeriesTargetNodeStats(f, ps.StartTime, ps.MetricFamilyDescriptions, nodeStats); err != nil {
			return err
		}
	}
	return nil
}
