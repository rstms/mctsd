package handler

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

func fail(w http.ResponseWriter, message string, status int) {
	log.Printf("  [%d] %s", status, message)
	http.Error(w, message, status)
}

var Verbose bool
var Debug bool

var Queue chan *Sample
var QueueCount int
var DequeueCount int

func Init(queueSize int) error {
	Queue = make(chan *Sample, queueSize)
	return nil
}

func HandleEndpoints(w http.ResponseWriter, r *http.Request) {

	if Verbose {
		log.Printf("%s %s %s (%d) debug=%v\n", r.RemoteAddr, r.Method, r.RequestURI, r.ContentLength, Debug)
	}
	switch r.Method {
	case "POST":
		if strings.HasPrefix(r.URL.Path, "/learn/") {
			path := strings.Split(r.URL.Path[7:], "/")
			if len(path) != 2 {
				fail(w, "invalid path", http.StatusBadRequest)
				return
			}
			class := path[0]
			username := path[1]
			if class != "ham" && class != "spam" {
				fail(w, "unknown class", http.StatusBadRequest)
				return
			}
			if len(username) < 1 {
				fail(w, "invalid user", http.StatusBadRequest)
				return
			}

			if !Debug {
				usernameHeader, ok := r.Header["X-Client-Cert-Dn"]
				if !ok {
					fail(w, "missing client cert DN", http.StatusBadRequest)
					return
				}
				if Verbose {
					log.Printf("client cert dn: %s\n", usernameHeader[0])
				}
				if usernameHeader[0] != "CN="+username {
					fail(w, fmt.Sprintf("client cert (%s) != path username (%s)", usernameHeader[0], username), http.StatusBadRequest)
					return
				}
			}
			err := r.ParseMultipartForm(256 << 20) // limit file size to 256MB
			if err != nil {
				fail(w, fmt.Sprintf("failed parsing upload form: %v", err), http.StatusBadRequest)
				return
			}

			uploadFile, _, err := r.FormFile("file")
			if err != nil {
				fail(w, fmt.Sprintf("failed retreiving upload file: %v", err), http.StatusBadRequest)
				return
			}
			defer uploadFile.Close()

			sample := NewSample(class, username)
			_, err = io.Copy(sample.Buf, uploadFile)
			if err != nil {
				fail(w, err.Error(), http.StatusBadRequest)
				return
			}

			Queue <- sample
			QueueCount++
			if Verbose {
				log.Printf("queued %s %s sample: queueCount=%d dequeCount=%d\n", username, class, QueueCount, DequeueCount)
			}

			return
		}
	default:
		fail(w, "error", http.StatusMethodNotAllowed)
		return

	}
	fail(w, "WAT?", http.StatusNotFound)

}
