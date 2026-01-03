package models

import (
	"time"

	"github.com/google/uuid"
)

// Session representa uma stream contínua de áudio
type Session struct {
	ID                   uuid.UUID      `json:"id"`
	StartedAt            time.Time      `json:"started_at"`
	LastActivityAt       time.Time      `json:"last_activity_at"`
	Status               SessionStatus  `json:"status"`
	TotalChunks          int            `json:"total_chunks"`
	TotalDurationSeconds float64        `json:"total_duration_seconds"`
	PrimaryLanguage      *string        `json:"primary_language,omitempty"`
	Metadata             map[string]any `json:"metadata"`
}

type SessionStatus string

const (
	SessionActive    SessionStatus = "active"
	SessionPaused    SessionStatus = "paused"
	SessionCompleted SessionStatus = "completed"
	SessionError     SessionStatus = "error"
)

// Transcription representa um chunk transcrito
type Transcription struct {
	ID              int64          `json:"id"`
	SessionID       uuid.UUID      `json:"session_id"`
	ChunkSequence   int            `json:"chunk_sequence"`
	AudioHash       string         `json:"audio_hash,omitempty"`
	Language        string         `json:"language"`
	Text            string         `json:"text"`
	Confidence      float64        `json:"confidence"`
	DurationSeconds float64        `json:"duration_seconds"`
	AudioFormat     string         `json:"audio_format"`
	SampleRate      int            `json:"sample_rate"`
	Channels        int            `json:"channels"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"created_at"` // Será TIMESTAMPTZ
}

// TranscriptionRequest representa uma requisição de transcrição
type TranscriptionRequest struct {
	SessionID     uuid.UUID `json:"session_id"`
	ChunkSequence int       `json:"chunk_sequence"`
	AudioData     []byte    `json:"audio_data"`
	AudioFormat   string    `json:"audio_format"`
}
