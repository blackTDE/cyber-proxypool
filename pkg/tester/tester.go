package tester

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"cyberproxypool/pkg/dialer"
	"cyberproxypool/pkg/model"
)

var ipRegex = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)

// TestResult holds outcome of node test
type TestResult struct {
	NodeID    string    `json:"node_id"`
	NodeName  string    `json:"node_name"`
	Latency   int64     `json:"latency"` // ms, -1 if unreachable
	ExitIP    string    `json:"exit_ip,omitempty"`
	Success   bool      `json:"success"`
	Error     string    `json:"error,omitempty"`
	TestedAt  time.Time `json:"tested_at"`
}

// Tester performs network reachability, latency measurement, and exit IP detection
type Tester struct {
	defaultURL string
	timeout    time.Duration
}

func NewTester(defaultURL string, timeoutSec int) *Tester {
	if defaultURL == "" {
		defaultURL = "https://api.ipify.org?format=json"
	}
	if timeoutSec <= 0 {
		timeoutSec = 8
	}
	return &Tester{
		defaultURL: defaultURL,
		timeout:    time.Duration(timeoutSec) * time.Second,
	}
}

// TestNode tests a single node using its outbound dialer
func (t *Tester) TestNode(ctx context.Context, node *model.Node, targetURL string) TestResult {
	if targetURL == "" {
		targetURL = t.defaultURL
	}

	result := TestResult{
		NodeID:   node.ID,
		NodeName: node.Name,
		TestedAt: time.Now(),
		Latency:  -1,
	}

	outbound, err := dialer.NewDialerFromNode(node)
	if err != nil {
		result.Error = fmt.Sprintf("dialer error: %v", err)
		node.Latency = -1
		node.ErrorMessage = result.Error
		node.LastTestedAt = result.TestedAt
		return result
	}

	client := &http.Client{
		Timeout: t.timeout,
		Transport: &http.Transport{
			DialContext: outbound.DialContext,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			DisableKeepAlives: true,
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		result.Error = fmt.Sprintf("request error: %v", err)
		node.Latency = -1
		node.ErrorMessage = result.Error
		node.LastTestedAt = result.TestedAt
		return result
	}
	req.Header.Set("User-Agent", "curl/8.4.0")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		result.Error = err.Error()
		node.Latency = -1
		node.ErrorMessage = result.Error
		node.LastTestedAt = result.TestedAt
		return result
	}
	defer resp.Body.Close()

	elapsed := time.Since(start).Milliseconds()
	body, _ := io.ReadAll(resp.Body)

	result.Success = true
	result.Latency = elapsed
	node.Latency = elapsed
	node.LastTestedAt = result.TestedAt
	node.ErrorMessage = ""

	// Attempt to extract exit IP from body
	exitIP := extractIPFromBody(body)
	if exitIP != "" {
		result.ExitIP = exitIP
		node.ExitIP = exitIP
	}

	return result
}

// TestAll concurrently tests all provided nodes with controlled concurrency
func (t *Tester) TestAll(ctx context.Context, nodes []*model.Node, targetURL string, concurrency int, onProgress func(result TestResult)) []TestResult {
	if concurrency <= 0 {
		concurrency = 10
	}

	results := make([]TestResult, len(nodes))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, n *model.Node) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			select {
			case <-ctx.Done():
				results[idx] = TestResult{
					NodeID:   n.ID,
					NodeName: n.Name,
					Latency:  -1,
					Error:    "test cancelled",
					TestedAt: time.Now(),
				}
				return
			default:
			}

			res := t.TestNode(ctx, n, targetURL)
			results[idx] = res
			if onProgress != nil {
				onProgress(res)
			}
		}(i, node)
	}

	wg.Wait()
	return results
}

func extractIPFromBody(b []byte) string {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err == nil {
		if ip, ok := m["ip"].(string); ok && ip != "" {
			return strings.TrimSpace(ip)
		}
		if origin, ok := m["origin"].(string); ok && origin != "" {
			return strings.TrimSpace(strings.Split(origin, ",")[0])
		}
	}

	// Regex search for IPv4
	found := ipRegex.FindString(string(b))
	return found
}
