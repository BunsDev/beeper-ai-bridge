package chattools

import (
	"net/http"
	"time"
)

type SessionInfo struct {
	Timestamp       string         `json:"timestamp"`
	Timezone        string         `json:"timezone"`
	RoomTitle       string         `json:"room_title,omitempty"`
	RoomID          string         `json:"room_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	ThreadID        string         `json:"thread_id,omitempty"`
	LoginID         string         `json:"login_id,omitempty"`
	ProviderID      string         `json:"provider_id,omitempty"`
	ModelID         string         `json:"model_id,omitempty"`
	ReasoningLevel  string         `json:"reasoning_level,omitempty"`
	DisabledTools   []string       `json:"disabled_tools,omitempty"`
	AttachmentCount int            `json:"attachment_count"`
	Attachments     []Attachment   `json:"attachments,omitempty"`
	Extra           map[string]any `json:"extra,omitempty"`
}

type Attachment struct {
	Type     string `json:"type,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
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
	CostDollars     map[string]any  `json:"costDollars,omitempty"`
	Error           string          `json:"error,omitempty"`
	FetchMethod     string          `json:"fetch_method,omitempty"`
}

type SearchResult struct {
	Query              string         `json:"query"`
	RequestID          string         `json:"requestId,omitempty"`
	ResolvedSearchType string         `json:"resolvedSearchType,omitempty"`
	SearchType         string         `json:"searchType,omitempty"`
	Context            string         `json:"context,omitempty"`
	CostDollars        map[string]any `json:"costDollars,omitempty"`
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
	ID            string `json:"id,omitempty"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	PublishedDate string `json:"publishedDate,omitempty"`
	Published     string `json:"published,omitempty"`
	Author        string `json:"author,omitempty"`
	Image         string `json:"image,omitempty"`
	Favicon       string `json:"favicon,omitempty"`
}
