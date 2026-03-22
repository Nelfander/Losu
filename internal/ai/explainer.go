package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Explainer struct {
	Endpoint string
	Model    string
}

func NewExplainer() *Explainer {
	// Get values from .env (via Docker) or use defaults
	_ = godotenv.Load()
	endpoint := os.Getenv("LOSU_OLLAMA_HOST")
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}

	model := os.Getenv("LOSU_AI_MODEL")
	if model == "" {
		model = "llama3:latest"
	}

	return &Explainer{
		Endpoint: endpoint + "/api/generate",
		Model:    model,
	}
}

// Update the signature to take float64 for precision
func (e *Explainer) AnalyzeSystem(errorPatterns string, warnPatterns string, avgEps float64, peakEps float64) (string, error) {
	// Sharp, context-aware prompt for a Senior SRE
	prompt := fmt.Sprintf(`Act as a Senior SRE. Analyze these live telemetry signals:

[TELEMETRY]
- Current Throughput: %.2f Errors+Warns/sec (Avg over 50s)
- Peak Intensity: %.1f E+W/sec (High Water Mark)

[ERROR PATTERNS]
%s

[WARNING PATTERNS]
%s

[TASK]
Provide a concise "Situation Report":
1. **Primary Incident**: Identify the most critical failure pattern.
2. **Root Cause Analysis**: One-sentence technical hypothesis.
3. **Mitigation**: Specific technical action (e.g., 'Flush Redis', 'Check DB connection pool').
4. **Health Trend**: Is this a burst (Peak >> Avg) or sustained saturation?

Use Markdown. No intro/outro fluff. Technical brevity is mandatory.`, avgEps, peakEps, errorPatterns, warnPatterns)

	payload := map[string]interface{}{
		"model":  e.Model,
		"prompt": prompt,
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	client := http.Client{Timeout: 30 * time.Second} // AI can be slow on first load

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
