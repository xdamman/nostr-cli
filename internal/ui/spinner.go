package ui

import (
	"fmt"
	"sync"
	"time"
)

// SpinnerFrames are the braille animation frames used by the spinner.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

var frames = SpinnerFrames

// Spinner displays an animated spinner with a message.
type Spinner struct {
	mu      sync.Mutex
	msg     string
	stop    chan struct{}
	stopped chan struct{}
}

// NewSpinner creates and starts a spinner with the given message.
func NewSpinner(msg string) *Spinner {
	s := &Spinner{
		msg:     msg,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer close(s.stopped)
	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			fmt.Printf("\r\033[K%s %s", frames[i%len(frames)], msg)
			i++
		}
	}
}

// Stop stops the spinner and clears the line.
func (s *Spinner) Stop() {
	close(s.stop)
	<-s.stopped
	fmt.Print("\r\033[K")
}
