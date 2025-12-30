package jira

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestParseJiraTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"with Z suffix", "2024-01-15T10:30:00.000Z", false},
		{"with +0000", "2024-01-15T10:30:00.000+0000", false},
		{"with +00:00", "2024-01-15T10:30:00.000+00:00", false},
		{"with offset", "2024-01-15T10:30:00.000-0500", false},
		{"without ms", "2024-01-15T10:30:00+0000", false},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseJiraTimestamp(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJiraTimestamp(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestConverter_MapStatus(t *testing.T) {
	converter := NewConverter(ConverterConfig{JiraURL: "https://test.atlassian.net"})

	tests := []struct {
		jiraStatus string
		want       types.Status
	}{
		{"To Do", types.StatusOpen},
		{"todo", types.StatusOpen},
		{"Open", types.StatusOpen},
		{"Backlog", types.StatusOpen},
		{"In Progress", types.StatusInProgress},
		{"In Review", types.StatusInProgress},
		{"Blocked", types.StatusBlocked},
		{"On Hold", types.StatusBlocked},
		{"Done", types.StatusClosed},
		{"Closed", types.StatusClosed},
		{"Resolved", types.StatusClosed},
		{"Unknown Status", types.StatusOpen}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.jiraStatus, func(t *testing.T) {
			got := converter.mapStatus(&JiraStatus{Name: tt.jiraStatus})
			if got != tt.want {
				t.Errorf("mapStatus(%q) = %v, want %v", tt.jiraStatus, got, tt.want)
			}
		})
	}
}

func TestConverter_MapIssueType(t *testing.T) {
	converter := NewConverter(ConverterConfig{JiraURL: "https://test.atlassian.net"})

	tests := []struct {
		jiraType string
		want     types.IssueType
	}{
		{"Bug", types.TypeBug},
		{"bug", types.TypeBug},
		{"Defect", types.TypeBug},
		{"Story", types.TypeFeature},
		{"Feature", types.TypeFeature},
		{"Enhancement", types.TypeFeature},
		{"Task", types.TypeTask},
		{"Sub-task", types.TypeTask},
		{"Epic", types.TypeEpic},
		{"Technical Task", types.TypeChore},
		{"Unknown", types.TypeTask}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.jiraType, func(t *testing.T) {
			got := converter.mapIssueType(&JiraIssueType{Name: tt.jiraType})
			if got != tt.want {
				t.Errorf("mapIssueType(%q) = %v, want %v", tt.jiraType, got, tt.want)
			}
		})
	}
}

func TestConverter_MapPriority(t *testing.T) {
	converter := NewConverter(ConverterConfig{JiraURL: "https://test.atlassian.net"})

	tests := []struct {
		jiraPriority string
		want         int
	}{
		{"Highest", 0},
		{"Critical", 0},
		{"High", 1},
		{"Major", 1},
		{"Medium", 2},
		{"Normal", 2},
		{"Low", 3},
		{"Minor", 3},
		{"Lowest", 4},
		{"Trivial", 4},
		{"Unknown", 2}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.jiraPriority, func(t *testing.T) {
			got := converter.mapPriority(&JiraPriority{Name: tt.jiraPriority})
			if got != tt.want {
				t.Errorf("mapPriority(%q) = %v, want %v", tt.jiraPriority, got, tt.want)
			}
		})
	}
}

func TestConverter_Convert(t *testing.T) {
	converter := NewConverter(ConverterConfig{
		JiraURL: "https://test.atlassian.net",
		Prefix:  "test",
	})

	jiraIssues := []*JiraIssue{
		{
			Key: "PROJ-123",
			Fields: JiraIssueFields{
				Summary:     "Test issue",
				Description: "Test description",
				Status:      &JiraStatus{Name: "In Progress"},
				Priority:    &JiraPriority{Name: "High"},
				IssueType:   &JiraIssueType{Name: "Bug"},
				Created:     "2024-01-15T10:30:00.000+0000",
				Updated:     "2024-01-16T11:00:00.000+0000",
				Reporter:    &JiraUser{DisplayName: "John Doe"},
				Assignee:    &JiraUser{DisplayName: "Jane Smith"},
				Labels:      []string{"label1", "label2"},
			},
		},
	}

	issues, err := converter.Convert(jiraIssues)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("Convert() returned %d issues, want 1", len(issues))
	}

	issue := issues[0]

	if issue.Title != "Test issue" {
		t.Errorf("Title = %q, want %q", issue.Title, "Test issue")
	}
	if issue.Description != "Test description" {
		t.Errorf("Description = %q, want %q", issue.Description, "Test description")
	}
	if issue.Status != types.StatusInProgress {
		t.Errorf("Status = %v, want %v", issue.Status, types.StatusInProgress)
	}
	if issue.Priority != 1 {
		t.Errorf("Priority = %d, want 1", issue.Priority)
	}
	if issue.IssueType != types.TypeBug {
		t.Errorf("IssueType = %v, want %v", issue.IssueType, types.TypeBug)
	}
	if issue.CreatedBy != "John Doe" {
		t.Errorf("CreatedBy = %q, want %q", issue.CreatedBy, "John Doe")
	}
	if issue.Assignee != "Jane Smith" {
		t.Errorf("Assignee = %q, want %q", issue.Assignee, "Jane Smith")
	}
	if issue.ExternalRef == nil || *issue.ExternalRef != "https://test.atlassian.net/browse/PROJ-123" {
		t.Errorf("ExternalRef = %v, want https://test.atlassian.net/browse/PROJ-123", issue.ExternalRef)
	}
	if len(issue.Labels) != 2 {
		t.Errorf("Labels count = %d, want 2", len(issue.Labels))
	}
}

func TestConverter_ConvertWithIDGenerator(t *testing.T) {
	counter := 0
	converter := NewConverter(ConverterConfig{
		JiraURL: "https://test.atlassian.net",
		IDGenerator: func(title string, timestamp time.Time) (string, error) {
			counter++
			return "custom-" + title[:4], nil
		},
	})

	jiraIssues := []*JiraIssue{
		{
			Key: "PROJ-1",
			Fields: JiraIssueFields{
				Summary: "First issue",
				Created: "2024-01-15T10:30:00.000+0000",
				Updated: "2024-01-15T10:30:00.000+0000",
			},
		},
	}

	issues, err := converter.Convert(jiraIssues)
	if err != nil {
		t.Fatalf("Convert() error = %v", err)
	}

	if issues[0].ID != "custom-Firs" {
		t.Errorf("ID = %q, want %q", issues[0].ID, "custom-Firs")
	}
}

func TestExtractKeyFromURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://company.atlassian.net/browse/PROJ-123", "PROJ-123"},
		{"https://jira.company.com/browse/ABC-456", "ABC-456"},
		{"https://test.atlassian.net/browse/TEST-1", "TEST-1"},
		{"https://example.com/not-a-jira-url", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ExtractKeyFromURL(tt.input)
			if got != tt.want {
				t.Errorf("ExtractKeyFromURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractTextFromADF(t *testing.T) {
	// Simple ADF document
	doc := map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{
			map[string]any{
				"type": "paragraph",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Hello ",
					},
					map[string]any{
						"type": "text",
						"text": "World",
					},
				},
			},
		},
	}

	result := extractTextFromADF(doc)
	if result != "Hello World" {
		t.Errorf("extractTextFromADF() = %q, want %q", result, "Hello World")
	}
}

func TestJiraIssueFields_GetDescription(t *testing.T) {
	t.Run("string description", func(t *testing.T) {
		fields := JiraIssueFields{Description: "Plain text description"}
		if got := fields.GetDescription(); got != "Plain text description" {
			t.Errorf("GetDescription() = %q, want %q", got, "Plain text description")
		}
	})

	t.Run("nil description", func(t *testing.T) {
		fields := JiraIssueFields{Description: nil}
		if got := fields.GetDescription(); got != "" {
			t.Errorf("GetDescription() = %q, want empty", got)
		}
	})

	t.Run("ADF description", func(t *testing.T) {
		fields := JiraIssueFields{
			Description: map[string]any{
				"type": "doc",
				"content": []any{
					map[string]any{
						"type": "paragraph",
						"content": []any{
							map[string]any{
								"type": "text",
								"text": "ADF content",
							},
						},
					},
				},
			},
		}
		if got := fields.GetDescription(); got != "ADF content" {
			t.Errorf("GetDescription() = %q, want %q", got, "ADF content")
		}
	})
}
