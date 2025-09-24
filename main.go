package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

var (
	pythonPath   = "python"
	pythonScript = "gamepad-ws-server/src/server.py"

	// Métricas
	framesProcessed int64
	framesDropped   int64
	activeStreams   int32
)

type OfferRequest struct {
	SDP    string `json:"sdp"`
	Codec  string `json:"codec"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	FPS    int    `json:"fps"`
}

type StreamSession struct {
	ID        string
	PC        *webrtc.PeerConnection
	FFmpegCmd *exec.Cmd
	Cancel    context.CancelFunc
	StartTime time.Time
}

var (
	sessions     = make(map[string]*StreamSession)
	sessionsLock sync.RWMutex
)

func main() {
	// Configurar logging
	logFile, err := os.OpenFile("chimera-go.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("Erro ao abrir arquivo de log: %v", err)
	}
	defer logFile.Close()
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)
	log.Println("--- Servidor Iniciado ---")

	// Iniciar monitoramento de métricas
	go logMetrics()
	go cleanupStaleSessions()

	// Inicia o servidor Python
	cmdPython := exec.Command(pythonPath, pythonScript)
	cmdPython.Stdout = multiWriter
	cmdPython.Stderr = multiWriter
	if err := cmdPython.Start(); err != nil {
		log.Fatalf("[Go] Erro ao iniciar server.py: %v", err)
	}
	log.Printf("[Go] server.py iniciado com PID: %d", cmdPython.Process.Pid)

	// Garante encerramento do Python no Ctrl+C
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Println("Sinal de encerramento recebido. Desligando...")

		// Encerrar todas as sessões ativas
		cleanupAllSessions()

		if cmdPython.Process != nil {
			cmdPython.Process.Kill()
		}
		os.Exit(0)
	}()

	// Servidor HTTP
	httpAddr := ":8080"
	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.HandleFunc("/offer", handleOffer)
	http.HandleFunc("/stats", handleStats)
	http.HandleFunc("/sessions", handleSessions)

	log.Printf("[Go] Servidor HTTP rodando em http://localhost%s", httpAddr)
	if err := http.ListenAndServe(httpAddr, nil); err != nil {
		log.Printf("Erro fatal no servidor HTTP: %v", err)
	}
}

// Função corrigida para scan de NALUs
func scanNALUs(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if len(data) == 0 {
		if atEOF {
			return 0, nil, nil
		}
		return 0, nil, nil
	}

	// Procura por start codes: 0x00000001 ou 0x000001
	for i := 0; i < len(data)-3; i++ {
		// Verifica por 0x00000001
		if i+3 < len(data) && data[i] == 0x00 && data[i+1] == 0x00 &&
			data[i+2] == 0x00 && data[i+3] == 0x01 {
			if i > 0 {
				return i, data[:i], nil
			}
		}

		// Verifica por 0x000001
		if i+2 < len(data) && data[i] == 0x00 && data[i+1] == 0x00 &&
			data[i+2] == 0x01 {
			if i > 0 {
				return i, data[:i], nil
			} else {
				// Encontrou start code no início, procura próximo
				for j := 3; j < len(data)-2; j++ {
					if data[j] == 0x00 && data[j+1] == 0x00 &&
						(data[j+2] == 0x01 || (j+3 < len(data) && data[j+2] == 0x00 && data[j+3] == 0x01)) {
						return j, data[:j], nil
					}
				}
			}
		}
	}

	if atEOF {
		return len(data), data, nil
	}

	// Precisa de mais dados
	return 0, nil, nil
}

func handleOffer(w http.ResponseWriter, r *http.Request) {
	// Adicionar timeout para a requisição
	_, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var req OfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Erro ao decodificar JSON", http.StatusBadRequest)
		return
	}
	log.Printf("Recebido offer com config: %dx%d @ %dfps", req.Width, req.Height, req.FPS)

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Printf("Erro ao criar PeerConnection: %v", err)
		http.Error(w, "Erro interno do servidor", http.StatusInternalServerError)
		return
	}

	// Criar contexto para esta sessão
	sessionCtx, sessionCancel := context.WithCancel(context.Background())

	// Criar sessão
	sessionID := generateSessionID()
	session := &StreamSession{
		ID:        sessionID,
		PC:        pc,
		Cancel:    sessionCancel,
		StartTime: time.Now(),
	}

	registerSession(session)

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[Session %s] WebRTC Connection State: %s", sessionID, state.String())

		switch state {
		case webrtc.PeerConnectionStateConnected:
			atomic.AddInt32(&activeStreams, 1)
		case webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed:
			atomic.AddInt32(&activeStreams, -1)
			sessionCancel()
			unregisterSession(sessionID)
			if pc.ConnectionState() != webrtc.PeerConnectionStateClosed {
				pc.Close()
			}
		}
	})

	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video",
		"pion",
	)
	if err != nil {
		sessionCancel()
		unregisterSession(sessionID)
		pc.Close()
		log.Printf("Erro ao criar video track: %v", err)
		http.Error(w, "Erro interno do servidor", http.StatusInternalServerError)
		return
	}

	if _, err = pc.AddTrack(videoTrack); err != nil {
		sessionCancel()
		unregisterSession(sessionID)
		pc.Close()
		log.Printf("Erro ao adicionar track: %v", err)
		http.Error(w, "Erro interno do servidor", http.StatusInternalServerError)
		return
	}

	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: req.SDP}
	if err := pc.SetRemoteDescription(offer); err != nil {
		sessionCancel()
		unregisterSession(sessionID)
		pc.Close()
		log.Printf("Erro ao set remote description: %v", err)
		http.Error(w, "Erro interno do servidor", http.StatusInternalServerError)
		return
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		sessionCancel()
		unregisterSession(sessionID)
		pc.Close()
		log.Printf("Erro ao criar answer: %v", err)
		http.Error(w, "Erro interno do servidor", http.StatusInternalServerError)
		return
	}

	if err = pc.SetLocalDescription(answer); err != nil {
		sessionCancel()
		unregisterSession(sessionID)
		pc.Close()
		log.Printf("Erro ao set local description: %v", err)
		http.Error(w, "Erro interno do servidor", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(answer); err != nil {
		log.Printf("Erro ao enviar resposta: %v", err)
	}

	// Iniciar FFmpeg em goroutine separada
	go startFFmpeg(sessionCtx, videoTrack, req.Width, req.Height, req.FPS, sessionID)
}

func startFFmpeg(ctx context.Context, track *webrtc.TrackLocalStaticSample, width, height, fps int, sessionID string) {
	log.Printf("[Session %s] Iniciando FFmpeg...", sessionID)

	args := []string{
		"-f", "gdigrab",
		"-framerate", fmt.Sprintf("%d", fps),
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-i", "desktop",
		"-c:v", "h264_nvenc",
		"-b:v", "8M",
		"-f", "rawvideo",
		"pipe:1",
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[Session %s] Erro ao criar stdout pipe: %v", sessionID, err)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("[Session %s] Erro ao criar stderr pipe: %v", sessionID, err)
		return
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[Session %s] Erro ao iniciar FFmpeg: %v", sessionID, err)
		return
	}

	// Atualizar sessão com comando FFmpeg
	updateSessionFFmpeg(sessionID, cmd)

	// Log do FFmpeg
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				log.Printf("[Session %s] FFMPEG: %s", sessionID, scanner.Text())
			}
		}
	}()

	// Buffer pool para otimização de memória
	bufferPool := &sync.Pool{
		New: func() interface{} {
			return make([]byte, 4*1024*1024) // 4MB buffer
		},
	}

	// Lê H.264 bruto e envia para WebRTC
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(bufferPool.Get().([]byte), 4*1024*1024)
	scanner.Split(scanNALUs)

	frameDuration := time.Second / time.Duration(fps)
	ticker := time.NewTicker(frameDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			cmd.Wait()
			log.Printf("[Session %s] FFmpeg encerrado.", sessionID)
			return

		case <-ticker.C:
			if scanner.Scan() {
				nalu := scanner.Bytes()
				if len(nalu) > 4 {
					// Verificar se já tem start code, se não, adicionar
					var naluWithStart []byte
					if !bytes.HasPrefix(nalu, []byte{0x00, 0x00, 0x00, 0x01}) &&
						!bytes.HasPrefix(nalu, []byte{0x00, 0x00, 0x01}) {
						naluWithStart = append([]byte{0x00, 0x00, 0x00, 0x01}, nalu...)
					} else {
						naluWithStart = nalu
					}

					err := track.WriteSample(media.Sample{
						Data:     naluWithStart,
						Duration: frameDuration,
					})

					atomic.AddInt64(&framesProcessed, 1)
					if err != nil {
						atomic.AddInt64(&framesDropped, 1)
						log.Printf("[Session %s] Erro ao escrever sample: %v", sessionID, err)
					}
				}
			} else {
				if err := scanner.Err(); err != nil {
					log.Printf("[Session %s] Erro no scanner: %v", sessionID, err)
				}
				return
			}
		}
	}
}

// Funções de gestão de sessões
func generateSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func registerSession(session *StreamSession) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	sessions[session.ID] = session
	log.Printf("[Session %s] Sessão registrada. Total: %d", session.ID, len(sessions))
}

func unregisterSession(sessionID string) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	if session, exists := sessions[sessionID]; exists {
		if session.FFmpegCmd != nil && session.FFmpegCmd.Process != nil {
			session.FFmpegCmd.Process.Kill()
		}
		delete(sessions, sessionID)
		log.Printf("[Session %s] Sessão removida. Total: %d", sessionID, len(sessions))
	}
}

func updateSessionFFmpeg(sessionID string, cmd *exec.Cmd) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	if session, exists := sessions[sessionID]; exists {
		session.FFmpegCmd = cmd
	}
}

func cleanupStaleSessions() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sessionsLock.Lock()
		now := time.Now()
		for id, session := range sessions {
			// Remove sessões inativas por mais de 5 minutos
			if now.Sub(session.StartTime) > 5*time.Minute &&
				session.PC.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
				if session.FFmpegCmd != nil && session.FFmpegCmd.Process != nil {
					session.FFmpegCmd.Process.Kill()
				}
				delete(sessions, id)
				log.Printf("[Session %s] Sessão stale removida", id)
			}
		}
		sessionsLock.Unlock()
	}
}

func cleanupAllSessions() {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()

	for id, session := range sessions {
		session.Cancel()
		if session.FFmpegCmd != nil && session.FFmpegCmd.Process != nil {
			session.FFmpegCmd.Process.Kill()
		}
		if session.PC != nil && session.PC.ConnectionState() != webrtc.PeerConnectionStateClosed {
			session.PC.Close()
		}
		delete(sessions, id)
	}
	log.Printf("Todas as sessões encerradas. Total: %d", len(sessions))
}

// Monitoramento e métricas
func logMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		processed := atomic.LoadInt64(&framesProcessed)
		dropped := atomic.LoadInt64(&framesDropped)
		active := atomic.LoadInt32(&activeStreams)

		var dropRate float64
		if processed > 0 {
			dropRate = float64(dropped) / float64(processed) * 100
		}

		log.Printf("📊 Métricas: StreamsAtivos=%d, FramesProcessados=%d, FramesDropados=%d, TaxaDrop=%.2f%%",
			active, processed, dropped, dropRate)
	}
}

// Handlers HTTP para monitoring
func handleStats(w http.ResponseWriter, r *http.Request) {
	processed := atomic.LoadInt64(&framesProcessed)
	dropped := atomic.LoadInt64(&framesDropped)
	active := atomic.LoadInt32(&activeStreams)

	var dropRate float64
	if processed > 0 {
		dropRate = float64(dropped) / float64(processed) * 100
	}

	stats := map[string]interface{}{
		"active_streams":    active,
		"frames_processed":  processed,
		"frames_dropped":    dropped,
		"drop_rate_percent": dropRate,
		"timestamp":         time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func handleSessions(w http.ResponseWriter, r *http.Request) {
	sessionsLock.RLock()
	defer sessionsLock.RUnlock()

	sessionInfo := make([]map[string]interface{}, 0, len(sessions))
	for id, session := range sessions {
		info := map[string]interface{}{
			"id":         id,
			"start_time": session.StartTime,
			"duration":   time.Since(session.StartTime).String(),
			"state":      session.PC.ConnectionState().String(),
			"has_ffmpeg": session.FFmpegCmd != nil,
		}
		sessionInfo = append(sessionInfo, info)
	}

	response := map[string]interface{}{
		"total_sessions": len(sessions),
		"sessions":       sessionInfo,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
