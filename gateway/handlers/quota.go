package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"9router-gateway/db"
)

type QuotaRow struct {
	ConnectionID        string                 `json:"connectionId"`
	Provider            string                 `json:"provider"`
	Name                string                 `json:"name"`
	Email               *string                `json:"email"`
	AuthType            string                 `json:"authType"`
	Status              string                 `json:"status"`
	RemainingPercentage int                    `json:"remainingPercentage"`
	ResetAt             *string                `json:"resetAt"`
	ResetInSeconds      *int64                 `json:"resetInSeconds"`
	Details             map[string]interface{} `json:"details"`
}

type QuotaSummary struct {
	TotalConnections    int `json:"totalConnections"`
	ActiveConnections   int `json:"activeConnections"`
	DepletedConnections int `json:"depletedConnections"`
}

type QuotaResponse struct {
	Success   bool         `json:"success"`
	Timestamp string       `json:"timestamp"`
	Summary   QuotaSummary `json:"summary"`
	Quotas    []QuotaRow   `json:"quotas"`
	Error     string       `json:"error,omitempty"`
}

type SingleQuota struct {
	Used                *float64 `json:"used,omitempty"`
	Total               *float64 `json:"total,omitempty"`
	Remaining           *float64 `json:"remaining,omitempty"`
	RemainingPercentage *float64 `json:"remainingPercentage,omitempty"`
	ResetAt             *string  `json:"resetAt,omitempty"`
}

func parseTimeISO(val interface{}) *string {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			iso := t.UTC().Format(time.RFC3339Nano)
			return &iso
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			iso := t.UTC().Format(time.RFC3339)
			return &iso
		}
		if num, err := strconv.ParseInt(v, 10, 64); err == nil {
			if num < 1e12 {
				num *= 1000
			}
			iso := time.UnixMilli(num).UTC().Format(time.RFC3339)
			return &iso
		}
		return &v
	case float64:
		num := int64(v)
		if num < 1e12 {
			num *= 1000
		}
		iso := time.UnixMilli(num).UTC().Format(time.RFC3339)
		return &iso
	case int64:
		if v < 1e12 {
			v *= 1000
		}
		iso := time.UnixMilli(v).UTC().Format(time.RFC3339)
		return &iso
	}
	return nil
}

func extractQuotasFromMap(usage map[string]interface{}) (string, int, *string, *int64) {
	if usage == nil {
		return "unknown", 0, nil, nil
	}

	if errVal, hasErr := usage["error"]; hasErr && errVal != nil {
		return "error", 0, nil, nil
	}

	quotasObj, ok := usage["quotas"].(map[string]interface{})
	if !ok || len(quotasObj) == 0 {
		if msg, ok := usage["message"].(string); ok && msg != "" {
			lower := strings.ToLower(msg)
			if strings.Contains(lower, "error") ||
				strings.Contains(lower, "unauthorized") ||
				strings.Contains(lower, "failed") ||
				strings.Contains(lower, "expired") {
				return "error", 0, nil, nil
			}
		}
		return "unknown", 0, nil, nil
	}

	var percentages []int
	var earliestResetTime *time.Time

	for _, item := range quotasObj {
		qMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		if remPct, ok := qMap["remainingPercentage"].(float64); ok && !math.IsNaN(remPct) {
			pct := int(math.Round(remPct))
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			percentages = append(percentages, pct)
		} else if rem, ok := qMap["remaining"].(float64); ok && !math.IsNaN(rem) {
			tot, hasTot := qMap["total"].(float64)
			if !hasTot || tot == 100 {
				pct := int(math.Round(rem))
				if pct < 0 {
					pct = 0
				}
				if pct > 100 {
					pct = 100
				}
				percentages = append(percentages, pct)
			}
		} else if tot, ok := qMap["total"].(float64); ok && tot > 0 {
			if used, ok := qMap["used"].(float64); ok && !math.IsNaN(used) {
				pct := int(math.Round(((tot - used) / tot) * 100))
				if pct < 0 {
					pct = 0
				}
				if pct > 100 {
					pct = 100
				}
				percentages = append(percentages, pct)
			}
		}

		if resetVal, hasReset := qMap["resetAt"]; hasReset && resetVal != nil {
			isoStr := parseTimeISO(resetVal)
			if isoStr != nil {
				if t, err := time.Parse(time.RFC3339Nano, *isoStr); err == nil {
					if earliestResetTime == nil || t.Before(*earliestResetTime) {
						earliestResetTime = &t
					}
				} else if t, err := time.Parse(time.RFC3339, *isoStr); err == nil {
					if earliestResetTime == nil || t.Before(*earliestResetTime) {
						earliestResetTime = &t
					}
				}
			}
		}
	}

	remainingPercentage := 0
	if len(percentages) > 0 {
		minPct := percentages[0]
		for _, p := range percentages {
			if p < minPct {
				minPct = p
			}
		}
		remainingPercentage = minPct
	}

	status := "unknown"
	if len(percentages) > 0 {
		if remainingPercentage <= 0 {
			status = "depleted"
		} else {
			status = "active"
		}
	}

	var resetAt *string
	var resetInSeconds *int64
	if earliestResetTime != nil {
		iso := earliestResetTime.UTC().Format(time.RFC3339)
		resetAt = &iso
		secs := int64(time.Until(*earliestResetTime).Seconds())
		if secs < 0 {
			secs = 0
		}
		resetInSeconds = &secs
	}

	return status, remainingPercentage, resetAt, resetInSeconds
}

func fetchProviderUsage(ctx context.Context, client *http.Client, conn db.Connection) (map[string]interface{}, error) {
	provider := conn.Provider
	var apiKey string
	if k, ok := conn.Data["apiKey"].(string); ok {
		apiKey = strings.TrimSpace(k)
	}
	var accessToken string
	if t, ok := conn.Data["accessToken"].(string); ok {
		accessToken = strings.TrimSpace(t)
	}

	psd, _ := conn.Data["providerSpecificData"].(map[string]interface{})

	switch provider {
	case "claude":
		if accessToken == "" {
			return map[string]interface{}{"message": "No access token"}, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/usage?cedar_ember=1", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("User-Agent", "claude-cli/2.1.280 (external, cli)")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			return map[string]interface{}{"message": "Authentication failed", "error": "unauthorized"}, nil
		}
		if resp.StatusCode != http.StatusOK {
			return map[string]interface{}{"message": fmt.Sprintf("API returned status %d", resp.StatusCode)}, nil
		}

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}

		quotas := make(map[string]interface{})
		parseClaudeWindow := func(wVal interface{}) map[string]interface{} {
			wMap, ok := wVal.(map[string]interface{})
			if !ok {
				return nil
			}
			util, ok := wMap["utilization"].(float64)
			if !ok {
				return nil
			}
			rem := math.Max(0, 100-util)
			return map[string]interface{}{
				"used":                util,
				"total":               100.0,
				"remaining":           rem,
				"remainingPercentage": rem,
				"resetAt":             parseTimeISO(wMap["resets_at"]),
				"unlimited":           false,
			}
		}

		if fiveH := parseClaudeWindow(data["five_hour"]); fiveH != nil {
			quotas["session (5h)"] = fiveH
		}
		if sevenD := parseClaudeWindow(data["seven_day"]); sevenD != nil {
			quotas["weekly (7d)"] = sevenD
		}

		return map[string]interface{}{"quotas": quotas}, nil

	case "codex":
		if accessToken == "" {
			return map[string]interface{}{"message": "No access token"}, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/backend-api/wham/usage", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return map[string]interface{}{"message": fmt.Sprintf("API returned status %d", resp.StatusCode)}, nil
		}

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}

		quotas := make(map[string]interface{})
		parseCodexWindow := func(wVal interface{}) map[string]interface{} {
			wMap, ok := wVal.(map[string]interface{})
			if !ok {
				return nil
			}
			var usedPct float64
			if u, ok := wMap["used_percent"].(float64); ok {
				usedPct = u
			} else if u, ok := wMap["percent_used"].(float64); ok {
				usedPct = u
			}
			rem := math.Max(0, 100-usedPct)
			return map[string]interface{}{
				"used":                usedPct,
				"total":               100.0,
				"remaining":           rem,
				"remainingPercentage": rem,
				"resetAt":             parseTimeISO(wMap["reset_at"]),
				"unlimited":           false,
			}
		}

		rateLimit, _ := data["rate_limit"].(map[string]interface{})
		if rateLimit != nil {
			if prim := parseCodexWindow(rateLimit["primary_window"]); prim != nil {
				quotas["session"] = prim
			}
			if sec := parseCodexWindow(rateLimit["secondary_window"]); sec != nil {
				quotas["weekly"] = sec
			}
		}

		return map[string]interface{}{"quotas": quotas}, nil

	case "deepseek":
		if apiKey == "" {
			return map[string]interface{}{"message": "No API key"}, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.deepseek.com/user/balance", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return map[string]interface{}{"message": fmt.Sprintf("API returned status %d", resp.StatusCode)}, nil
		}

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}

		quotas := make(map[string]interface{})
		if balances, ok := data["balance_infos"].([]interface{}); ok {
			for _, b := range balances {
				bMap, ok := b.(map[string]interface{})
				if !ok {
					continue
				}
				curr, _ := bMap["currency"].(string)
				tot, _ := bMap["total_balance"].(float64)
				remPct := 0.0
				if tot > 0 {
					remPct = 100.0
				}
				quotas["Balance ("+curr+")"] = map[string]interface{}{
					"used":                0.0,
					"total":               tot,
					"remainingPercentage": remPct,
					"resetAt":             nil,
				}
			}
		}

		return map[string]interface{}{"quotas": quotas}, nil

	case "groq":
		if apiKey == "" {
			return map[string]interface{}{"message": "No API key"}, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.groq.com/openai/v1/models", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return map[string]interface{}{"message": fmt.Sprintf("API returned status %d", resp.StatusCode)}, nil
		}

		quotas := make(map[string]interface{})
		limitReq := resp.Header.Get("x-ratelimit-limit-requests")
		remReq := resp.Header.Get("x-ratelimit-remaining-requests")
		if limitReq != "" && remReq != "" {
			lVal, _ := strconv.ParseFloat(limitReq, 64)
			rVal, _ := strconv.ParseFloat(remReq, 64)
			pct := 0.0
			if lVal > 0 {
				pct = (rVal / lVal) * 100
			}
			quotas["Requests"] = map[string]interface{}{
				"used":                lVal - rVal,
				"total":               lVal,
				"remainingPercentage": pct,
			}
		}

		return map[string]interface{}{"quotas": quotas}, nil

	case "opencode-go":
		if apiKey == "" {
			return map[string]interface{}{"message": "No API key"}, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://opencode.ai/zen/go/v1/usage", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return map[string]interface{}{"message": fmt.Sprintf("API returned status %d", resp.StatusCode)}, nil
		}

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}

		quotas := make(map[string]interface{})
		usageBlock, _ := data["usage"].(map[string]interface{})
		for k, val := range usageBlock {
			qMap, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			pctVal, ok := qMap["percent"].(float64)
			if !ok {
				continue
			}
			rem := math.Max(0, 100-pctVal)
			quotas[k] = map[string]interface{}{
				"used":                pctVal,
				"total":               100.0,
				"remaining":           rem,
				"remainingPercentage": rem,
				"resetAt":             parseTimeISO(qMap["resetsAt"]),
			}
		}

		return map[string]interface{}{"quotas": quotas}, nil

	case "github":
		token := accessToken
		if token == "" {
			token = apiKey
		}
		if token == "" {
			return map[string]interface{}{"message": "No token"}, nil
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/copilot_internal/user", nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "GitHubCopilotChat/0.38.0")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return map[string]interface{}{"message": fmt.Sprintf("API returned status %d", resp.StatusCode)}, nil
		}

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}

		quotas := make(map[string]interface{})
		resetAt := parseTimeISO(data["quota_reset_date"])
		if snap, ok := data["quota_snapshots"].(map[string]interface{}); ok {
			for k, v := range snap {
				vMap, ok := v.(map[string]interface{})
				if !ok {
					continue
				}
				ent, _ := vMap["entitlement"].(float64)
				rem, _ := vMap["remaining"].(float64)
				pct := 0.0
				if ent > 0 {
					pct = (rem / ent) * 100
				}
				quotas[k] = map[string]interface{}{
					"used":                ent - rem,
					"total":               ent,
					"remaining":           rem,
					"remainingPercentage": pct,
					"resetAt":             resetAt,
				}
			}
		}

		return map[string]interface{}{"quotas": quotas}, nil

	case "gemini-cli":
		if accessToken == "" {
			return map[string]interface{}{"message": "No access token"}, nil
		}
		var project string
		if psd != nil {
			if p, ok := psd["projectId"].(string); ok {
				project = p
			}
		}
		if project == "" {
			return map[string]interface{}{"message": "Missing projectId"}, nil
		}

		bodyBytes, _ := json.Marshal(map[string]string{"project": project})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://cloudcode-pa.googleapis.com/v1internal:retrieveUserQuota", strings.NewReader(string(bodyBytes)))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return map[string]interface{}{"message": fmt.Sprintf("API returned status %d", resp.StatusCode)}, nil
		}

		var data map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}

		quotas := make(map[string]interface{})
		if buckets, ok := data["buckets"].([]interface{}); ok {
			for _, b := range buckets {
				bMap, ok := b.(map[string]interface{})
				if !ok {
					continue
				}
				mID, _ := bMap["modelId"].(string)
				remFrac, _ := bMap["remainingFraction"].(float64)
				pct := remFrac * 100
				tot := 1000.0
				rem := math.Round(tot * remFrac)
				quotas[mID] = map[string]interface{}{
					"used":                tot - rem,
					"total":               tot,
					"remaining":           rem,
					"remainingPercentage": pct,
					"resetAt":             parseTimeISO(bMap["resetTime"]),
				}
			}
		}

		return map[string]interface{}{"quotas": quotas}, nil

	default:
		return map[string]interface{}{
			"message": "Usage tracking not supported for this provider",
		}, nil
	}
}

func Quota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(QuotaResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	connections, err := db.GetProviderConnections()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(QuotaResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	client := &http.Client{
		Timeout: 6 * time.Second,
	}

	results := make([]QuotaRow, len(connections))
	var wg sync.WaitGroup

	for i, conn := range connections {
		wg.Add(1)
		go func(idx int, c db.Connection) {
			defer wg.Done()

			row := QuotaRow{
				ConnectionID: c.ID,
				Provider:     c.Provider,
				Email:        c.Email,
				AuthType:     c.AuthType,
				Details:      make(map[string]interface{}),
			}

			if c.Name != nil && *c.Name != "" {
				row.Name = *c.Name
			} else {
				row.Name = c.Provider
			}

			if row.AuthType == "" {
				if _, hasKey := c.Data["apiKey"]; hasKey {
					row.AuthType = "apikey"
				} else {
					row.AuthType = "oauth"
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			usage, fetchErr := fetchProviderUsage(ctx, client, c)
			if fetchErr != nil {
				row.Status = "error"
				row.Details = map[string]interface{}{"error": fetchErr.Error()}
			} else {
				row.Details = usage
				status, remPct, resetAt, resetSec := extractQuotasFromMap(usage)
				row.Status = status
				row.RemainingPercentage = remPct
				row.ResetAt = resetAt
				row.ResetInSeconds = resetSec
			}

			results[idx] = row
		}(i, conn)
	}

	wg.Wait()

	totalConn := len(results)
	activeConn := 0
	depletedConn := 0

	for _, r := range results {
		if r.Status == "active" {
			activeConn++
		} else if r.Status == "depleted" {
			depletedConn++
		}
	}

	resp := QuotaResponse{
		Success:   true,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Summary: QuotaSummary{
			TotalConnections:    totalConn,
			ActiveConnections:   activeConn,
			DepletedConnections: depletedConn,
		},
		Quotas: results,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
