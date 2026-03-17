package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Explainer struct {
	Endpoint string
}

func NewExplainer() *Explainer {
	return &Explainer{
		Endpoint: "http://localhost:11434/api/generate", // Default Ollama port
	}
}

func (e *Explainer) AnalyzeSystem(errorPatterns string, warnPatterns string, eps int) (string, error) {
	// Instructions for AI
	prompt := fmt.Sprintf(`Act as a Senior SRE. Analyze these logs:
ERRORS:
%s
WARNS:
%s
STATS: %d EPS.

Provide a "Situation Report": 
- [Primary Issue Name]
1. **Root Cause**: What is the most likely single point of failure?
2. **Action**: What should the dev check first? (e.g., 'Check Redis memory', 'Restart worker_3')
3. **Status**: Is the system degrading or stable?

Be extremely concise. Use technical language. No fluff.`, errorPatterns, warnPatterns, eps)

	payload := map[string]interface{}{
		"model":  "llama3", // Ensure this matches what you pulled
		"prompt": prompt,
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	client := http.Client{Timeout: 15 * time.Second} // AI can be slow on first load

	resp, err := client.Post(e.Endpoint, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Response string `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.Response, nil
}
