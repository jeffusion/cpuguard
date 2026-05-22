package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"cpuguard/internal/model"
)

type Client struct {
	http *http.Client
}

func NewClient(socketPath string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{http: &http.Client{Transport: tr, Timeout: 10 * time.Second}}
}

func (c *Client) Get(path string) ([]byte, error) {
	resp, err := c.http.Get("http://unix" + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return readResponse(resp)
}

func (c *Client) Post(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, "http://unix"+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return readResponse(resp)
}

func (c *Client) ListSubjects() ([]model.Subject, error) {
	page, err := c.QuerySubjects(model.SubjectQuery{Limit: 100})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

func (c *Client) QuerySubjects(query model.SubjectQuery) (model.SubjectPage, error) {
	if query.Limit <= 0 {
		query.Limit = 100
	}
	values := url.Values{}
	values.Set("offset", strconv.Itoa(query.Offset))
	values.Set("limit", strconv.Itoa(query.Limit))
	if query.Sort != "" {
		values.Set("sort", query.Sort)
	}
	if query.Dir != "" {
		values.Set("dir", query.Dir)
	}
	if query.SelectedID != "" {
		values.Set("selected_id", query.SelectedID)
	}
	body, err := c.Get("/v1/subjects?" + values.Encode())
	if err != nil {
		return model.SubjectPage{}, err
	}
	var page model.SubjectPage
	if err := json.Unmarshal(body, &page); err != nil {
		return model.SubjectPage{}, err
	}
	return page, nil
}

func (c *Client) ListLimits() ([]model.Subject, error) {
	body, err := c.Get("/v1/limits")
	if err != nil {
		return nil, err
	}
	var subjects []model.Subject
	if err := json.Unmarshal(body, &subjects); err != nil {
		return nil, err
	}
	return subjects, nil
}

func (c *Client) Status() (model.DaemonStatus, error) {
	body, err := c.Get("/v1/status")
	if err != nil {
		return model.DaemonStatus{}, err
	}
	var status model.DaemonStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return model.DaemonStatus{}, err
	}
	return status, nil
}

func (c *Client) ListEvents(limit int) ([]model.Event, error) {
	return c.ListEventsForSubject("", limit)
}

func (c *Client) ListEventsForSubject(subjectID string, limit int) ([]model.Event, error) {
	return c.QueryEvents(model.EventQuery{SubjectID: subjectID, Limit: limit})
}

func (c *Client) QueryEvents(query model.EventQuery) ([]model.Event, error) {
	if query.Limit <= 0 {
		query.Limit = 100
	}
	values := url.Values{}
	values.Set("limit", strconv.Itoa(query.Limit))
	if query.SubjectID != "" {
		values.Set("subject_id", query.SubjectID)
	}
	if query.Type != "" {
		values.Set("type", query.Type)
	}
	if !query.Since.IsZero() {
		values.Set("since", query.Since.Format(time.RFC3339Nano))
	}
	body, err := c.Get("/v1/events?" + values.Encode())
	if err != nil {
		return nil, err
	}
	var events []model.Event
	if err := json.Unmarshal(body, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func (c *Client) ProtectedPolicy() (model.ProtectedPolicy, error) {
	body, err := c.Get("/v1/protected")
	if err != nil {
		return model.ProtectedPolicy{}, err
	}
	var policy model.ProtectedPolicy
	if err := json.Unmarshal(body, &policy); err != nil {
		return model.ProtectedPolicy{}, err
	}
	return policy, nil
}

func (c *Client) Unthrottle(subjectID string) error {
	_, err := c.Post("/v1/subjects/" + subjectID + "/unthrottle")
	return err
}

func (c *Client) Hold(subjectID string) error {
	_, err := c.Post("/v1/subjects/" + subjectID + "/hold")
	return err
}

func readResponse(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s", string(body))
	}
	return body, nil
}
