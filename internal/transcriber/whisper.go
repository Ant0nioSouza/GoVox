package transcriber

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	whisper "github.com/ggerganov/whisper.cpp/bindings/go/pkg/whisper"
)

type WhisperTranscriber struct {
	model     whisper.Model
	modelPath string
	mu        sync.Mutex
}

type TranscriptionResult struct {
	Language   string
	Text       string
	Confidence float64
	Duration   float64
	Segments   []Segment
}

// Representa um segmento de transcrição
type Segment struct {
	StartTime  time.Duration
	EndTime    time.Duration
	Text       string
	Confidence float64
}

// Adicione esta função nova:
func saveDebugAudio(samples []float32, filename string) error {
	// Converte float32 de volta para PCM 16-bit
	pcmData := make([]byte, len(samples)*2)
	for i, s := range samples {
		// Clamp para evitar overflow
		if s > 1.0 {
			s = 1.0
		}
		if s < -1.0 {
			s = -1.0
		}

		// Converte para int16
		sample := int16(s * 32767.0)
		pcmData[i*2] = byte(sample & 0xFF)
		pcmData[i*2+1] = byte((sample >> 8) & 0xFF)
	}

	// Cria arquivo WAV completo com header
	wavData := createWavHeader(len(pcmData), 16000, 1)
	wavData = append(wavData, pcmData...)

	return os.WriteFile(filename, wavData, 0644)
}

// Adicione esta função auxiliar:
func createWavHeader(dataSize, sampleRate, channels int) []byte {
	header := make([]byte, 44)

	// ChunkID "RIFF"
	copy(header[0:4], []byte("RIFF"))

	// ChunkSize
	binary.LittleEndian.PutUint32(header[4:8], uint32(36+dataSize))

	// Format "WAVE"
	copy(header[8:12], []byte("WAVE"))

	// Subchunk1ID "fmt "
	copy(header[12:16], []byte("fmt "))

	// Subchunk1Size (16 for PCM)
	binary.LittleEndian.PutUint32(header[16:20], 16)

	// AudioFormat (1 for PCM)
	binary.LittleEndian.PutUint16(header[20:22], 1)

	// NumChannels
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))

	// SampleRate
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))

	// ByteRate
	byteRate := sampleRate * channels * 2 // 16 bits = 2 bytes
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))

	// BlockAlign
	binary.LittleEndian.PutUint16(header[32:34], uint16(channels*2))

	// BitsPerSample
	binary.LittleEndian.PutUint16(header[34:36], 16)

	// Subchunk2ID "data"
	copy(header[36:40], []byte("data"))

	// Subchunk2Size
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataSize))

	return header
}

func NewWhisperTranscriber(modelPath string) (*WhisperTranscriber, error) {
	log.Printf("Loading Whisper model from: %s", modelPath)

	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("Model file does not exist at path: %s", modelPath)
	}

	model, err := whisper.New(modelPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load Whisper model: %v", err)
	}

	log.Println("Whisper model loaded successfully")

	return &WhisperTranscriber{
		model:     model,
		modelPath: modelPath,
	}, nil
}

func (wt *WhisperTranscriber) Transcribe(ctx context.Context, audioData []byte, format string, sampleRate, channels int) (*TranscriptionResult, error) {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	log.Printf("Starting transcription (format: %s, sampleRate: %d, channels: %d)", format, sampleRate, channels)

	startTime := time.Now()

	samples, err := wt.convertToSamples(audioData, sampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("failed to convert audio data: %v", err)
	}

	debugFile := fmt.Sprintf("/tmp/whisper_debug_%d.wav", time.Now().Unix())
	if err := saveDebugAudio(samples, debugFile); err != nil {
		log.Printf("⚠️  Failed to save debug audio: %v", err)
	} else {
		log.Printf("💾 Debug audio saved: %s", debugFile)
		log.Printf("   Play with: aplay %s", debugFile)
	}

	context, err := wt.model.NewContext()
	if err != nil {
		return nil, fmt.Errorf("failed to create Whisper context: %v", err)
	}

	context.SetLanguage("en")
	context.SetTranslate(false)
	context.SetMaxTokensPerSegment(0)

	// Processa o áudio
	if err := context.Process(samples, nil, nil, nil); err != nil {
		return nil, fmt.Errorf("failed to process audio: %w", err)
	}

	// Extrai resultados
	segments := make([]Segment, 0)
	fullText := ""

	for {
		segment, err := context.NextSegment()
		if err != nil {
			// io.EOF signals end of segments
			break
		}

		segmentData := Segment{
			StartTime:  segment.Start,
			EndTime:    segment.End,
			Text:       segment.Text,
			Confidence: 0.0, // Whisper.cpp não fornece confidence por segmento facilmente
		}

		segments = append(segments, segmentData)
		fullText += segment.Text + " "
	}

	// Detecta o idioma usado
	language := context.Language()

	duration := time.Since(startTime)
	audioDuration := float64(len(samples)) / float64(sampleRate)

	log.Printf("🎤 Transcription completed in %.2fs (audio: %.2fs, %.1fx realtime)",
		duration.Seconds(),
		audioDuration,
		audioDuration/duration.Seconds(),
	)

	return &TranscriptionResult{
		Language:   language,
		Text:       fullText,
		Confidence: wt.estimateConfidence(segments),
		Duration:   audioDuration,
		Segments:   segments,
	}, nil

}

// convertToSamples converte bytes de áudio para []float32
// Whisper espera áudio mono 16kHz em float32
func (w *WhisperTranscriber) convertToSamples(audioData []byte, sampleRate, channels int) ([]float32, error) {
	// Se não for 16kHz mono, precisamos converter
	// Por enquanto, assumimos que o áudio já está em formato correto (16-bit PCM)

	if len(audioData)%2 != 0 {
		return nil, fmt.Errorf("invalid audio data length (must be even for 16-bit samples)")
	}

	numSamples := len(audioData) / 2
	samples := make([]float32, numSamples)

	// Converte de 16-bit PCM para float32 [-1.0, 1.0]
	for i := 0; i < numSamples; i++ {
		// Little-endian 16-bit signed integer
		sample := int16(audioData[i*2]) | int16(audioData[i*2+1])<<8
		samples[i] = float32(sample) / 32768.0
	}

	// Se for stereo, converte para mono (média dos canais)
	if channels == 2 {
		monoSamples := make([]float32, numSamples/2)
		for i := 0; i < len(monoSamples); i++ {
			monoSamples[i] = (samples[i*2] + samples[i*2+1]) / 2.0
		}
		samples = monoSamples
	}

	// Se não for 16kHz, precisamos resamplear
	// Por enquanto, vamos apenas avisar
	if sampleRate != 16000 {
		log.Printf("⚠️  Audio is %dHz, Whisper expects 16kHz. Quality may be affected.", sampleRate)
		// TODO: Implementar resampling ou usar FFmpeg
	}

	return samples, nil
}

// estimateConfidence estima a confiança média baseado nos segmentos
func (w *WhisperTranscriber) estimateConfidence(segments []Segment) float64 {
	if len(segments) == 0 {
		return 0.0
	}

	// Por enquanto, retornamos um valor fixo alto
	// O Whisper.cpp não expõe facilmente confidence scores
	// Podemos implementar heurísticas mais sofisticadas depois
	return 0.92
}

// Close fecha e libera recursos
func (w *WhisperTranscriber) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.model != nil {
		w.model.Close()
		log.Println("✅ Whisper model closed")
	}

	return nil
}
