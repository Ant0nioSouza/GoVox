// cmd/server/main.go
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "github.com/Ant0nioSouza/GoVox/api/proto"
	"github.com/Ant0nioSouza/GoVox/internal/database"
	grpcserver "github.com/Ant0nioSouza/GoVox/internal/grpc"
	"github.com/Ant0nioSouza/GoVox/pkg/models"
	"github.com/Ant0nioSouza/GoVox/pkg/utils"
)

func main() {
	fmt.Println("🎙️  Audio Transcriber Server")
	fmt.Println("===========================\n")

	// Carrega .env
	if err := godotenv.Load(); err != nil {
		log.Println("⚠️  No .env file found, using environment variables")
	}

	ctx := context.Background()

	// Configura database
	fmt.Println("📡 Connecting to database...")
	dbConfig := database.Config{
		Host:        getEnv("DB_HOST", "localhost"),
		Port:        getEnvInt("DB_PORT", 5432),
		User:        getEnv("DB_USER", "audio_user"),
		Password:    getEnv("DB_PASSWORD", "secret_password"),
		DBName:      getEnv("DB_NAME", "audio_transcriber"),
		MaxConns:    int32(getEnvInt("DB_MAX_CONNS", 20)),
		MinConns:    int32(getEnvInt("DB_MIN_CONNS", 5)),
		MaxConnLife: parseDuration(getEnv("DB_MAX_CONN_LIFE", "1h")),
		MaxConnIdle: parseDuration(getEnv("DB_MAX_CONN_IDLE", "10m")),
	}

	// Conecta ao banco
	db, err := database.New(ctx, dbConfig)
	if err != nil {
		log.Fatalf("❌ Failed to connect to database: %v", err)
	}
	defer db.Close()

	fmt.Println("✅ Database connected successfully")

	// Mostra stats do pool
	stats := db.GetPoolStats()
	fmt.Printf("📊 Connection pool: %d/%d active, %d idle\n\n",
		stats["acquired_conns"], stats["max_conns"], stats["idle_conns"])

	// TODO: Inicializar o whisper aqui
	var transcriber grpcserver.TranscriberInterface

	fmt.Println("🔧 Setting up gRPC server...")
	grpcServer := grpc.NewServer(
		grpc.MaxRecvMsgSize(50*1024*1024), // 50MB max message size (para chunks grandes)
		grpc.MaxSendMsgSize(50*1024*1024),
	)

	// Registra o serviço
	transcriptionServer := grpcserver.NewServer(db, transcriber)
	pb.RegisterTranscriptionServiceServer(grpcServer, transcriptionServer)

	reflection.Register(grpcServer)

	// Inicia o servidor gRPC
	port := getEnv("SERVER_PORT", "50051")
	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("❌ Failed to listen on port %s: %v", port, err)
	}

	fmt.Printf("✅ gRPC server listening on port %s\n", port)
	fmt.Println("🚀 Server ready!\n")
	fmt.Println("Available endpoints:")
	fmt.Println("  - CreateSession")
	fmt.Println("  - StreamAudio (bidirectional streaming)")
	fmt.Println("  - EndSession")
	fmt.Println("  - GetSessionTranscriptions")
	fmt.Println("  - SearchTranscriptions")
	fmt.Println("\nPress Ctrl+C to stop\n")

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		fmt.Println("\n🛑 Shutting down gracefully...")
		grpcServer.GracefulStop()
		fmt.Println("✅ Server stopped")
		os.Exit(0)
	}()

	if err := grpcServer.Serve(listener); err != nil {
		log.Fatalf("❌ Failed to serve: %v", err)
	}

	// Mantém o servidor rodando
	select {}
}

// testDatabase executa testes básicos no banco
func testDatabase(ctx context.Context, db *database.Database) error {
	fmt.Println("\n  → Creating test session...")

	// 1. Cria uma sessão de teste
	metadata := map[string]any{
		"test":   true,
		"source": "unit_test",
	}

	session, err := db.CreateSession(ctx, metadata)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	fmt.Printf("     ✓ Session created: %s\n", session.ID)
	fmt.Printf("     ✓ Session started at: %s (UTC)\n", session.StartedAt.UTC().Format(time.RFC3339))

	// Verifica se é UUIDv7
	if !utils.IsUUIDv7(session.ID) {
		return fmt.Errorf("session ID is not UUIDv7")
	}
	fmt.Println("     ✓ Session ID is valid UUIDv7")

	// Extrai e mostra timestamp do UUID
	uuidTime := utils.ExtractTimestampFromUUIDv7(session.ID)
	fmt.Printf("     ✓ UUID timestamp: %s\n", uuidTime.Format(time.RFC3339))

	// 2. Salva algumas transcrições de teste
	fmt.Println("\n  → Saving test transcriptions...")

	transcriptions := []*models.Transcription{
		{
			SessionID:       session.ID,
			ChunkSequence:   1,
			AudioHash:       "hash123",
			Language:        "pt",
			Text:            "Olá, este é um teste de transcrição.",
			Confidence:      0.95,
			DurationSeconds: 2.5,
			AudioFormat:     "wav",
			SampleRate:      16000,
			Channels:        1,
			Metadata:        map[string]any{"test": true},
		},
		{
			SessionID:       session.ID,
			ChunkSequence:   2,
			AudioHash:       "hash456",
			Language:        "pt",
			Text:            "Este é o segundo chunk de áudio.",
			Confidence:      0.92,
			DurationSeconds: 2.0,
			AudioFormat:     "wav",
			SampleRate:      16000,
			Channels:        1,
			Metadata:        map[string]any{"test": true},
		},
		{
			SessionID:       session.ID,
			ChunkSequence:   3,
			AudioHash:       "hash789",
			Language:        "en",
			Text:            "This is the third chunk in English.",
			Confidence:      0.98,
			DurationSeconds: 1.8,
			AudioFormat:     "wav",
			SampleRate:      16000,
			Channels:        1,
			Metadata:        map[string]any{"test": true},
		},
	}

	// Testa insert individual
	if err := db.SaveTranscription(ctx, transcriptions[0]); err != nil {
		return fmt.Errorf("failed to save transcription: %w", err)
	}
	fmt.Printf("     ✓ Saved transcription #1 (ID: %d)\n", transcriptions[0].ID)

	// Testa batch insert (mais rápido)
	if err := db.SaveTranscriptionsBatch(ctx, transcriptions[1:]); err != nil {
		return fmt.Errorf("failed to save batch: %w", err)
	}
	fmt.Println("     ✓ Saved transcriptions #2-3 (batch)")

	// 3. Busca as transcrições
	fmt.Println("\n  → Retrieving transcriptions...")

	retrieved, err := db.GetSessionTranscriptions(ctx, session.ID.String(), 10, 0)
	if err != nil {
		return fmt.Errorf("failed to retrieve transcriptions: %w", err)
	}

	fmt.Printf("     ✓ Retrieved %d transcriptions\n", len(retrieved))

	for _, t := range retrieved {
		fmt.Printf("     • Chunk %d [%s]: \"%s\"\n",
			t.ChunkSequence, t.Language, truncate(t.Text, 40))
	}

	// 4. Testa busca full-text
	fmt.Println("\n  → Testing full-text search...")

	searchResults, err := db.SearchTranscriptions(ctx, "chunk", 10)
	if err != nil {
		return fmt.Errorf("failed to search: %w", err)
	}

	fmt.Printf("     ✓ Found %d results for 'chunk'\n", len(searchResults))

	// 5. Verifica a sessão foi atualizada (pelo trigger)
	fmt.Println("\n  → Verifying session updates...")

	updatedSession, err := db.GetSession(ctx, session.ID.String())
	if err != nil {
		return fmt.Errorf("failed to get updated session: %w", err)
	}

	fmt.Printf("     ✓ Total chunks: %d\n", updatedSession.TotalChunks)
	fmt.Printf("     ✓ Total duration: %.2fs\n", updatedSession.TotalDurationSeconds)
	fmt.Printf("     ✓ Last activity: %s\n", updatedSession.LastActivityAt.Format(time.RFC3339))

	if updatedSession.TotalChunks != 3 {
		return fmt.Errorf("expected 3 chunks, got %d", updatedSession.TotalChunks)
	}

	// 6. Atualiza status da sessão
	fmt.Println("\n  → Updating session status...")

	if err := db.UpdateSessionStatus(ctx, session.ID.String(), models.SessionCompleted); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}
	fmt.Println("     ✓ Session marked as completed")

	return nil
}

// Funções auxiliares
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 1 * time.Hour
	}
	return d
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
