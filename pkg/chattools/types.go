package chattools

import (
	"context"
	"net/http"
	"time"
)

type SessionInfo struct {
	CurrentTimestamp   string   `json:"current_timestamp"`
	ChatID             string   `json:"chat_id,omitempty"`
	ChatTitle          string   `json:"chat_title,omitempty"`
	ChatFirstMessageAt string   `json:"chat_first_message_at,omitempty"`
	SelectedModel      string   `json:"selected_model,omitempty"`
	SelectedReasoning  string   `json:"selected_reasoning,omitempty"`
	DisabledTools      []string `json:"disabled_tools,omitempty"`
	BeeperUsername     string   `json:"beeper_username,omitempty"`
	BeeperDisplayName  string   `json:"beeper_display_name,omitempty"`
	BeeperAccountEmail string   `json:"beeper_account_email,omitempty"`
	GravatarProfile    any      `json:"gravatar_profile,omitempty"`
	LastKnownTimestamp string   `json:"last_known_timestamp"`
}

type SessionProfile struct {
	Email           string `json:"email,omitempty"`
	Username        string `json:"username,omitempty"`
	FullName        string `json:"full_name,omitempty"`
	MatrixProfile   any    `json:"matrix_profile,omitempty"`
	GravatarProfile any    `json:"gravatar_profile,omitempty"`
}

type SessionOptions struct {
	ResolveProfile func(context.Context, string) (*SessionProfile, error)
}

type FetchOptions struct {
	Timeout     time.Duration
	MaxBytes    int64
	MaxChars    int
	Client      *http.Client
	ExaEndpoint string
	APIKey      string
}

type SearchOptions struct {
	Enabled  bool
	Endpoint string
	APIKey   string
	Timeout  time.Duration
	Client   *http.Client
}

type SearchRequestOptions struct {
	IncludeDomains     []string       `json:"includeDomains,omitempty"`
	ExcludeDomains     []string       `json:"excludeDomains,omitempty"`
	StartCrawlDate     string         `json:"startCrawlDate,omitempty"`
	EndCrawlDate       string         `json:"endCrawlDate,omitempty"`
	StartPublishedDate string         `json:"startPublishedDate,omitempty"`
	EndPublishedDate   string         `json:"endPublishedDate,omitempty"`
	Context            any            `json:"context,omitempty"`
	Moderation         *bool          `json:"moderation,omitempty"`
	Contents           map[string]any `json:"contents,omitempty"`
	AdditionalQueries  []string       `json:"additionalQueries,omitempty"`
	Type               string         `json:"type,omitempty"`
	Category           string         `json:"category,omitempty"`
	UserLocation       string         `json:"userLocation,omitempty"`
	Compliance         string         `json:"compliance,omitempty"`
	OutputSchema       map[string]any `json:"outputSchema,omitempty"`
	SystemPrompt       string         `json:"systemPrompt,omitempty"`
}

type FetchResult struct {
	URL             string          `json:"url"`
	FinalURL        string          `json:"final_url"`
	Status          int             `json:"status"`
	ContentType     string          `json:"content_type,omitempty"`
	Title           string          `json:"title,omitempty"`
	Description     string          `json:"description,omitempty"`
	Text            string          `json:"text,omitempty"`
	Truncated       bool            `json:"truncated"`
	ID              string          `json:"id,omitempty"`
	Published       string          `json:"published,omitempty"`
	Author          string          `json:"author,omitempty"`
	Image           string          `json:"image,omitempty"`
	Favicon         string          `json:"favicon,omitempty"`
	Highlights      []string        `json:"highlights,omitempty"`
	HighlightScores []float64       `json:"highlightScores,omitempty"`
	Summary         any             `json:"summary,omitempty"`
	Subpages        []SearchSubpage `json:"subpages,omitempty"`
	Entities        []any           `json:"entities,omitempty"`
	Extras          map[string]any  `json:"extras,omitempty"`
	Source          string          `json:"source,omitempty"`
	RequestID       string          `json:"requestId,omitempty"`
	Context         string          `json:"context,omitempty"`
	Error           string          `json:"error,omitempty"`
	FetchMethod     string          `json:"-"`
}

type SearchResult struct {
	Query              string         `json:"query"`
	RequestID          string         `json:"requestId,omitempty"`
	ResolvedSearchType string         `json:"resolvedSearchType,omitempty"`
	SearchType         string         `json:"searchType,omitempty"`
	Context            string         `json:"context,omitempty"`
	Output             map[string]any `json:"output,omitempty"`
	Results            []SearchItem   `json:"results"`
}

type SearchItem struct {
	ID              string          `json:"id,omitempty"`
	Title           string          `json:"title"`
	URL             string          `json:"url"`
	Snippet         string          `json:"snippet,omitempty"`
	Text            string          `json:"text,omitempty"`
	Highlights      []string        `json:"highlights,omitempty"`
	HighlightScores []float64       `json:"highlightScores,omitempty"`
	Summary         string          `json:"summary,omitempty"`
	Description     string          `json:"description,omitempty"`
	Published       string          `json:"published,omitempty"`
	PublishedDate   string          `json:"publishedDate,omitempty"`
	SiteName        string          `json:"siteName,omitempty"`
	Author          string          `json:"author,omitempty"`
	Image           string          `json:"image,omitempty"`
	Favicon         string          `json:"favicon,omitempty"`
	Source          string          `json:"source,omitempty"`
	Subpages        []SearchSubpage `json:"subpages,omitempty"`
	Entities        []any           `json:"entities,omitempty"`
	Extras          map[string]any  `json:"extras,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
}

type SearchSubpage struct {
	ID              string         `json:"id,omitempty"`
	Title           string         `json:"title"`
	URL             string         `json:"url"`
	Text            string         `json:"text,omitempty"`
	Highlights      []string       `json:"highlights,omitempty"`
	HighlightScores []float64      `json:"highlightScores,omitempty"`
	Summary         string         `json:"summary,omitempty"`
	Description     string         `json:"description,omitempty"`
	PublishedDate   string         `json:"publishedDate,omitempty"`
	Published       string         `json:"published,omitempty"`
	SiteName        string         `json:"siteName,omitempty"`
	Author          string         `json:"author,omitempty"`
	Image           string         `json:"image,omitempty"`
	Favicon         string         `json:"favicon,omitempty"`
	Source          string         `json:"source,omitempty"`
	Entities        []any          `json:"entities,omitempty"`
	Extras          map[string]any `json:"extras,omitempty"`
}
