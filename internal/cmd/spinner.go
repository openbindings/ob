package cmd

import (
	"fmt"
	"io"
	"sync"
	"time"
)

var spinnerFrames = []string{"⢎ ", "⠎⠁", "⠊⠑", "⠈⠱", " ⡱", "⢀⡰", "⢄⡠", "⢆⡀"}

// withSpinner runs fn while displaying an animated spinner with a label on w.
// The spinner is cleared when fn completes and the result is returned.
func withSpinner[T any](w io.Writer, label string, fn func() (T, error)) (T, error) {
	var (
		result T
		err    error
		wg     sync.WaitGroup
		done   = make(chan struct{})
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		result, err = fn()
		close(done)
	}()

	frame := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()

	fmt.Fprintf(w, "\r%s %s", spinnerFrames[frame], label)
	for {
		select {
		case <-done:
			fmt.Fprintf(w, "\r\033[K")
			wg.Wait()
			return result, err
		case <-ticker.C:
			frame = (frame + 1) % len(spinnerFrames)
			fmt.Fprintf(w, "\r%s %s", spinnerFrames[frame], label)
		}
	}
}
