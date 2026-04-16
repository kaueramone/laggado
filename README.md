<div align="center">

<img src="assets/logo.png" alt="LAGGADO" width="340"/>

**Game Route Optimizer — Alternativa open source ao ExitLag e NoPing**

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Fyne](https://img.shields.io/badge/UI-Fyne_v2-4A90D9?style=flat-square)](https://fyne.io)
[![WireGuard](https://img.shields.io/badge/Tunnel-WireGuard-88171A?style=flat-square&logo=wireguard)](https://wireguard.com)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![Windows](https://img.shields.io/badge/Windows-10%2F11-0078D4?style=flat-square&logo=windows)](https://github.com/kaueramone/laggado/releases)

</div>

---

## O que é?

**LAGGADO** é um otimizador de rotas para jogos online, construído para a comunidade brasileira (e europeia) que sofre com ping alto, packet loss e jitter.

Funciona como o ExitLag ou NoPing — mas é **100% gratuito, open source e movido pela própria comunidade**: cada instalação pode virar um nó relay que ajuda outros jogadores.

```
Você (PT 🇵🇹)  ──→  Lagger (BR 🇧🇷)  ──→  Servidor CS2 (SA)
     40ms ping direto       12ms via relay        ping cai de 180ms → 95ms
```

---

## Como funciona?

```
┌─────────────────────────────────────────────────────────────┐
│                      Lagger Network                         │
│                                                             │
│  [PC do jogador]                                            │
│       │                                                     │
│       │  1. LAGGADO detecta o jogo aberto                   │
│       │  2. Busca Laggers ativos na região                  │
│       │  3. Testa latência → escolhe o melhor               │
│       │  4. Handshake WireGuard com o Lagger                │
│       │  5. Ativa split tunnel — só o tráfego do jogo       │
│       │     passa pelo Lagger, resto da net fica direto     │
│       ↓                                                     │
│  [Lagger Node]  →  [Servidor do Jogo]                       │
└─────────────────────────────────────────────────────────────┘
```

- **Split Tunnel** — apenas os IPs do servidor do jogo passam pelo túnel. YouTube, Discord, tudo o mais fica direto.
- **WireGuard** — protocolo moderno, criptografado, com overhead mínimo (~5ms extra vs OpenVPN que adiciona 15-30ms).
- **Lagger Network** — rede P2P via Cloudflare Workers. Cada instalação tenta se tornar um relay. Zero infraestrutura central.
- **Detecção automática** — o app detecta o jogo aberto e sugere criar a rota automaticamente.

---

## Jogos suportados

| Jogo | Região SA | Região EU | Região NA |
|------|:---------:|:---------:|:---------:|
| Counter-Strike 2 | ✅ | ✅ | ✅ |
| Dota 2 | ✅ | ✅ | ✅ |
| Deadlock | ✅ | ✅ | ✅ |
| Valorant | ✅ | ✅ | ✅ |
| League of Legends | ✅ | ✅ | ✅ |
| Apex Legends | ✅ | ✅ | ✅ |
| Fortnite | ✅ | ✅ | ✅ |
| PUBG | ✅ | ✅ | ✅ |
| Rocket League | ✅ | ✅ | ✅ |
| Rainbow Six Siege | ✅ | ✅ | ✅ |
| Rust | ✅ | ✅ | ✅ |
| Overwatch 2 | ✅ | ✅ | ✅ |
| Warzone / MW | ✅ | ✅ | ✅ |
| Team Fortress 2 | ✅ | ✅ | ✅ |
| Minecraft | ✅ | ✅ | ✅ |
| Battlefield 2042 | ✅ | ✅ | ✅ |

---

## Screenshots

<div align="center">

| Conexões | Biblioteca | Analisador de Rede |
|----------|------------|-------------------|
| Gerencie rotas por jogo e região | Detecta o jogo aberto automaticamente | Monitora ping, jitter e packet loss em tempo real |

</div>

---

## Instalação

### Opção 1 — Instalador (recomendado)

1. Baixe o `LAGGADO-Setup-v0.2.0.exe` na página de [Releases](https://github.com/kaueramone/laggado/releases)
2. Execute como **Administrador**
3. Instale o [WireGuard](https://wireguard.com/install/) quando solicitado
4. Abra o LAGGADO → adicione um jogo → conecte

### Opção 2 — Executável direto

Baixe `LAGGADO.exe` nas Releases, coloque em qualquer pasta e execute como Administrador.

> ⚠️ **Requer execução como Administrador** para gerenciar rotas de rede e o túnel WireGuard.

---

## Compilar do zero

### Pré-requisitos

- [Go 1.22+](https://go.dev/dl/)
- [MSYS2 + MinGW-w64 GCC](https://www.msys2.org/) — necessário para o Fyne (CGo)
  ```
  pacman -S mingw-w64-x86_64-gcc
  ```
  Adicione `C:\msys64\mingw64\bin` ao PATH.
- [Inno Setup 6](https://jrsoftware.org/isdl.php) *(opcional, para gerar o instalador)*

### Build

```bat
git clone https://github.com/kaueramone/laggado.git
cd laggado
build.bat
```

O `build.bat` faz tudo: baixa dependências, compila o ícone, gera o `.exe` e pergunta se quer criar o instalador.

---

## Rodar um Lagger Node (VPS)

Quer contribuir com um VPS como relay para a comunidade? O LAGGADO tenta automaticamente usar UPnP para se tornar um Lagger, mas em um VPS você tem controle total.

### Setup rápido (Ubuntu/Debian)

```bash
# 1. Compile o binário Linux no Windows
build-lagger.bat
# gera dist/laggado-lagger

# 2. Envie para o VPS
scp dist/laggado-lagger root@<IP>:/opt/laggado-lagger/
scp vps-setup.sh root@<IP>:~/

# 3. No VPS
sudo bash vps-setup.sh SA "Sao Paulo" BR
```

O script configura WireGuard, NAT, e cria um serviço systemd que registra o node automaticamente na Lagger Network.

**Abra as portas no firewall do VPS:**
- `UDP 51820` — WireGuard
- `TCP 7735` — Relay HTTP API

### VPS gratuito recomendado

[Oracle Cloud Free Tier](https://cloud.oracle.com/free) — 2 VMs AMD permanentes (sem expirar), o único free tier verdadeiramente vitalício.

---

## Arquitetura

```
laggado/
├── cmd/
│   ├── laggado-gui/          # App Windows (Fyne v2)
│   │   ├── main.go
│   │   ├── ui/
│   │   │   ├── conexoespanel.go   # Gerenciamento de rotas
│   │   │   ├── bibliotecapanel.go # Biblioteca de jogos
│   │   │   ├── statuspanel.go     # Analisador de rede
│   │   │   ├── configpanel.go     # Configurações
│   │   │   ├── colaborepanel.go   # Suporte / doações
│   │   │   ├── connect.go         # Fluxo de conexão WireGuard
│   │   │   ├── lagger.go          # Auto-registro como Lagger
│   │   │   └── i18n.go            # PT / ES / EN
│   │   └── resource.rc            # Ícone e versão do .exe
│   └── laggado-lagger/       # Daemon Linux para VPS
├── internal/
│   ├── discovery/            # Registro na Lagger Network (Cloudflare Worker)
│   ├── relay/                # Servidor relay WireGuard HTTP
│   ├── tunnel/               # Gerenciamento de túnel WireGuard (cliente)
│   ├── gameservers/          # CIDRs dos servidores de jogos por região
│   ├── gamelib/              # Biblioteca de jogos suportados
│   ├── gamedet/              # Detecção de processo do jogo em execução
│   ├── upnp/                 # Mapeamento de portas UPnP automático
│   ├── geoip/                # Geolocalização de IPs
│   └── store/                # Persistência de configurações (JSON)
├── worker/                   # Cloudflare Worker (registry de Laggers)
├── assets/                   # Ícones e imagens
├── installer/                # Script Inno Setup
├── build.bat                 # Build Windows
├── build-lagger.bat          # Cross-compile do daemon Linux
└── vps-setup.sh              # Setup automatizado do VPS
```

---

## Discovery — Cloudflare Worker

O registro de Laggers usa um Worker na Cloudflare com KV storage. É gratuito para até ~10 mil Laggers no free tier.

```
POST /register   — registra ou renova um Lagger (TTL 5 min)
GET  /laggers    — lista Laggers ativos por região
GET  /count      — contagem global de Laggers
DELETE /leave    — remove o Lagger imediatamente
```

Para fazer deploy do seu próprio Worker:
```bash
cd worker/
wrangler deploy
```

---

## Contribuindo

Pull requests são muito bem-vindos! Algumas ideias:

- 🎮 Adicionar CIDRs de novos jogos em `internal/gameservers/gameservers.go`
- 🌍 Adicionar traduções em `cmd/laggado-gui/ui/i18n.go`
- 🖼️ Ícones de jogos em `internal/gamelib/`
- 🐛 Reportar bugs via [Issues](https://github.com/kaueramone/laggado/issues)

---

## Apoie o projeto

O LAGGADO é freeware desenvolvido nas horas livres. Se ele te ajudou a melhorar seu ping:

| PIX | PayPal |
|-----|--------|
| `kaueramone@live.com` | [paypal.me/kaueramone](https://paypal.me/kaueramone) |

---

## Licença

MIT © 2026 [Kaue Da Costa Pacheco](https://github.com/kaueramone)

---

<div align="center">

*Feito no Brasil 🇧🇷 para jogadores do mundo inteiro*

**⭐ Se curtiu, dá uma estrela — ajuda o projeto a crescer!**

</div>
