package main

import (
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// O nome do arquivo .m3u8 para verificar se está pronto
const manifestFileName = "hls/stream.m3u8"

func main() {
	// Inicia o FFmpeg em uma goroutine separada
	go startFFmpeg()

	// Espera até que o arquivo de manifesto HLS seja criado
	log.Println("Aguardando o FFmpeg criar o arquivo de manifesto HLS...")
	for {
		if _, err := os.Stat(manifestFileName); err == nil {
			log.Println("Manifesto encontrado! Iniciando o servidor web...")
			break
		}
		time.Sleep(500 * time.Millisecond) // Espera 500ms antes de tentar novamente
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		htmlContent, err := os.ReadFile("index.html")
		if err != nil {
			log.Printf("Erro ao ler index.html: %v", err)
			http.Error(w, "Erro ao carregar a página", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(htmlContent)
	})

	http.HandleFunc("/hls/", func(w http.ResponseWriter, r *http.Request) {
		filePath := filepath.Join("hls", r.URL.Path[len("/hls/"):])
		var contentType string
		switch filepath.Ext(filePath) {
		case ".m3u8":
			contentType = "application/x-mpegURL"
		case ".ts":
			contentType = "video/mp2t"
		default:
			contentType = "application/octet-stream"
		}
		log.Printf("Servindo %s com Content-Type: %s", filePath, contentType)
		w.Header().Set("Content-Type", contentType)
		http.ServeFile(w, r, filePath)
	})

	log.Println("Servidor iniciado na porta 8080. Acesse http://172.26.1.103:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// startFFmpeg (sem mudanças)
func startFFmpeg() {
	os.RemoveAll("hls")
	os.Mkdir("hls", 0755)

	log.Println("Iniciando o jogo...")
	gamePath := "C:/Program Files (x86)/Steam/steamapps/common/Salt and Sanctuary/salt.exe"
	cmdGame := exec.Command(gamePath)
	if err := cmdGame.Start(); err != nil {
		log.Fatalf("Erro ao iniciar o jogo: %v", err)
	}

	log.Println("Esperando 3 segundos para o jogo carregar...")
	time.Sleep(3 * time.Second)

	log.Println("Iniciando FFmpeg para gerar o stream HLS...")
	cmdFFmpeg := exec.Command("ffmpeg",
		"-f", "dshow",
		"-i", "video=screen-capture-recorder",
		"-tune", "zerolatency",
		"-c:v", "h264_qsv",
		"-g", "1", // Keyframe em todo frame
		//"-bf", "0", // Sem B-frames
		"-f", "hls",
		"-hls_list_size", "6",
		"-hls_time", "0.3",
		"hls/stream.m3u8")

	stderr, err := cmdFFmpeg.StderrPipe()
	if err != nil {
		log.Fatalf("Erro ao obter o pipe de erro do FFmpeg: %v", err)
	}
	if err := cmdFFmpeg.Start(); err != nil {
		log.Fatalf("Erro ao iniciar o FFmpeg: %v", err)
	}

	go func() {
		log.SetPrefix("[FFmpeg ERR] ")
		buf := make([]byte, 1024)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				log.Printf("%s", buf[:n])
			}
			if err != nil {
				log.Printf("Pipe de erro do FFmpeg fechado: %v", err)
				return
			}
		}
	}()

	cmdFFmpeg.Wait()
}
