package proto

import (
	"io"
	"sync"
)

// Proxy copies data bidirectionally between a and b until one side closes
// or hits an error. Returns the first error from either direction.
func Proxy(a, b io.ReadWriteCloser) error {
	var once sync.Once
	var firstErr error
	done := make(chan struct{})

	record := func(err error) {
		once.Do(func() {
			if err != nil && err != io.EOF {
				firstErr = err
			}
		})
	}

	go func() {
		_, err := io.Copy(b, a)
		record(err)
		b.Close()
		close(done)
	}()

	_, err := io.Copy(a, b)
	record(err)
	a.Close()
	<-done

	return firstErr
}
