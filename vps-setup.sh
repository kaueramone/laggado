#!/bin/bash
# ============================================================
#  LAGGADO Lagger Node — Script de Setup para VPS Linux
#  Testado em: Ubuntu 22.04 / Debian 12
#  Uso: bash vps-setup.sh
# ============================================================
set -e

REGION="${1:-SA}"
CITY="${2:-}"
COUNTRY="${3:-BR}"
WG_IFACE="wg0"
WG_PORT=51820
API_PORT=7735
TUNNEL_SUBNET="10.100.1.0/24"
TUNNEL_GW="10.100.0.1"
INSTALL_DIR="/opt/laggado-lagger"
SERVICE_NAME="laggado-lagger"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[INFO]${NC}  $*"; }
warn()  { echo -e "${YELLOW}[AVISO]${NC} $*"; }
error() { echo -e "${RED}[ERRO]${NC}  $*"; exit 1; }

# ── Verificações ──────────────────────────────────────────────────────────────
[[ $EUID -ne 0 ]] && error "Execute como root: sudo bash vps-setup.sh"

info "LAGGADO Lagger Node — Setup"
echo

# Detecta IP público
PUBLIC_IP=$(curl -s --max-time 5 https://api.ipify.org || \
            curl -s --max-time 5 https://ipv4.icanhazip.com || echo "")
[[ -z "$PUBLIC_IP" ]] && error "Não foi possível detectar o IP público."
info "IP público detectado: $PUBLIC_IP"

# ── Instalar dependências ─────────────────────────────────────────────────────
info "Instalando WireGuard..."
apt-get update -qq
apt-get install -y -qq wireguard curl

# ── Configurar WireGuard ──────────────────────────────────────────────────────
info "Configurando WireGuard ($WG_IFACE)..."

WG_DIR="/etc/wireguard"
KEY_FILE="$WG_DIR/server.key"
PUB_FILE="$WG_DIR/server.pub"

if [[ ! -f "$KEY_FILE" ]]; then
    wg genkey | tee "$KEY_FILE" | wg pubkey > "$PUB_FILE"
    chmod 600 "$KEY_FILE"
    info "Chaves WireGuard geradas."
else
    info "Chaves WireGuard já existem — reutilizando."
fi

SERVER_PUBKEY=$(cat "$PUB_FILE")
SERVER_PRIVKEY=$(cat "$KEY_FILE")

# Detecta interface de rede principal (para NAT/masquerade)
NET_IFACE=$(ip route | grep default | awk '{print $5}' | head -1)
[[ -z "$NET_IFACE" ]] && NET_IFACE="eth0"
info "Interface de rede: $NET_IFACE"

# Cria config wg0
cat > "$WG_DIR/$WG_IFACE.conf" <<EOF
[Interface]
Address = $TUNNEL_GW/24
ListenPort = $WG_PORT
PrivateKey = $SERVER_PRIVKEY

# NAT: encaminha tráfego dos clientes para a internet
PostUp   = iptables -A FORWARD -i $WG_IFACE -j ACCEPT; \
           iptables -A FORWARD -o $WG_IFACE -j ACCEPT; \
           iptables -t nat -A POSTROUTING -o $NET_IFACE -j MASQUERADE
PostDown = iptables -D FORWARD -i $WG_IFACE -j ACCEPT; \
           iptables -D FORWARD -o $WG_IFACE -j ACCEPT; \
           iptables -t nat -D POSTROUTING -o $NET_IFACE -j MASQUERADE

# Peers são adicionados dinamicamente pelo laggado-lagger via "wg set"
EOF

# Ativa IP forwarding
info "Ativando IP forwarding..."
sysctl -w net.ipv4.ip_forward=1 > /dev/null
grep -qxF 'net.ipv4.ip_forward=1' /etc/sysctl.conf || \
    echo 'net.ipv4.ip_forward=1' >> /etc/sysctl.conf

# Inicia WireGuard
systemctl enable wg-quick@$WG_IFACE 2>/dev/null || true
wg-quick down $WG_IFACE 2>/dev/null || true
wg-quick up $WG_IFACE
info "WireGuard $WG_IFACE ativo."

# ── Instalar binário ──────────────────────────────────────────────────────────
mkdir -p "$INSTALL_DIR"

BINARY_URL=""  # Deixe vazio para usar o binário compilado manualmente
if [[ -f "./dist/laggado-lagger" ]]; then
    info "Copiando binário local para $INSTALL_DIR..."
    cp ./dist/laggado-lagger "$INSTALL_DIR/laggado-lagger"
    chmod +x "$INSTALL_DIR/laggado-lagger"
elif [[ -n "$BINARY_URL" ]]; then
    info "Baixando laggado-lagger..."
    curl -L "$BINARY_URL" -o "$INSTALL_DIR/laggado-lagger"
    chmod +x "$INSTALL_DIR/laggado-lagger"
else
    warn "Binário não encontrado em ./dist/laggado-lagger"
    warn "Compile no Windows com build-lagger.bat e envie para o VPS:"
    warn "  scp dist/laggado-lagger root@$PUBLIC_IP:$INSTALL_DIR/"
    warn "Depois execute: systemctl start $SERVICE_NAME"
fi

# ── Criar serviço systemd ─────────────────────────────────────────────────────
info "Criando serviço systemd..."

CITY_ARG=""
[[ -n "$CITY" ]] && CITY_ARG="--city \"$CITY\""

cat > "/etc/systemd/system/$SERVICE_NAME.service" <<EOF
[Unit]
Description=LAGGADO Lagger Node — Community Game Relay
After=network-online.target wg-quick@$WG_IFACE.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/laggado-lagger \\
    --region $REGION \\
    --country $COUNTRY \\
    $CITY_ARG \\
    --public-ip $PUBLIC_IP \\
    --wg-interface $WG_IFACE \\
    --wg-port $WG_PORT \\
    --api-port $API_PORT \\
    --gateway $TUNNEL_GW \\
    --subnet $TUNNEL_SUBNET
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"

if [[ -f "$INSTALL_DIR/laggado-lagger" ]]; then
    systemctl start "$SERVICE_NAME"
    sleep 2
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        info "Serviço iniciado com sucesso!"
    else
        warn "Serviço falhou ao iniciar. Verifique: journalctl -u $SERVICE_NAME -n 50"
    fi
fi

# ── Resumo ────────────────────────────────────────────────────────────────────
echo
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "${GREEN}  LAGGADO Lagger Node — Setup Completo  ${NC}"
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo
echo "  IP público       : $PUBLIC_IP"
echo "  Chave pública WG : $SERVER_PUBKEY"
echo "  WireGuard        : udp/$WG_PORT"
echo "  Relay HTTP API   : tcp/$API_PORT"
echo "  Região           : $REGION / $COUNTRY"
echo
echo "  Comandos úteis:"
echo "    journalctl -u $SERVICE_NAME -f     # ver logs ao vivo"
echo "    systemctl status $SERVICE_NAME      # status do serviço"
echo "    wg show $WG_IFACE                   # peers WireGuard ativos"
echo
echo "  ⚠️  Abra as portas no firewall do painel do VPS:"
echo "      UDP $WG_PORT  — WireGuard"
echo "      TCP $API_PORT  — Relay HTTP API"
echo
