package handler

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
)

type Sample struct {
	Username string
	Class    string
	Domains  []string
	Buf      *bytes.Buffer
}

func NewSample(class, username string, domains []string) *Sample {
	var sample Sample
	sample.Class = class
	sample.Username = username
	sample.Domains = domains
	var buf bytes.Buffer
	sample.Buf = &buf
	return &sample
}

func (s *Sample) Submit() {
	if Verbose {
		log.Printf("Submitting %s %s", s.Username, s.Class)
	}
	for domain := range s.Domains {
		cmd := exec.Command("rspamc", "-d", fmt.Sprintf("%s@%s", s.Username, domain), "learn_"+s.Class)
		var oBuf bytes.Buffer
		var eBuf bytes.Buffer
		cmd.Stdin = s.Buf
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

	}
	// debugging delay
	//time.Sleep(1 * time.Second)

}
