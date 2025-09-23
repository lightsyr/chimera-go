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
	"syscall"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
)

var (
	pythonPath   = "python"
	pythonScript = "gamepad-ws-server/src/server.py"
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
		log.Println("Sinal de encerramento recebido. Desligando Python...")
		if cmdPython.Process != nil {
			cmdPython.Process.Kill()
		}
		os.Exit(0)
	}()

	// Servidor HTTP
	httpAddr := ":8080"
	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.HandleFunc("/offer", handleOffer)
	log.Printf("[Go] Servidor HTTP rodando em http://localhost%s", httpAddr)
	if err := http.ListenAndServe(httpAddr, nil); err != nil {
		log.Printf("Erro fatal no servidor HTTP: %v", err)
	}
}

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
	log.Printf("Recebido offer com config: %dx%d @ %dfps", req.Width, req.Height, req.FPS)

	config := webrtc.Configuration{ICEServers: []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}}
	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	gCtx, cancel := context.WithCancel(context.Background())
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("WebRTC Connection State: %s\n", state.String())
		if state >= webrtc.PeerConnectionStateDisconnected {
			cancel()
		}
	})

	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264}, "video", "pion",
	)
	if err != nil {
		panic(err)
	}
	pc.AddTrack(videoTrack)

	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: req.SDP}
	pc.SetRemoteDescription(offer)
	answer, _ := pc.CreateAnswer(nil)
	pc.SetLocalDescription(answer)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(answer)

	go startFFmpeg(gCtx, videoTrack, req.Width, req.Height, req.FPS)
}

func startFFmpeg(ctx context.Context, track *webrtc.TrackLocalStaticSample, width, height, fps int) {
	args := []string{
		"-f", "gdigrab",
		"-framerate", fmt.Sprintf("%d", fps),
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-i", "desktop",
		"-c:v", "h264_amf",
		"-b:v", "8M",
		"-f", "rawvideo", // envia H.264 cru para stdout
		"pipe:1",
	}
	cmd := exec.Command("ffmpeg", args...)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	cmd.Start()

	// Log do FFmpeg
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("FFMPEG: %s", scanner.Text())
		}
	}()

	// Lê H.264 bruto e envia para WebRTC
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 2*1024*1024), 2*1024*1024)
	scanner.Split(scanNALUs)

	for scanner.Scan() {
		nalu := scanner.Bytes()
		if len(nalu) > 0 {
			naluWithStart := append([]byte{0x00, 0x00, 0x00, 0x01}, nalu...)
			track.WriteSample(media.Sample{Data: naluWithStart, Duration: time.Second / time.Duration(fps)})
		}
	}

	<-ctx.Done()
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
	log.Println("FFmpeg encerrado.")
}
