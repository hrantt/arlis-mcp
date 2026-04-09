package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	base        = "https://www.arlis.am"
	maxBodyChars = 40_000
)

var catalogs = map[string]int{
	"EEU":      1,
	"ECHR":     3,
	"cassation": 4,
	"yerevan":  5,
}

var actIDRe = regexp.MustCompile(`/acts/(\d+)`)

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func newClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func ajaxGet(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; arlis-mcp/1.0)")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Accept", "application/json, text/html")
	req.Header.Set("Referer", base+"/")

	resp, err := newClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

type ActCard struct {
	ID            int               `json:"id"`
	URL           string            `json:"url"`
	Title         string            `json:"title"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

func parseActCards(html, lang string) []ActCard {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}
	var cards []ActCard
	doc.Find("div.act-card").Each(func(_ int, s *goquery.Selection) {
		var card ActCard
		card.Metadata = map[string]string{}

		// ID and URL
		if href, ok := s.Find("a.title").Attr("href"); ok {
			if m := actIDRe.FindStringSubmatch(href); m != nil {
				card.ID, _ = strconv.Atoi(m[1])
				card.URL = base + href
			}
		}
		// Title
		card.Title = strings.TrimSpace(s.Find("span.text-content").Text())

		// Metadata items
		s.Find("div.act-card__about-item").Each(func(_ int, item *goquery.Selection) {
			label := strings.TrimRight(strings.TrimSpace(item.Find("div.act-card__about-title").Text()), "`")
			value := strings.TrimSpace(item.Find("div.act-card__about-value").Text())
			if label != "" && value != "" {
				card.Metadata[label] = value
			}
		})

		if card.ID != 0 {
			cards = append(cards, card)
		}
	})
	return cards
}

type ActPage struct {
	ID               int               `json:"id"`
	URL              string            `json:"url"`
	Title            string            `json:"title"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	LanguageVersions map[string]string `json:"language_versions,omitempty"`
	DownloadURL      string            `json:"download_url"`
	Body             string            `json:"body"`
	BodyOffset       int               `json:"body_offset,omitempty"`
	BodyChunkSize    int               `json:"body_chunk_size,omitempty"`
	BodyTotalChars   int               `json:"body_total_chars"`
	BodyHasMore      bool              `json:"body_has_more,omitempty"`
	MatchCount       int               `json:"match_count,omitempty"`
}

var langHrefRe = regexp.MustCompile(`/(hy|en|ru)/acts/(\d+)`)

func parseActPage(html string, actID int, lang string, bodyOffset, bodyChunkSize int, search string) ActPage {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ActPage{}
	}

	act := ActPage{
		ID:          actID,
		URL:         fmt.Sprintf("%s/%s/acts/%d/latest", base, lang, actID),
		DownloadURL: fmt.Sprintf("%s/%s/acts/%d/download/act", base, lang, actID),
		Metadata:    map[string]string{},
		LanguageVersions: map[string]string{},
	}

	// Title
	act.Title = strings.TrimSpace(doc.Find("div.act-info__title").Text())
	if act.Title == "" {
		act.Title = strings.TrimSpace(doc.Find("title").Text())
	}

	// Metadata
	doc.Find("div.act-info__item").Each(func(_ int, s *goquery.Selection) {
		label := strings.TrimSpace(s.Find("div.act-info__label").Text())
		value := strings.TrimSpace(s.Find("div.act-info__value").Text())
		if label != "" && value != "" {
			act.Metadata[label] = value
		}
	})

	// Language versions
	doc.Find("a.cursor-pointer").Each(func(_ int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			if m := langHrefRe.FindStringSubmatch(href); m != nil {
				act.LanguageVersions[m[1]] = base + href
			}
		}
	})

	// Body: collect all <p> tags
	var paragraphs []string
	doc.Find("p").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text != "" {
			paragraphs = append(paragraphs, text)
		}
	})

	// Keyword filter: return only matching paragraphs (±1 context paragraph each)
	if search != "" {
		q := strings.ToLower(search)
		var matched []string
		included := make(map[int]bool)
		for i, p := range paragraphs {
			if strings.Contains(strings.ToLower(p), q) {
				for _, j := range []int{i - 1, i, i + 1} {
					if j >= 0 && j < len(paragraphs) && !included[j] {
						included[j] = true
						matched = append(matched, paragraphs[j])
					}
				}
			}
		}
		fullBody := strings.Join(paragraphs, "\n\n")
		act.BodyTotalChars = len([]rune(fullBody))
		act.MatchCount = len(matched)
		act.Body = strings.Join(matched, "\n\n---\n\n")
		return act
	}

	// Default: character-based pagination
	fullBody := strings.Join(paragraphs, "\n\n")
	act.BodyTotalChars = len([]rune(fullBody))
	runes := []rune(fullBody)
	end := bodyOffset + bodyChunkSize
	if end > len(runes) {
		end = len(runes)
	}
	start := bodyOffset
	if start > len(runes) {
		start = len(runes)
	}
	act.Body = string(runes[start:end])
	act.BodyOffset = bodyOffset
	act.BodyChunkSize = len([]rune(act.Body))
	act.BodyHasMore = end < len(runes)

	return act
}

// ---------------------------------------------------------------------------
// Ajax response wrapper
// ---------------------------------------------------------------------------

type ajaxResp struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	HTML    string `json:"html"`
}

func hasNextPage(html string, nextPage int) bool {
	return strings.Contains(html, fmt.Sprintf(`data-page="%d"`, nextPage))
}

// ---------------------------------------------------------------------------
// Tool: search_acts
// ---------------------------------------------------------------------------

func handleSearchActs(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	lang := req.GetString("lang", "en")
	page := req.GetInt("page", 1)
	if page < 1 {
		page = 1
	}

	params := map[string]any{}
	if v := req.GetString("query", ""); v != "" {
		params["simple_text"] = v
	}
	if v := req.GetString("number", ""); v != "" {
		params["number"] = v
	}
	if v := req.GetInt("year", 0); v > 0 {
		params["year"] = strconv.Itoa(v)
	}
	if v := req.GetString("text_filter", "all"); v == "title" || v == "body" {
		params["text_filter"] = v
	}
	if req.GetBool("exact_match", false) {
		params["exact_match"] = "true"
	}
	if req.GetBool("exclude_amending", false) {
		params["exclude_changing_acts"] = "true"
	}
	if v := req.GetString("catalog", "all"); v != "" && v != "all" {
		if id, ok := catalogs[v]; ok {
			params["catalog_id"] = id
		}
	}

	orderBy := req.GetString("order_by", "")
	orderDir := req.GetString("order_dir", "ASC")
	order := ""
	if orderBy != "" {
		order = strings.TrimSpace(orderBy + " " + orderDir)
	}

	paramsJSON, _ := json.Marshal(params)
	encoded := url.QueryEscape(string(paramsJSON))
	path := fmt.Sprintf("/%s/search/page/%d?%s&order_by=%s", lang, page, encoded, url.QueryEscape(order))

	body, err := ajaxGet(path)
	if err != nil {
		return mcp.NewToolResultError("Search request failed: " + err.Error()), nil
	}

	var resp ajaxResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return mcp.NewToolResultError("Failed to parse search response"), nil
	}
	if resp.Status != 0 {
		return mcp.NewToolResultError("Search failed: " + resp.Message), nil
	}

	cards := parseActCards(resp.HTML, lang)
	out := map[string]any{
		"page":          page,
		"has_next_page": hasNextPage(resp.HTML, page+1),
		"results":       cards,
	}
	outJSON, _ := json.MarshalIndent(out, "", "  ")
	return mcp.NewToolResultText(string(outJSON)), nil
}

// ---------------------------------------------------------------------------
// Tool: get_act
// ---------------------------------------------------------------------------

func handleGetAct(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	actID, err := req.RequireInt("act_id")
	if err != nil {
		return mcp.NewToolResultError("act_id is required"), nil
	}

	lang := req.GetString("lang", "en")
	bodyOffset := req.GetInt("body_offset", 0)
	if bodyOffset < 0 {
		bodyOffset = 0
	}
	bodyChunkSize := req.GetInt("body_chunk_size", 20_000)
	if bodyChunkSize <= 0 {
		bodyChunkSize = 20_000
	}
	if bodyChunkSize > maxBodyChars {
		bodyChunkSize = maxBodyChars
	}

	search := req.GetString("search", "")

	path := fmt.Sprintf("/%s/acts/%d/latest", lang, actID)
	body, err := ajaxGet(path)
	if err != nil {
		if err.Error() == "not found" {
			return mcp.NewToolResultError(fmt.Sprintf("Act %d not found", actID)), nil
		}
		return mcp.NewToolResultError("Request failed: " + err.Error()), nil
	}

	act := parseActPage(string(body), actID, lang, bodyOffset, bodyChunkSize, search)
	out, _ := json.MarshalIndent(act, "", "  ")
	return mcp.NewToolResultText(string(out)), nil
}

// ---------------------------------------------------------------------------
// Tool: get_recent_acts
// ---------------------------------------------------------------------------

func handleGetRecentActs(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	lang := req.GetString("lang", "en")
	page := req.GetInt("page", 1)
	if page < 1 {
		page = 1
	}

	path := fmt.Sprintf("/%s/acts/recents/page/%d", lang, page)
	body, err := ajaxGet(path)
	if err != nil {
		return mcp.NewToolResultError("Request failed: " + err.Error()), nil
	}

	var resp ajaxResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return mcp.NewToolResultError("Failed to parse response"), nil
	}
	if resp.Status != 0 {
		return mcp.NewToolResultError("Failed to fetch recent acts: " + resp.Message), nil
	}

	cards := parseActCards(resp.HTML, lang)
	out := map[string]any{
		"page":          page,
		"has_next_page": hasNextPage(resp.HTML, page+1),
		"results":       cards,
	}
	outJSON, _ := json.MarshalIndent(out, "", "  ")
	return mcp.NewToolResultText(string(outJSON)), nil
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	s := server.NewMCPServer(
		"arlis-mcp",
		"1.0.0",
		server.WithToolCapabilities(false),
	)

	s.AddTool(mcp.NewTool("search_acts",
		mcp.WithDescription("Search the Armenian Legal Information System (ARLIS) for legal acts. Supports keyword search, filtering by year, act number, document type, and collection (ECHR, EEU, Cassation Court, Yerevan Municipality)."),
		mcp.WithString("query", mcp.Description("Keyword(s) to search for in act titles and/or body text.")),
		mcp.WithString("number", mcp.Description("Act number (e.g. 'HO-239').")),
		mcp.WithNumber("year", mcp.Description("Enactment year (e.g. 1998).")),
		mcp.WithString("lang", mcp.Description("Language: 'en' English, 'hy' Armenian, 'ru' Russian."), mcp.DefaultString("en"), mcp.Enum("en", "hy", "ru")),
		mcp.WithNumber("page", mcp.Description("Page number (1-based)."), mcp.DefaultNumber(1)),
		mcp.WithString("catalog", mcp.Description("Document collection: all, EEU, ECHR, cassation, yerevan."), mcp.DefaultString("all"), mcp.Enum("all", "EEU", "ECHR", "cassation", "yerevan")),
		mcp.WithString("text_filter", mcp.Description("Where to search: all, title, body."), mcp.DefaultString("all"), mcp.Enum("all", "title", "body")),
		mcp.WithBoolean("exact_match", mcp.Description("Require exact phrase match."), mcp.DefaultBool(false)),
		mcp.WithBoolean("exclude_amending", mcp.Description("Exclude acts that merely amend other acts."), mcp.DefaultBool(false)),
		mcp.WithString("order_by", mcp.Description("Sort field: empty (relevance), title, enactment_date, effective_date."), mcp.DefaultString("")),
		mcp.WithString("order_dir", mcp.Description("Sort direction."), mcp.DefaultString("ASC"), mcp.Enum("ASC", "DESC")),
	), handleSearchActs)

	s.AddTool(mcp.NewTool("get_act",
		mcp.WithDescription("Fetch the text and metadata of a specific Armenian legal act by its numeric ARLIS ID. Use 'search' to filter the body to only paragraphs matching a keyword — this avoids paginating through large documents like codes. Without 'search', large documents are paginated — check body_has_more and advance body_offset by body_chunk_size to read more."),
		mcp.WithNumber("act_id", mcp.Description("Numeric ARLIS act ID (e.g. 205622 for the Civil Code)."), mcp.Required()),
		mcp.WithString("lang", mcp.Description("Language version to retrieve."), mcp.DefaultString("en"), mcp.Enum("en", "hy", "ru")),
		mcp.WithString("search", mcp.Description("Keyword to filter body paragraphs. Returns only matching paragraphs with one paragraph of context on each side. Use this instead of pagination for targeted lookups in large documents.")),
		mcp.WithNumber("body_offset", mcp.Description("Character offset to start reading from (0-based). Only used when 'search' is not set."), mcp.DefaultNumber(0)),
		mcp.WithNumber("body_chunk_size", mcp.Description("Max characters to return. Hard-capped at 40000. Only used when 'search' is not set."), mcp.DefaultNumber(20000)),
	), handleGetAct)

	s.AddTool(mcp.NewTool("get_recent_acts",
		mcp.WithDescription("Return the most recently published acts in the ARLIS database."),
		mcp.WithNumber("page", mcp.Description("Page number (1-based)."), mcp.DefaultNumber(1)),
		mcp.WithString("lang", mcp.Description("Language for titles and metadata."), mcp.DefaultString("en"), mcp.Enum("en", "hy", "ru")),
	), handleGetRecentActs)

	if err := server.ServeStdio(s); err != nil {
		panic(err)
	}
}
