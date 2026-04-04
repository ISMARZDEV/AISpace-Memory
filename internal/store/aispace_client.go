package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// stringPtrToStr convierte un *string a string, devolviendo "" si es nil
func stringPtrToStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

// PushObservations envía observaciones locales a la API
func (c *AIspaceClient) PushObservations(obs []Observation) error {
	if c.baseURL == "" {
		return fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return fmt.Errorf("API token not configured")
	}

	// Convertir observaciones a formato de la API
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

	// Parse content from JSON string to map
	var dtos []ObservationDTO
	for _, o := range obs {
		var content map[string]interface{}
		if err := json.Unmarshal([]byte(o.Content), &content); err != nil {
			// If content is not valid JSON, wrap it
			content = map[string]interface{}{"raw": o.Content}
		}

		dto := ObservationDTO{
			UserID:   c.userID,
			Type:     o.Type,
			Title:    o.Title,
			Content:  content,
			Project:  stringPtrToStr(o.Project),
			Scope:    "project", // default scope
			TopicKey: stringPtrToStr(o.TopicKey),
		}
		if o.SessionID != "" {
			dto.SessionID = o.SessionID
		}
		if o.SyncID != "" {
			dto.SyncID = o.SyncID
		}
		if o.ToolName != nil {
			dto.ToolName = *o.ToolName
		}
		if o.SessionID != "" {
			dto.SessionID = o.SessionID
		}
		if o.SyncID != "" {
			dto.SyncID = o.SyncID
		}
		dtos = append(dtos, dto)
	}

	payload := struct {
		UserId       string           `json:"userId"`
		Observations []ObservationDTO `json:"observations"`
	}{
		UserId:       c.userID,
		Observations: dtos,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal observations: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/men/sync/push", bytes.NewReader(body))
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	return nil
}

// PushObservationsWithResponse envía observaciones a la API y retorna las observaciones creadas con sus IDs
func (c *AIspaceClient) PushObservationsWithResponse(obs []Observation) ([]Observation, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return nil, fmt.Errorf("API token not configured")
	}

	// Convertir observaciones a formato de la API
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

	// Parse content from JSON string to map
	var dtos []ObservationDTO
	for _, o := range obs {
		var content map[string]interface{}
		if err := json.Unmarshal([]byte(o.Content), &content); err != nil {
			// If content is not valid JSON, wrap it
			content = map[string]interface{}{"raw": o.Content}
		}

		dto := ObservationDTO{
			UserID:   c.userID,
			Type:     o.Type,
			Title:    o.Title,
			Content:  content,
			Project:  stringPtrToStr(o.Project),
			Scope:    "project", // default scope
			TopicKey: stringPtrToStr(o.TopicKey),
		}
		if o.SessionID != "" {
			dto.SessionID = o.SessionID
		}
		if o.SyncID != "" {
			dto.SyncID = o.SyncID
		}
		if o.ToolName != nil {
			dto.ToolName = *o.ToolName
		}
		dtos = append(dtos, dto)
	}

	payload := struct {
		UserId       string           `json:"userId"`
		Observations []ObservationDTO `json:"observations"`
	}{
		UserId:       c.userID,
		Observations: dtos,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal observations: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/men/sync/push", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	// Parse response to get created observations with IDs
	var response struct {
		Observations []Observation `json:"observations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return response.Observations, nil
}

// PullObservations obtiene observaciones desde la API
func (c *AIspaceClient) PullObservations(since time.Time) ([]Observation, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return nil, fmt.Errorf("API token not configured")
	}

	// Use the correct API endpoint with limit parameter
	url := fmt.Sprintf("%s/men/sync/pull?userId=%s&limit=100",
		c.baseURL, c.userID)

	// Only add lastSyncAt if we have a valid time
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

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(respBody))
	}

	// API returns observations directly as array
	var observations []Observation
	if err := json.NewDecoder(resp.Body).Decode(&observations); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
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
