# scripts/setup_db.sh
#!/bin/bash

set -e  # Para em caso de erro

echo "🗄️  Setting up PostgreSQL database..."
echo ""

# Variáveis (pode sobrescrever via environment)
DB_USER=${DB_USER:-audio_user}
DB_PASSWORD=${DB_PASSWORD:-secret_password}
DB_NAME=${DB_NAME:-audio_transcriber}

# Cores para output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Verifica se PostgreSQL está instalado
if ! command -v psql &> /dev/null; then
    echo -e "${RED}❌ PostgreSQL não está instalado!${NC}"
    echo "Instale com: sudo apt install postgresql postgresql-contrib"
    exit 1
fi

# Verifica se PostgreSQL está rodando
if ! sudo systemctl is-active --quiet postgresql; then
    echo -e "${YELLOW}⚠️  PostgreSQL não está rodando. Iniciando...${NC}"
    sudo systemctl start postgresql
fi

echo -e "${GREEN}✓${NC} PostgreSQL está rodando"
echo ""

# Cria usuário e database
echo "📝 Creating user and database..."
sudo -u postgres psql <<EOF
-- Cria usuário se não existir
DO \$\$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_user WHERE usename = '$DB_USER') THEN
        CREATE USER $DB_USER WITH PASSWORD '$DB_PASSWORD';
        RAISE NOTICE 'User $DB_USER created';
    ELSE
        RAISE NOTICE 'User $DB_USER already exists';
    END IF;
END
\$\$;

-- Cria database se não existir
SELECT 'CREATE DATABASE $DB_NAME OWNER $DB_USER'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$DB_NAME')\gexec

-- Concede privilégios
GRANT ALL PRIVILEGES ON DATABASE $DB_NAME TO $DB_USER;

-- Conecta ao database para configurar extensões
\c $DB_NAME

-- Garante que o usuário tenha permissões no schema public
GRANT ALL ON SCHEMA public TO $DB_USER;
GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public TO $DB_USER;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO $DB_USER;

EOF

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓${NC} Database and user created"
else
    echo -e "${RED}❌ Failed to create database${NC}"
    exit 1
fi

echo ""

# Aplica schema
if [ -f "scripts/schema.sql" ]; then
    echo "📝 Applying schema..."
    PGPASSWORD=$DB_PASSWORD psql -U $DB_USER -d $DB_NAME -f scripts/schema.sql
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓${NC} Schema applied successfully"
    else
        echo -e "${RED}❌ Failed to apply schema${NC}"
        exit 1
    fi
else
    echo -e "${YELLOW}⚠️  Schema file not found at scripts/schema.sql${NC}"
    exit 1
fi

echo ""
echo -e "${GREEN}🎉 Database setup complete!${NC}"
echo ""
echo "Connection details:"
echo "  Host:     localhost"
echo "  Port:     5432"
echo "  Database: $DB_NAME"
echo "  User:     $DB_USER"
echo "  Password: $DB_PASSWORD"
echo ""
echo "To connect with psql:"
echo "  PGPASSWORD=$DB_PASSWORD psql -U $DB_USER -d $DB_NAME"