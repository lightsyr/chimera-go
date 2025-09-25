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

	// Metrics
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
	mutex     sync.RWMutex
}

var (
	sessions     = make(map[string]*StreamSession)
	sessionsLock sync.RWMutex
)

func main() {
	// Setup logging
	logFile, err := os.OpenFile("chimera-go.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("Error opening log file: %v", err)
	}
	defer logFile.Close()
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multiWriter)
	log.Println("--- Server Started ---")

	// Start monitoring goroutines
	go logMetrics()
	go cleanupStaleSessions()

	// Start Python server
	cmdPython := exec.Command(pythonPath, pythonScript)
	cmdPython.Stdout = multiWriter
	cmdPython.Stderr = multiWriter
	if err := cmdPython.Start(); err != nil {
		log.Fatalf("[Go] Error starting server.py: %v", err)
	}
	log.Printf("[Go] server.py started with PID: %d", cmdPython.Process.Pid)

	// Graceful shutdown on Ctrl+C
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		log.Println("Shutdown signal received. Shutting down...")

		// Cleanup all active sessions
		cleanupAllSessions()

		if cmdPython.Process != nil {
			cmdPython.Process.Kill()
		}
		os.Exit(0)
	}()

	// HTTP server setup
	httpAddr := ":8080"
	http.Handle("/", http.FileServer(http.Dir("./web")))
	http.HandleFunc("/offer", handleOffer)
	http.HandleFunc("/stats", handleStats)
	http.HandleFunc("/sessions", handleSessions)

	log.Printf("[Go] HTTP server running on http://localhost%s", httpAddr)
	if err := http.ListenAndServe(httpAddr, nil); err != nil {
		log.Printf("Fatal HTTP server error: %v", err)
	}
}

// Fixed NALU scanner
func scanNALUs(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if len(data) < 4 {
		if atEOF && len(data) > 0 {
			return len(data), data, nil
		}
		return 0, nil, nil
	}

	// Look for start codes: 0x00000001 or 0x000001
	for i := 0; i <= len(data)-4; i++ {
		// Check for 0x00000001
		if data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x00 && data[i+3] == 0x01 {
			if i > 0 {
				return i, data[:i], nil
			}
			// Found start code at beginning, look for next one
			for j := i + 4; j <= len(data)-4; j++ {
				if data[j] == 0x00 && data[j+1] == 0x00 &&
					((data[j+2] == 0x00 && data[j+3] == 0x01) ||
						(j <= len(data)-3 && data[j+2] == 0x01)) {
					return j, data[i:j], nil
				}
			}
		}

		// Check for 0x000001 (if we haven't found 0x00000001)
		if i <= len(data)-3 && data[i] == 0x00 && data[i+1] == 0x00 && data[i+2] == 0x01 {
			if i > 0 {
				return i, data[:i], nil
			}
			// Found start code at beginning, look for next one
			for j := i + 3; j <= len(data)-3; j++ {
				if data[j] == 0x00 && data[j+1] == 0x00 &&
					(data[j+2] == 0x01 || (j <= len(data)-4 && data[j+2] == 0x00 && data[j+3] == 0x01)) {
					return j, data[i:j], nil
				}
			}
		}
	}

	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}

	return 0, nil, nil
}

func handleOffer(w http.ResponseWriter, r *http.Request) {
	var req OfferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Error decoding JSON", http.StatusBadRequest)
		return
	}

	// Validate input parameters
	if req.Width <= 0 || req.Width > 3840 || req.Height <= 0 || req.Height > 2160 {
		http.Error(w, "Invalid resolution", http.StatusBadRequest)
		return
	}
	if req.FPS <= 0 || req.FPS > 144 {
		http.Error(w, "Invalid FPS", http.StatusBadRequest)
		return
	}

	log.Printf("Received offer with config: %dx%d @ %dfps", req.Width, req.Height, req.FPS)

	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
			{URLs: []string{"stun:stun1.l.google.com:19302"}},
		},
	}

	pc, err := webrtc.NewPeerConnection(config)
	if err != nil {
		log.Printf("Error creating PeerConnection: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create session context - DON'T tie it to request context
	sessionCtx, sessionCancel := context.WithCancel(context.Background())

	// Create session
	sessionID := generateSessionID()
	session := &StreamSession{
		ID:        sessionID,
		PC:        pc,
		Cancel:    sessionCancel,
		StartTime: time.Now(),
	}

	registerSession(session)

	// Setup connection state handler
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

	// Create video track
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeH264,
			ClockRate:   90000,
			Channels:    0,
			SDPFmtpLine: "level-id=1;profile-level-id=42e01e;packetization-mode=1",
		},
		"video",
		"chimera-stream",
	)
	if err != nil {
		cleanup := func() {
			sessionCancel()
			unregisterSession(sessionID)
			pc.Close()
		}
		cleanup()
		log.Printf("Error creating video track: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if _, err = pc.AddTrack(videoTrack); err != nil {
		sessionCancel()
		unregisterSession(sessionID)
		pc.Close()
		log.Printf("Error adding track: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Set remote description
	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: req.SDP}
	if err := pc.SetRemoteDescription(offer); err != nil {
		sessionCancel()
		unregisterSession(sessionID)
		pc.Close()
		log.Printf("Error setting remote description: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create and set answer
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		sessionCancel()
		unregisterSession(sessionID)
		pc.Close()
		log.Printf("Error creating answer: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err = pc.SetLocalDescription(answer); err != nil {
		sessionCancel()
		unregisterSession(sessionID)
		pc.Close()
		log.Printf("Error setting local description: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(answer); err != nil {
		log.Printf("Error sending response: %v", err)
	}

	// Start FFmpeg in separate goroutine with proper delay
	go func() {
		// Wait a bit for WebRTC connection to be established
		time.Sleep(500 * time.Millisecond)
		startFFmpeg(sessionCtx, videoTrack, req.Width, req.Height, req.FPS, sessionID)
	}()
}

func startFFmpeg(ctx context.Context, track *webrtc.TrackLocalStaticSample, width, height, fps int, sessionID string) {
	log.Printf("[Session %s] Starting FFmpeg...", sessionID)

	// Check if context is already canceled
	select {
	case <-ctx.Done():
		log.Printf("[Session %s] Context already canceled, not starting FFmpeg", sessionID)
		return
	default:
	}

	// Optimized FFmpeg arguments
	args := []string{
		"-f", "gdigrab",
		"-framerate", fmt.Sprintf("%d", fps),
		"-video_size", fmt.Sprintf("%dx%d", width, height),
		"-i", "desktop",
		"-c:v", "libx264", // Use software encoder for compatibility
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-crf", "23",
		"-maxrate", "8M",
		"-bufsize", "16M",
		"-g", fmt.Sprintf("%d", fps*2), // GOP size
		"-keyint_min", fmt.Sprintf("%d", fps),
		"-pix_fmt", "yuv420p",
		"-f", "h264",
		"-an", // No audio
		"pipe:1",
	}

	// Create command with context
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("[Session %s] Error creating stdout pipe: %v", sessionID, err)
		return
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		log.Printf("[Session %s] Error creating stderr pipe: %v", sessionID, err)
		return
	}

	// Start FFmpeg
	if err := cmd.Start(); err != nil {
		log.Printf("[Session %s] Error starting FFmpeg: %v", sessionID, err)
		return
	}

	log.Printf("[Session %s] FFmpeg started successfully (PID: %d)", sessionID, cmd.Process.Pid)

	// Update session with FFmpeg command
	updateSessionFFmpeg(sessionID, cmd)

	// FFmpeg logging goroutine
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
				line := scanner.Text()
				if len(line) > 0 && !bytes.Contains([]byte(line), []byte("frame=")) {
					log.Printf("[Session %s] FFMPEG: %s", sessionID, line)
				}
			}
		}
	}()

	// Video processing loop
	const bufferSize = 1024 * 1024 // 1MB buffer
	scanner := bufio.NewScanner(stdout)
	buffer := make([]byte, bufferSize)
	scanner.Buffer(buffer, bufferSize*4)
	scanner.Split(scanNALUs)

	frameDuration := time.Second / time.Duration(fps)
	lastFrameTime := time.Now()
	frameCount := 0

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Session %s] Context canceled, stopping FFmpeg", sessionID)
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			cmd.Wait()
			return

		default:
			if scanner.Scan() {
				nalu := scanner.Bytes()
				if len(nalu) > 4 {
					now := time.Now()

					// Frame rate control
					if now.Sub(lastFrameTime) >= frameDuration {
						// Ensure NALU has start code
						var naluWithStart []byte
						if !bytes.HasPrefix(nalu, []byte{0x00, 0x00, 0x00, 0x01}) &&
							!bytes.HasPrefix(nalu, []byte{0x00, 0x00, 0x01}) {
							naluWithStart = append([]byte{0x00, 0x00, 0x00, 0x01}, nalu...)
						} else {
							naluWithStart = make([]byte, len(nalu))
							copy(naluWithStart, nalu)
						}

						err := track.WriteSample(media.Sample{
							Data:     naluWithStart,
							Duration: frameDuration,
						})

						atomic.AddInt64(&framesProcessed, 1)
						frameCount++

						if err != nil {
							atomic.AddInt64(&framesDropped, 1)
							if frameCount%100 == 0 { // Log every 100th error
								log.Printf("[Session %s] Error writing sample: %v", sessionID, err)
							}
						}

						lastFrameTime = now

						// Log progress every 5 seconds
						if frameCount%300 == 0 {
							log.Printf("[Session %s] Frames processed: %d", sessionID, frameCount)
						}
					}
				}
			} else {
				if err := scanner.Err(); err != nil {
					log.Printf("[Session %s] Scanner error: %v", sessionID, err)
				}

				// Check if FFmpeg process is still running
				if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
					log.Printf("[Session %s] FFmpeg process exited", sessionID)
				}
				return
			}
		}
	}
}

// Session management functions
func generateSessionID() string {
	return fmt.Sprintf("session_%d", time.Now().UnixNano())
}

func registerSession(session *StreamSession) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	sessions[session.ID] = session
	log.Printf("[Session %s] Session registered. Total: %d", session.ID, len(sessions))
}

func unregisterSession(sessionID string) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	if session, exists := sessions[sessionID]; exists {
		session.mutex.Lock()
		if session.FFmpegCmd != nil && session.FFmpegCmd.Process != nil {
			session.FFmpegCmd.Process.Kill()
		}
		session.mutex.Unlock()

		delete(sessions, sessionID)
		log.Printf("[Session %s] Session removed. Total: %d", sessionID, len(sessions))
	}
}

func updateSessionFFmpeg(sessionID string, cmd *exec.Cmd) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	if session, exists := sessions[sessionID]; exists {
		session.mutex.Lock()
		session.FFmpegCmd = cmd
		session.mutex.Unlock()
	}
}

func cleanupStaleSessions() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sessionsLock.Lock()
		now := time.Now()
		for id, session := range sessions {
			// Remove sessions inactive for more than 10 minutes
			if now.Sub(session.StartTime) > 10*time.Minute {
				state := session.PC.ConnectionState()
				if state == webrtc.PeerConnectionStateDisconnected ||
					state == webrtc.PeerConnectionStateFailed ||
					state == webrtc.PeerConnectionStateClosed {

					session.Cancel()
					session.mutex.RLock()
					if session.FFmpegCmd != nil && session.FFmpegCmd.Process != nil {
						session.FFmpegCmd.Process.Kill()
					}
					session.mutex.RUnlock()

					delete(sessions, id)
					log.Printf("[Session %s] Stale session removed", id)
				}
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
		session.mutex.RLock()
		if session.FFmpegCmd != nil && session.FFmpegCmd.Process != nil {
			session.FFmpegCmd.Process.Kill()
		}
		session.mutex.RUnlock()

		if session.PC != nil && session.PC.ConnectionState() != webrtc.PeerConnectionStateClosed {
			session.PC.Close()
		}
		delete(sessions, id)
	}
	log.Printf("All sessions terminated. Total: %d", len(sessions))
}

// Monitoring and metrics
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

		log.Printf("📊 Metrics: ActiveStreams=%d, FramesProcessed=%d, FramesDropped=%d, DropRate=%.2f%%",
			active, processed, dropped, dropRate)
	}
}

// HTTP handlers for monitoring
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
		session.mutex.RLock()
		hasFFmpeg := session.FFmpegCmd != nil
		session.mutex.RUnlock()

		info := map[string]interface{}{
			"id":         id,
			"start_time": session.StartTime.Format(time.RFC3339),
			"duration":   time.Since(session.StartTime).String(),
			"state":      session.PC.ConnectionState().String(),
			"has_ffmpeg": hasFFmpeg,
		}
		sessionInfo = append(sessionInfo, info)
	}

	response := map[string]interface{}{
		"total_sessions": len(sessions),
		"sessions":       sessionInfo,
		"timestamp":      time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
