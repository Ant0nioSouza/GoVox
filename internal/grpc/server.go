package grpc

import (
	"context"
	"sync"
	"time"

	pb "github.com/Ant0nioSouza/GoVox/api/proto"
	"github.com/Ant0nioSouza/GoVox/internal/database"
	"github.com/google/uuid"
)

type StreamContext struct {
	SessionID     uuid.UUID
	ChunkSequence int
	LastActivity  time.Time
	Cancel        context.CancelFunc
}

type Server struct {
	pb.UnimplementedTranscriptionServiceServer
	db            *database.Database
	transcriber   TranscriberInterface // Interface para o Whisper
	activeStreams map[string]*StreamContext
	StreamsMutex  sync.RWMutex
}

type TranscriberInterface interface {
	Transcribe(ctx context.Context, audioData []byte, format string, sampleRate, channels int) (*TranscriptionOutput, error)
}

type TranscriptionOutput struct {
	Language   string
	Text       string
	Confidence float64
	Duration   float64
}
