package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	model      string
	dimensions int
	http       *http.Client
}

func NewClient(baseURL, apiKey, model string, dimensions int) *Client {
	return &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		model:      model,
		dimensions: dimensions,
		http:       &http.Client{Timeout: 60 * time.Second},
	}
}

type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Embed returns embeddings for the given texts (batch up to 20).
func (c *Client) Embed(texts []string) ([][]float32, error) {
	req := embeddingRequest{
		Model: c.model,
		Input: texts,
	}
	if c.dimensions > 0 {
		req.Dimensions = c.dimensions
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embedding API %d: %s", resp.StatusCode, string(respBody))
	}

	var embResp embeddingResponse
	if err := json.Unmarshal(respBody, &embResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if embResp.Error != nil {
		return nil, fmt.Errorf("embedding error: %s", embResp.Error.Message)
	}

	// Re-order by index
	result := make([][]float32, len(texts))
	for _, d := range embResp.Data {
		if d.Index < len(result) {
			result[d.Index] = d.Embedding
		}
	}

	return result, nil
}

// EmbedSingle embeds a single text.
func (c *Client) EmbedSingle(text string) ([]float32, error) {
	results, err := c.Embed([]string{text})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return results[0], nil
}

// BatchEmbed handles texts in batches of batchSize.
func (c *Client) BatchEmbed(texts []string, batchSize int) ([][]float32, error) {
	if batchSize <= 0 {
		batchSize = 6
	}

	all := make([][]float32, len(texts))
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := c.Embed(texts[i:end])
		if err != nil {
			return nil, fmt.Errorf("batch %d-%d: %w", i, end, err)
		}
		copy(all[i:end], batch)
	}
	return all, nil
}
