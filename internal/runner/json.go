package runner

import (
	"encoding/json"
	"fmt"
)

// JSON 把一次运行结果编成 JSON（字段与 Result 对齐，便于 jq 取用）。
//
// 为什么不直接 json.Marshal(results)：Result 的字段没有 tag，编出来是
// "Name"/"Duration" 那种大写键，而且 Duration 会变成纳秒整数。这里的形状是
// 给脚本 / CI 用的稳定契约：顶层带汇总，逐条带 durationMs 与 failed。
func JSON(collection string, results []Result) (string, error) {
	report := jsonReport{
		Collection: collection,
		Total:      len(results),
		Results:    make([]jsonResult, 0, len(results)),
	}
	for _, result := range results {
		if result.Failed {
			report.Failed++
		}
		report.Results = append(report.Results, jsonResult{
			Name:       result.Name,
			File:       result.File,
			Method:     result.Method,
			URL:        result.URL,
			Status:     result.Status,
			DurationMS: result.Duration.Milliseconds(),
			Error:      result.Err,
			Failures:   result.Failures,
			Extracted:  result.Extracted,
			Failed:     result.Failed,
		})
	}
	report.Passed = report.Total - report.Failed
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("编码 JSON 报告: %w", err)
	}
	return string(encoded) + "\n", nil
}

// jsonReport 是 JSON 报告的顶层形状。
type jsonReport struct {
	Collection string       `json:"collection"`
	Total      int          `json:"total"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	Results    []jsonResult `json:"results"`
}

// jsonResult 是一条请求的结果（durationMs 用毫秒，Duration 原样编会变纳秒）。
type jsonResult struct {
	Name       string            `json:"name"`
	File       string            `json:"file"`
	Method     string            `json:"method"`
	URL        string            `json:"url"`
	Status     int               `json:"status"`
	DurationMS int64             `json:"durationMs"`
	Error      string            `json:"error,omitempty"`
	Failures   []string          `json:"failures,omitempty"`
	Extracted  map[string]string `json:"extracted,omitempty"`
	Failed     bool              `json:"failed"`
}
