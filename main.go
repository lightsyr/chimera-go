package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
)

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Fatal(err)
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

var (
	udpListenIP   = getLocalIP()
	udpListenPort = 5004
)

// SDPRequest recebe JSON do navegador
type SDPRequest struct {
	Type string `json:"type"`
	SDP  string `json:"sdp"`
}

func main() {
	// Flag para escolher codec via linha de comando
	codec := flag.String("codec", "h264_qsv", "Codec de vídeo para FFmpeg (ex: h264_qsv, h264_nvenc, h264_amf)")
	flag.Parse()

	// Inicia FFmpeg para streaming RTP
	go startFFmpeg(*codec)

	// Inicia servidor Python WebSocket
	go startPythonWebSocketServer()

	// Servidor HTTP para HTML/JS
	fs := http.FileServer(http.Dir("./web")) // index.html dentro de ./web
	http.Handle("/", fs)

	// Endpoint para receber offer do navegador
	http.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
			return
		}

		var req SDPRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Erro ao parsear JSON", http.StatusBadRequest)
			return
		}

		// Converte string para SDPType
		var sdpType webrtc.SDPType
		switch req.Type {
		case "offer":
			sdpType = webrtc.SDPTypeOffer
		case "answer":
			sdpType = webrtc.SDPTypeAnswer
		default:
			http.Error(w, "Tipo de SDP inválido", http.StatusBadRequest)
			return
		}

		offer := webrtc.SessionDescription{
			Type: sdpType,
			SDP:  req.SDP,
		}

		answer, err := handleOffer(offer)
		if err != nil {
			http.Error(w, fmt.Sprintf("Erro ao processar offer: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(answer)
	})

	log.Println("Servidor HTTP rodando em http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// handleOffer cria PeerConnection, track de vídeo e DataChannel
func handleOffer(offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	// Cria PeerConnection
	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	// Track de vídeo
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video", "pion",
	)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	rtpSender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}

	// Ponte RTP → Track
	go startRTPListener(videoTrack)

	// Rotina para RTCP (mantém a conexão viva)
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, err := rtpSender.Read(rtcpBuf); err != nil {
				return
			}
		}
	}()

	// DataChannel gamepad
	dc, err := peerConnection.CreateDataChannel("gamepad", nil)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	dc.OnOpen(func() { log.Println("DataChannel do gamepad aberto!") })
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		log.Printf("Recebido do gamepad: %v\n", msg.Data)
	})

	// Set remote description (offer do navegador)
	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		return webrtc.SessionDescription{}, err
	}

	// Cria answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		return webrtc.SessionDescription{}, err
	}
	if err := peerConnection.SetLocalDescription(answer); err != nil {
		return webrtc.SessionDescription{}, err
	}

	return answer, nil
}

// startRTPListener lê RTP do FFmpeg e escreve no videoTrack
func startRTPListener(videoTrack *webrtc.TrackLocalStaticRTP) {
	addr := fmt.Sprintf("%s:%d", udpListenIP, udpListenPort)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		log.Fatalf("Erro ao abrir listener UDP: %v", err)
	}
	defer conn.Close()
	log.Printf("Ouvindo RTP em %s\n", addr)

	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			log.Printf("Erro lendo RTP: %v", err)
			continue
		}

		packet := &rtp.Packet{}
		if err := packet.Unmarshal(buf[:n]); err != nil {
			continue
		}

		if err := videoTrack.WriteRTP(packet); err != nil {
			continue
		}
	}
}

// FFmpeg via GPU para enviar RTP
func startFFmpeg(codec string) {
	os.RemoveAll("tmp")
	os.Mkdir("tmp", 0755)

	cmd := exec.Command("ffmpeg",
		"-f", "dshow",
		"-i", "video=screen-capture-recorder",
		"-c:v", codec,
		"-preset", "llhp",
		"-g", "15",
		"-bf", "0",
		"-pix_fmt", "yuv420p",
		"-an",
		"-f", "rtp",
		fmt.Sprintf("rtp://%s:%d", udpListenIP, udpListenPort),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Fatalf("Erro ao iniciar FFmpeg: %v", err)
	}
	log.Printf("FFmpeg iniciado com codec %s, enviando RTP para %s:%d\n", codec, udpListenIP, udpListenPort)
	cmd.Wait()
}

// Inicia servidor Python WebSocket
func startPythonWebSocketServer() {
	pythonPath := "python"
	serverScript := "gamepad-ws-server/src/server.py"

	cmd := exec.Command(pythonPath, serverScript)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Printf("Erro ao iniciar o servidor WebSocket Python: %v", err)
		return
	}
	log.Println("Servidor WebSocket Python iniciado.")
}
