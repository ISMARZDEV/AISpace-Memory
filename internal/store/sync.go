package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
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

	// 1. Buscar por sync_id (ID remoto) — never use numeric obs.ID as syncId
	syncIDLookup := obs.SyncID
	if syncIDLookup != "" {
		var existingID int64
		var existingUpdatedAtStr string
		err := s.db.QueryRow(`
			SELECT id, updated_at FROM observations
			WHERE sync_id = ?
		`, syncIDLookup).Scan(&existingID, &existingUpdatedAtStr)

		if err == nil {
			// Ya existe - actualizar si es más reciente
			existingUpdatedAt, _ := time.Parse(time.RFC3339, existingUpdatedAtStr)
			if obsUpdatedAt.After(existingUpdatedAt) {
				_, err = s.db.Exec(`
					UPDATE observations
					SET type = ?, title = ?, content = ?, project = ?, scope = ?, updated_at = ?
					WHERE id = ?
				`, obs.Type, obs.Title, obs.Content, obs.Project, obs.Scope, obs.UpdatedAt, existingID)
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
				_, err = s.db.Exec(`
					UPDATE observations
					SET type = ?, title = ?, content = ?, updated_at = ?
					WHERE id = ?
				`, obs.Type, obs.Title, obs.Content, obs.UpdatedAt, existingID)
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
				_, err = s.db.Exec(`
					UPDATE observations
					SET type = ?, content = ?, project = ?, scope = ?, updated_at = ?
					WHERE id = ?
				`, obs.Type, obs.Content, obs.Project, obs.Scope, obs.UpdatedAt, existingID)
			}
			return err
		}
	}

	// No existe - insertar nuevo (always use a proper UUID as syncId)
	syncIDStr := obs.SyncID
	if syncIDStr == "" {
		syncIDStr = newSyncID("obs")
	}
	_, err := s.db.Exec(`
		INSERT INTO observations (sync_id, session_id, type, title, content, project, scope, topic_key, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, syncIDStr, obs.SessionID, obs.Type, obs.Title, obs.Content, obs.Project, obs.Scope, obs.TopicKey,
		obs.CreatedAt, obs.UpdatedAt)
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

// SyncMutations realiza sincronización usando la cola de mutaciones (correcto orden)
func (s *Store) SyncMutations(client *AIspaceClient) (*SyncResult, error) {
	result := &SyncResult{}

	// Auto-enroll projects with pending mutations before syncing
	if err := s.enrollPendingMutationProjects(); err != nil {
		// Log warning but continue - sync will handle it
	}

	// Obtener mutaciones pendientes ordenadas por seq
	mutations, err := s.ListPendingSyncMutations(DefaultSyncTargetKey, 100)
	if err != nil {
		return nil, fmt.Errorf("list pending mutations: %w", err)
	}

	if len(mutations) == 0 {
		return result, nil
	}

	// Agrupar por entidad para enviar
	var sessionMutations []SyncMutation
	var observationMutations []SyncMutation
	var promptMutations []SyncMutation

	for _, m := range mutations {
		switch m.Entity {
		case SyncEntitySession:
			sessionMutations = append(sessionMutations, m)
		case SyncEntityObservation:
			observationMutations = append(observationMutations, m)
		case SyncEntityPrompt:
			promptMutations = append(promptMutations, m)
		}
	}

	// Preparar sessions para incluir en el push
	var sessions []SessionPayload
	sessionsSent := make(map[string]bool)
	for _, m := range sessionMutations {
		var payload syncSessionPayload
		if err := json.Unmarshal([]byte(m.Payload), &payload); err != nil {
			result.Errors++
			continue
		}
		sessions = append(sessions, SessionPayload{
			ID:        payload.ID,
			Project:   payload.Project,
			Directory: payload.Directory,
			EndedAt:   payload.EndedAt,
			Summary:   payload.Summary,
		})
		sessionsSent[payload.ID] = true
	}

	// Preparar observations
	var observations []Observation
	for _, m := range observationMutations {
		var payload syncObservationPayload
		if err := json.Unmarshal([]byte(m.Payload), &payload); err != nil {
			result.Errors++
			continue
		}
		obs := Observation{
			SyncID:    payload.SyncID,
			SessionID: payload.SessionID,
			Type:      payload.Type,
			Title:     payload.Title,
			Content:   payload.Content,
			ToolName:  payload.ToolName,
			Project:   payload.Project,
			Scope:     payload.Scope,
			TopicKey:  payload.TopicKey,
		}
		observations = append(observations, obs)

		// Ensure the session referenced by this observation is included in the push,
		// even if its mutation was already acked in a previous sync cycle.
		if obs.SessionID != "" && !sessionsSent[obs.SessionID] {
			if session, err := s.getSessionPayload(obs.SessionID); err == nil {
				sessions = append(sessions, session)
				sessionsSent[obs.SessionID] = true
			}
		}
	}

	// Preparar prompts
	var prompts []PromptPayload
	for _, m := range promptMutations {
		var payload syncPromptPayload
		if err := json.Unmarshal([]byte(m.Payload), &payload); err != nil {
			result.Errors++
			continue
		}
		prompts = append(prompts, PromptPayload{
			SyncID:    payload.SyncID,
			SessionID: payload.SessionID,
			Content:   payload.Content,
			Project:   payload.Project,
		})
	}

	// Push todo junto en un solo request
	pushedObs, err := client.PushAll(sessions, observations, prompts)
	if err != nil {
		return nil, fmt.Errorf("push: %w", err)
	}
	result.Pushed = len(sessions) + pushedObs + len(prompts)

	// Ack all mutations
	var allSeqs []int64
	for _, m := range sessionMutations {
		allSeqs = append(allSeqs, m.Seq)
	}
	for _, m := range observationMutations {
		allSeqs = append(allSeqs, m.Seq)
	}
	for _, m := range promptMutations {
		allSeqs = append(allSeqs, m.Seq)
	}
	if err := s.AckSyncMutationSeqs(DefaultSyncTargetKey, allSeqs); err != nil {
		return nil, fmt.Errorf("ack mutations: %w", err)
	}

	// Pull: obtener observaciones nuevas desde última sync (paginado)
	lastSync := s.GetLastSyncTime()
	var totalPulled int
	var latestPullTime time.Time

	for {
		remote, err := client.PullObservations(lastSync, syncPullPageSize)
		if err != nil {
			log.Printf("[sync] pull error: %v", err)
			break
		}
		if len(remote) == 0 {
			break
		}
		for _, obs := range remote {
			if applyErr := s.UpsertObservation(obs); applyErr != nil {
				log.Printf("[sync] warn: could not upsert pulled observation %s: %v", obs.SyncID, applyErr)
				result.Errors++
				continue
			}
		}
		totalPulled += len(remote)
		result.Pulled = totalPulled

		for _, obs := range remote {
			t, err := time.Parse(time.RFC3339, obs.UpdatedAt)
			if err == nil && t.After(latestPullTime) {
				latestPullTime = t
			}
		}

		if len(remote) < syncPullPageSize {
			break
		}

		lastSync = latestPullTime.Add(time.Millisecond)
	}

	if latestPullTime.After(time.Time{}) {
		s.SetLastSyncTime(latestPullTime)
	} else if totalPulled == 0 {
		s.SetLastSyncTime(time.Now())
	}

	if n, err := s.PurgeAckedMutations("cloud", 7*24*time.Hour); err == nil && n > 0 {
		log.Printf("[sync] purged %d old acknowledged mutations", n)
	}

	return result, nil
}

// enrollPendingMutationProjects inscribes proyectos que tienen mutaciones pendientes
func (s *Store) enrollPendingMutationProjects() error {
	return s.withTx(func(tx *sql.Tx) error {
		// Find projects with pending mutations that are not enrolled
		rows, err := s.queryItHook(tx, `
			SELECT DISTINCT sm.project
			FROM sync_mutations sm
			WHERE sm.acked_at IS NULL
			  AND sm.project != ''
			  AND sm.project NOT IN (SELECT project FROM sync_enrolled_projects)
		`)
		if err != nil {
			return err
		}
		defer rows.Close()

		var projects []string
		for rows.Next() {
			var project string
			if err := rows.Scan(&project); err != nil {
				return err
			}
			projects = append(projects, project)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// Enroll each project
		for _, project := range projects {
			if _, err := s.execHook(tx,
				`INSERT OR IGNORE INTO sync_enrolled_projects (project) VALUES (?)`,
				project,
			); err != nil {
				return err
			}
		}

		return nil
	})
}

// getSessionPayload fetches a session from the local DB and returns it as a SessionPayload.
// Used to include already-acked sessions that are referenced by pending observations.
func (s *Store) getSessionPayload(sessionID string) (SessionPayload, error) {
	var project, directory string
	var endedAt, summary sql.NullString
	err := s.db.QueryRow(`
		SELECT project, directory, ended_at, summary
		FROM sessions
		WHERE id = ?
	`, sessionID).Scan(&project, &directory, &endedAt, &summary)
	if err != nil {
		return SessionPayload{}, fmt.Errorf("session %s not found: %w", sessionID, err)
	}
	session := SessionPayload{
		ID:        sessionID,
		Project:   project,
		Directory: directory,
	}
	if endedAt.Valid {
		session.EndedAt = &endedAt.String
	}
	if summary.Valid {
		session.Summary = &summary.String
	}
	return session, nil
}
