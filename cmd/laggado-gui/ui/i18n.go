package ui

// Lang representa o idioma ativo.
type Lang int

const (
	LangPT Lang = iota // Português Brasileiro (padrão)
	LangES             // Español
	LangEN             // English
)

// currentLang é o idioma atual da interface.
var currentLang = LangPT

// SetLang troca o idioma ativo.
func SetLang(l Lang) { currentLang = l }

// CurrentLang retorna o idioma ativo.
func CurrentLang() Lang { return currentLang }

// T retorna a tradução de uma chave no idioma atual.
// Fallback para PT se não encontrado.
func T(key string) string {
	if m, ok := translations[currentLang]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if s, ok := translations[LangPT][key]; ok {
		return s
	}
	return key
}

var translations = map[Lang]map[string]string{
	LangPT: {
		// App
		"app.tagline":      "Reduza seu ping. Jogue melhor.",
		"app.version":      "v0.2.0 • open source",
		// Nav
		"nav.conexoes":   "Conexões",
		"nav.biblioteca": "Biblioteca",
		"nav.analisador": "Analisador de Rede",
		"nav.config":     "Configurações",
		"nav.colabore":   "Colabore",
		// Como funciona
		"howworks.title": "Como funciona?",
		"howworks.step1": "🔍  Detecta o servidor do jogo em tempo real",
		"howworks.step2": "🌐  Testa rotas via Lagger Network comunitária",
		"howworks.step3": "⚡  Ativa o caminho mais rápido com WireGuard",
		// Biblioteca
		"lib.title":        "Biblioteca",
		"lib.available":    "%d jogos disponíveis",
		"lib.found":        "%d jogos encontrados",
		"lib.search":       "🔍  Pesquisar jogo...",
		"lib.add":          "+ Adicionar",
		"lib.detected":     "Jogo detectado",
		"lib.detected.sub": "Quer criar uma rota para jogar com menor ping?",
		"lib.detected.btn": "Criar Rota Agora",
		// Status / Analisador
		"status.title":       "Analisador de Rede",
		"status.ping":        "Ping",
		"status.jitter":      "Jitter",
		"status.loss":        "Perda",
		"status.route":       "Rota",
		"status.ping.desc":   "Tempo de ida e volta até o servidor. Abaixo de 80ms é ótimo.",
		"status.jitter.desc": "Variação no ping. Acima de 20ms causa lag irregular.",
		"status.loss.desc":   "% de pacotes perdidos. Acima de 1% causa travamentos.",
		"status.route.desc":  "Direta = seu ISP • via Lagger = rota otimizada pelo LAGGADO.",
		"status.monitor.start": "▶  Iniciar Monitor ao Vivo",
		"status.monitor.stop":  "⏹  Parar Monitor",
		"status.monitor.hint":  "O monitor mede ping, jitter e perda a cada 5 segundos.",
		"status.sessions":      "Últimas Sessões",
		"status.nosession":     "Nenhuma sessão ainda. Conecte um jogo para começar.",
		// Config
		"config.title": "Configurações",
		"config.weights.title": "Pesos de Pontuação de Rota",
		"config.weights.desc":  "Como o LAGGADO classifica as rotas. Aumente o peso de Latência para priorizar velocidade; de Perda para priorizar estabilidade.",
		"config.vps.title": "Seus Servidores VPS",
		"config.vps.desc":  "Adicione VPS próprios como relay. Equivalente aos servidores do ExitLag — mas gratuitos. Qualquer VPS Linux com WireGuard funciona.",
		"config.ping.title": "Parâmetros de Teste",
		"config.ping.desc":  "Probes por rota = quantas medições por nó (mais = mais preciso). Intervalo = a cada quantos segundos o LAGGADO refaz o scan.",
		"config.wg.title": "WireGuard",
		"config.wg.desc":  "WireGuard cria um túnel criptografado entre você e o servidor do jogo. Apenas o tráfego do jogo é roteado (split tunnel) — o resto da internet fica normal.",
		"config.ac.title": "Compatibilidade Anti-Cheat",
		"config.ac.desc":  "O LAGGADO não injeta código, não lê memória e não usa drivers kernel. É equivalente a mudar o DNS ou usar uma VPN de roteador.",
		// Lagger Network
		"config.lagger.title":           "Lagger Network — Sua Contribuição",
		"config.lagger.status.active":   "Você está contribuindo como Lagger ativo!",
		"config.lagger.status.inactive": "Você não está sendo um Lagger no momento",
		"config.lagger.desc":            "Laggers são pessoas que compartilham sua conexão para ajudar outros jogadores a ter menos ping. Quanto mais Laggers, melhor a rede para todos.",
		"config.lagger.req.app":         "App aberto em segundo plano",
		"config.lagger.req.wg":          "WireGuard instalado",
		"config.lagger.req.upnp":        "Roteador com UPnP ativado (abre a porta automaticamente)",
		"config.lagger.tip":             "⚠  Seu roteador não abriu a porta automaticamente. Para ser Lagger, ative o UPnP no roteador ou use um VPS gratuito (veja github.com/kaueramone/laggado).",
		// Colabore
		"collab.title":     "Colabore com o LAGGADO",
		"collab.sub":       "LAGGADO é open source, feito com 💙",
		"collab.sub2":      "Se ajudou a reduzir seu ping, considere uma doação!",
		"collab.lang":      "Idioma / Language / Idioma",
		"collab.laggers":   "Laggers online",
		"collab.lagger.me": "✓ Você é um Lagger ativo! Obrigado por contribuir.",
		"collab.lagger.join": "Mantenha o app aberto em 2° plano para se tornar um Lagger",
	},
	LangES: {
		"app.tagline":      "Reduce tu ping. Juega mejor.",
		"app.version":      "v0.2.0 • open source",
		"nav.conexoes":   "Conexiones",
		"nav.biblioteca": "Biblioteca",
		"nav.analisador": "Analizador de Red",
		"nav.config":     "Configuración",
		"nav.colabore":   "Colaborar",
		"howworks.title": "¿Cómo funciona?",
		"howworks.step1": "🔍  Detecta el servidor del juego en tiempo real",
		"howworks.step2": "🌐  Prueba rutas via Lagger Network comunitaria",
		"howworks.step3": "⚡  Activa el camino más rápido con WireGuard",
		"lib.title":        "Biblioteca",
		"lib.available":    "%d juegos disponibles",
		"lib.found":        "%d juegos encontrados",
		"lib.search":       "🔍  Buscar juego...",
		"lib.add":          "+ Agregar",
		"lib.detected":     "Juego detectado",
		"lib.detected.sub": "¿Quieres crear una ruta para jugar con menor ping?",
		"lib.detected.btn": "Crear Ruta Ahora",
		"status.title":       "Analizador de Red",
		"status.ping":        "Ping",
		"status.jitter":      "Jitter",
		"status.loss":        "Pérdida",
		"status.route":       "Ruta",
		"status.ping.desc":   "Tiempo de ida y vuelta al servidor. Menos de 80ms es excelente.",
		"status.jitter.desc": "Variación en el ping. Más de 20ms causa lag irregular.",
		"status.loss.desc":   "% de paquetes perdidos. Más de 1% causa congeladas.",
		"status.route.desc":  "Directa = tu ISP • via Lagger = ruta optimizada por LAGGADO.",
		"status.monitor.start": "▶  Iniciar Monitor en Vivo",
		"status.monitor.stop":  "⏹  Detener Monitor",
		"status.monitor.hint":  "El monitor mide ping, jitter y pérdida cada 5 segundos.",
		"status.sessions":      "Últimas Sesiones",
		"status.nosession":     "Ninguna sesión aún. Conecta un juego para empezar.",
		"config.title": "Configuración",
		"config.weights.title": "Pesos de Puntuación de Ruta",
		"config.weights.desc":  "Cómo LAGGADO clasifica las rutas. Aumenta el peso de Latencia para priorizar velocidad; de Pérdida para estabilidad.",
		"config.vps.title": "Tus Servidores VPS",
		"config.vps.desc":  "Añade VPS propios como relay. Equivalente a los servidores de ExitLag — pero gratis. Cualquier VPS Linux con WireGuard funciona.",
		"config.ping.title": "Parámetros de Prueba",
		"config.ping.desc":  "Probes por ruta = cuántas mediciones por nodo. Intervalo = cada cuántos segundos LAGGADO rehace el scan.",
		"config.wg.title": "WireGuard",
		"config.wg.desc":  "WireGuard crea un túnel cifrado entre tú y el servidor del juego. Solo el tráfico del juego es redirigido (split tunnel) — el resto de internet queda normal.",
		"config.ac.title": "Compatibilidad Anti-Cheat",
		"config.ac.desc":  "LAGGADO no inyecta código, no lee memoria y no usa drivers kernel. Equivalente a cambiar el DNS o usar una VPN de router.",
		"config.lagger.title":           "Lagger Network — Tu Contribución",
		"config.lagger.status.active":   "¡Estás contribuyendo como Lagger activo!",
		"config.lagger.status.inactive": "No eres Lagger en este momento",
		"config.lagger.desc":            "Los Laggers son personas que comparten su conexión para ayudar a otros jugadores a tener menos ping. Más Laggers = mejor red para todos.",
		"config.lagger.req.app":         "App abierta en segundo plano",
		"config.lagger.req.wg":          "WireGuard instalado",
		"config.lagger.req.upnp":        "Router con UPnP activado (abre el puerto automáticamente)",
		"config.lagger.tip":             "⚠  Tu router no abrió el puerto automáticamente. Para ser Lagger, activa UPnP en el router o usa un VPS gratuito (ver github.com/kaueramone/laggado).",
		"collab.title":     "Colabora con LAGGADO",
		"collab.sub":       "LAGGADO es open source, hecho con 💙",
		"collab.sub2":      "¡Si te ayudó a reducir el ping, considera una donación!",
		"collab.lang":      "Idioma / Language / Idioma",
		"collab.laggers":   "Laggers en línea",
		"collab.lagger.me": "✓ ¡Eres un Lagger activo! Gracias por contribuir.",
		"collab.lagger.join": "Mantén la app abierta en 2° plano para ser Lagger",
	},
	LangEN: {
		"app.tagline":      "Lower your ping. Play better.",
		"app.version":      "v0.2.0 • open source",
		"nav.conexoes":   "Connections",
		"nav.biblioteca": "Library",
		"nav.analisador": "Network Analyzer",
		"nav.config":     "Settings",
		"nav.colabore":   "Support Us",
		"howworks.title": "How does it work?",
		"howworks.step1": "🔍  Detects your game server in real time",
		"howworks.step2": "🌐  Tests alternative routes via community Lagger Network",
		"howworks.step3": "⚡  Activates the fastest path using WireGuard",
		"lib.title":        "Library",
		"lib.available":    "%d games available",
		"lib.found":        "%d games found",
		"lib.search":       "🔍  Search game...",
		"lib.add":          "+ Add",
		"lib.detected":     "Game detected",
		"lib.detected.sub": "Want to create a route to play with lower ping?",
		"lib.detected.btn": "Create Route Now",
		"status.title":       "Network Analyzer",
		"status.ping":        "Ping",
		"status.jitter":      "Jitter",
		"status.loss":        "Loss",
		"status.route":       "Route",
		"status.ping.desc":   "Round-trip time to the server. Below 80ms is great.",
		"status.jitter.desc": "Ping variation. Above 20ms causes irregular lag.",
		"status.loss.desc":   "% of lost packets. Above 1% causes freezes.",
		"status.route.desc":  "Direct = your ISP • via Lagger = route optimized by LAGGADO.",
		"status.monitor.start": "▶  Start Live Monitor",
		"status.monitor.stop":  "⏹  Stop Monitor",
		"status.monitor.hint":  "Monitor measures ping, jitter and loss every 5 seconds.",
		"status.sessions":      "Recent Sessions",
		"status.nosession":     "No sessions yet. Connect a game to get started.",
		"config.title": "Settings",
		"config.weights.title": "Route Scoring Weights",
		"config.weights.desc":  "How LAGGADO ranks routes. Increase Latency weight to prioritize speed; Loss to prioritize stability.",
		"config.vps.title": "Your VPS Servers",
		"config.vps.desc":  "Add your own VPS as relay nodes. Equivalent to ExitLag's paid servers — but free. Any Linux VPS with WireGuard works.",
		"config.ping.title": "Test Parameters",
		"config.ping.desc":  "Probes per route = how many measurements per node. Interval = how often LAGGADO rescans routes.",
		"config.wg.title": "WireGuard",
		"config.wg.desc":  "WireGuard creates an encrypted tunnel between you and the game server. Only game traffic is rerouted (split tunnel) — the rest of your internet stays normal.",
		"config.ac.title": "Anti-Cheat Compatibility",
		"config.ac.desc":  "LAGGADO doesn't inject code, read memory, or use kernel drivers. It's equivalent to changing DNS or using a router-level VPN.",
		"config.lagger.title":           "Lagger Network — Your Contribution",
		"config.lagger.status.active":   "You are contributing as an active Lagger!",
		"config.lagger.status.inactive": "You are not a Lagger right now",
		"config.lagger.desc":            "Laggers share their connection to help other players get lower ping. More Laggers = better network for everyone.",
		"config.lagger.req.app":         "App open in the background",
		"config.lagger.req.wg":          "WireGuard installed",
		"config.lagger.req.upnp":        "Router with UPnP enabled (opens the port automatically)",
		"config.lagger.tip":             "⚠  Your router did not open the port automatically. To become a Lagger, enable UPnP on your router or use a free VPS (see github.com/kaueramone/laggado).",
		"collab.title":     "Support LAGGADO",
		"collab.sub":       "LAGGADO is open source, made with 💙",
		"collab.sub2":      "If it helped reduce your ping, consider a donation!",
		"collab.lang":      "Idioma / Language / Idioma",
		"collab.laggers":   "Laggers online",
		"collab.lagger.me": "✓ You are an active Lagger! Thank you for contributing.",
		"collab.lagger.join": "Keep the app open in the background to become a Lagger",
	},
}
