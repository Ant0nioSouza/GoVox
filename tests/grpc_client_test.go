// tests/grpc_client_test.go
package tests

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/Ant0nioSouza/GoVox/api/proto"
)

const (
	serverAddress = "localhost:50051"
	testTimeout   = 30 * time.Second
)

// TestCreateSession testa criação de sessão
func TestCreateSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Conecta ao servidor
	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTranscriptionServiceClient(conn)

	// Cria sessão
	req := &pb.CreateSessionRequest{
		Metadata: map[string]string{
			"client": "go_test",
			"test":   "true",
		},
	}

	resp, err := client.CreateSession(ctx, req)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	t.Logf("✅ Session created: %s", resp.SessionId)
	t.Logf("   Created at: %s", resp.CreatedAt)

	if resp.SessionId == "" {
		t.Error("Session ID should not be empty")
	}
}

// TestStreamAudio testa streaming de áudio
func TestStreamAudio(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Conecta ao servidor
	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTranscriptionServiceClient(conn)

	// 1. Cria uma sessão
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		Metadata: map[string]string{"test": "stream_audio"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	sessionID := sessionResp.SessionId
	t.Logf("📝 Session created: %s", sessionID)

	// 2. Inicia stream de áudio
	stream, err := client.StreamAudio(ctx)
	if err != nil {
		t.Fatalf("Failed to start stream: %v", err)
	}

	// 3. Envia chunks em uma goroutine
	numChunks := 5
	errChan := make(chan error, 1)

	go func() {
		for i := 1; i <= numChunks; i++ {
			// Gera dados de áudio simulados (1 segundo de áudio mono 16kHz)
			audioData := generateMockAudio(16000)

			chunk := &pb.AudioChunk{
				SessionId:     sessionID,
				ChunkSequence: int32(i),
				AudioData:     audioData,
				AudioFormat:   "wav",
				SampleRate:    16000,
				Channels:      1,
			}

			t.Logf("📤 Sending chunk #%d (%d bytes)", i, len(audioData))

			if err := stream.Send(chunk); err != nil {
				errChan <- fmt.Errorf("failed to send chunk #%d: %w", i, err)
				return
			}

			// Simula delay entre chunks
			time.Sleep(200 * time.Millisecond)
		}

		// Fecha o envio
		if err := stream.CloseSend(); err != nil {
			errChan <- fmt.Errorf("failed to close send: %w", err)
			return
		}

		errChan <- nil
	}()

	// 4. Recebe respostas
	receivedCount := 0
	for {
		result, err := stream.Recv()
		if err != nil {
			// Verifica se acabou normalmente
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("Failed to receive: %v", err)
		}

		receivedCount++
		t.Logf("✅ Received transcription for chunk #%d:", result.ChunkSequence)
		t.Logf("   Language: %s", result.Language)
		t.Logf("   Text: \"%s\"", result.Text)
		t.Logf("   Confidence: %.2f%%", result.Confidence*100)
		t.Logf("   Duration: %.2fs", result.DurationSeconds)
	}

	// Verifica erro do envio
	if err := <-errChan; err != nil {
		t.Fatalf("Send error: %v", err)
	}

	// Valida que recebemos todas as transcrições
	if receivedCount != numChunks {
		t.Errorf("Expected %d results, got %d", numChunks, receivedCount)
	}

	t.Logf("✅ Successfully processed %d chunks", receivedCount)
}

// TestEndSession testa finalização de sessão
func TestEndSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTranscriptionServiceClient(conn)

	// Cria uma sessão
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		Metadata: map[string]string{"test": "end_session"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	sessionID := sessionResp.SessionId

	// Envia alguns chunks
	stream, err := client.StreamAudio(ctx)
	if err != nil {
		t.Fatalf("Failed to start stream: %v", err)
	}

	for i := 1; i <= 3; i++ {
		chunk := &pb.AudioChunk{
			SessionId:     sessionID,
			ChunkSequence: int32(i),
			AudioData:     generateMockAudio(16000),
			AudioFormat:   "wav",
			SampleRate:    16000,
			Channels:      1,
		}
		stream.Send(chunk)
	}
	stream.CloseSend()

	// Consome as respostas
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
	}

	// Finaliza a sessão
	endReq := &pb.EndSessionRequest{
		SessionId: sessionID,
		Status:    "completed",
	}

	endResp, err := client.EndSession(ctx, endReq)
	if err != nil {
		t.Fatalf("Failed to end session: %v", err)
	}

	t.Logf("✅ Session ended successfully:")
	t.Logf("   Total chunks: %d", endResp.TotalChunks)
	t.Logf("   Total duration: %.2fs", endResp.TotalDurationSeconds)
	t.Logf("   Primary language: %s", endResp.PrimaryLanguage)

	if !endResp.Success {
		t.Error("Expected success=true")
	}

	if endResp.TotalChunks != 3 {
		t.Errorf("Expected 3 chunks, got %d", endResp.TotalChunks)
	}
}

// TestGetSessionTranscriptions testa busca de transcrições
func TestGetSessionTranscriptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTranscriptionServiceClient(conn)

	// Cria sessão e envia chunks
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		Metadata: map[string]string{"test": "get_transcriptions"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	sessionID := sessionResp.SessionId

	// Envia chunks
	stream, err := client.StreamAudio(ctx)
	if err != nil {
		t.Fatalf("Failed to start stream: %v", err)
	}

	numChunks := 3
	for i := 1; i <= numChunks; i++ {
		chunk := &pb.AudioChunk{
			SessionId:     sessionID,
			ChunkSequence: int32(i),
			AudioData:     generateMockAudio(16000),
			AudioFormat:   "wav",
			SampleRate:    16000,
			Channels:      1,
		}
		stream.Send(chunk)
	}
	stream.CloseSend()

	// Consome respostas
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
	}

	// Busca as transcrições
	getReq := &pb.GetSessionTranscriptionsRequest{
		SessionId: sessionID,
		Limit:     100,
		Offset:    0,
	}

	getResp, err := client.GetSessionTranscriptions(ctx, getReq)
	if err != nil {
		t.Fatalf("Failed to get transcriptions: %v", err)
	}

	t.Logf("✅ Found %d transcriptions:", getResp.TotalCount)

	for _, trans := range getResp.Transcriptions {
		t.Logf("   Chunk #%d [%s]: \"%s\" (%.2f%% confidence)",
			trans.ChunkSequence,
			trans.Language,
			trans.Text,
			trans.Confidence*100,
		)
	}

	if int(getResp.TotalCount) != numChunks {
		t.Errorf("Expected %d transcriptions, got %d", numChunks, getResp.TotalCount)
	}
}

// TestSearchTranscriptions testa busca por texto
func TestSearchTranscriptions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTranscriptionServiceClient(conn)

	// Busca por "chunk" (deve ter várias entradas dos testes anteriores)
	searchReq := &pb.SearchTranscriptionsRequest{
		Query: "chunk",
		Limit: 10,
	}

	searchResp, err := client.SearchTranscriptions(ctx, searchReq)
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}

	t.Logf("✅ Found %d results for 'chunk':", searchResp.TotalCount)

	for _, trans := range searchResp.Transcriptions {
		t.Logf("   Session: %s | Chunk #%d: \"%s\"",
			trans.SessionId[:8]+"...",
			trans.ChunkSequence,
			trans.Text,
		)
	}

	if searchResp.TotalCount == 0 {
		t.Error("Expected at least some results")
	}
}

// TestFullWorkflow testa o fluxo completo
func TestFullWorkflow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTranscriptionServiceClient(conn)

	t.Log("🧪 Testing full workflow...")

	// 1. Criar sessão
	t.Log("\n1️⃣  Creating session...")
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		Metadata: map[string]string{
			"test":        "full_workflow",
			"description": "Complete end-to-end test",
		},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	sessionID := sessionResp.SessionId
	t.Logf("   ✅ Session: %s", sessionID)

	// 2. Stream de áudio
	t.Log("\n2️⃣  Streaming audio...")
	stream, err := client.StreamAudio(ctx)
	if err != nil {
		t.Fatalf("Failed to start stream: %v", err)
	}

	numChunks := 4
	go func() {
		for i := 1; i <= numChunks; i++ {
			chunk := &pb.AudioChunk{
				SessionId:     sessionID,
				ChunkSequence: int32(i),
				AudioData:     generateMockAudio(16000),
				AudioFormat:   "wav",
				SampleRate:    16000,
				Channels:      1,
			}
			stream.Send(chunk)
			time.Sleep(100 * time.Millisecond)
		}
		stream.CloseSend()
	}()

	receivedCount := 0
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
		receivedCount++
	}
	t.Logf("   ✅ Processed %d chunks", receivedCount)

	// 3. Buscar transcrições
	t.Log("\n3️⃣  Retrieving transcriptions...")
	getResp, err := client.GetSessionTranscriptions(ctx, &pb.GetSessionTranscriptionsRequest{
		SessionId: sessionID,
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("Failed to get transcriptions: %v", err)
	}
	t.Logf("   ✅ Retrieved %d transcriptions", getResp.TotalCount)

	// 4. Buscar por texto
	t.Log("\n4️⃣  Searching transcriptions...")
	searchResp, err := client.SearchTranscriptions(ctx, &pb.SearchTranscriptionsRequest{
		Query: "chunk",
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("Failed to search: %v", err)
	}
	t.Logf("   ✅ Found %d search results", searchResp.TotalCount)

	// 5. Finalizar sessão
	t.Log("\n5️⃣  Ending session...")
	endResp, err := client.EndSession(ctx, &pb.EndSessionRequest{
		SessionId: sessionID,
		Status:    "completed",
	})
	if err != nil {
		t.Fatalf("Failed to end session: %v", err)
	}
	t.Logf("   ✅ Session ended: %d chunks, %.2fs total, language: %s",
		endResp.TotalChunks,
		endResp.TotalDurationSeconds,
		endResp.PrimaryLanguage,
	)

	t.Log("\n✅ Full workflow completed successfully!")
}

// Funções auxiliares

// generateMockAudio gera dados de áudio simulados
func generateMockAudio(samples int) []byte {
	data := make([]byte, samples*2) // 16-bit audio = 2 bytes per sample
	rand.Read(data)
	return data
}

// init configura o logger para os testes
func init() {
	log.SetFlags(log.Ltime | log.Lmicroseconds)
}
