// tests/real_audio_test.go
package tests

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/Ant0nioSouza/GoVox/api/proto"
)

const (
	chunkSize = 32000 * 10 // 1 segundo de áudio 16kHz mono (16000 samples * 2 bytes)
)

func convertAudioToPCM(inputFile string) ([]byte, error) {
	// Cria arquivo temporário para output
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("converted_%d.raw", time.Now().UnixNano()))
	defer os.Remove(tmpFile)

	// Usa FFmpeg para converter para PCM raw
	cmd := exec.Command("ffmpeg",
		"-i", inputFile, // Input (qualquer formato)
		"-f", "s16le", // Format: PCM 16-bit little-endian
		"-acodec", "pcm_s16le", // Codec: PCM
		"-ar", "16000", // Sample rate: 16kHz
		"-ac", "1", // Channels: mono
		"-y", // Overwrite
		tmpFile,
	)

	// Executa conversão
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w\nOutput: %s", err, string(output))
	}

	// Lê o PCM convertido
	pcmData, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read converted file: %w", err)
	}

	return pcmData, nil
}

// TestRealAudioFile testa com um arquivo de áudio real
func TestRealAudioFile(t *testing.T) {
	audioFile := "../testdata/audio/testdata2.mp3" // Pode ser .mp3, .wav, .ogg, etc

	if _, err := os.Stat(audioFile); os.IsNotExist(err) {
		t.Skipf("Audio file not found: %s", audioFile)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTranscriptionServiceClient(conn)

	// 1. Cria sessão
	t.Log("📝 Creating session for real audio test...")
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		Metadata: map[string]string{
			"test": "real_audio",
			"file": audioFile,
		},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	sessionID := sessionResp.SessionId
	t.Logf("✅ Session created: %s", sessionID)

	// 2. NOVO: Converte o áudio para PCM
	t.Logf("🔄 Converting audio to PCM: %s", audioFile)
	pcmData, err := convertAudioToPCM(audioFile)
	if err != nil {
		t.Fatalf("Failed to convert audio: %v", err)
	}
	t.Logf("✅ Converted to PCM: %d bytes", len(pcmData))

	// 3. Inicia stream
	t.Log("🎙️  Starting audio stream...")
	stream, err := client.StreamAudio(ctx)
	if err != nil {
		t.Fatalf("Failed to start stream: %v", err)
	}

	// 4. Envia PCM em chunks
	chunkNum := 0
	totalChunks := (len(pcmData) + chunkSize - 1) / chunkSize

	go func() {
		for offset := 0; offset < len(pcmData); offset += chunkSize {
			chunkNum++
			end := offset + chunkSize
			if end > len(pcmData) {
				end = len(pcmData)
			}

			chunk := &pb.AudioChunk{
				SessionId:     sessionID,
				ChunkSequence: int32(chunkNum),
				AudioData:     pcmData[offset:end], // Agora é PCM puro
				AudioFormat:   "pcm",               // Formato correto
				SampleRate:    16000,
				Channels:      1,
			}

			t.Logf("📤 Sending chunk %d/%d (%d bytes)", chunkNum, totalChunks, len(chunk.AudioData))

			if err := stream.Send(chunk); err != nil {
				t.Errorf("Failed to send chunk: %v", err)
				return
			}

			time.Sleep(100 * time.Millisecond)
		}

		stream.CloseSend()
		t.Logf("✅ Sent all %d chunks", chunkNum)
	}()

	// 5. Recebe transcrições
	t.Log("\n📥 Receiving transcriptions...\n")
	var transcriptions []string

	for {
		result, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to receive: %v", err)
		}

		t.Logf("✅ Chunk #%d transcribed:", result.ChunkSequence)
		t.Logf("   Language: %s", result.Language)
		t.Logf("   Text: \"%s\"", result.Text)
		t.Logf("   Confidence: %.2f%%", result.Confidence*100)
		t.Logf("   Duration: %.2fs", result.DurationSeconds)
		t.Log("")

		transcriptions = append(transcriptions, result.Text)
	}

	// 6. Mostra resultado completo
	t.Log("============================================================================")
	t.Log("📝 FULL TRANSCRIPTION:")
	t.Log("============================================================================")
	for i, text := range transcriptions {
		t.Logf("Chunk %d: %s", i+1, text)
	}
	t.Log("============================================================================")

	// 7. Finaliza sessão
	t.Log("\n🏁 Ending session...")
	endResp, err := client.EndSession(ctx, &pb.EndSessionRequest{
		SessionId: sessionID,
		Status:    "completed",
	})
	if err != nil {
		t.Fatalf("Failed to end session: %v", err)
	}

	t.Logf("✅ Session ended:")
	t.Logf("   Total chunks: %d", endResp.TotalChunks)
	t.Logf("   Total duration: %.2fs", endResp.TotalDurationSeconds)
	t.Logf("   Primary language: %s", endResp.PrimaryLanguage)

	// Valida que recebemos transcrições
	if len(transcriptions) == 0 {
		t.Error("Expected at least one transcription")
	}
}

// TestStreamMicrophone simula streaming de microfone (chunks pequenos)
func TestStreamMicrophone(t *testing.T) {
	audioFile := "../testdata/audio/test_pt.wav"

	if _, err := os.Stat(audioFile); os.IsNotExist(err) {
		t.Skipf("Audio file not found: %s", audioFile)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn, err := grpc.DialContext(ctx, serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTranscriptionServiceClient(conn)

	// Cria sessão
	sessionResp, err := client.CreateSession(ctx, &pb.CreateSessionRequest{
		Metadata: map[string]string{"test": "microphone_simulation"},
	})
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	sessionID := sessionResp.SessionId
	t.Logf("🎤 Simulating microphone stream for session: %s", sessionID)

	// Lê áudio
	audioData, err := os.ReadFile(audioFile)
	if err != nil {
		t.Fatalf("Failed to read audio: %v", err)
	}

	stream, err := client.StreamAudio(ctx)
	if err != nil {
		t.Fatalf("Failed to start stream: %v", err)
	}

	// Envia em chunks menores (simulando mic real)
	microChunkSize := 8000 // 0.25 segundos
	chunkNum := 0

	go func() {
		for offset := 0; offset < len(audioData); offset += microChunkSize {
			chunkNum++
			end := offset + microChunkSize
			if end > len(audioData) {
				end = len(audioData)
			}

			chunk := &pb.AudioChunk{
				SessionId:     sessionID,
				ChunkSequence: int32(chunkNum),
				AudioData:     audioData[offset:end],
				AudioFormat:   "wav",
				SampleRate:    16000,
				Channels:      1,
			}

			stream.Send(chunk)

			// Simula delay real de captura de mic (250ms)
			time.Sleep(250 * time.Millisecond)
		}
		stream.CloseSend()
	}()

	// Recebe e mostra transcrições em tempo real
	t.Log("🎙️  Receiving real-time transcriptions...\n")
	count := 0
	for {
		result, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Receive error: %v", err)
		}

		count++
		t.Logf("[%02d] %s: \"%s\"", result.ChunkSequence, result.Language, result.Text)
	}

	t.Logf("\n✅ Received %d transcriptions", count)

	// Finaliza
	client.EndSession(ctx, &pb.EndSessionRequest{
		SessionId: sessionID,
		Status:    "completed",
	})
}
