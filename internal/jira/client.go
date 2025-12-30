// Package jira provides a native Go client for Jira REST API integration.
// This replaces the previous Python script-based approach for better distribution.
package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Client provides methods to interact with Jira REST API.
type Client struct {
	baseURL    string
	project    string
	username   string
	apiToken   string
	httpClient *http.Client
	isCloud    bool
}

// Config holds the Jira client configuration.
type Config struct {
	URL      string // Jira instance URL (e.g., https://company.atlassian.net)
	Project  string // Jira project key (e.g., PROJ)
	Username string // Username (email for Cloud, username for Server)
	APIToken string // API token (Cloud) or PAT/password (Server)
}

// NewClient creates a new Jira API client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("jira URL is required")
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("jira API token is required")
	}

	// Normalize URL
	baseURL := strings.TrimSuffix(cfg.URL, "/")
	isCloud := strings.Contains(baseURL, "atlassian.net")

	if isCloud && cfg.Username == "" {
		return nil, fmt.Errorf("username (email) is required for Jira Cloud")
	}

	return &Client{
		baseURL:    baseURL,
		project:    cfg.Project,
		username:   cfg.Username,
		apiToken:   cfg.APIToken,
		isCloud:    isCloud,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// authHeader returns the appropriate Authorization header value.
func (c *Client) authHeader() string {
	if c.isCloud || c.username != "" {
		// Basic auth with username:token (Cloud) or username:password (Server)
		credentials := c.username + ":" + c.apiToken
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(credentials))
	}
	// Bearer token (PAT) for Server/DC without username
	return "Bearer " + c.apiToken
}

// doRequest executes an HTTP request with authentication.
func (c *Client) doRequest(ctx context.Context, method, endpoint string, body io.Reader) (*http.Response, error) {
	reqURL := c.baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", c.authHeader())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bd-jira/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	return resp, nil
}

// SearchIssues fetches issues from Jira using JQL.
// If jql is empty, it searches all issues in the configured project.
// state can be "open", "closed", or "all".
func (c *Client) SearchIssues(ctx context.Context, jql string, state string) ([]*JiraIssue, error) {
	// Build JQL query
	query := jql
	if query == "" {
		if c.project == "" {
			return nil, fmt.Errorf("either project or JQL query is required")
		}
		query = fmt.Sprintf("project = %s", c.project)
		switch state {
		case "open":
			query += " AND status != Done AND status != Closed"
		case "closed":
			query += " AND (status = Done OR status = Closed)"
		// "all" or empty - no additional filter
		}
	}

	var allIssues []*JiraIssue
	startAt := 0
	maxResults := 100

	for {
		// Use API v3 (v2 returns HTTP 410 Gone)
		// See: https://developer.atlassian.com/changelog/#CHANGE-2046
		endpoint := fmt.Sprintf("/rest/api/3/search/jql?jql=%s&startAt=%d&maxResults=%d&expand=changelog",
			url.QueryEscape(query), startAt, maxResults)

		resp, err := c.doRequest(ctx, "GET", endpoint, nil)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, c.handleAPIError(resp.StatusCode, body)
		}

		var result searchResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding response: %w", err)
		}
		resp.Body.Close()

		allIssues = append(allIssues, result.Issues...)

		startAt += len(result.Issues)
		if startAt >= result.Total || len(result.Issues) == 0 {
			break
		}
	}

	return allIssues, nil
}

// handleAPIError creates descriptive error messages for API errors.
func (c *Client) handleAPIError(statusCode int, body []byte) error {
	msg := fmt.Sprintf("Jira API error %d", statusCode)

	switch statusCode {
	case http.StatusUnauthorized:
		msg += "\nAuthentication failed. Check your credentials."
		if c.isCloud {
			msg += "\nFor Jira Cloud, use your email as username and an API token."
			msg += "\nCreate a token at: https://id.atlassian.com/manage-profile/security/api-tokens"
		} else {
			msg += "\nFor Jira Server/DC, use a Personal Access Token or username/password."
		}
	case http.StatusForbidden:
		msg += fmt.Sprintf("\nAccess forbidden. Check permissions for project.\n%s", string(body))
	case http.StatusBadRequest:
		msg += fmt.Sprintf("\nBad request (invalid JQL?): %s", string(body))
	default:
		msg += fmt.Sprintf("\n%s", string(body))
	}

	return fmt.Errorf("%s", msg)
}

// searchResponse represents the Jira search API response.
type searchResponse struct {
	StartAt    int           `json:"startAt"`
	MaxResults int           `json:"maxResults"`
	Total      int           `json:"total"`
	Issues     []*JiraIssue  `json:"issues"`
}

// JiraIssue represents a Jira issue from the API.
type JiraIssue struct {
	Key    string          `json:"key"`
	Fields JiraIssueFields `json:"fields"`
}

// JiraIssueFields contains the issue field data.
type JiraIssueFields struct {
	Summary     string         `json:"summary"`
	Description any            `json:"description"` // Can be string or ADF document
	Status      *JiraStatus    `json:"status"`
	Priority    *JiraPriority  `json:"priority"`
	IssueType   *JiraIssueType `json:"issuetype"`
	Assignee    *JiraUser      `json:"assignee"`
	Reporter    *JiraUser      `json:"reporter"`
	Labels      []string       `json:"labels"`
	Created     string         `json:"created"`
	Updated     string         `json:"updated"`
	Resolution  *JiraResolution `json:"resolution"`
	ResolutionDate string      `json:"resolutiondate"`
	Parent      *JiraParent    `json:"parent"`
	IssueLinks  []*JiraIssueLink `json:"issuelinks"`
}

// JiraStatus represents a Jira status.
type JiraStatus struct {
	Name string `json:"name"`
}

// JiraPriority represents a Jira priority.
type JiraPriority struct {
	Name string `json:"name"`
}

// JiraIssueType represents a Jira issue type.
type JiraIssueType struct {
	Name    string `json:"name"`
	Subtask bool   `json:"subtask"`
}

// JiraUser represents a Jira user.
type JiraUser struct {
	Name        string `json:"name"`        // Server/DC
	DisplayName string `json:"displayName"` // Cloud
	EmailAddress string `json:"emailAddress"`
}

// JiraResolution represents a Jira resolution.
type JiraResolution struct {
	Name string `json:"name"`
}

// JiraParent represents a parent issue reference.
type JiraParent struct {
	Key string `json:"key"`
}

// JiraIssueLink represents an issue link.
type JiraIssueLink struct {
	Type         *JiraLinkType `json:"type"`
	InwardIssue  *JiraLinkedIssue `json:"inwardIssue"`
	OutwardIssue *JiraLinkedIssue `json:"outwardIssue"`
}

// JiraLinkType represents an issue link type.
type JiraLinkType struct {
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

// JiraLinkedIssue represents a linked issue reference.
type JiraLinkedIssue struct {
	Key string `json:"key"`
}

// GetDescription returns the description as a plain string.
// Handles both string descriptions (Server/DC) and ADF documents (Cloud).
func (f *JiraIssueFields) GetDescription() string {
	if f.Description == nil {
		return ""
	}

	// Try string first (Jira Server/DC)
	if s, ok := f.Description.(string); ok {
		return s
	}

	// Try ADF document (Jira Cloud)
	if doc, ok := f.Description.(map[string]any); ok {
		return extractTextFromADF(doc)
	}

	return ""
}

// extractTextFromADF extracts plain text from Atlassian Document Format.
func extractTextFromADF(doc map[string]any) string {
	var sb strings.Builder
	extractTextFromNode(doc, &sb)
	return strings.TrimSpace(sb.String())
}

func extractTextFromNode(node map[string]any, sb *strings.Builder) {
	// Handle text nodes
	if nodeType, ok := node["type"].(string); ok {
		if nodeType == "text" {
			if text, ok := node["text"].(string); ok {
				sb.WriteString(text)
			}
			return
		}

		// Add newlines for block elements
		if nodeType == "paragraph" || nodeType == "heading" || nodeType == "bulletList" ||
			nodeType == "orderedList" || nodeType == "listItem" || nodeType == "codeBlock" {
			if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
				sb.WriteString("\n")
			}
		}
	}

	// Recurse into content array
	if content, ok := node["content"].([]any); ok {
		for _, child := range content {
			if childNode, ok := child.(map[string]any); ok {
				extractTextFromNode(childNode, sb)
			}
		}
	}
}

// GetDisplayName returns the best available name for a user.
func (u *JiraUser) GetDisplayName() string {
	if u == nil {
		return ""
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	if u.Name != "" {
		return u.Name
	}
	return u.EmailAddress
}

// ExtractKeyFromURL extracts a Jira issue key from a browse URL.
// Returns empty string if no key is found.
func ExtractKeyFromURL(externalRef string) string {
	// Match patterns like:
	// https://company.atlassian.net/browse/PROJ-123
	// https://jira.company.com/browse/PROJ-123
	re := regexp.MustCompile(`/browse/([A-Z]+-\d+)`)
	if matches := re.FindStringSubmatch(externalRef); len(matches) == 2 {
		return matches[1]
	}
	return ""
}
