package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const syncPullPageSize = 100

// stringPtrToStr convierte un *string a string, devolviendo "" si es nil
func stringPtrToStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// normalizeSyncID normalizes a syncId to proper UUID format
// - Strips "obs-" and "prompt-" prefixes
// - Converts 32-char hex (no dashes) to UUID format (8-4-4-4-12)
func normalizeSyncID(syncID string) string {
	// Strip prefixes
	if after, ok := strings.CutPrefix(syncID, "obs-"); ok {
		syncID = after
	}
	if after, ok := strings.CutPrefix(syncID, "prompt-"); ok {
		syncID = after
	}

	// If it's 32 hex chars without dashes, convert to UUID format
	if len(syncID) == 32 && isHexOnly(syncID) {
		return strings.ToLower(syncID[0:8] + "-" + syncID[8:12] + "-" + syncID[12:16] + "-" + syncID[16:20] + "-" + syncID[20:32])
	}

	return syncID
}

// isHexOnly checks if a string contains only hex characters
func isHexOnly(s string) bool {
	for _, c := range s {
		if !isHexDigit(c) {
			return false
		}
	}
	return true
}

func isHexDigit(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// AIspaceClient es el cliente HTTP para AIspace API
type AIspaceClient struct {
	baseURL string
	token   string
	userID  string
	client  *http.Client
}

// NewAIspaceClient crea un nuevo cliente
func NewAIspaceClient(baseURL, token string) *AIspaceClient {
	return &AIspaceClient{
		baseURL: baseURL,
		token:   token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetUserID establece el ID de usuario
func (c *AIspaceClient) SetUserID(userID string) {
	c.userID = userID
}

// PullObservations obtiene observaciones desde la API
func (c *AIspaceClient) PullObservations(since time.Time, limit int) ([]Observation, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return nil, fmt.Errorf("API token not configured")
	}

	url := fmt.Sprintf("%s/men/sync/pull?userId=%s&limit=%d", c.baseURL, c.userID, limit)
	if !since.IsZero() {
		url = fmt.Sprintf("%s&lastSyncAt=%s", url, since.Format(time.RFC3339))
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed (401): your token has expired or is invalid — run 'aispace-men config --token <new-token>' to re-authenticate")
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	// API returns observations directly as array
	var observations []Observation
	if err := json.NewDecoder(resp.Body).Decode(&observations); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Normalize camelCase fields from API response
	for i := range observations {
		observations[i].NormalizePulled()
		// If syncId is still empty, generate a UUID so it's never numeric
		if observations[i].SyncID == "" {
			observations[i].SyncID = newSyncID("obs")
		}
	}

	return observations, nil
}

// GetSyncStatus obtiene el estado de sincronización desde la API
func (c *AIspaceClient) GetSyncStatus() (*SyncStatus, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return nil, fmt.Errorf("API token not configured")
	}

	// Use stats endpoint to get sync status
	req, err := http.NewRequest("GET", c.baseURL+"/men/stats?userId="+c.userID, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication failed (401): your token has expired or is invalid — run 'aispace-men config --token <new-token>' to re-authenticate")
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	var stats struct {
		Sessions     int `json:"sessions"`
		Observations int `json:"observations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Convert stats to SyncStatus
	return &SyncStatus{
		LocalChanges:  stats.Observations,
		RemoteChanges: 0,           // Would need separate API call
		LastSyncAt:    time.Time{}, // Would need to track locally
	}, nil
}

// PushAll envía sessions + observations + prompts en un solo request
func (c *AIspaceClient) PushAll(sessions []SessionPayload, observations []Observation, prompts []PromptPayload) (int, error) {
	if c.baseURL == "" {
		return 0, fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return 0, fmt.Errorf("API token not configured")
	}

	// Build payload
	type SessionDTO struct {
		ID        string  `json:"id"`
		Project   string  `json:"project"`
		Directory string  `json:"directory"`
		EndedAt   *string `json:"endedAt,omitempty"`
		Summary   *string `json:"summary,omitempty"`
	}

	type ObservationDTO struct {
		SessionID string                 `json:"sessionId"`
		UserID    string                 `json:"userId"`
		SyncID    string                 `json:"syncId,omitempty"`
		Type      string                 `json:"type"`
		Title     string                 `json:"title"`
		Content   map[string]interface{} `json:"content"`
		ToolName  string                 `json:"toolName,omitempty"`
		Project   string                 `json:"project,omitempty"`
		Scope     string                 `json:"scope,omitempty"`
		TopicKey  string                 `json:"topicKey,omitempty"`
	}

	type PromptDTO struct {
		SyncID    string  `json:"syncId"`
		SessionID string  `json:"sessionId"`
		Content   string  `json:"content"`
		Project   *string `json:"project,omitempty"`
	}

	var sessionDTOs []SessionDTO
	for _, s := range sessions {
		sessionDTOs = append(sessionDTOs, SessionDTO{
			ID:        s.ID,
			Project:   s.Project,
			Directory: s.Directory,
			EndedAt:   s.EndedAt,
			Summary:   s.Summary,
		})
	}

	observationDTOs := make([]ObservationDTO, 0, len(observations))
	for _, o := range observations {
		var content map[string]interface{}
		if err := json.Unmarshal([]byte(o.Content), &content); err != nil {
			content = map[string]interface{}{"raw": o.Content}
		}

		// Normalize sync_id to proper UUID format
		syncID := normalizeSyncID(o.SyncID)

		// Normalize type to values accepted by the backend API.
		// Internal types like "session_summary" and "passive" are mapped to
		// their closest accepted equivalent. Empty type defaults to "manual".
		obsType := normalizeTypeForAPI(o.Type)

		dto := ObservationDTO{
			UserID:   c.userID,
			SyncID:   syncID,
			Type:     obsType,
			Title:    o.Title,
			Content:  content,
			Project:  stringPtrToStr(o.Project),
			Scope:    o.Scope,
			TopicKey: stringPtrToStr(o.TopicKey),
		}
		if o.SessionID != "" {
			dto.SessionID = o.SessionID
		}
		if o.ToolName != nil {
			dto.ToolName = *o.ToolName
		}
		observationDTOs = append(observationDTOs, dto)
	}

	var promptDTOs []PromptDTO
	for _, p := range prompts {
		// Normalize sync_id to proper UUID format
		syncID := normalizeSyncID(p.SyncID)
		promptDTOs = append(promptDTOs, PromptDTO{
			SyncID:    syncID,
			SessionID: p.SessionID,
			Content:   p.Content,
			Project:   p.Project,
		})
	}

	payload := struct {
		UserID       string           `json:"userId"`
		Sessions     []SessionDTO     `json:"sessions,omitempty"`
		Observations []ObservationDTO `json:"observations"`
		Prompts      []PromptDTO      `json:"prompts,omitempty"`
	}{
		UserID:       c.userID,
		Sessions:     sessionDTOs,
		Observations: observationDTOs,
		Prompts:      promptDTOs,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/men/sync/push", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return 0, fmt.Errorf("authentication failed (401): your token has expired or is invalid — run 'aispace-men config --token <new-token>' to re-authenticate")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	var response struct {
		Observations []Observation `json:"observations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	return len(response.Observations), nil
}

// GetUserInfo obtiene información del usuario desde la API
func (c *AIspaceClient) GetUserInfo() (userID string, err error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return "", fmt.Errorf("API token not configured")
	}

	req, err := http.NewRequest("GET", c.baseURL+"/men/user", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("authentication failed (401): your token has expired or is invalid — run 'aispace-men config --token <new-token>' to re-authenticate")
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	var result struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return result.UserID, nil
}

// SessionPayload represents a session to sync
type SessionPayload struct {
	ID        string  `json:"id"`
	Project   string  `json:"project"`
	Directory string  `json:"directory"`
	EndedAt   *string `json:"ended_at,omitempty"`
	Summary   *string `json:"summary,omitempty"`
}

// PushSessions envía sesiones a la API (deben sincronizarse antes que observaciones)
func (c *AIspaceClient) PushSessions(sessions []SessionPayload) error {
	if c.baseURL == "" {
		return fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return fmt.Errorf("API token not configured")
	}

	payload := struct {
		UserID   string           `json:"userId"`
		Sessions []SessionPayload `json:"sessions"`
	}{
		UserID:   c.userID,
		Sessions: sessions,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/men/sync/sessions", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed (401): your token has expired or is invalid — run 'aispace-men config --token <new-token>' to re-authenticate")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// PromptPayload represents a prompt to sync
type PromptPayload struct {
	SyncID    string  `json:"syncId"`
	SessionID string  `json:"sessionId"`
	Content   string  `json:"content"`
	Project   *string `json:"project,omitempty"`
}

// PushPrompts envía prompts a la API
func (c *AIspaceClient) PushPrompts(prompts []PromptPayload) error {
	if c.baseURL == "" {
		return fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return fmt.Errorf("API token not configured")
	}

	payload := struct {
		UserID  string          `json:"userId"`
		Prompts []PromptPayload `json:"prompts"`
	}{
		UserID:  c.userID,
		Prompts: prompts,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal prompts: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/men/sync/prompts", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed (401): your token has expired or is invalid — run 'aispace-men config --token <new-token>' to re-authenticate")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// normalizeTypeForAPI maps internal observation types to values accepted by the
// backend API. Internal types like "session_summary" and "passive" are not part
// of the API contract and must be mapped before sending.
//
// Accepted API types: manual, decision, architecture, bugfix, pattern,
// config, discovery, learning.
// normalizeTypeForAPI maps internal observation types to values accepted by the
// backend API. Only types confirmed to be accepted are passed through — everything
// else falls back to "manual" to prevent 400 errors.
//
// Confirmed accepted by backend: manual, decision, architecture, bugfix, pattern, config, discovery.
func normalizeTypeForAPI(t string) string {
	switch t {
	case "manual", "decision", "architecture", "bugfix", "pattern", "config", "discovery":
		return t
	default:
		// session_summary, learning, passive, and any unknown type → manual
		return "manual"
	}
}
