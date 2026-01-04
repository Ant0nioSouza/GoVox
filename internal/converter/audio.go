package converter

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AudioConverter converte diversos formatos de áudio para PCM 16kHz mono
type AudioConverter struct{}

// NewAudioConverter cria uma nova instância do conversor
func NewAudioConverter() *AudioConverter {
	return &AudioConverter{}
}

// ConvertToPCM converte áudio de qualquer formato para PCM 16kHz mono 16-bit
func (c *AudioConverter) ConvertToPCM(inputData []byte, inputFormat string) ([]byte, error) {
	// Se já é WAV PCM 16kHz mono, pode retornar direto (otimização)
	if inputFormat == "pcm" || inputFormat == "wav" {
		// Verifica se está no formato correto
		if c.isPCM16kHzMono(inputData) {
			return inputData, nil
		}
	}

	// Cria arquivo temporário para input
	tmpDir := os.TempDir()
	inputPath := filepath.Join(tmpDir, fmt.Sprintf("input_%d.%s", os.Getpid(), inputFormat))
	outputPath := filepath.Join(tmpDir, fmt.Sprintf("output_%d.wav", os.Getpid()))

	defer os.Remove(inputPath)
	defer os.Remove(outputPath)

	// Escreve dados de input
	if err := os.WriteFile(inputPath, inputData, 0644); err != nil {
		return nil, fmt.Errorf("failed to write temp input file: %w", err)
	}

	// Executa FFmpeg para converter
	cmd := exec.Command("ffmpeg",
		"-i", inputPath, // Input file
		"-ar", "16000", // Sample rate 16kHz
		"-ac", "1", // Mono (1 channel)
		"-f", "s16le", // Format: signed 16-bit little-endian PCM
		"-acodec", "pcm_s16le", // Codec: PCM 16-bit
		"-y", // Overwrite output
		outputPath,
	)

	// Captura stderr para debug
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg conversion failed: %w\nStderr: %s", err, stderr.String())
	}

	// Lê o arquivo convertido
	pcmData, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read converted audio: %w", err)
	}

	return pcmData, nil
}

// isPCM16kHzMono verifica se o áudio já está no formato correto
// Implementação simplificada - pode ser melhorada
func (c *AudioConverter) isPCM16kHzMono(data []byte) bool {
	// Verifica header WAV
	if len(data) < 44 {
		return false
	}

	// Checa assinatura RIFF
	if string(data[0:4]) != "RIFF" {
		return false
	}

	// Checa formato WAVE
	if string(data[8:12]) != "WAVE" {
		return false
	}

	// Extrai sample rate (bytes 24-27, little-endian)
	sampleRate := uint32(data[24]) | uint32(data[25])<<8 | uint32(data[26])<<16 | uint32(data[27])<<24

	// Extrai número de canais (bytes 22-23, little-endian)
	channels := uint16(data[22]) | uint16(data[23])<<8

	// Extrai bits por sample (bytes 34-35, little-endian)
	bitsPerSample := uint16(data[34]) | uint16(data[35])<<8

	return sampleRate == 16000 && channels == 1 && bitsPerSample == 16
}

// GetAudioInfo retorna informações sobre o áudio usando FFprobe
func (c *AudioConverter) GetAudioInfo(data []byte, format string) (sampleRate, channels, duration int, err error) {
	// Cria arquivo temporário
	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("probe_%d.%s", os.Getpid(), format))
	defer os.Remove(tmpPath)

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to write temp file: %w", err)
	}

	// Executa ffprobe
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-show_entries", "stream=sample_rate,channels,duration",
		"-of", "default=noprint_wrappers=1",
		tmpPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	// Parse output (simplificado)
	fmt.Sscanf(string(output), "sample_rate=%d\nchannels=%d\nduration=%d", &sampleRate, &channels, &duration)

	return sampleRate, channels, duration, nil
}
