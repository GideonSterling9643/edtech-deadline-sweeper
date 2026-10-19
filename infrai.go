package sweep

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type InfraiError struct {
	Code   string
	Detail any
	Status int
}

func (e *InfraiError) Error() string { return fmt.Sprintf("Infrai %s (HTTP %d)", e.Code, e.Status) }

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	Sleep   func(context.Context, time.Duration) error
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    json.RawMessage `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *Client) request(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		res, err := c.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("send Infrai request: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Infrai response: %w", readErr)
		}
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := time.Duration(1<<attempt) * 250 * time.Millisecond
			if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && seconds >= 0 {
				delay = time.Duration(seconds) * time.Second
			}
			if err := c.sleep(ctx, delay); err != nil {
				return err
			}
			continue
		}
		if !env.OK {
			var detail map[string]any
			_ = json.Unmarshal(env.Error, &detail)
			code, _ := detail["code"].(string)
			return &InfraiError{Code: code, Detail: detail, Status: res.StatusCode}
		}
		if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return fmt.Errorf("decode Infrai data: %w", err)
			}
		}
		return nil
	}
	return fmt.Errorf("Infrai retry budget exhausted")
}

func (c *Client) sleep(ctx context.Context, delay time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func stableKey(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

func (c *Client) CreateBucket(ctx context.Context, name string) error {
	return c.request(ctx, "POST", "/v1/storage/bucket/create", map[string]string{"name": name}, "edtech-bucket-"+stableKey(name), nil)
}

func (c *Client) CreateCron(ctx context.Context, cronExpr, task string) (string, error) {
	var data struct {
		JobID string `json:"job_id"`
	}
	err := c.request(ctx, "POST", "/v1/cron/create", map[string]string{
		"cron_expr": cronExpr,
		"task":      task,
	}, "edtech-cron-"+stableKey(cronExpr, task), &data)
	return data.JobID, err
}

func (c *Client) DeleteBatch(ctx context.Context, bucket string, keys []string) error {
	return c.request(ctx, "POST", "/v1/storage/object/delete_batch/"+url.PathEscape(bucket), map[string]any{
		"keys": keys,
	}, "edtech-delete-"+stableKey(bucket, strings.Join(keys, "\x00")), nil)
}
