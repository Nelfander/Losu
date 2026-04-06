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
	// Get values from .env or use defaults
	_ = godotenv.Load()
	//  If we are running locally (go run),
	// ollama won't resolve. We override it to localhost.
	endpoint := os.Getenv("LOSU_OLLAMA_HOST")
	if endpoint == "http://ollama:11434" || endpoint == "" {
		// We try to see if we can reach localhost instead
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

// AnalyzeSystem sends telemetry and pattern data to an LLM to generate a concise SRE-style incident report
func (e *Explainer) AnalyzeSystem(errorPatterns string, warnPatterns string, avgEps float64, peakEps float64, avgWps float64, peakWps float64) (string, error) {
	// Sharp, context-aware prompt for a Senior SRE
	prompt := fmt.Sprintf(`Act as a Senior SRE. Analyze these live telemetry signals:

[TELEMETRY]
- Errors/sec: %.2f avg | %.1f peak
- Warns/sec:  %.2f avg | %.1f peak

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

Use Markdown. No intro/outro fluff. Technical brevity is mandatory.`, avgEps, peakEps, avgWps, peakWps, errorPatterns, warnPatterns)

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

// AnalyzeHeartbeat provides a high-level summary for the periodic window report
func (e *Explainer) AnalyzeHeartbeat(counts map[string]int, topMsg string) (string, error) {
	prompt := fmt.Sprintf(`Act as a Lead SRE. Review this hourly telemetry:

[STATS]
- Errors: %d | Warns: %d | Info: %d
- Top Issue: %s

[REPORT FORMAT]
1. **Status**: One sentence (e.g., Stable, Degraded, Critical).
2. **Analysis**: Brief technical breakdown of the numbers.
3. **Action**: One recommended next step.

Keep the entire response under 100 words. Prioritize errors. No intro/outro fluff.`, counts["ERROR"], counts["WARN"], counts["INFO"], topMsg)

	payload := map[string]interface{}{
		"model":  e.Model,
		"prompt": prompt,
		"stream": false,
	}

	body, _ := json.Marshal(payload)
	client := http.Client{Timeout: 20 * time.Second}

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
