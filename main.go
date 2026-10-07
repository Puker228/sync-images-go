package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	dirName  = "./files"
	apiURL   = "http://localhost:8000/api/v1/buckets/interevm/files"
	workers  = 8
	retries  = 3
	doneFile = "uploaded.log"
)

var client = &http.Client{
	Timeout: 2 * time.Minute,
	Transport: &http.Transport{
		MaxIdleConns:        workers,
		MaxIdleConnsPerHost: workers,
		IdleConnTimeout:     90 * time.Second,
	},
}

type httpError struct {
	code int
	msg  string
}

func (e *httpError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.code, e.msg) }

func isImage(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".heic":
		return true
	}
	return false
}

func upload(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, f); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	resp, err := client.Post(apiURL, w.FormDataContentType(), &body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	io.Copy(io.Discard, resp.Body) // чтобы соединение вернулось в пул

	if resp.StatusCode/100 != 2 {
		return &httpError{resp.StatusCode, strings.TrimSpace(string(msg))}
	}
	return nil
}

func uploadWithRetry(path string) error {
	var err error
	for i := range retries {
		if err = upload(path); err == nil {
			return nil
		}
		var he *httpError
		if errors.As(err, &he) && he.code < 500 && he.code != http.StatusTooManyRequests {
			return err
		}
		time.Sleep(time.Duration(1<<i) * time.Second)
	}
	return err
}

func loadDone() map[string]bool {
	done := map[string]bool{}
	data, err := os.ReadFile(doneFile)
	if err != nil {
		return done
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			done[line] = true
		}
	}
	return done
}

func main() {
	done := loadDone()
	logF, err := os.OpenFile(doneFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	defer logF.Close()

	entries, err := os.ReadDir(dirName)
	if err != nil {
		log.Fatal(err)
	}

	jobs := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok, failed atomic.Int64

	for range workers {
		wg.Go(func() {
			for name := range jobs {
				if err := uploadWithRetry(filepath.Join(dirName, name)); err != nil {
					failed.Add(1)
					log.Printf("FAIL %s: %v", name, err)
					continue
				}

				mu.Lock()
				fmt.Fprintln(logF, name)
				mu.Unlock()

				if n := ok.Add(1); n%500 == 0 {
					log.Printf("uploaded %d", n)
				}
			}
		})
	}

	for _, e := range entries {
		if e.IsDir() || !isImage(e.Name()) || done[e.Name()] {
			continue
		}
		jobs <- e.Name()
	}
	close(jobs)
	wg.Wait()

	log.Printf("done: ok=%d failed=%d skipped(already uploaded)=%d",
		ok.Load(), failed.Load(), len(done))
}
