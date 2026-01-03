CREATE EXTENSION IF NOT EXISTS "pg_trgm";  -- Para busca fuzzy  

-- Tabela principal de transcrições
CREATE TABLE transcriptions (
    id BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL,  -- UUIDv7
    chunk_sequence INT NOT NULL,  -- Ordem dos chunks na stream
    audio_hash VARCHAR(64),  -- Hash do chunk para deduplicação
    language VARCHAR(10) NOT NULL,
    text TEXT NOT NULL,
    confidence FLOAT,
    duration_seconds FLOAT NOT NULL,
    audio_format VARCHAR(20),
    sample_rate INT,
    channels INT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    
    -- Índices inline para performance
    CONSTRAINT unique_session_chunk UNIQUE(session_id, chunk_sequence)
);

-- Tabela de sessões
CREATE TABLE sessions (
    id UUID PRIMARY KEY,  -- UUIDv7
    started_at TIMESTAMPTZ DEFAULT NOW(),
    last_activity_at TIMESTAMPTZ DEFAULT NOW(),
    status VARCHAR(20) DEFAULT 'active',  -- active, paused, completed
    total_chunks INT DEFAULT 0,
    total_duration_seconds FLOAT DEFAULT 0,
    primary_language VARCHAR(10),
    metadata JSONB DEFAULT '{}',
    
    CHECK (status IN ('active', 'paused', 'completed', 'error'))
);

-- Índices otimizados
CREATE INDEX idx_transcriptions_session ON transcriptions(session_id, chunk_sequence);
CREATE INDEX idx_transcriptions_created ON transcriptions(created_at DESC);
CREATE INDEX idx_transcriptions_language ON transcriptions(language);

-- Índice para full-text search (multi-idioma)
CREATE INDEX idx_transcriptions_text_search ON transcriptions USING GIN(to_tsvector('simple', text));

-- Índice para busca em metadados JSONB
CREATE INDEX idx_transcriptions_metadata ON transcriptions USING GIN(metadata);

-- Índice para sessões ativas
CREATE INDEX idx_sessions_active ON sessions(status, last_activity_at) WHERE status = 'active';

-- Índice B-tree para UUIDv7 (otimizado por ser ordenável)
CREATE INDEX idx_sessions_id_created ON sessions(id);

-- Função para atualizar última atividade da sessão
CREATE OR REPLACE FUNCTION update_session_activity()
RETURNS TRIGGER AS $$
BEGIN
    UPDATE sessions 
    SET 
        last_activity_at = NOW(),
        total_chunks = total_chunks + 1,
        total_duration_seconds = total_duration_seconds + NEW.duration_seconds
    WHERE id = NEW.session_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger para atualizar automaticamente
CREATE TRIGGER trigger_update_session_activity
    AFTER INSERT ON transcriptions
    FOR EACH ROW
    EXECUTE FUNCTION update_session_activity();

-- View para estatísticas rápidas
CREATE VIEW session_stats AS
SELECT 
    s.id,
    s.started_at,
    s.last_activity_at,
    s.status,
    s.total_chunks,
    s.total_duration_seconds,
    COUNT(DISTINCT t.language) as languages_detected,
    s.primary_language,
    EXTRACT(EPOCH FROM (s.last_activity_at - s.started_at)) as session_duration_seconds
FROM sessions s
LEFT JOIN transcriptions t ON t.session_id = s.id
GROUP BY s.id;