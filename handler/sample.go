package handler

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

type Sample struct {
	Username string
	Class    string
	Domains  []string
	Message  *[]byte
}

func NewSample(class, username string, domains []string, message *[]byte) *Sample {
	var sample Sample
	sample.Class = class
	sample.Username = username
	sample.Domains = domains
	sample.Message = message
	return &sample
}

func (s *Sample) Submit() {
	if Verbose {
		log.Printf("Submitting %s %s domains=%v\n", s.Username, s.Class, s.Domains)
	}
	for _, domain := range s.Domains {
		args := []string{"-d", fmt.Sprintf("%s@%s", s.Username, domain)}
		if Verbose {
			args = append(args, "-v")
		}
		args = append(args, "learn_"+s.Class)
		if Verbose {
			log.Printf("cmd=rspamc %s\n", strings.Join(args, " "))
		}
		cmd := exec.Command("rspamc", args...)
		cmd.Stdin = bytes.NewBuffer(*s.Message)
		var oBuf bytes.Buffer
		var eBuf bytes.Buffer
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
