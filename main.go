package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"io/ioutil"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
)

var metricLabels = make(map[string][]string)

type queryInformation struct {
	metric     string
	labelNames []string
}

func main() {
	url := "https://raw.githubusercontent.com/appscode/grafana-dashboards/metric/mongodb/mongodb-summary-dashboard.json"
	response, err := http.Get(url)
	if err != nil {
		fmt.Errorf("error : %s", err)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		fmt.Printf("Error fetching url. status : %s", response.Status)
		return
	}
	// Read the content of the JSON file
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		fmt.Println("Error reading JSON file:", err)
		return
	}

	// Define a struct to unmarshal the JSON data into
	var dashboardData map[string]interface{} // You can define a more specific struct based on your JSON structure

	// Unmarshal the JSON data into the struct
	err = json.Unmarshal(body, &dashboardData)
	if err != nil {
		fmt.Println("Error unmarshalling JSON data:", err)
		return
	}
	ex := 0
	var queries []queryInformation
	if panels, ok := dashboardData["panels"].([]interface{}); ok {
		for _, panel := range panels {
			if targets, ok := panel.(map[string]interface{})["targets"].([]interface{}); ok {
				for _, target := range targets {
					if expr, ok := target.(map[string]interface{})["expr"]; ok {
						if expr != "" {
							ex++
							query := expr.(string)
							queries = append(queries, getMetricAndLabels(query)...)
						}
					}
				}
			}
		}
	}
	fmt.Println("total expression", ex)

	// Prom

	prometheusURL := "http://localhost:9090/"

	// Create a new HTTP client
	client, err := api.NewClient(api.Config{
		Address: prometheusURL,
	})
	if err != nil {
		fmt.Println("Error creating Prometheus client:", err)
		return
	}

	// Create a new Prometheus API client
	apiClient := v1.NewAPI(client)
	cnt := 0
	for _, query := range queries {
		metricName := query.metric
		for _, labelKey := range query.labelNames {
			//startTime := time.Now().Add(-1 * time.Hour)
			endTime := time.Now()

			result, _, err := apiClient.Query(context.TODO(), metricName, endTime)
			if err != nil {
				fmt.Println("Error querying Prometheus:", err, "metric: ", metricName)
				return
			}

			matrix := result.(model.Vector)
			if len(matrix) > 0 {
				// Check if the label exists for any result in the matrix
				labelExists := false

				for _, sample := range matrix {
					if sample.Metric != nil {
						if _, ok := sample.Metric[model.LabelName(labelKey)]; ok {
							labelExists = true
							break
						}
					}
				}

				if labelExists {
					cnt++
				} else {
					fmt.Println(labelKey, "label does not exist for the metric", metricName)
				}
			} else {
				fmt.Println(metricName, "metric does not exist")
			}
		}
	}
	fmt.Println("total metric", cnt)

}

func excludeQuotedSubstrings(input string) string {
	// Define the regular expression pattern to match string inside double quotation
	re := regexp.MustCompile(`"[^"]*"`)

	// Replace all quoted substring with an empty string
	result := re.ReplaceAllString(input, "")

	return result
}
func excludeNonAlphanumericUnderscore(input string) string {
	// Define the regular expression pattern to match non-alphanumeric characters except underscore
	pattern := `[^a-zA-Z0-9_]`
	re := regexp.MustCompile(pattern)

	// Replace non-alphanumeric or underscore characters with an empty string
	result := re.ReplaceAllString(input, "")

	return result
}

// Labels may contain ASCII letters, numbers, as well as underscores. They must match the regex [a-zA-Z_][a-zA-Z0-9_]*
// So we need to split the selector string by comma. then extract label name with the help of the regex format
// Ref: https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels
func getLabelNames(labelSelector string) []string {
	var labelNames []string
	unQuoted := excludeQuotedSubstrings(labelSelector)
	commaSeparated := strings.Split(unQuoted, ",")
	for _, s := range commaSeparated {
		labelName := excludeNonAlphanumericUnderscore(s)
		labelNames = append(labelNames, labelName)
	}
	return labelNames
}

// Finding valid bracket sequence from startPosition
func substringInsideLabelSelector(query string, startPosition int) string {
	balance := 0
	closingPosition := startPosition
	for i := startPosition; i < len(query); i++ {
		if query[i] == '{' {
			balance++
		}
		if query[i] == '}' {
			balance--
		}
		if balance == 0 {
			closingPosition = i
			break
		}
	}

	return query[startPosition+1 : closingPosition]
}

// Metric names may contain ASCII letters, digits, underscores, and colons. It must match the regex [a-zA-Z_:][a-zA-Z0-9_:]*
// So we can use this if the character is in a metric name
// Ref: https://prometheus.io/docs/concepts/data_model/#metric-names-and-labels
func matchMetricRegex(char rune) bool {
	return unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == ':'
}

// Steps:
// - if current character is '{'
//   - extract metric name by matching metric regex
//   - get label selector substring inside { }
//   - get label name from this substring by matching label regex
func getMetricAndLabels(query string) []queryInformation {
	var queries []queryInformation
	for i := 0; i < len(query); i++ {
		if query[i] == '{' {
			j := i
			for {
				if j-1 < 0 || (!matchMetricRegex(rune(query[j-1]))) {
					break
				}
				j--
			}
			metric := query[j:i]
			labelSelector := substringInsideLabelSelector(query, i)
			labelNames := getLabelNames(labelSelector)
			queries = append(queries, queryInformation{
				metric:     metric,
				labelNames: labelNames,
			})
		}
	}
	return queries
}
