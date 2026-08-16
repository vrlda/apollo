package tools

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"
)

// Extracts paragraphs of raw text from an arbitrary URL and strips HTML tags
func executeWebScrape(rawArgs string) string {
	var args struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.URL == "" {
		return "Error: Invalid arguments. 'url' is required."
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(func() *http.Request {
		req, _ := http.NewRequest("GET", args.URL, nil)
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AgentHQ/1.0 Agentic Proxy")
		return req
	}())

	if err != nil {
		return fmt.Sprintf("Error fetching URL: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Sprintf("Error: Website returned HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "Error reading response body"
	}

	// Use Bluemonday strict policy to strip EVERYTHING except raw text
	p := bluemonday.StrictPolicy()
	cleanHtml := p.Sanitize(string(bodyBytes))

	// Some basic whitespace cleanup since stripping tags leaves massive blank gaps
	lines := strings.Split(cleanHtml, "\n")
	var out []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}

	finalText := strings.Join(out, "\n")
	if len(finalText) > 15000 {
		return finalText[:15000] + "\n\n... [WEBPAGE TRUNCATED FOR LENGTH LIMITS]"
	}

	return finalText
}

// executeWebSearch queries DuckDuckGo Lite for search results (no API key required)
func executeWebSearch(rawArgs string) string {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil || args.Query == "" {
		return "Error: 'query' is required."
	}

	client := &http.Client{Timeout: 15 * time.Second}
	searchURL := "https://lite.duckduckgo.com/lite/?q=" + strings.ReplaceAll(strings.TrimSpace(args.Query), " ", "+")

	req, _ := http.NewRequest("GET", searchURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AgentHQ/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("Error performing search: %v", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := ioutil.ReadAll(resp.Body)

	p := bluemonday.StrictPolicy()
	clean := p.Sanitize(string(bodyBytes))

	// Collect non-empty lines, limit to top results
	lines := strings.Split(clean, "\n")
	var results []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if len(t) > 20 {
			results = append(results, t)
		}
		if len(results) >= 40 {
			break
		}
	}

	if len(results) == 0 {
		return "No search results found."
	}
	return fmt.Sprintf("Search results for: %s\n\n%s", args.Query, strings.Join(results, "\n"))
}
