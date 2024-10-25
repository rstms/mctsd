package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"github.com/sevlyar/go-daemon"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const SHUTDOWN_TIMEOUT = 5
const QUEUE_SIZE = 256 * 1024
const DEFAULT_PORT = 2015

const Version = "0.1.4"

var verbose bool

type Response struct {
	Message string `json:"message"`
}

type Sample struct {
	username string
	class    string
	buf      *bytes.Buffer
}

func NewSample(class, username string) *Sample {
	var sample Sample
	sample.class = class
	sample.username = username
	var buf bytes.Buffer
	sample.buf = &buf
	return &sample
}

func (s *Sample) Submit() {
	if verbose {
		log.Printf("Submitting %s %s", s.username, s.class)
	}
	cmd := exec.Command("rspamc", "-u", s.username, "learn_"+s.class)
	var oBuf bytes.Buffer
	var eBuf bytes.Buffer
	cmd.Stdin = s.buf
	cmd.Stdout = &oBuf
	cmd.Stderr = &eBuf
	exitCode := -1
	err := cmd.Run()
	if err != nil {
		switch e := err.(type) {
		case *exec.ExitError:
			exitCode = e.ExitCode()
		default:
			log.Printf("rspamc error: %v", err)
			return
		}
	} else {
		exitCode = cmd.ProcessState.ExitCode()
	}
	if exitCode != 0 {
		log.Printf("rspamc exited: %d", exitCode)
	}
	stderr := eBuf.String()
	if stderr != "" {
		log.Printf("rspamc stderr: %s", stderr)
	}
	stdout := oBuf.String()
	if stdout != "" {
		log.Printf("rspamc stdout: %s", stdout)
	}

	// debugging delay
	//time.Sleep(1 * time.Second)

}

var (
	signalFlag = flag.String("s", "", `send signal:
    stop - shutdown
    reload - reload config
    `)
	shutdown = make(chan struct{})
	reload   = make(chan struct{})
)

func fail(w http.ResponseWriter, message string, status int) {
	log.Printf("  [%d] %s", status, message)
	http.Error(w, message, status)
}

var queue chan *Sample
var queueCount int
var dequeueCount int

func handleEndpoints(w http.ResponseWriter, r *http.Request) {

	if verbose {
		log.Printf("%s %s %s (%d)\n", r.RemoteAddr, r.Method, r.RequestURI, r.ContentLength)
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

			usernameHeader, ok := r.Header["X-Client-Cert-Dn"]
			if !ok {
				fail(w, "missing client cert DN", http.StatusBadRequest)
				return
			}
			if verbose {
				log.Printf("client cert dn: %s\n", usernameHeader[0])
			}
			if usernameHeader[0] != "DN="+username {
				fail(w, fmt.Sprintf("client cert (%s) != path username (%s)", usernameHeader[0], username), http.StatusBadRequest)
				return
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
			_, err = io.Copy(sample.buf, uploadFile)
			if err != nil {
				fail(w, err.Error(), http.StatusBadRequest)
				return
			}

			queue <- sample
			queueCount++
			if verbose {
				log.Printf("queued %s %s sample: queueCount=%d dequeCount=%d\n", username, class, queueCount, dequeueCount)
			}

			return
		}
	default:
		fail(w, "error", http.StatusMethodNotAllowed)
		return

	}
	fail(w, "WAT?", http.StatusNotFound)

}

func runServer(addr *string, port *int) {

	queue = make(chan *Sample, QUEUE_SIZE)
	var wg sync.WaitGroup
	listen := fmt.Sprintf("%s:%d", *addr, *port)
	server := &http.Server{
		Addr:    listen,
		Handler: http.HandlerFunc(handleEndpoints),
	}

	go func() {
		wg.Add(1)
		defer wg.Done()
		for job := range queue {
			dequeueCount++
			if verbose {
				log.Printf("Dequeued %s %s sample: queueCount=%d dequeueCount=%d\n", job.username, job.class, queueCount, dequeueCount)
			}
			job.Submit()
		}
	}()

	go func() {
		log.Printf("mctsd v%s started as PID %d listening on %s\n", Version, os.Getpid(), listen)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalln("ListenAndServe failed: ", err)
		}
	}()

	<-shutdown

	log.Println("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), SHUTDOWN_TIMEOUT*time.Second)
	defer cancel()

	err := server.Shutdown(ctx)
	if err != nil {
		log.Fatalln("Server Shutdown failed: ", err)
	}
	log.Println("shutdown complete")

	log.Printf("queueCount=%d dequeueCount=%d\n", queueCount, dequeueCount)
	if queueCount != dequeueCount {
		log.Println("draining submission queue...")
	}
	close(queue)
	wg.Wait()
	log.Println("submissions completed.")
}

func stopHandler(sig os.Signal) error {
	log.Println("received stop signal")
	shutdown <- struct{}{}
	return daemon.ErrStop
}

func reloadHandler(sig os.Signal) error {
	log.Println("received reload signal")
	return nil
}

func main() {
	addr := flag.String("addr", "127.0.0.1", "listen address")
	port := flag.Int("port", DEFAULT_PORT, "listen port")
	verboseFlag := flag.Bool("verbose", false, "write non-error output to log")
	debugFlag := flag.Bool("debug", false, "run in foreground mode")
	flag.Parse()
	verbose = *verboseFlag
	if !*debugFlag {
		daemonize(addr, port)
		os.Exit(0)
	}
	go runServer(addr, port)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	<-sigs
	shutdown <- struct{}{}
	os.Exit(0)
}

func daemonize(addr *string, port *int) {

	daemon.AddCommand(daemon.StringFlag(signalFlag, "stop"), syscall.SIGTERM, stopHandler)
	daemon.AddCommand(daemon.StringFlag(signalFlag, "reload"), syscall.SIGHUP, reloadHandler)

	ctx := &daemon.Context{
		LogFileName: "/var/log/mctsd.log",
		LogFilePerm: 0600,
		WorkDir:     "/",
		Umask:       007,
	}

	if len(daemon.ActiveFlags()) > 0 {
		d, err := ctx.Search()
		if err != nil {
			log.Fatalln("Unable to signal daemon: ", err)
		}
		daemon.SendCommands(d)
		return
	}

	child, err := ctx.Reborn()
	if err != nil {
		log.Fatalln("Fork failed: ", err)
	}

	if child != nil {
		return
	}
	defer ctx.Release()

	go runServer(addr, port)

	err = daemon.ServeSignals()
	if err != nil {
		log.Fatalln("Error: ServeSignals: ", err)
	}
}
