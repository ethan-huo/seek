package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// BatchJob tracks a batch embedding job.
type BatchJob struct {
	ID           string
	Status       string
	OutputFileID string
	ErrorFileID  string
}

// batchRequestLine is one line in the batch input JSONL.
type batchRequestLine struct {
	CustomID string                 `json:"custom_id"`
	Method   string                 `json:"method"`
	URL      string                 `json:"url"`
	Body     map[string]interface{} `json:"body"`
}

// batchResponseLine is one line in the batch output JSONL.
type batchResponseLine struct {
	CustomID string `json:"custom_id"`
	Response struct {
		StatusCode int             `json:"status_code"`
		Body       json.RawMessage `json:"body"`
	} `json:"response"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// PrepareBatchJSONL creates the JSONL content for batch embedding.
// Each chunk gets a custom_id of "chunk-{index}".
func (c *Client) PrepareBatchJSONL(texts []string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	for i, text := range texts {
		line := batchRequestLine{
			CustomID: fmt.Sprintf("chunk-%d", i),
			Method:   "POST",
			URL:      "/v1/embeddings",
			Body: map[string]interface{}{
				"model": c.model,
				"input": text,
			},
		}
		if c.dimensions > 0 {
			line.Body["dimensions"] = c.dimensions
		}
		if err := enc.Encode(line); err != nil {
			return nil, fmt.Errorf("encode line %d: %w", i, err)
		}
	}
	return buf.Bytes(), nil
}

// UploadBatchFile uploads a JSONL file for batch processing.
func (c *Client) UploadBatchFile(jsonlData []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("purpose", "batch"); err != nil {
		return "", err
	}
	part, err := writer.CreateFormFile("file", "batch_embeddings.jsonl")
	if err != nil {
		return "", err
	}
	if _, err := part.Write(jsonlData); err != nil {
		return "", err
	}
	writer.Close()

	req, err := http.NewRequest("POST", c.baseURL+"/files", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("upload failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse upload response: %w", err)
	}
	return result.ID, nil
}

// CreateBatch creates a batch job with the uploaded file.
func (c *Client) CreateBatch(fileID string) (*BatchJob, error) {
	reqBody, _ := json.Marshal(map[string]interface{}{
		"input_file_id":     fileID,
		"endpoint":          "/v1/embeddings",
		"completion_window": "24h",
	})

	req, err := http.NewRequest("POST", c.baseURL+"/batches", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create batch: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("create batch failed %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	return &BatchJob{ID: result.ID, Status: result.Status}, nil
}

// PollBatch polls until the batch job completes. Returns the final job state.
func (c *Client) PollBatch(batchID string, onStatus func(status string, elapsed time.Duration)) (*BatchJob, error) {
	start := time.Now()

	for {
		req, err := http.NewRequest("GET", c.baseURL+"/batches/"+batchID, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("poll batch: %w", err)
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var result struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			OutputFileID string `json:"output_file_id"`
			ErrorFileID  string `json:"error_file_id"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("parse poll response: %w", err)
		}

		job := &BatchJob{
			ID:           result.ID,
			Status:       result.Status,
			OutputFileID: result.OutputFileID,
			ErrorFileID:  result.ErrorFileID,
		}

		if onStatus != nil {
			onStatus(result.Status, time.Since(start))
		}

		switch result.Status {
		case "completed":
			return job, nil
		case "failed", "expired", "cancelled":
			return job, fmt.Errorf("batch %s: %s", result.Status, string(respBody))
		}

		time.Sleep(5 * time.Second)
	}
}

// DownloadBatchResults downloads and parses the batch output file.
// Returns a map of custom_id -> embedding.
func (c *Client) DownloadBatchResults(fileID string) (map[string][]float32, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/files/"+fileID+"/content", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download results: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download failed %d: %s", resp.StatusCode, string(respBody))
	}

	results := make(map[string][]float32)
	lines := strings.Split(strings.TrimSpace(string(respBody)), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}
		var resp batchResponseLine
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.Error != nil {
			continue
		}
		if resp.Response.StatusCode != 200 {
			continue
		}

		var embResp embeddingResponse
		if err := json.Unmarshal(resp.Response.Body, &embResp); err != nil {
			continue
		}
		if len(embResp.Data) > 0 {
			results[resp.CustomID] = embResp.Data[0].Embedding
		}
	}

	return results, nil
}

// BatchEmbed runs the full batch embedding flow:
// prepare JSONL → upload → create batch → poll → download → return embeddings.
func (c *Client) BatchEmbedAsync(texts []string, onStatus func(status string, elapsed time.Duration)) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// 1. Prepare JSONL
	jsonl, err := c.PrepareBatchJSONL(texts)
	if err != nil {
		return nil, fmt.Errorf("prepare batch: %w", err)
	}

	// 2. Upload file
	fileID, err := c.UploadBatchFile(jsonl)
	if err != nil {
		return nil, fmt.Errorf("upload batch file: %w", err)
	}

	// 3. Create batch job
	job, err := c.CreateBatch(fileID)
	if err != nil {
		return nil, fmt.Errorf("create batch job: %w", err)
	}

	// 4. Poll until done
	job, err = c.PollBatch(job.ID, onStatus)
	if err != nil {
		return nil, err
	}

	if job.OutputFileID == "" {
		return nil, fmt.Errorf("no output file from batch")
	}

	// 5. Download results
	resultMap, err := c.DownloadBatchResults(job.OutputFileID)
	if err != nil {
		return nil, fmt.Errorf("download batch results: %w", err)
	}

	// 6. Reassemble in order
	embeddings := make([][]float32, len(texts))
	for i := range texts {
		key := fmt.Sprintf("chunk-%d", i)
		if emb, ok := resultMap[key]; ok {
			embeddings[i] = emb
		}
	}

	return embeddings, nil
}
