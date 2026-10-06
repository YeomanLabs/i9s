// Package graph is a small Microsoft Graph client: paging, throttling
// retries, and errors that say what went wrong.
package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TokenFunc returns a bearer token for the given Graph scopes.
type TokenFunc func(ctx context.Context, scopes []string) (string, error)

// Client talks to https://graph.microsoft.com.
type Client struct {
	HTTP  *http.Client
	Token TokenFunc
	Base  string
	// Sleep is replaced in tests so retries don't wait.
	Sleep func(time.Duration)
}

// Error is a non-2xx Graph response.
type Error struct {
	Status  int
	Code    string
	Message string
	Path    string
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s (%d %s)", e.Path, e.Message, e.Status, e.Code)
	}
	return fmt.Sprintf("%s: %s (%d)", e.Path, e.Message, e.Status)
}

// Denied reports a permission, consent or licensing problem.
func (e *Error) Denied() bool {
	return e.Status == 401 || e.Status == 403
}

func New(token TokenFunc) *Client {
	return &Client{HTTP: &http.Client{Timeout: 60 * time.Second}, Token: token, Base: "https://graph.microsoft.com", Sleep: time.Sleep}
}

func (c *Client) url(path string) string {
	if strings.HasPrefix(path, "https://") {
		return path
	}
	p := strings.TrimPrefix(path, "/")
	if !strings.HasPrefix(p, "beta/") && !strings.HasPrefix(p, "v1.0/") {
		p = "v1.0/" + p
	}
	return c.Base + "/" + p
}

// Do sends a request and decodes a JSON response into out (which may be nil).
func (c *Client) Do(ctx context.Context, method, path string, scopes []string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}
	u := c.url(path)
	for attempt := 0; ; attempt++ {
		tok, err := c.Token(ctx, scopes)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("ConsistencyLevel", "eventual")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()

		if res.StatusCode >= 200 && res.StatusCode < 300 {
			if out == nil || len(data) == 0 {
				return nil
			}
			return json.Unmarshal(data, out)
		}
		// Throttled or briefly unavailable: honour Retry-After, back off otherwise.
		if (res.StatusCode == 429 || res.StatusCode == 503 || res.StatusCode == 504) && attempt < 5 {
			wait := time.Duration(1<<attempt) * time.Second
			if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s >= 0 {
				wait = time.Duration(s) * time.Second
			}
			c.Sleep(wait)
			continue
		}
		ge := &Error{Status: res.StatusCode, Message: http.StatusText(res.StatusCode), Path: strings.SplitN(strings.TrimPrefix(u, c.Base), "?", 2)[0]}
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error.Message != "" {
			ge.Code, ge.Message = e.Error.Code, e.Error.Message
		}
		return ge
	}
}

// Get decodes one response.
func (c *Client) Get(ctx context.Context, path string, scopes []string, out any) error {
	return c.Do(ctx, http.MethodGet, path, scopes, nil, out)
}

// All follows @odata.nextLink and returns every item of a collection.
func All[T any](ctx context.Context, c *Client, path string, scopes []string) ([]T, error) {
	var out []T
	next := path
	for next != "" {
		var page struct {
			Value    []T    `json:"value"`
			NextLink string `json:"@odata.nextLink"`
		}
		if err := c.Get(ctx, next, scopes, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Value...)
		next = page.NextLink
	}
	return out, nil
}

// Report runs an Intune report action and returns rows as maps. These
// actions answer with {TotalRowCount, Schema:[{Column}], Values:[[...]]}.
func Report(ctx context.Context, c *Client, action string, scopes []string, body map[string]any) ([]map[string]any, error) {
	const pageSize = 1000
	var rows []map[string]any
	for skip := 0; ; skip += pageSize {
		b := map[string]any{"skip": skip, "top": pageSize}
		for k, v := range body {
			b[k] = v
		}
		var page struct {
			TotalRowCount int `json:"TotalRowCount"`
			Schema        []struct {
				Column string `json:"Column"`
			} `json:"Schema"`
			Values [][]any `json:"Values"`
		}
		if err := c.Do(ctx, http.MethodPost, "beta/deviceManagement/reports/"+action, scopes, b, &page); err != nil {
			return nil, err
		}
		for _, v := range page.Values {
			r := make(map[string]any, len(page.Schema))
			for i, col := range page.Schema {
				if i < len(v) {
					r[col.Column] = v[i]
				}
			}
			rows = append(rows, r)
		}
		if len(page.Values) < pageSize || (page.TotalRowCount > 0 && len(rows) >= page.TotalRowCount) {
			return rows, nil
		}
	}
}

// Text reads a report cell, preferring the localized "_loc" twin column.
func Text(r map[string]any, col string) string {
	if v, ok := r[col+"_loc"]; ok && v != nil {
		return fmt.Sprint(v)
	}
	if v, ok := r[col]; ok && v != nil {
		return fmt.Sprint(v)
	}
	return ""
}
