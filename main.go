package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/rstms/mctsd/handler"
	"github.com/sevlyar/go-daemon"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const SHUTDOWN_TIMEOUT = 5
const QUEUE_SIZE = 256 * 1024
const DEFAULT_PORT = 2015

const Version = "0.2.1"

var verbose bool

type Response struct {
	Message string `json:"message"`
}

var (
	signalFlag = flag.String("s", "", `send signal:
    stop - shutdown
    reload - reload config
    `)
	shutdown = make(chan struct{})
	reload   = make(chan struct{})
)

func Banner() string {
	return fmt.Sprintf("mctsd v%s", Version)
}

func runServer(addr *string, port *int) {

	err := handler.Init(QUEUE_SIZE)
	if err != nil {
		log.Fatalln("Handler.Init failed: ", err)
	}
	var wg sync.WaitGroup
	listen := fmt.Sprintf("%s:%d", *addr, *port)
	server := &http.Server{
		Addr:    listen,
		Handler: http.HandlerFunc(handler.HandleEndpoints),
	}

	go func() {
		wg.Add(1)
		defer wg.Done()
		for job := range handler.Queue {
			handler.DequeueCount++
			if verbose {
				log.Printf("Dequeued %s %s sample: queueCount=%d dequeueCount=%d\n", job.Username, job.Class, handler.QueueCount, handler.DequeueCount)
			}
			job.Submit()
		}
	}()

	go func() {
		log.Printf("%s started as PID %d listening on %s\n", Banner(), os.Getpid(), listen)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalln("ListenAndServe failed: ", err)
		}
	}()

	<-shutdown

	log.Println("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), SHUTDOWN_TIMEOUT*time.Second)
	defer cancel()

	err = server.Shutdown(ctx)
	if err != nil {
		log.Fatalln("Server Shutdown failed: ", err)
	}
	log.Println("shutdown complete")

	log.Printf("queueCount=%d dequeueCount=%d\n", handler.QueueCount, handler.DequeueCount)
	if handler.QueueCount != handler.DequeueCount {
		log.Println("draining submission queue...")
	}
	close(handler.Queue)
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
	versionFlag := flag.Bool("version", false, "show version and exit")
	flag.Parse()
	if *versionFlag {
		fmt.Printf("%s\n", Banner())
		os.Exit(0)
	}
	if *verboseFlag {
		verbose = true
		handler.Verbose = true
	}
	if *debugFlag {
		handler.Debug = true
	} else {
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
