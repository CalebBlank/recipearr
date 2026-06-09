// Package tandoor is a thin client for the Tandoor Recipes REST API.
//
// Endpoints and contract were confirmed empirically against a live Tandoor instance:
//   - Auth:   Authorization: Bearer <tda_…token>
//   - Parse:  POST /api/recipe-from-source/  {"url":…} or {"url":…,"data":"<html>"}
//   - Create: POST /api/recipe/              (name + steps required; food/unit/keyword
//             auto-create by name; unit must be present, null allowed)
//   - Image:  PUT  /api/recipe/{id}/image/   multipart, field image_url (or image=@file)
//   - Foods:  GET  /api/food/                (paginated)
//   - Delete: DELETE /api/recipe/{id}/       -> 204
package tandoor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New returns a client for the given base URL (e.g. http://host:9928) and API token.
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		http:    &http.Client{Timeout: 90 * time.Second},
	}
}

// APIError carries a non-2xx response for inspection by callers.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tandoor: HTTP %d: %s", e.Status, e.Body)
}

// do sends a request with auth and returns the body bytes for a 2xx, else an *APIError.
func (c *Client) do(req *http.Request) ([]byte, error) {
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return body, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any) ([]byte, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// TestConnection verifies the base URL + token by hitting a cheap authenticated endpoint.
// Returns the total recipe count on success.
func (c *Client) TestConnection(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/recipe/?page_size=1", nil)
	if err != nil {
		return 0, err
	}
	body, err := c.do(req)
	if err != nil {
		return 0, err
	}
	var r struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, fmt.Errorf("tandoor: unexpected response from %s: %w", c.baseURL, err)
	}
	return r.Count, nil
}

// RecipeFromSource parses a recipe. Prefer passing dataHTML (raw page HTML) when available:
// it lets Tandoor skip its own fetch (dodging anti-bot blocks and the URL-import rate limit).
// Pass dataHTML="" to let Tandoor fetch sourceURL itself.
func (c *Client) RecipeFromSource(ctx context.Context, sourceURL, dataHTML string) (*RFSResponse, error) {
	payload := map[string]string{"url": sourceURL, "data": dataHTML}
	body, err := c.postJSON(ctx, "/api/recipe-from-source/", payload)
	if err != nil {
		// recipe-from-source returns 400 with a JSON body {error:true,msg:…} on parse failure.
		if apiErr, ok := err.(*APIError); ok {
			var rfs RFSResponse
			if json.Unmarshal([]byte(apiErr.Body), &rfs) == nil && rfs.Error {
				return &rfs, nil
			}
		}
		return nil, err
	}
	var rfs RFSResponse
	if err := json.Unmarshal(body, &rfs); err != nil {
		return nil, err
	}
	return &rfs, nil
}

// CreateRecipe creates a recipe and returns its new id.
func (c *Client) CreateRecipe(ctx context.Context, r RecipeCreate) (int, error) {
	body, err := c.postJSON(ctx, "/api/recipe/", r)
	if err != nil {
		return 0, err
	}
	var cr createResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return 0, err
	}
	return cr.ID, nil
}

// AttachImageURL attaches an image to a recipe by URL (Tandoor fetches it).
func (c *Client) AttachImageURL(ctx context.Context, recipeID int, imageURL string) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("image_url", imageURL); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.baseURL+"/api/recipe/"+strconv.Itoa(recipeID)+"/image/", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	_, err = c.do(req)
	return err
}

// DeleteRecipe removes a recipe (used in tests/cleanup).
func (c *Client) DeleteRecipe(ctx context.Context, recipeID int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.baseURL+"/api/recipe/"+strconv.Itoa(recipeID)+"/", nil)
	if err != nil {
		return err
	}
	_, err = c.do(req)
	return err
}

// ListBooks returns all recipe books (paginated under the hood).
func (c *Client) ListBooks(ctx context.Context) ([]Book, error) {
	var all []Book
	next := c.baseURL + "/api/recipe-book/?page_size=200"
	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		body, err := c.do(req)
		if err != nil {
			return nil, err
		}
		var page bookListResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Results...)
		next = page.Next
	}
	return all, nil
}

// CreateBook creates a recipe book by name and returns its id.
func (c *Client) CreateBook(ctx context.Context, name string) (int, error) {
	body, err := c.postJSON(ctx, "/api/recipe-book/", bookCreate{Name: name, Shared: []int{}})
	if err != nil {
		return 0, err
	}
	var b Book
	if err := json.Unmarshal(body, &b); err != nil {
		return 0, err
	}
	return b.ID, nil
}

// AddRecipeToBook links a recipe to a book (a "book entry").
func (c *Client) AddRecipeToBook(ctx context.Context, bookID, recipeID int) error {
	_, err := c.postJSON(ctx, "/api/recipe-book-entry/", bookEntryCreate{Book: bookID, Recipe: recipeID})
	return err
}

// ListRecipeIDs returns the ids of every recipe (paginated).
func (c *Client) ListRecipeIDs(ctx context.Context) ([]int, error) {
	var ids []int
	next := c.baseURL + "/api/recipe/?page_size=100"
	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		body, err := c.do(req)
		if err != nil {
			return nil, err
		}
		var page struct {
			Next    string `json:"next"`
			Results []struct {
				ID int `json:"id"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		for _, r := range page.Results {
			ids = append(ids, r.ID)
		}
		next = page.Next
	}
	return ids, nil
}

// PostRecipeRaw POSTs a raw JSON recipe body to /api/recipe/ and returns the response.
func (c *Client) PostRecipeRaw(ctx context.Context, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/recipe/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// PatchRecipeRaw PATCHes a raw JSON body onto /api/recipe/{id}/ and returns the response.
func (c *Client) PatchRecipeRaw(ctx context.Context, recipeID int, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		c.baseURL+"/api/recipe/"+strconv.Itoa(recipeID)+"/", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// GetRecipeRaw returns the raw JSON of a recipe (used for verification and UI previews).
func (c *Client) GetRecipeRaw(ctx context.Context, recipeID int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/recipe/"+strconv.Itoa(recipeID)+"/", nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// ListFoods returns every Food in the user's space (paginated under the hood).
// Used by the "communize" enricher to reuse existing foods.
func (c *Client) ListFoods(ctx context.Context) ([]Food, error) {
	var all []Food
	next := c.baseURL + "/api/food/?page_size=200"
	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		body, err := c.do(req)
		if err != nil {
			return nil, err
		}
		var page foodListResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Results...)
		next = page.Next
		// Defensive: ensure absolute URL (Tandoor returns absolute "next" links).
		if next != "" {
			if _, err := url.Parse(next); err != nil {
				break
			}
		}
	}
	return all, nil
}
