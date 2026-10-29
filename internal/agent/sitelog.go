package agent

import (
	"bytes"
	"errors"
	"io"
)

// siteLogTail is how much of the end of a log file is returned.
const siteLogTail = 256 << 10

// readTail returns up to limit bytes from the end of a file of the given
// size, starting at a line boundary, and whether earlier content was left out.
func readTail(f io.ReaderAt, size, limit int64) (string, bool, error) {
	start := max(0, size-limit)
	// One byte early, to tell whether start is already a line boundary.
	off := max(0, start-1)
	buf := make([]byte, size-off)
	n, err := f.ReadAt(buf, off)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	buf = buf[:n]
	if start > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return "", true, nil
		}
		buf = buf[i+1:]
	}
	return string(buf), start > 0, nil
}
