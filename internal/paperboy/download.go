package paperboy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func downloadFile(ctx context.Context, timeout time.Duration, sourceURL, destPath string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return 0, fmt.Errorf("create download request: %w", err)
	}
	req.Header.Set("User-Agent", "paperboy/0.1 (+https://github.com/pablasso/lucio)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("download PDF: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		msg := strings.TrimSpace(string(body))
		if msg != "" {
			return 0, fmt.Errorf("download PDF: %s: %s", resp.Status, msg)
		}
		return 0, fmt.Errorf("download PDF: %s", resp.Status)
	}

	if resp.ContentLength > maxPDFBytes {
		return 0, fmt.Errorf("PDF download exceeds 32 MiB")
	}
	reader := bufio.NewReader(io.LimitReader(resp.Body, maxPDFBytes+1))
	header, err := reader.Peek(5)
	if err != nil || !bytes.Equal(header, []byte("%PDF-")) {
		return 0, fmt.Errorf("download is not a PDF")
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
		return 0, fmt.Errorf("create destination dir: %w", err)
	}
	out, err := os.CreateTemp(filepath.Dir(destPath), ".paperboy-download-*")
	if err != nil {
		return 0, fmt.Errorf("create temporary PDF: %w", err)
	}
	tmpPath := out.Name()
	defer os.Remove(tmpPath)
	n, copyErr := io.Copy(out, reader)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return n, fmt.Errorf("write %s: %w", tmpPath, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return n, fmt.Errorf("close %s: %w", tmpPath, closeErr)
	}
	if n > maxPDFBytes {
		return n, fmt.Errorf("PDF download exceeds 32 MiB")
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return n, fmt.Errorf("rename %s to %s: %w", tmpPath, destPath, err)
	}
	return n, nil
}
