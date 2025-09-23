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
	"runtime"
	"syscall"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

var (
	pythonPath   = "python"
	pythonScript = "./gamepad-ws-server/src/server.py"
)

type OfferRequest struct {
	SDP    string `json:"sdp"`
	Codec  string `json:"codec"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	FPS    int    `json:"fps"`
}

func main() {
	logFile, err := os.OpenFile("chimera-go.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("Erro ao abrir arquivo de log: %v", err)
	}
	defer logFile.Close()

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)
	log.Println("--- Servidor Iniciado ---")

	cmdPython := exec.Command(pythonPath, pythonScript)
	cmdPython.Stdout = multiWriter
	cmdPython.Stderr = multiWriter

	if err := cmdPython.Start(); err != nil {
		log.Fatalf("[Go] Erro ao iniciar server.py: %v", err)
	}
	log.Printf("[Go] server.py iniciado com PID: %d", cmdPython.Process.Pid)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Println("Sinal de encerramento recebido. Desligando o servidor Python...")
		if cmdPython.Process != nil {
			if err := cmdPython.Process.Kill(); err != nil {
				log.Printf("Falha ao encerrar o processo Python: %v", err)
			}
		}
		os.Exit(0)
	}()

	httpAddr := ":8080"
	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.HandleFunc("/offer", handleOffer)
	log.Printf("[Go] Servidor HTTP rodando em http://localhost%s", httpAddr)
	if err := http.ListenAndServe(httpAddr, nil); err != nil {
		log.Printf("Erro fatal no servidor HTTP: %v", err)
	}
}

// scanNALUs é uma função auxiliar para encontrar os limites dos frames H.264
func scanNALUs(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	startCode := []byte{0x00, 0x00, 0x01}
	startCodeWithZero := []byte{0x00, 0x00, 0x00, 0x01}

	nextStart := bytes.Index(data[1:], startCode)
	if bytes.HasPrefix(data, startCodeWithZero) {
		nextStart = bytes.Index(data[4:], startCode)
	}

	if nextStart != -1 {
		return nextStart + 1, data[:nextStart+1], nil
	}

	if atEOF || len(data) > 2*1024*1024 {
		return len(data), data, nil
	}

	return 0, nil, nil
}

func handleOffer(w http.ResponseWriter, r *http.Request) {
	var req OfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Erro ao decodificar JSON", http.StatusBadRequest)
		return
	}
	log.Printf("Recebido offer com config: %s %dx%d @ %dfps", req.Codec, req.Width, req.Height, req.FPS)

	config := webrtc.Configuration{ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}}
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	ffmpegCtx, cancel := context.WithCancel(context.Background())
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("WebRTC Connection State mudou para: %s\n", state.String())
		if state >= webrtc.PeerConnectionStateDisconnected {
			log.Println("PeerConnection state finalizado, cancelando o contexto do FFmpeg...")
			cancel()
		}
	})

	videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion")
	if err != nil {
		panic(err)
	}
	if _, err = pc.AddTrack(videoTrack); err != nil {
		panic(err)
	}

	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: req.SDP}
	if err := pc.SetRemoteDescription(offer); err != nil {
		panic(err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		panic(err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(answer)
	go startFFmpeg(ffmpegCtx, videoTrack, req.Codec, req.Width, req.Height, req.FPS)
}

func startFFmpeg(ctx context.Context, track *webrtc.TrackLocalStaticSample, codec string, width, height, fps int) {
	var cmd *exec.Cmd

	// Parâmetros base para ambos os sistemas operacionais
	ffmpegArgs := []string{
		"-framerate", fmt.Sprintf("%d", fps),
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-c:v", codec,
		"-pix_fmt", "yuv420p",
		// Otimizações para baixa latência
		"-preset", "speed",
		"-tune", "ultralowlatency ",
		"-profile:v", "constrained_baseline",
		// Controle de bitrate para evitar travamentos na rede
		"-b:v", "3M", // Bitrate de 3 Mbps (ajuste conforme sua rede)
		"-maxrate", "3M",
		"-bufsize", "6M",
		// Outros parâmetros
		"-g", fmt.Sprintf("%d", fps*2), // GOP size
		"-bf", "0",
		"-f", "h264",
		"pipe:1", // Saída para o pipe
	}

	if runtime.GOOS == "windows" {
		// Constrói o comando para Windows (gdigrab)
		args := append([]string{"-f", "gdigrab", "-i", "desktop"}, ffmpegArgs...)
		cmd = exec.Command("ffmpeg", args...)
	} else { // Linux
		display := os.Getenv("DISPLAY")
		if display == "" {
			display = ":0"
		}
		// Constrói o comando para Linux (x11grab)
		args := append([]string{"-f", "x11grab", "-i", display}, ffmpegArgs...)
		cmd = exec.Command("ffmpeg", args...)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		panic(err)
	}
	if err := cmd.Start(); err != nil {
		panic(err)
	}

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("FFMPEG (stderr): %s", scanner.Text())
		}
	}()

	log.Printf("FFmpeg iniciado (%s %dx%d @ %dfps), PID: %d", codec, width, height, fps, cmd.Process.Pid)

	go func() {
		<-ctx.Done()
		log.Printf("Contexto cancelado. Encerrando FFmpeg (PID: %d)...", cmd.Process.Pid)
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil {
				log.Printf("Falha ao encerrar FFmpeg, talvez já tenha terminado: %v", err)
			}
		}
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 2*1024*1024), 2*1024*1024)
	scanner.Split(scanNALUs)

	for scanner.Scan() {
		nalu := scanner.Bytes()
		if len(nalu) > 0 {
			naluWithStartCode := append([]byte{0x00, 0x00, 0x00, 0x01}, nalu...)
			if writeErr := track.WriteSample(media.Sample{Data: naluWithStartCode, Duration: time.Second / time.Duration(fps)}); writeErr != nil {
				log.Printf("Erro escrevendo sample WebRTC: %v", writeErr)
				return
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("Erro lendo stdout do FFmpeg: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.String() != "signal: killed" {
			log.Printf("FFmpeg encerrou com erro inesperado: %v", err)
		}
	} else {
		log.Println("FFmpeg encerrou com sucesso.")
	}
}
