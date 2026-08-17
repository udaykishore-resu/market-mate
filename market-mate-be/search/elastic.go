package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"market-mate/models"
)

// requestTimeout bounds every call. Search sits on a user request, and an
// Elasticsearch that has stopped answering must degrade to an error the handler
// can report, not hold the connection.
const requestTimeout = 5 * time.Second

// Client is a small typed client for the three Elasticsearch calls this service
// makes.
type Client struct {
	baseURL string
	index   string
	http    *http.Client
}

// document is the indexed shape. Field names are snake_case to match the
// mapping; the API's own JSON stays camelCase, so the two can change
// independently.
type document struct {
	VideoID      string          `json:"video_id"`
	Title        string          `json:"title"`
	Channel      string          `json:"channel"`
	Ingredients  []docIngredient `json:"ingredients"`
	Source       string          `json:"source"`
	Simulated    bool            `json:"simulated"`
	ModelVersion string          `json:"model_version,omitempty"`
	IndexedAt    time.Time       `json:"indexed_at"`
}

type docIngredient struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	// Unit is mapped and reserved, but empty today: the extractor emits a single
	// free-text quantity ("400 g", "to taste") and splitting that into a number
	// and a unit would be inventing structure the model never produced.
	Unit string `json:"unit"`
}

// NewClient validates the URL and index name up front. Both come from the
// environment, and a typo should surface at boot next to the other dependency
// lines rather than as a 404 on the first search.
func NewClient(rawURL, index string) (*Client, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("MM_ELASTIC_URL %q is not an absolute URL", rawURL)
	}
	index = strings.TrimSpace(index)
	if index == "" {
		return nil, fmt.Errorf("MM_ELASTIC_INDEX must not be empty")
	}
	return &Client{
		baseURL: strings.TrimRight(u.String(), "/"),
		index:   index,
		http:    &http.Client{Timeout: requestTimeout},
	}, nil
}

func (c *Client) Kind() string { return KindElasticsearch }

// Health reports cluster reachability. A yellow cluster is healthy for this
// purpose: single-node development clusters are permanently yellow because
// replicas have nowhere to go.
func (c *Client) Health(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/_cluster/health", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("decoding cluster health: %w", err)
	}
	if body.Status == "red" {
		return fmt.Errorf("elasticsearch cluster status is red")
	}
	return nil
}

// EnsureIndex creates the index if it is missing. Called once at boot: an index
// created implicitly by the first write gets dynamic mappings, which would make
// ingredients a flat object array and quietly break the nested query.
func (c *Client) EnsureIndex(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodHead, "/"+c.index, nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("checking index %s: unexpected status %d", c.index, resp.StatusCode)
	}

	create, err := c.do(ctx, http.MethodPut, "/"+c.index, indexMapping())
	if err != nil {
		return err
	}
	defer create.Body.Close()
	if create.StatusCode >= 300 {
		// Another replica winning the same race is success, not failure.
		body, _ := io.ReadAll(io.LimitReader(create.Body, 2048))
		if strings.Contains(string(body), "resource_already_exists_exception") {
			return nil
		}
		return fmt.Errorf("creating index %s: %s: %s", c.index, create.Status, body)
	}
	return nil
}

// Index writes one recipe, keyed by video ID so a re-extraction replaces the
// document instead of duplicating it.
//
// refresh=wait_for rather than the default: a user who has just processed a
// video and searches for it must find it. Waiting for the next refresh costs up
// to a second on the write path and saves a result that is inexplicably absent.
func (c *Client) Index(ctx context.Context, recipe models.Recipe) error {
	if strings.TrimSpace(recipe.VideoID) == "" {
		return fmt.Errorf("indexing recipe: empty video id")
	}
	doc := toDocument(recipe)

	resp, err := c.do(ctx, http.MethodPut,
		"/"+c.index+"/_doc/"+url.PathEscape(recipe.VideoID)+"?refresh=wait_for", doc)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("indexing %s: %s: %s", recipe.VideoID, resp.Status, body)
	}
	return nil
}

func (c *Client) Search(ctx context.Context, q Query) ([]models.Recipe, error) {
	resp, err := c.do(ctx, http.MethodPost, "/"+c.index+"/_search", buildSearchBody(q))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// The index has not been created yet: no documents, not an outage.
		return []models.Recipe{}, nil
	}
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("searching %s: %s: %s", c.index, resp.Status, body)
	}

	var payload struct {
		Hits struct {
			Hits []struct {
				Source document `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}

	out := make([]models.Recipe, 0, len(payload.Hits.Hits))
	for _, h := range payload.Hits.Hits {
		out = append(out, fromDocument(h.Source))
	}
	return out, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any) (*http.Response, error) {
	// The cancel func cannot be deferred here — the response body is still being
	// read by the caller — so it is bound to the body: closing the body cancels
	// the request context.
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("encoding request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// cancelOnClose ties the request context's lifetime to the response body, so no
// call site can leak the context by forgetting a defer.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}

func toDocument(r models.Recipe) document {
	ingredients := make([]docIngredient, 0, len(r.Ingredients))
	for _, i := range r.Ingredients {
		ingredients = append(ingredients, docIngredient{Name: i.Name, Quantity: i.Quantity})
	}
	indexedAt := r.IndexedAt
	if indexedAt.IsZero() {
		indexedAt = time.Now().UTC()
	}
	return document{
		VideoID:      r.VideoID,
		Title:        r.Title,
		Channel:      r.Channel,
		Ingredients:  ingredients,
		Source:       r.Source,
		Simulated:    r.Simulated,
		ModelVersion: r.ModelVersion,
		IndexedAt:    indexedAt,
	}
}

func fromDocument(d document) models.Recipe {
	ingredients := make([]models.Ingredient, 0, len(d.Ingredients))
	for _, i := range d.Ingredients {
		ingredients = append(ingredients, models.Ingredient{Name: i.Name, Quantity: i.Quantity})
	}
	// Simulated is read back from the document rather than recomputed, but a
	// fixture source forces it: an old document written before this field
	// existed must not come back looking live.
	simulated := d.Simulated || d.Source == models.SourceFixture
	return models.Recipe{
		VideoID:      d.VideoID,
		Title:        d.Title,
		Channel:      d.Channel,
		Ingredients:  ingredients,
		Source:       d.Source,
		Simulated:    simulated,
		ModelVersion: d.ModelVersion,
		IndexedAt:    d.IndexedAt,
	}
}
