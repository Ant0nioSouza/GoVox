package tests

import (
	"testing"
	"time"

	"github.com/Ant0nioSouza/GoVox/pkg/utils"
)

func TestNewUUIDv7(t *testing.T) {
	// Gera dois UUIDs sequenciais
	uuid1 := utils.NewUUIDv7()
	time.Sleep(2 * time.Millisecond) // Pequeno delay
	uuid2 := utils.NewUUIDv7()

	// Verifica se são válidos
	if !utils.IsUUIDv7(uuid1) {
		t.Error("uuid1 não é UUIDv7")
	}
	if !utils.IsUUIDv7(uuid2) {
		t.Error("uuid2 não é UUIDv7")
	}

	// Verifica se são diferentes
	if uuid1 == uuid2 {
		t.Error("UUIDs devem ser únicos")
	}

	// Verifica ordenação (uuid2 deve ser maior que uuid1)
	if uuid1.String() >= uuid2.String() {
		t.Error("UUIDs não estão em ordem cronológica")
	}

	t.Logf("UUID1: %s", uuid1)
	t.Logf("UUID2: %s", uuid2)
}

func TestExtractTimestamp(t *testing.T) {
	before := time.Now().UnixMilli()
	uuid := utils.NewUUIDv7()
	after := time.Now().UnixMilli()

	extractedMs := utils.ExtractTimestampFromUUIDv7(uuid).UnixMilli()
	if extractedMs < before || extractedMs > after {
		t.Fatalf("timestamp fora do intervalo")
	}

	t.Logf("Timestamp extraído: %v", extractedMs)
}

func BenchmarkNewUUIDv7(b *testing.B) {
	for i := 0; i < b.N; i++ {
		utils.NewUUIDv7()
	}
}
