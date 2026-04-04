package store

import (
	"database/sql"
	"fmt"
	"time"
)

// SyncStatus representa el estado de sincronización
type SyncStatus struct {
	LocalChanges  int       `json:"local_changes"`
	RemoteChanges int       `json:"remote_changes"`
	LastSyncAt    time.Time `json:"last_sync_at"`
	Error         string    `json:"error,omitempty"`
}

// SyncResult representa el resultado de una sincronización
type SyncResult struct {
	Pushed int `json:"pushed"`
	Pulled int `json:"pulled"`
	Errors int `json:"errors"`
}

// Sync realiza sincronización bidireccional
func (s *Store) Sync(client *AIspaceClient) (*SyncResult, error) {
	result := &SyncResult{}

	// 1. Push: enviar observaciones pendientes
	pending, err := s.GetPendingSync()
	if err != nil {
		return nil, fmt.Errorf("get pending observations: %w", err)
	}

	if len(pending) > 0 {
		// Push y obtener los IDs remotos
		pushedObs, err := client.PushObservationsWithResponse(pending)
		if err != nil {
			return nil, fmt.Errorf("push observations: %w", err)
		}

		// Marcar como sincronizadas y guardar sync_id
		now := time.Now()
		for i, obs := range pending {
			syncID := ""
			if i < len(pushedObs) && pushedObs[i].ID > 0 {
				syncID = fmt.Sprintf("%d", pushedObs[i].ID)
			}
			if err := s.MarkSyncedWithSyncID(obs.ID, now, syncID); err != nil {
				result.Errors++
				continue
			}
			result.Pushed++
		}
	}

	// 2. Pull: obtener observaciones nuevas desde última sync
	lastSync := s.GetLastSyncTime()
	remote, err := client.PullObservations(lastSync)
	if err != nil {
		return nil, fmt.Errorf("pull observations: %w", err)
	}

	// 3. Upsert: guardar observaciones remotas (last-write-wins)
	for _, obs := range remote {
		if err := s.UpsertObservation(obs); err != nil {
			result.Errors++
			continue
		}
		result.Pulled++
	}

	// Actualizar última sincronización
	s.SetLastSyncTime(time.Now())

	return result, nil
}

// GetPendingSync obtiene observaciones pendientes de sincronizar
func (s *Store) GetPendingSync() ([]Observation, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, type, title, content, project, scope, topic_key, created_at, updated_at
		FROM observations
		WHERE sync_status = 'pending' OR sync_status IS NULL
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query pending: %w", err)
	}
	defer rows.Close()

	var observations []Observation
	for rows.Next() {
		var obs Observation
		var topicKey, project sql.NullString
		if err := rows.Scan(&obs.ID, &obs.SessionID, &obs.Type, &obs.Title, &obs.Content,
			&project, &obs.Scope, &topicKey, &obs.CreatedAt, &obs.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan observation: %w", err)
		}
		if project.Valid {
			obs.Project = &project.String
		}
		if topicKey.Valid {
			obs.TopicKey = &topicKey.String
		}
		observations = append(observations, obs)
	}

	return observations, nil
}

// MarkSynced marca una observación como sincronizada
func (s *Store) MarkSynced(id int64, syncedAt time.Time) error {
	_, err := s.db.Exec(`
		UPDATE observations
		SET sync_status = 'synced', synced_at = ?
		WHERE id = ?
	`, syncedAt.Format(time.RFC3339), id)
	return err
}

// MarkSyncedWithSyncID marca una observación como sincronizada y guarda el ID remoto
func (s *Store) MarkSyncedWithSyncID(id int64, syncedAt time.Time, syncID string) error {
	_, err := s.db.Exec(`
		UPDATE observations
		SET sync_status = 'synced', synced_at = ?, sync_id = ?
		WHERE id = ?
	`, syncedAt.Format(time.RFC3339), syncID, id)
	return err
}

// UpsertObservation inserta o actualiza una observación desde la API
func (s *Store) UpsertObservation(obs Observation) error {
	// Asegurar que la sesión existe
	if err := s.ensureSession(obs.SessionID, obs.Project); err != nil {
		return fmt.Errorf("ensure session: %w", err)
	}

	// Estrategia de deduplicación:
	// 1. Si tiene ID de la API (obs.ID > 0), buscar por sync_id
	// 2. Si tiene topic_key, buscar por topic_key + project
	// 3. Si no, buscar por title + created_at (similar contenido)

	// Parse timestamps for comparison
	obsUpdatedAt, _ := time.Parse(time.RFC3339, obs.UpdatedAt)

	// 1. Buscar por sync_id (ID remoto)
	if obs.ID > 0 {
		var existingID int64
		var existingUpdatedAtStr string
		err := s.db.QueryRow(`
			SELECT id, updated_at FROM observations
			WHERE sync_id = ?
		`, fmt.Sprintf("%d", obs.ID)).Scan(&existingID, &existingUpdatedAtStr)

		if err == nil {
			// Ya existe - actualizar si es más reciente
			existingUpdatedAt, _ := time.Parse(time.RFC3339, existingUpdatedAtStr)
			if obsUpdatedAt.After(existingUpdatedAt) {
				_, err = s.db.Exec(`
					UPDATE observations
					SET type = ?, title = ?, content = ?, project = ?, scope = ?, updated_at = ?, sync_status = 'synced', synced_at = ?
					WHERE id = ?
				`, obs.Type, obs.Title, obs.Content, obs.Project, obs.Scope, obs.UpdatedAt, time.Now().Format(time.RFC3339), existingID)
			}
			return err
		}
	}

	// 2. Buscar por topic_key + project
	if obs.TopicKey != nil && *obs.TopicKey != "" {
		var existingID int64
		var existingUpdatedAtStr string
		err := s.db.QueryRow(`
			SELECT id, updated_at FROM observations
			WHERE topic_key = ? AND (project = ? OR (project IS NULL AND ? IS NULL))
		`, *obs.TopicKey, obs.Project, obs.Project).Scan(&existingID, &existingUpdatedAtStr)

		if err == nil {
			// Existe - comparar timestamps
			existingUpdatedAt, _ := time.Parse(time.RFC3339, existingUpdatedAtStr)
			if obsUpdatedAt.After(existingUpdatedAt) {
				// Actualizar y guardar sync_id
				syncIDStr := ""
				if obs.ID > 0 {
					syncIDStr = fmt.Sprintf("%d", obs.ID)
				}
				_, err = s.db.Exec(`
					UPDATE observations
					SET type = ?, title = ?, content = ?, updated_at = ?, sync_status = 'synced', synced_at = ?, sync_id = ?
					WHERE id = ?
				`, obs.Type, obs.Title, obs.Content, obs.UpdatedAt, time.Now().Format(time.RFC3339), syncIDStr, existingID)
			}
			return err
		}
	}

	// 3. Buscar por title + session_id (observaciones en la misma sesión)
	if obs.SessionID != "" {
		var existingID int64
		var existingUpdatedAtStr string
		err := s.db.QueryRow(`
			SELECT id, updated_at FROM observations
			WHERE title = ? AND session_id = ?
		`, obs.Title, obs.SessionID).Scan(&existingID, &existingUpdatedAtStr)

		if err == nil {
			// Ya existe en la misma sesión con el mismo título - actualizar
			existingUpdatedAt, _ := time.Parse(time.RFC3339, existingUpdatedAtStr)
			if obsUpdatedAt.After(existingUpdatedAt) {
				syncIDStr := ""
				if obs.ID > 0 {
					syncIDStr = fmt.Sprintf("%d", obs.ID)
				}
				_, err = s.db.Exec(`
					UPDATE observations
					SET type = ?, content = ?, project = ?, scope = ?, updated_at = ?, sync_status = 'synced', synced_at = ?, sync_id = ?
					WHERE id = ?
				`, obs.Type, obs.Content, obs.Project, obs.Scope, obs.UpdatedAt, time.Now().Format(time.RFC3339), syncIDStr, existingID)
			}
			return err
		}
	}

	// No existe - insertar nuevo
	syncIDStr := ""
	if obs.ID > 0 {
		syncIDStr = fmt.Sprintf("%d", obs.ID)
	}
	_, err := s.db.Exec(`
		INSERT INTO observations (sync_id, session_id, type, title, content, project, scope, topic_key, created_at, updated_at, sync_status, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'synced', ?)
	`, syncIDStr, obs.SessionID, obs.Type, obs.Title, obs.Content, obs.Project, obs.Scope, obs.TopicKey,
		obs.CreatedAt, obs.UpdatedAt, time.Now().Format(time.RFC3339))
	return err
}

// ensureSession crea la sesión si no existe
func (s *Store) ensureSession(sessionID string, project *string) error {
	if sessionID == "" {
		return nil
	}

	// Verificar si existe
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM sessions WHERE id = ?`, sessionID).Scan(&exists)
	if err == nil {
		return nil // Ya existe
	}

	// Crear sesión con valores por defecto
	projectVal := "unknown"
	if project != nil && *project != "" {
		projectVal = *project
	}

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO sessions (id, project, directory, started_at)
		VALUES (?, ?, '/synced', datetime('now'))
	`, sessionID, projectVal)
	return err
}

// GetLastSyncTime obtiene la fecha de última sincronización
func (s *Store) GetLastSyncTime() time.Time {
	var lastSyncStr string
	err := s.db.QueryRow(`SELECT value FROM config WHERE key = 'last_sync_at'`).Scan(&lastSyncStr)
	if err != nil {
		return time.Time{} // No hay sincronización previa
	}

	lastSync, err := time.Parse(time.RFC3339, lastSyncStr)
	if err != nil {
		return time.Time{}
	}
	return lastSync
}

// SetLastSyncTime establece la fecha de última sincronización
func (s *Store) SetLastSyncTime(t time.Time) error {
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO config (key, value) VALUES ('last_sync_at', ?)
	`, t.Format(time.RFC3339))
	return err
}
