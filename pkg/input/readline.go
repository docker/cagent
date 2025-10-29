package input

import (
	"bufio"
	"context"
	"io"
)

func ReadLine(ctx context.Context, rd io.Reader) (string, error) {
	lines := make(chan string)
	errs := make(chan error)

	go func() {
		defer close(lines)
		defer close(errs)

		reader := bufio.NewReader(rd)
		line, err := reader.ReadString('\n')
		if err != nil {
			errs <- err
		} else {
			lines <- line
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-errs:
		return "", err
	case line := <-lines:
		return line, nil
	}
}
