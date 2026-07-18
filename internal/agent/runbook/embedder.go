package runbook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Embedder struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
	timeout    time.Duration
	dims       int
}

type EmbedderOptions struct {
	APIURL     string
	APIKey     string
	Model      string
	Timeout    time.Duration
	Dimensions int
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Model string `json:"model"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func NewEmbedder(opts EmbedderOptions) *Embedder {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Embedder{
		apiURL:  opts.APIURL,
		apiKey:  opts.APIKey,
		model:   opts.Model,
		timeout: timeout,
		dims:    opts.Dimensions,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	results, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 || results[0] == nil {
		return nil, fmt.Errorf("embedding returned no result")
	}
	return results[0], nil
}

func (e *Embedder) EmbedBatch(ctx context.Context, texts []string) (vectors [][]float32, retErr error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	if len(texts) > 20 {
		return nil, fmt.Errorf("batch size exceeds 20")
	}

	_, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeout)
		defer cancel()
	}

	reqBody := embeddingRequest{
		Model: e.model,
		Input: texts,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, resp.Body.Close()) }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(respBody)
		if len(snippet) > 4096 {
			snippet = snippet[:4096]
		}
		return nil, fmt.Errorf("embedding API returned status %d: %s", resp.StatusCode, snippet)
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, fmt.Errorf("parse embedding response: %w", err)
	}

	if len(embResp.Data) == 0 {
		return nil, fmt.Errorf("embedding API returned empty data")
	}

	results := make([][]float32, len(texts))
	for _, item := range embResp.Data {
		if item.Index >= 0 && item.Index < len(results) {
			results[item.Index] = item.Embedding
		}
	}

	if e.dims == 0 && len(results) > 0 && results[0] != nil {
		e.dims = len(results[0])
	}

	return results, nil
}

func (e *Embedder) Dims() int {
	return e.dims
}
