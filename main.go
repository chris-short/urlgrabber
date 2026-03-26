package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxMemoryFileSize = 1024 * 1024 * 1024 // 1 GB

var timeout time.Duration

func init() {
	flag.DurationVar(&timeout, "timeout", 10*time.Second, "Timeout duration for HTTP requests")
	flag.DurationVar(&timeout, "t", 10*time.Second, "Shorthand for --timeout")
}

func isValidURL(url string) bool {
	client := &http.Client{
		Timeout: timeout,
	}

	resp, err := client.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Check if the response is an image
	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "image/") {
		fmt.Fprintln(os.Stderr, "Response is an image")
		return false
	}

	// Accept any 2xx or 3xx response code
	statusCode := resp.StatusCode
	return statusCode >= 200 && statusCode < 400
}

func checkURLs(candidates []string, urlChan chan<- string, workerPool chan struct{}) {
	var wg sync.WaitGroup
	for _, u := range candidates {
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			continue
		}
		wg.Add(1)
		workerPool <- struct{}{}
		go func(u string) {
			defer wg.Done()
			defer func() { <-workerPool }()
			if isValidURL(u) {
				urlChan <- u
			}
		}(u)
	}
	wg.Wait()
}

func extractURLs(filePath string, wg *sync.WaitGroup, urlChan chan<- string, workerPool chan struct{}) {
	defer wg.Done()

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file %s: %v\n", filePath, err)
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting file info for %s: %v\n", filePath, err)
		return
	}

	if fileInfo.Size() < maxMemoryFileSize {
		content, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", filePath, err)
			return
		}
		var candidates []string
		for _, line := range strings.Split(string(content), "\n") {
			for _, part := range strings.Split(line, ",") {
				candidates = append(candidates, strings.TrimSpace(part))
			}
		}
		checkURLs(candidates, urlChan, workerPool)
	} else {
		// Process the file line by line for large files
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var candidates []string
			for _, part := range strings.Split(scanner.Text(), ",") {
				candidates = append(candidates, strings.TrimSpace(part))
			}
			checkURLs(candidates, urlChan, workerPool)
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "Error reading file %s: %v\n", filePath, err)
		}
	}
}

func main() {
	flag.Parse()

	if len(flag.Args()) == 0 {
		fmt.Println("Usage: urlgrabber [--timeout DURATION | -t DURATION] <file1> <file2> ...")
		flag.PrintDefaults()
		return
	}

	urlChan := make(chan string)
	workerPool := make(chan struct{}, 100)
	var wg sync.WaitGroup

	for _, filePath := range flag.Args() {
		err := filepath.Walk(filePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() {
				wg.Add(1)
				go extractURLs(path, &wg, urlChan, workerPool)
			}

			return nil
		})

		if err != nil {
			fmt.Fprintf(os.Stderr, "Error walking path %s: %v\n", filePath, err)
		}
	}

	go func() {
		wg.Wait()
		close(urlChan)
	}()

	for url := range urlChan {
		fmt.Println(url)
	}
}
