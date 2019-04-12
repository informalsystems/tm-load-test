package loadtest

import (
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"

	"github.com/interchainio/tm-load-test/pkg/loadtest/messages"
)

// SingleTestSummaryPlot is an HTML page template that facilitates plotting the
// summary results (using plotly.js) of a single load test.
const SingleTestSummaryPlot = `<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
	<title>Load Test Summary</title>
	<link rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/bulma/0.7.4/css/bulma.min.css">
</head>
<body>
	<section class="section">
	<div class="container">
		<h1 class="title is-1">Load Test Summary</h1>
		
		<table class="table is-striped is-hoverable">
		<thead>
				<tr>
					<th>Parameter</th>
					<th>Value</th>
					<th></th>
				</tr>
			</thead>
			<tbody>
				<tr>
					<td>Client type</td>
					<td><code>{{.ClientType}}</code></td>
					<td>
						<i class="fas fa-info-circle" title="The load testing client type used to interact with the Tendermint network"></i>
					</td>
				</tr>

				<tr>
					<td>Slave node count</td>
					<td>{{.SlaveNodeCount}}</td>
					<td>
						<i class="fas fa-info-circle" title="The number of slave nodes used to execute this load test (each responsible for spawning clients)"></i>
					</td>
				</tr>

				<tr>
					<td>Client spawn</td>
					<td>{{.ClientSpawn}}</td>
					<td>
						<i class="fas fa-info-circle" title="The maximum number of clients that were spawned per slave node during this load test"></i>
					</td>
				</tr>

				<tr>
					<td>Client spawn rate</td>
					<td>{{.ClientSpawnRate}}</td>
					<td>
						<i class="fas fa-info-circle" title="The number of clients spawned per second on a single slave node"></i>
					</td>
				</tr>

				<tr>
					<td>Client request wait min</td>
					<td>{{.ClientRequestWaitMin}}</td>
					<td>
						<i class="fas fa-info-circle" title="The minimum time a client waited before firing off each request"></i>
					</td>
				</tr>

				<tr>
					<td>Client request wait max</td>
					<td>{{.ClientRequestWaitMax}}</td>
					<td>
						<i class="fas fa-info-circle" title="The maximum time a client waited before firing off each request"></i>
					</td>
				</tr>

				<tr>
					<td>Client max interactions</td>
					<td>{{.ClientMaxInteractions}}</td>
					<td>
						<i class="fas fa-info-circle" title="The maximum number of interactions between a client and the Tendermint network"></i>
					</td>
				</tr>

				<tr>
					<td>Test run time</td>
					<td>{{.TotalTestTime}}</td>
					<td>
						<i class="fas fa-info-circle" title="The total time (from the master's perspective) in which the load testing completed"></i>
					</td>
				</tr>
			</tbody>
		</table>
	</div>
	</section>

	<section class="section">
	<div class="container">
		<h2 class="title is-2">Charts</h2>
		<h3 class="subtitle is-3">Interaction Response Times</h3>
		<div class="columns">
			<div class="column is-one-quarter">
				<table class="table is-striped is-hoverable">
					<thead>
						<tr>
							<th>Parameter</th>
							<th>Value</th>
						</tr>
					</thead>
					<tbody>
						<tr>
							<td>Total clients</td>
							<td>{{.InteractionsTotalClients}}</td>
						</tr>
						<tr>
							<td>Interactions/sec overall (absolute)</td>
							<td>{{.InteractionsPerSec}}</td>
						</tr>
						<tr>
							<td>Interaction count</td>
							<td>{{.InteractionsCount}}</td>
						</tr>
						<tr>
							<td>Interaction error count</td>
							<td>{{.InteractionsErrors}}</td>
						</tr>
						<tr>
							<td>Interaction error rate</td>
							<td>{{.InteractionsErrorRate}}</td>
						</tr>
						<tr>
							<td>Interaction min time</td>
							<td>{{.InteractionsMinTime}}</td>
						</tr>
						<tr>
							<td>Interaction max time</td>
							<td>{{.InteractionsMaxTime}}</td>
						</tr>
					</tbody>
				</table>
			</div>
			<div class="column">
				<div id="interaction-rt-chart"></div>
			</div>
		</div>

		{{if $.ErrorCounts}}
		<div class="columns">
			<div class="column">
				<h5 class="subtitle is-5">Top Interaction Errors</code></h5>
				<table class="table is-striped is-hoverable is-fullwidth">
					<thead>
						<tr>
							<th>No.</th>
							<th>Error</th>
							<th>Count</th>
						</tr>
					</thead>
					<tbody>
						{{range $i, $errCount := $.ErrorCounts}}
						<tr>
							<td>{{$i}}</td>
							<td>{{$errCount.Description}}</td>
							<td>{{$errCount.Count}}</td>
						</tr>
						{{end}}
					</tbody>
				</table>
				<h5 class="subtitle is-5">&nbsp;</h5>
			</div>
		</div>
		{{end}}
	</div>
	</section>

	<section class="section">
		<div class="container">
			<h3 class="subtitle is-3">Request Response Times</h3>

			{{range $i, $req := $.Requests}}
			<h4 class="subtitle is-4"><code>{{$req.Name}}</code></h4>
			<div class="columns">
				<div class="column is-one-quarter">
					<table class="table is-striped is-hoverable is-fullwidth">
						<thead>
							<tr>
								<th>Parameter</th>
								<th>Value</th>
							</tr>
						</thead>
						<tbody>
							<tr>
								<td>Requests/sec overall (absolute)</td>
								<td>{{$req.PerSec}}</td>
							</tr>
							<tr>
								<td>Request count</td>
								<td>{{$req.Count}}</td>
							</tr>
							<tr>
								<td>Request error count</td>
								<td>{{$req.Errors}}</td>
							</tr>
							<tr>
								<td>Request error rate</td>
								<td>{{$req.ErrorRate}}</td>
							</tr>
							<tr>
								<td>Request min time</td>
								<td>{{$req.MinTime}}</td>
							</tr>
							<tr>
								<td>Request max time</td>
								<td>{{$req.MaxTime}}</td>
							</tr>
						</tbody>
					</table>
				</div>
				<div class="column">
					<div id="{{$req.Name}}-rt-chart"></div>
				</div>
			</div>

			{{if $req.ErrorCounts}}
			<div class="columns">
				<div class="column">
					<h5 class="subtitle is-5">Top Errors for <code>{{$req.Name}}</code></h5>
					<table class="table is-striped is-hoverable is-fullwidth">
						<thead>
							<tr>
								<th>No.</th>
								<th>Error</th>
								<th>Count</th>
							</tr>
						</thead>
						<tbody>
							{{range $j, $errCount := $req.ErrorCounts}}
							<tr>
								<td>{{$j}}</td>
								<td>{{$errCount.Description}}</td>
								<td>{{$errCount.Count}}</td>
							</tr>
							{{end}}
						</tbody>
					</table>
					<h5 class="subtitle is-5">&nbsp;</h5>
				</div>
			</div>
			{{end}}
			{{end}}

		</div>
	</section>

	<section class="section">
		<div class="container">
			<h3 class="subtitle is-3">Node Charts</h3>

			{{range $i, $nodeChart := $.NodeCharts}}
			<div id="{{$nodeChart.ID}}-chart"></div>
			<div class="container">&nbsp;</div>
			<div class="container">&nbsp;</div>
			<div class="container">&nbsp;</div>
			{{end}}
		</div>
	</section>

	<script src="https://use.fontawesome.com/releases/v5.3.1/js/all.js"></script>
	<script src="https://cdn.plot.ly/plotly-1.45.3.min.js"></script>

	<script>
		function generateHistogram(elemID, bins, counts, xtitle, ytitle) {
			var trace = {
				x: bins,
				y: counts,
				type: 'bar',
				name: ytitle
			};
			var data = [trace];
			var layout = {
				title: ytitle,
				xaxis: {
					title: xtitle
				},
				yaxis: {
					title: ytitle
				}
			};

			Plotly.newPlot(elemID, data, layout);
		}

		function generateNodeChart(elemID, hostnames, nodesData, xtitle, ytitle) {
			var traces = [];

			for (var i=0;i<hostnames.length;i++) {
				traces.push({
					x: nodesData[i].x,
					y: nodesData[i].y,
					type: 'scatter',
					mode: 'lines',
					name: hostnames[i]
				});
			}

			var layout = {
				title: ytitle,
				xaxis: {
					title: xtitle
				}
			};

			Plotly.newPlot(elemID, traces, layout);
		}

		generateHistogram(
			"interaction-rt-chart", 
			[
{{.InteractionsResponseTimesBins}}
			], 
			[
{{.InteractionsResponseTimesCounts}}
			],
			"Interaction Response Time (milliseconds)",
			"Counts"
		);

		{{range $i, $req := $.Requests}}
		generateHistogram(
			"{{$req.Name}}-rt-chart", 
			[
{{$req.ResponseTimesBins}}
			], 
			[
{{$req.ResponseTimesCounts}}
			],
			"{{$req.Name}} Response Time (milliseconds)",
			"Counts"
		);
		{{end}}

		{{range $i, $nodeChart := $.NodeCharts}}
		generateNodeChart(
			"{{$nodeChart.ID}}-chart",
			[
{{$nodeChart.Hostnames}}
			],
			[
{{$nodeChart.NodesData}}
			],
			"Time (s)",
			"{{$nodeChart.Title}}"
		);
		{{end}}
	</script>
</body>
</html>
`

// SingleTestSummaryContext represents context for being able to render a single
// load test's summary plot.
type SingleTestSummaryContext struct {
	// Summary parameters
	ClientType            template.HTML
	SlaveNodeCount        template.HTML
	ClientSpawn           template.HTML
	ClientSpawnRate       template.HTML
	ClientRequestWaitMin  template.HTML
	ClientRequestWaitMax  template.HTML
	ClientRequestTimeout  template.HTML
	ClientMaxInteractions template.HTML
	TotalTestTime         template.HTML

	// Interaction-related parameters
	InteractionsTotalClients template.HTML
	InteractionsPerSec       template.HTML
	InteractionsCount        template.HTML
	InteractionsErrors       template.HTML
	InteractionsErrorRate    template.HTML
	InteractionsMinTime      template.HTML
	InteractionsMaxTime      template.HTML

	// For the interaction response times histogram
	InteractionsResponseTimesBins   template.JS
	InteractionsResponseTimesCounts template.JS

	// Top errors encountered by interactions
	ErrorCounts []SingleTestSummaryErrorCount

	// Request-related parameters
	Requests []SingleTestSummaryRequestParams

	// Node-related chart parameters
	NodeCharts []SingleTestNodeChart
}

// SingleTestSummaryRequestParams encapsulates parameters for a single request
// type in the above plot.
type SingleTestSummaryRequestParams struct {
	Name      template.HTML
	PerSec    template.HTML
	Count     template.HTML
	Errors    template.HTML
	ErrorRate template.HTML
	MinTime   template.HTML
	MaxTime   template.HTML

	// For the request response times histogram
	ResponseTimesBins   template.JS
	ResponseTimesCounts template.JS

	// Error counts
	ErrorCounts []SingleTestSummaryErrorCount
}

// SingleTestSummaryErrorCount helps us represent one of the top errors
// encountered by an interaction or request.
type SingleTestSummaryErrorCount struct {
	Description template.HTML
	Count       template.HTML
}

// SingleTestNodeChart encapsulates parameters for a specific metric family.
type SingleTestNodeChart struct {
	ID        template.JS
	Title     template.JS
	Hostnames template.JS
	NodesData template.JS
}

// MetricFamilyData is read from a node's Prometheus statistics CSV file. It
// represents the data of a single metric family.
type MetricFamilyData struct {
	ID         string
	Help       string
	Timestamps []float64
	Data       []float64
}

// NewSingleTestSummaryContext creates the relevant context to be able to render
// the single load test plot.
func NewSingleTestSummaryContext(cfg *Config, stats *messages.CombinedStats, nodesData map[string]map[string]MetricFamilyData) SingleTestSummaryContext {
	icstats := CalculateStats(stats.Interactions, stats.TotalTestTime)
	// flatten the interaction response time histogram
	ibins, icounts := FlattenResponseTimeHistogram(stats.Interactions.ResponseTimes, "				")
	return SingleTestSummaryContext{
		ClientType:            template.HTML(cfg.Clients.Type),
		SlaveNodeCount:        template.HTML(fmt.Sprintf("%d", cfg.Master.ExpectSlaves)),
		ClientSpawn:           template.HTML(fmt.Sprintf("%d", cfg.Clients.Spawn)),
		ClientSpawnRate:       template.HTML(fmt.Sprintf("%.1f", cfg.Clients.SpawnRate)),
		ClientRequestWaitMin:  template.HTML(fmt.Sprintf("%.0fms", float64(time.Duration(cfg.Clients.RequestWaitMin))/float64(time.Millisecond))),
		ClientRequestWaitMax:  template.HTML(fmt.Sprintf("%.0fms", float64(time.Duration(cfg.Clients.RequestWaitMax))/float64(time.Millisecond))),
		ClientRequestTimeout:  template.HTML(fmt.Sprintf("%.0fms", float64(time.Duration(cfg.Clients.RequestTimeout))/float64(time.Millisecond))),
		ClientMaxInteractions: template.HTML(fmt.Sprintf("%d", cfg.Clients.MaxInteractions)),
		TotalTestTime:         template.HTML(fmt.Sprintf("%.2fs", float64(stats.TotalTestTime)/float64(time.Second))),

		InteractionsTotalClients: template.HTML(fmt.Sprintf("%d", stats.Interactions.TotalClients)),
		InteractionsPerSec:       template.HTML(fmt.Sprintf("%.2f", icstats.AbsPerSec)),
		InteractionsCount:        template.HTML(fmt.Sprintf("%d", stats.Interactions.Count)),
		InteractionsErrors:       template.HTML(fmt.Sprintf("%d", stats.Interactions.Errors)),
		InteractionsErrorRate:    template.HTML(fmt.Sprintf("%.2f%%", icstats.FailureRate)),
		InteractionsMinTime:      template.HTML(fmt.Sprintf("%.1fms", float64(time.Duration(stats.Interactions.MinTime))/float64(time.Millisecond))),
		InteractionsMaxTime:      template.HTML(fmt.Sprintf("%.1fms", float64(time.Duration(stats.Interactions.MaxTime))/float64(time.Millisecond))),

		InteractionsResponseTimesBins:   template.JS(ibins),
		InteractionsResponseTimesCounts: template.JS(icounts),

		ErrorCounts: buildErrorCounts(stats.Interactions.ErrorsByType),

		Requests: buildRequestsCtx(stats.Requests, stats.TotalTestTime),

		NodeCharts: buildNodeCharts(nodesData, "				"),
	}
}

func buildRequestsCtx(stats map[string]*messages.SummaryStats, totalTestTime int64) []SingleTestSummaryRequestParams {
	result := make([]SingleTestSummaryRequestParams, 0)
	reqNames := make([]string, 0)
	rcstats := make(map[string]*CalculatedStats)
	for reqName, reqStats := range stats {
		reqNames = append(reqNames, reqName)
		rcstats[reqName] = CalculateStats(reqStats, totalTestTime)
	}
	// now sort the request names alphabetically
	sort.SliceStable(reqNames[:], func(i, j int) bool {
		return strings.Compare(reqNames[i], reqNames[j]) < 0
	})
	for _, reqName := range reqNames {
		rbins, rcounts := FlattenResponseTimeHistogram(stats[reqName].ResponseTimes, "				")
		params := SingleTestSummaryRequestParams{
			Name:                template.HTML(reqName),
			PerSec:              template.HTML(fmt.Sprintf("%.2f", rcstats[reqName].AbsPerSec)),
			Count:               template.HTML(fmt.Sprintf("%d", stats[reqName].Count)),
			Errors:              template.HTML(fmt.Sprintf("%d", stats[reqName].Errors)),
			ErrorRate:           template.HTML(fmt.Sprintf("%.2f%%", rcstats[reqName].FailureRate)),
			MinTime:             template.HTML(fmt.Sprintf("%.1fms", float64(stats[reqName].MinTime)/float64(time.Millisecond))),
			MaxTime:             template.HTML(fmt.Sprintf("%.1fms", float64(stats[reqName].MaxTime)/float64(time.Millisecond))),
			ResponseTimesBins:   template.JS(rbins),
			ResponseTimesCounts: template.JS(rcounts),
			ErrorCounts:         buildErrorCounts(stats[reqName].ErrorsByType),
		}
		result = append(result, params)
	}
	return result
}

// nodesData is a mapping of metric family name -> hostname -> metric family data
func buildNodeCharts(nodesData map[string]map[string]MetricFamilyData, indent string) []SingleTestNodeChart {
	charts := make([]SingleTestNodeChart, 0)
	metricFamilies := make([]string, 0)
	metricFamilyTitles := make(map[string]string)
	hostnamesSet := make(map[string]interface{})

	for mfID, mfData := range nodesData {
		metricFamilies = append(metricFamilies, mfID)
		for hostname, hostData := range mfData {
			hostnamesSet[hostname] = nil
			metricFamilyTitles[mfID] = hostData.Help
		}
	}
	hostnames := make([]string, 0)
	for hostname := range hostnamesSet {
		hostnames = append(hostnames, hostname)
	}

	sort.SliceStable(metricFamilies[:], func(i, j int) bool {
		return strings.Compare(metricFamilies[i], metricFamilies[j]) < 0
	})
	sort.SliceStable(hostnames[:], func(i, j int) bool {
		return strings.Compare(hostnames[i], hostnames[j]) < 0
	})

	hostnamesQuoted := make([]string, 0)
	for _, hostname := range hostnames {
		hostnamesQuoted = append(hostnamesQuoted, fmt.Sprintf("\"%s\"", hostname))
	}

	for _, mf := range metricFamilies {
		mfParts := strings.Split(mf, ":")
		prometheusID := ""
		titleSuffix := ""
		mfID := mf
		if len(mfParts) > 1 {
			mfID = strings.Join(mfParts[1:], ":")
			prometheusID = mfParts[0]
			titleSuffix = fmt.Sprintf(" [%s]", prometheusID)
		}
		charts = append(charts, SingleTestNodeChart{
			ID:        template.JS(prometheusID + "__" + mfID),
			Title:     template.JS(metricFamilyTitles[mf] + titleSuffix),
			Hostnames: template.JS(indent + strings.Join(hostnamesQuoted, ", ")),
			NodesData: template.JS(flattenNodesChartData(nodesData[mf], hostnames, indent)),
		})
	}

	return charts
}

// Takes the given data, assuming it's the same metric family from multiple
// different hosts, and flattens it into a string containing a
// JavaScript-compatible array of {x:, y:} coordinates for each host.
func flattenNodesChartData(nodesData map[string]MetricFamilyData, hostnames []string, indent string) string {
	var builder strings.Builder

	for i, hostname := range hostnames {
		nodeData := nodesData[hostname]
		builder.WriteString(indent + "{\n")
		builder.WriteString(indent + "	x: [")
		for j, ts := range nodeData.Timestamps {
			if (j % 10) == 0 {
				builder.WriteString("\n" + indent + "		")
			}
			builder.WriteString(fmt.Sprintf("%.2f", ts))
			if j < len(nodeData.Timestamps)-1 {
				builder.WriteString(",")
			}
		}
		builder.WriteString("\n" + indent + "	],\n") // x

		builder.WriteString(indent + "	y: [")
		for j, value := range nodeData.Data {
			if (j % 10) == 0 {
				builder.WriteString("\n" + indent + "		")
			}
			builder.WriteString(fmt.Sprintf("%.2f", value))
			if j < len(nodeData.Timestamps)-1 {
				builder.WriteString(",")
			}
		}
		builder.WriteString("\n" + indent + "	]\n") // y

		builder.WriteString(indent + "}") // host data
		if i < len(nodesData)-1 {
			builder.WriteString(",")
		}
		builder.WriteString("\n")
	}

	return builder.String()
}

func buildErrorCounts(errorsByType map[string]int64) []SingleTestSummaryErrorCount {
	counts := make([]SingleTestSummaryErrorCount, 0)
	sortedErrors := make([]string, 0)
	for errType := range errorsByType {
		sortedErrors = append(sortedErrors, errType)
	}
	sort.SliceStable(sortedErrors[:], func(i, j int) bool {
		return errorsByType[sortedErrors[i]] > errorsByType[sortedErrors[j]]
	})
	for _, errType := range sortedErrors {
		counts = append(counts, SingleTestSummaryErrorCount{
			Description: template.HTML(errType),
			Count:       template.HTML(fmt.Sprintf("%d", errorsByType[errType])),
		})
	}
	return counts
}

// RenderSingleTestSummaryPlot is a convenience method to render the single test
// summary plot to a string, ready to be written to an output file.
func RenderSingleTestSummaryPlot(cfg *Config, stats *messages.CombinedStats, nodesData map[string]map[string]MetricFamilyData) (string, error) {
	ctx := NewSingleTestSummaryContext(cfg, stats, nodesData)
	tmpl, err := template.New("single-test-summary-plot").Parse(SingleTestSummaryPlot)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if err = tmpl.Execute(&b, ctx); err != nil {
		return "", err
	}
	return b.String(), nil
}
