package embed

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	vlEndpoint     = "https://dashscope.aliyuncs.com/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding"
	vlMaxContents  = 20 // max content elements per request
	vlMaxImages    = 5  // max images per request
)

// EmbedItem represents a single item to embed — either text-only or image+context.
type EmbedItem struct {
	Text     string // required for text chunks
	ImageURI string // optional: "data:image/png;base64,..." for image chunks
}

// VLClient is a client for the DashScope multimodal embedding API (qwen3-vl-embedding).
type VLClient struct {
	apiKey     string
	model      string // "qwen3-vl-embedding"
	dimensions int
	http       *http.Client
}

// NewVLClient creates a new multimodal embedding client.
func NewVLClient(apiKey, model string, dimensions int) *VLClient {
	return &VLClient{
		apiKey:     apiKey,
		model:      model,
		dimensions: dimensions,
		http:       &http.Client{Timeout: 120 * time.Second},
	}
}

// vlRequest is the DashScope multimodal embedding request format.
type vlRequest struct {
	Model      string      `json:"model"`
	Input      vlInput     `json:"input"`
	Parameters vlParams    `json:"parameters"`
}

type vlInput struct {
	Contents []vlContent `json:"contents"`
}

type vlContent map[string]string // {"text": "..."} or {"image": "data:..."} or {"text": "...", "image": "data:..."}

type vlParams struct {
	Dimension int `json:"dimension"`
}

// vlResponse is the DashScope multimodal embedding response format.
type vlResponse struct {
	Output struct {
		Embeddings []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"embeddings"`
	} `json:"output"`
	Usage struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// EmbedText embeds pure text via the VL API.
func (c *VLClient) EmbedText(text string) ([]float32, error) {
	items := []EmbedItem{{Text: text}}
	results, err := c.EmbedBatch(items)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return results[0], nil
}

// EmbedImage embeds an image with optional context text.
func (c *VLClient) EmbedImage(imageDataURI string, context string) ([]float32, error) {
	item := EmbedItem{
		Text:     context,
		ImageURI: imageDataURI,
	}
	results, err := c.EmbedBatch([]EmbedItem{item})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return results[0], nil
}

// EmbedBatch embeds multiple items. It automatically splits into sub-batches
// respecting the 20-content and 5-image limits per request.
func (c *VLClient) EmbedBatch(items []EmbedItem) ([][]float32, error) {
	if len(items) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(items))

	// Split items into batches respecting limits
	batches := splitIntoBatches(items)

	globalOffset := 0
	for _, batch := range batches {
		embeddings, err := c.doRequest(batch)
		if err != nil {
			return nil, fmt.Errorf("vl batch at offset %d: %w", globalOffset, err)
		}

		for i, emb := range embeddings {
			if globalOffset+i < len(results) {
				results[globalOffset+i] = emb
			}
		}
		globalOffset += len(batch)
	}

	return results, nil
}

// splitIntoBatches splits items into batches respecting vlMaxContents and vlMaxImages limits.
func splitIntoBatches(items []EmbedItem) [][]EmbedItem {
	var batches [][]EmbedItem
	var currentBatch []EmbedItem
	imageCount := 0

	for _, item := range items {
		isImage := item.ImageURI != ""

		// Check if adding this item would exceed limits
		if len(currentBatch) >= vlMaxContents || (isImage && imageCount >= vlMaxImages) {
			if len(currentBatch) > 0 {
				batches = append(batches, currentBatch)
			}
			currentBatch = nil
			imageCount = 0
		}

		currentBatch = append(currentBatch, item)
		if isImage {
			imageCount++
		}
	}

	if len(currentBatch) > 0 {
		batches = append(batches, currentBatch)
	}

	return batches
}

// doRequest sends a single batch request to the VL API.
func (c *VLClient) doRequest(items []EmbedItem) ([][]float32, error) {
	var contents []vlContent

	for _, item := range items {
		content := make(vlContent)
		if item.ImageURI != "" {
			content["image"] = item.ImageURI
			if item.Text != "" {
				content["text"] = item.Text
			}
		} else {
			text := item.Text
			if text == "" {
				text = " " // VL API requires non-empty text
			}
			content["text"] = text
		}
		contents = append(contents, content)
	}

	req := vlRequest{
		Model: c.model,
		Input: vlInput{Contents: contents},
		Parameters: vlParams{
			Dimension: c.dimensions,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", vlEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("vl request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vl API %d: %s", resp.StatusCode, string(respBody))
	}

	var vlResp vlResponse
	if err := json.Unmarshal(respBody, &vlResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if vlResp.Code != "" {
		return nil, fmt.Errorf("vl API error [%s]: %s", vlResp.Code, vlResp.Message)
	}

	// Re-order by index
	result := make([][]float32, len(items))
	for _, e := range vlResp.Output.Embeddings {
		if e.Index < len(result) {
			result[e.Index] = e.Embedding
		}
	}

	return result, nil
}

// ImageToDataURI reads an image file and returns a data URI string.
func ImageToDataURI(imagePath string, mediaType string) (string, error) {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image %s: %w", imagePath, err)
	}
	b64 := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mediaType, b64), nil
}

// ImagePathToMediaType infers media type from file extension.
func ImagePathToMediaType(path string) string {
	ext := ""
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			ext = path[i+1:]
			break
		}
	}
	switch ext {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}
