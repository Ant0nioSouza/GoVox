// internal/database/database.go
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/Ant0nioSouza/GoVox/pkg/models"
	"github.com/Ant0nioSouza/GoVox/pkg/utils"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct {
	pool *pgxpool.Pool
}

// Config para configuração do banco
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	// Pool settings
	MaxConns    int32
	MinConns    int32
	MaxConnLife time.Duration
	MaxConnIdle time.Duration
}

// New cria uma nova conexão com pool otimizado
func New(ctx context.Context, cfg Config) (*Database, error) {
	// Connection string com timezone
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable pool_max_conns=%d pool_min_conns=%d timezone=UTC",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.MaxConns, cfg.MinConns,
	)

	// Configuração do pool
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Otimizações para alta concorrência
	poolConfig.MaxConns = cfg.MaxConns             // Máximo de conexões
	poolConfig.MinConns = cfg.MinConns             // Mínimo mantido aberto
	poolConfig.MaxConnLifetime = cfg.MaxConnLife   // Recicla conexões antigas
	poolConfig.MaxConnIdleTime = cfg.MaxConnIdle   // Fecha conexões idle
	poolConfig.HealthCheckPeriod = 1 * time.Minute // Verifica saúde das conexões

	// Cria o pool
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}

	// Testa a conexão
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Database{pool: pool}, nil
}

// Close fecha o pool de conexões
func (db *Database) Close() {
	db.pool.Close()
}

// CreateSession cria uma nova sessão para stream contínua
// Agora gera UUIDv7 no Go ao invés do banco
func (db *Database) CreateSession(ctx context.Context, metadata map[string]any) (*models.Session, error) {
	// Gera UUIDv7 (ordenável por timestamp)
	sessionID := utils.NewUUIDv7()

	query := `
        INSERT INTO sessions (id, metadata)
        VALUES ($1, $2)
        RETURNING id, started_at, last_activity_at, status, total_chunks, total_duration_seconds, metadata
    `

	session := &models.Session{}
	err := db.pool.QueryRow(ctx, query, sessionID, metadata).Scan(
		&session.ID,
		&session.StartedAt,
		&session.LastActivityAt,
		&session.Status,
		&session.TotalChunks,
		&session.TotalDurationSeconds,
		&session.Metadata,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return session, nil
}

// GetSession busca uma sessão por ID
func (db *Database) GetSession(ctx context.Context, sessionID string) (*models.Session, error) {
	query := `
        SELECT id, started_at, last_activity_at, status, total_chunks, 
               total_duration_seconds, primary_language, metadata
        FROM sessions
        WHERE id = $1
    `

	session := &models.Session{}
	err := db.pool.QueryRow(ctx, query, sessionID).Scan(
		&session.ID,
		&session.StartedAt,
		&session.LastActivityAt,
		&session.Status,
		&session.TotalChunks,
		&session.TotalDurationSeconds,
		&session.PrimaryLanguage,
		&session.Metadata,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	return session, nil
}

// UpdateSessionStatus atualiza o status de uma sessão
func (db *Database) UpdateSessionStatus(ctx context.Context, sessionID string, status models.SessionStatus) error {
	query := `
        UPDATE sessions 
        SET status = $1, last_activity_at = NOW()
        WHERE id = $2
    `

	_, err := db.pool.Exec(ctx, query, status, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	return nil
}

// SaveTranscription salva um chunk de transcrição
func (db *Database) SaveTranscription(ctx context.Context, trans *models.Transcription) error {
	query := `
        INSERT INTO transcriptions (
            session_id, chunk_sequence, audio_hash, language, text,
            confidence, duration_seconds, audio_format, sample_rate, channels, metadata
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        RETURNING id, created_at
    `

	err := db.pool.QueryRow(
		ctx, query,
		trans.SessionID,
		trans.ChunkSequence,
		trans.AudioHash,
		trans.Language,
		trans.Text,
		trans.Confidence,
		trans.DurationSeconds,
		trans.AudioFormat,
		trans.SampleRate,
		trans.Channels,
		trans.Metadata,
	).Scan(&trans.ID, &trans.CreatedAt)

	if err != nil {
		return fmt.Errorf("failed to save transcription: %w", err)
	}

	return nil
}

// SaveTranscriptionsBatch salva múltiplos chunks em batch (muito mais rápido!)
func (db *Database) SaveTranscriptionsBatch(ctx context.Context, transcriptions []*models.Transcription) error {
	// Inicia transação
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Prepara statement
	stmt := `
        INSERT INTO transcriptions (
            session_id, chunk_sequence, audio_hash, language, text,
            confidence, duration_seconds, audio_format, sample_rate, channels, metadata
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
    `

	// Executa batch
	for _, trans := range transcriptions {
		_, err := tx.Exec(
			ctx, stmt,
			trans.SessionID,
			trans.ChunkSequence,
			trans.AudioHash,
			trans.Language,
			trans.Text,
			trans.Confidence,
			trans.DurationSeconds,
			trans.AudioFormat,
			trans.SampleRate,
			trans.Channels,
			trans.Metadata,
		)
		if err != nil {
			return fmt.Errorf("failed to insert transcription: %w", err)
		}
	}

	// Commit da transação
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetSessionTranscriptions busca todas as transcrições de uma sessão (ordenado)
func (db *Database) GetSessionTranscriptions(ctx context.Context, sessionID string, limit, offset int) ([]*models.Transcription, error) {
	query := `
        SELECT 
            id, session_id, chunk_sequence, audio_hash, language, text,
            confidence, duration_seconds, audio_format, sample_rate, channels, metadata, created_at
        FROM transcriptions
        WHERE session_id = $1
        ORDER BY chunk_sequence ASC
        LIMIT $2 OFFSET $3
    `

	rows, err := db.pool.Query(ctx, query, sessionID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to query transcriptions: %w", err)
	}
	defer rows.Close()

	var transcriptions []*models.Transcription
	for rows.Next() {
		trans := &models.Transcription{}
		err := rows.Scan(
			&trans.ID,
			&trans.SessionID,
			&trans.ChunkSequence,
			&trans.AudioHash,
			&trans.Language,
			&trans.Text,
			&trans.Confidence,
			&trans.DurationSeconds,
			&trans.AudioFormat,
			&trans.SampleRate,
			&trans.Channels,
			&trans.Metadata,
			&trans.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan transcription: %w", err)
		}
		transcriptions = append(transcriptions, trans)
	}

	return transcriptions, nil
}

// SearchTranscriptions busca por texto (full-text search)
func (db *Database) SearchTranscriptions(ctx context.Context, searchTerm string, limit int) ([]*models.Transcription, error) {
	query := `
        SELECT 
            id, session_id, chunk_sequence, audio_hash, language, text,
            confidence, duration_seconds, audio_format, sample_rate, channels, metadata, created_at,
            ts_rank(to_tsvector('simple', text), plainto_tsquery('simple', $1)) as rank
        FROM transcriptions
        WHERE to_tsvector('simple', text) @@ plainto_tsquery('simple', $1)
        ORDER BY rank DESC, created_at DESC
        LIMIT $2
    `

	rows, err := db.pool.Query(ctx, query, searchTerm, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search transcriptions: %w", err)
	}
	defer rows.Close()

	var transcriptions []*models.Transcription
	for rows.Next() {
		trans := &models.Transcription{}
		var rank float64
		err := rows.Scan(
			&trans.ID,
			&trans.SessionID,
			&trans.ChunkSequence,
			&trans.AudioHash,
			&trans.Language,
			&trans.Text,
			&trans.Confidence,
			&trans.DurationSeconds,
			&trans.AudioFormat,
			&trans.SampleRate,
			&trans.Channels,
			&trans.Metadata,
			&trans.CreatedAt,
			&rank,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan search result: %w", err)
		}
		transcriptions = append(transcriptions, trans)
	}

	return transcriptions, nil
}

// GetPoolStats retorna estatísticas do pool de conexões
func (db *Database) GetPoolStats() map[string]int32 {
	stat := db.pool.Stat()
	return map[string]int32{
		"total_conns":    stat.TotalConns(),
		"acquired_conns": stat.AcquiredConns(),
		"idle_conns":     stat.IdleConns(),
		"max_conns":      stat.MaxConns(),
	}
}
