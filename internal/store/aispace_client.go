package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

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

	// Convertir a formato JSON para la API
	payload := struct {
		Observations []Observation `json:"observations"`
	}{
		Observations: obs,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal observations: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/men/observations/sync", bytes.NewReader(body))
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

// PullObservations obtiene observaciones desde la API
func (c *AIspaceClient) PullObservations(since time.Time) ([]Observation, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return nil, fmt.Errorf("API token not configured")
	}

	url := fmt.Sprintf("%s/men/observations?since=%s", c.baseURL, since.Format(time.RFC3339))

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

	var result struct {
		Observations []Observation `json:"observations"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.Observations, nil
}

// GetSyncStatus obtiene el estado de sincronización desde la API
func (c *AIspaceClient) GetSyncStatus() (*SyncStatus, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("API URL not configured")
	}
	if c.token == "" {
		return nil, fmt.Errorf("API token not configured")
	}

	req, err := http.NewRequest("GET", c.baseURL+"/men/sync/status", nil)
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

	var status SyncStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &status, nil
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
