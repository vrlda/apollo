package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/danilrybalkin/apollo-dash/db"
)

type MemoryNode struct {
	ID        int
	SessionID string
	Content   string
	Vector    []float32
}

var (
	MemoryBank []MemoryNode
	memoryLock sync.RWMutex
)

func buildEmbeddingRequest(baseURL, model, text string) (*http.Request, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	isOpenAICompatible := strings.Contains(baseURL, "/v1") || strings.Contains(baseURL, "openrouter.ai")
	endpoint := baseURL + "/api/embeddings"
	payload := map[string]interface{}{
		"model":  model,
		"prompt": text,
	}
	if isOpenAICompatible {
		endpoint = baseURL + "/embeddings"
		payload = map[string]interface{}{
			"model": model,
			"input": text,
		}
	}

	payloadBytes, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if isOpenAICompatible {
		apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
		if apiKey == "" || apiKey == "your_openrouter_api_key_here" {
			apiKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
		}
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
	}
	return req, nil
}

func decodeEmbeddingResponse(resp *http.Response) ([]float32, error) {
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("embedding endpoint status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var ollamaResult struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.Unmarshal(body, &ollamaResult); err == nil && len(ollamaResult.Embedding) > 0 {
		return ollamaResult.Embedding, nil
	}

	var openAIResult struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &openAIResult); err != nil {
		return nil, err
	}
	if len(openAIResult.Data) == 0 || len(openAIResult.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding vector")
	}
	return openAIResult.Data[0].Embedding, nil
}

// Generates an embedding array for a given text using Ollama or OpenAI-compatible APIs.
func GenerateEmbedding(text string) ([]float32, error) {
	settings := GetCurrentSettings("")

	req, err := buildEmbeddingRequest(settings.EmbeddingApiUrl, settings.EmbeddingModel, text)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return decodeEmbeddingResponse(resp)
}

// Bootstraps the in-memory cache from the SQLite database
func LoadMemoryBank() {
	memoryLock.Lock()
	defer memoryLock.Unlock()

	MemoryBank = []MemoryNode{}

	rows, err := db.DB.Query("SELECT id, session_id, content, vector_embedding FROM chat_messages WHERE vector_embedding != '' AND vector_embedding IS NOT NULL")
	if err != nil {
		log.Println("Memory System: Failed to load embeddings from DB:", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var node MemoryNode
		var vectorStr string

		if err := rows.Scan(&node.ID, &node.SessionID, &node.Content, &vectorStr); err != nil {
			continue
		}

		if err := json.Unmarshal([]byte(vectorStr), &node.Vector); err == nil {
			MemoryBank = append(MemoryBank, node)
			count++
		}
	}

	log.Printf("Memory System: Loaded %d vectorized nodes into RAM\n", count)
}

// Background thread that hunts for unembedded messages and calculates their RAG matrices
func StartMemoryIndexer() {
	for {
		time.Sleep(3 * time.Minute)

		settings := GetCurrentSettings("")
		if settings.EmbeddingApiUrl == "" || settings.EmbeddingModel == "" {
			continue
		}

		rows, err := db.DB.Query("SELECT id, session_id, content FROM chat_messages WHERE vector_embedding = '' OR vector_embedding IS NULL LIMIT 10")
		if err != nil {
			continue
		}

		var pending []MemoryNode
		for rows.Next() {
			var node MemoryNode
			if err := rows.Scan(&node.ID, &node.SessionID, &node.Content); err == nil {
				pending = append(pending, node)
			}
		}
		rows.Close()

		for _, node := range pending {
			plainText := node.Content
			if len(plainText) > 0 && plainText[0] == '[' {
				var contentArr []map[string]interface{}
				if err := json.Unmarshal([]byte(plainText), &contentArr); err == nil {
					for _, item := range contentArr {
						if t, ok := item["type"].(string); ok && t == "text" {
							if textStr, ok := item["text"].(string); ok {
								plainText = textStr
								break
							}
						}
					}
				}
			}

			// Don't embed completely empty strings
			if len(plainText) < 2 {
				db.DB.Exec("UPDATE chat_messages SET vector_embedding = '[]' WHERE id = ?", node.ID)
				continue
			}

			vector, err := GenerateEmbedding(plainText)
			if err != nil || len(vector) == 0 {
				log.Println("Memory System Indexer: Failed to embed message", node.ID, err)
				continue
			}

			vecBytes, _ := json.Marshal(vector)

			_, err = db.DB.Exec("UPDATE chat_messages SET vector_embedding = ? WHERE id = ?", string(vecBytes), node.ID)
			if err == nil {
				node.Vector = vector
				memoryLock.Lock()
				MemoryBank = append(MemoryBank, node)
				memoryLock.Unlock()
				log.Println("Memory System Indexer: Successfully mapped conversation block", node.ID)
			}
		}
	}
}
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0.0
	}
	var dotProduct, normA, normB float32
	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0.0
	}
	return dotProduct / float32(math.Sqrt(float64(normA))*math.Sqrt(float64(normB)))
}

type MemoryHit struct {
	Node  MemoryNode
	Score float32
}

// Builds the background RAG injection string dynamically by sweeping RAM matrices
func SearchEpisodes(query string, excludeSession string) string {
	vector, err := GenerateEmbedding(query)
	if err != nil || len(vector) == 0 {
		return ""
	}

	memoryLock.RLock()
	var hits []MemoryHit
	for _, node := range MemoryBank {
		if node.SessionID == excludeSession {
			continue
		}
		score := cosineSimilarity(vector, node.Vector)
		// 0.5 is a safe semantic threshold for normalized nomic vectors
		if score > 0.5 {
			hits = append(hits, MemoryHit{Node: node, Score: score})
		}
	}
	memoryLock.RUnlock()

	// Sort hits descending by best mathematical match
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	// Take Top 3 hits
	limit := 3
	if len(hits) < limit {
		limit = len(hits)
	}
	topHits := hits[:limit]

	if len(topHits) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("<episodic_recollections>\n")
	builder.WriteString("Here are distinct chronological memories from past sessions that have strong semantic similarity to the recent user prompt context. Factor them in when providing your answer:\n\n")

	for i, hit := range topHits {
		builder.WriteString(fmt.Sprintf("-- Recalled Episode %d (Similarity Score: %.2f) --\n", i+1, hit.Score))

		// Grab robust timeline: (-2 to +2 messages) around the hit inside this session linearly
		rows, err := db.DB.Query("SELECT role, content FROM chat_messages WHERE session_id = ? AND id >= ? AND id <= ? ORDER BY id ASC", hit.Node.SessionID, hit.Node.ID-2, hit.Node.ID+2)
		if err == nil {
			for rows.Next() {
				var role, content string
				rows.Scan(&role, &content)

				// Extract raw textual payload safely
				plainText := content
				if len(plainText) > 0 && plainText[0] == '[' {
					var contentArr []map[string]interface{}
					if err := json.Unmarshal([]byte(plainText), &contentArr); err == nil {
						for _, item := range contentArr {
							if t, ok := item["type"].(string); ok && t == "text" {
								if textStr, ok := item["text"].(string); ok {
									plainText = textStr
									break
								}
							}
						}
					}
				}

				builder.WriteString(fmt.Sprintf("[%s]: %s\n\n", strings.ToUpper(role), plainText))
			}
			rows.Close()
		}
	}

	builder.WriteString("</episodic_recollections>\n")
	return builder.String()
}
