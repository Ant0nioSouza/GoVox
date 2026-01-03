package grpc

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	pb "github.com/Ant0nioSouza/GoVox/api/proto"
	"github.com/Ant0nioSouza/GoVox/internal/database"
	"github.com/Ant0nioSouza/GoVox/pkg/models"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type StreamContext struct {
	SessionID     uuid.UUID
	ChunkSequence int
	LastActivity  time.Time
	WaitGroup     *sync.WaitGroup
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

func NewServer(db *database.Database, transcriber TranscriberInterface) *Server {
	return &Server{
		db:            db,
		transcriber:   transcriber,
		activeStreams: make(map[string]*StreamContext),
	}
}

func (s *Server) CreateSession(ctx context.Context, req *pb.CreateSessionRequest) (*pb.CreateSessionResponse, error) {
	log.Printf("Creating new session...")

	metadata := make(map[string]any)

	for key, value := range req.Metadata {
		metadata[key] = value
	}

	session, err := s.db.CreateSession(ctx, metadata)
	if err != nil {
		log.Printf("Error creating session: %v", err)
		return nil, err
	}
	log.Printf("Session created: %s", session.ID)

	return &pb.CreateSessionResponse{
		SessionId: session.ID.String(),
		CreatedAt: session.StartedAt.Format(time.RFC3339),
	}, nil
}

// EndSession finaliza uma sessão
func (s *Server) EndSession(ctx context.Context, req *pb.EndSessionRequest) (*pb.EndSessionResponse, error) {
	log.Printf("🏁 Ending session: %s", req.SessionId)

	// Parse status
	var sessionStatus models.SessionStatus
	switch req.Status {
	case "completed":
		sessionStatus = models.SessionCompleted
	case "error":
		sessionStatus = models.SessionError
	case "paused":
		sessionStatus = models.SessionPaused
	default:
		return nil, status.Errorf(codes.InvalidArgument, "invalid status: %s", req.Status)
	}

	// Atualiza status no banco
	if err := s.db.UpdateSessionStatus(ctx, req.SessionId, sessionStatus); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update session: %v", err)
	}

	// Busca sessão atualizada
	session, err := s.db.GetSession(ctx, req.SessionId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get session: %v", err)
	}

	primaryLang := ""
	if session.PrimaryLanguage != nil {
		primaryLang = *session.PrimaryLanguage
	}

	log.Printf("✅ Session ended: %d chunks, %.2fs total",
		session.TotalChunks, session.TotalDurationSeconds)

	return &pb.EndSessionResponse{
		Success:              true,
		Message:              "Session ended successfully",
		TotalChunks:          int32(session.TotalChunks),
		TotalDurationSeconds: session.TotalDurationSeconds,
		PrimaryLanguage:      primaryLang,
	}, nil
}

func (s *Server) StreamAudio(stream pb.TranscriptionService_StreamAudioServer) error {
	log.Println("New audio stream started")

	var streamCtx *StreamContext
	var sessionID uuid.UUID
	var wg sync.WaitGroup

	for {
		// Recebe chunk do cliente
		chunk, err := stream.Recv()
		if err == io.EOF {
			log.Println("Audio stream ended by client")
			wg.Wait()
			log.Printf("All chunks processed for session: %s", sessionID)
			break
		}
		if err != nil {
			log.Printf("Error receiving audio chunk: %v", err)
			return status.Errorf(codes.Internal, "failed to receive chunk %v", err)
		}

		if sessionID == uuid.Nil {
			parsedID, err := uuid.Parse(chunk.SessionId)
			if err != nil {
				log.Printf("Invalid session ID: %v", err)
				return status.Errorf(codes.InvalidArgument, "invalid session ID: %v", err)
			}
			sessionID = parsedID

			// Registra stream ativa
			_, cancel := context.WithCancel(context.Background())
			streamCtx = &StreamContext{
				SessionID:     sessionID,
				ChunkSequence: 0,
				LastActivity:  time.Now(),
				WaitGroup:     &wg,
				Cancel:        cancel,
			}

			s.StreamsMutex.Lock()
			s.activeStreams[sessionID.String()] = streamCtx
			s.StreamsMutex.Unlock()

			defer func() {
				s.StreamsMutex.Lock()
				delete(s.activeStreams, sessionID.String())
				s.StreamsMutex.Unlock()
				cancel()
			}()
		}

		log.Printf("📦 Received chunk #%d from session %s (%d bytes, format: %s)",
			chunk.ChunkSequence, sessionID, len(chunk.AudioData), chunk.AudioFormat)

		// Atualiza contexto do stream
		streamCtx.ChunkSequence = int(chunk.ChunkSequence)
		streamCtx.LastActivity = time.Now()

		wg.Add(1)

		go func(chunk *pb.AudioChunk) {
			defer wg.Done()

			result, err := s.processAudioChunk(stream.Context(), chunk)
			if err != nil {
				log.Printf("Error processing audio chunk: %v", err)
				return
			}

			// Envia resultado de volta ao cliente
			if err := stream.Send(result); err != nil {
				log.Printf("Error sending transcription result: %v", err)
				return
			}
			log.Printf("✅ Sent transcription for chunk #%d: \"%s\"",
				chunk.ChunkSequence, truncate(result.Text, 50))
		}(chunk)

	}
	return nil
}

func (s *Server) processAudioChunk(ctx context.Context, audioChunk *pb.AudioChunk) (*pb.TranscriptionResult, error) {
	// TODO: Implementar processamento do chunk de áudio
	// Por enquanto vamos simular

	time.Sleep(50 * time.Millisecond) // Simula tempo de processamento
	transcriptionOutput := &TranscriptionOutput{
		Language:   "pt",
		Text:       fmt.Sprintf("Transcription of chunk #%d", audioChunk.ChunkSequence),
		Confidence: .97,
		Duration:   float64(len(audioChunk.AudioData)) / float64(audioChunk.SampleRate*audioChunk.Channels*2), // estimativa
	}

	// Salva no banco
	sessionID, err := uuid.Parse(audioChunk.SessionId)
	if err != nil {
		log.Printf("Invalid session ID: %v", err)
		return nil, status.Errorf(codes.InvalidArgument, "invalid session ID: %v", err)
	}

	trans := models.Transcription{
		SessionID:       sessionID,
		ChunkSequence:   int(audioChunk.ChunkSequence),
		Language:        transcriptionOutput.Language,
		Text:            transcriptionOutput.Text,
		Confidence:      transcriptionOutput.Confidence,
		DurationSeconds: transcriptionOutput.Duration,
		AudioFormat:     audioChunk.AudioFormat,
		SampleRate:      int(audioChunk.SampleRate),
		Channels:        int(audioChunk.Channels),
		Metadata:        make(map[string]any),
	}

	if err := s.db.SaveTranscription(ctx, &trans); err != nil {
		log.Printf("Error saving transcription: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to save transcription: %v", err)
	}
	log.Printf("Transcription saved: %d bytes", len(trans.Text))

	// Retorna resultado
	return &pb.TranscriptionResult{
		SessionId:       audioChunk.SessionId,
		ChunkSequence:   audioChunk.ChunkSequence,
		Language:        transcriptionOutput.Language,
		Text:            transcriptionOutput.Text,
		Confidence:      transcriptionOutput.Confidence,
		DurationSeconds: transcriptionOutput.Duration,
		CreatedAt:       time.Now().Format(time.RFC3339),
		IsFinal:         true,
	}, nil

}

// GetSessionTranscriptions busca transcrições de uma sessão
func (s *Server) GetSessionTranscriptions(ctx context.Context, req *pb.GetSessionTranscriptionsRequest) (*pb.GetSessionTranscriptionsResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	offset := int(req.Offset)
	if offset < 0 {
		offset = 0
	}

	transcriptions, err := s.db.GetSessionTranscriptions(ctx, req.SessionId, limit, offset)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get transcriptions: %v", err)
	}

	// Converte para proto
	pbTranscriptions := make([]*pb.Transcription, len(transcriptions))
	for i, t := range transcriptions {
		pbTranscriptions[i] = &pb.Transcription{
			Id:              t.ID,
			SessionId:       t.SessionID.String(),
			ChunkSequence:   int32(t.ChunkSequence),
			Language:        t.Language,
			Text:            t.Text,
			Confidence:      t.Confidence,
			DurationSeconds: t.DurationSeconds,
			AudioFormat:     t.AudioFormat,
			SampleRate:      int32(t.SampleRate),
			Channels:        int32(t.Channels),
			CreatedAt:       t.CreatedAt.Format(time.RFC3339),
		}
	}

	return &pb.GetSessionTranscriptionsResponse{
		Transcriptions: pbTranscriptions,
		TotalCount:     int32(len(transcriptions)),
	}, nil
}

// SearchTranscriptions busca por texto
func (s *Server) SearchTranscriptions(ctx context.Context, req *pb.SearchTranscriptionsRequest) (*pb.SearchTranscriptionsResponse, error) {
	limit := int(req.Limit)
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	transcriptions, err := s.db.SearchTranscriptions(ctx, req.Query, limit)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to search: %v", err)
	}

	// Converte para proto
	pbTranscriptions := make([]*pb.Transcription, len(transcriptions))
	for i, t := range transcriptions {
		pbTranscriptions[i] = &pb.Transcription{
			Id:              t.ID,
			SessionId:       t.SessionID.String(),
			ChunkSequence:   int32(t.ChunkSequence),
			Language:        t.Language,
			Text:            t.Text,
			Confidence:      t.Confidence,
			DurationSeconds: t.DurationSeconds,
			AudioFormat:     t.AudioFormat,
			SampleRate:      int32(t.SampleRate),
			Channels:        int32(t.Channels),
			CreatedAt:       t.CreatedAt.Format(time.RFC3339),
		}
	}

	return &pb.SearchTranscriptionsResponse{
		Transcriptions: pbTranscriptions,
		TotalCount:     int32(len(transcriptions)),
	}, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
