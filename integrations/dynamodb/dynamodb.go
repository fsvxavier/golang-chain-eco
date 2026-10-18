// Package dynamodb provides a checkpoint.Saver and a store.Store on Amazon
// DynamoDB (or DynamoDB Local), speaking the JSON API with SigV4 signing —
// no SDK needed. Tables are created on demand (on-demand billing).
//
//	c := dynamodb.Client{Region: "us-east-1", Credentials: bedrock.Credentials{...}}
//	saver, _ := dynamodb.NewSaver(ctx, &c, "checkpoints")
//
// Limits: items are capped at 400 KB by DynamoDB, so very large checkpoints
// do not fit; Store.Search scans the table (filtering server-side), which is
// fine for modest memories but not a substitute for an indexed search.
package dynamodb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fsvxavier/golang-chain-eco/providers/bedrock"
)

// Client talks to DynamoDB.
type Client struct {
	Region      string // default "us-east-1"
	Credentials bedrock.Credentials
	Endpoint    string // default https://dynamodb.<region>.amazonaws.com; set for DynamoDB Local
	HTTP        *http.Client
}

// Error is a DynamoDB error response.
type Error struct{ Type, Message string }

func (e *Error) Error() string { return fmt.Sprintf("dynamodb: %s: %s", e.Type, e.Message) }

type attr = map[string]any // {"S": "..."} or {"N": "..."}
type item = map[string]attr

func (c *Client) call(ctx context.Context, op string, in any) (map[string]json.RawMessage, error) {
	region := c.Region
	if region == "" {
		region = "us-east-1"
	}
	ep := c.Endpoint
	if ep == "" {
		ep = "https://dynamodb." + region + ".amazonaws.com"
	}
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-amz-json-1.0")
	req.Header.Set("X-Amz-Target", "DynamoDB_20120810."+op)
	bedrock.Sign(req, body, c.Credentials, region, "dynamodb", time.Now())
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var e struct {
			Type    string `json:"__type"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		if _, after, ok := strings.Cut(e.Type, "#"); ok {
			e.Type = after
		}
		if e.Type == "" {
			e.Type = resp.Status
		}
		return nil, &Error{e.Type, e.Message}
	}
	var out map[string]json.RawMessage
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ensureTable creates a table keyed by (pk S, sk S) if it does not exist.
func (c *Client) ensureTable(ctx context.Context, name, pk, sk string) error {
	_, err := c.call(ctx, "CreateTable", map[string]any{
		"TableName": name, "BillingMode": "PAY_PER_REQUEST",
		"AttributeDefinitions": []map[string]string{{"AttributeName": pk, "AttributeType": "S"}, {"AttributeName": sk, "AttributeType": "S"}},
		"KeySchema":            []map[string]string{{"AttributeName": pk, "KeyType": "HASH"}, {"AttributeName": sk, "KeyType": "RANGE"}},
	})
	if e, ok := err.(*Error); ok && e.Type == "ResourceInUseException" {
		err = nil
	}
	if err != nil {
		return err
	}
	for range 60 { // wait until ACTIVE
		out, err := c.call(ctx, "DescribeTable", map[string]string{"TableName": name})
		if err != nil {
			return err
		}
		var d struct{ TableStatus string }
		_ = json.Unmarshal(out["Table"], &d)
		if d.TableStatus == "ACTIVE" {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("dynamodb: table %s not active", name)
}

func sv(v string) attr  { return attr{"S": v} }
func n(v int64) attr    { return attr{"N": fmt.Sprint(v)} }
func str(a attr) string { v, _ := a["S"].(string); return v }
func num(a attr) int64 {
	var v int64
	fmt.Sscan(fmt.Sprint(a["N"]), &v)
	return v
}

// queryAll pages through Query/Scan results.
func (c *Client) pages(ctx context.Context, op string, in map[string]any, limit int) ([]item, error) {
	var all []item
	for {
		out, err := c.call(ctx, op, in)
		if err != nil {
			return nil, err
		}
		var page struct {
			Items            []item
			LastEvaluatedKey item
		}
		for k, dst := range map[string]any{"Items": &page.Items, "LastEvaluatedKey": &page.LastEvaluatedKey} {
			if v := out[k]; v != nil {
				if err := json.Unmarshal(v, dst); err != nil {
					return nil, err
				}
			}
		}
		all = append(all, page.Items...)
		if page.LastEvaluatedKey == nil || (limit > 0 && len(all) >= limit) {
			return all, nil
		}
		in["ExclusiveStartKey"] = page.LastEvaluatedKey
	}
}
