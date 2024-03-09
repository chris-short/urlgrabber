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

func extractURLs(filePath string, wg *sync.WaitGroup, urlChan chan<- string, workerPool chan struct{}) {
	defer wg.Done()

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Printf("Error opening file %s: %v\n", filePath, err)
		return
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		fmt.Printf("Error getting file info for %s: %v\n", filePath, err)
		return
	}

	if fileInfo.Size() < int64(maxMemoryFileSize) {
		workerPool <- struct{}{}
		// Cache the file in memory if it's small enough
		content, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("Error reading file %s: %v\n", filePath, err)
			return
		}

		// Process the cached content
		for _, line := range strings.Split(string(content), ",") {
			line = strings.TrimSpace(line)
			if isValidURL(line) {
				urlChan <- line
			}
		}
		<-workerPool
	} else {
		// Process the file line by line for large files
		scanner := bufio.NewScanner(file)

		for scanner.Scan() {
			workerPool <- struct{}{}
			line := scanner.Text()
			for _, part := range strings.Split(line, ",") {
				part = strings.TrimSpace(part)
				if isValidURL(part) {
					urlChan <- part
				}
			}
			<-workerPool
		}

		if err := scanner.Err(); err != nil {
			fmt.Printf("Error reading file %s: %v\n", filePath, err)
			return
		}
	}
}

func main() {
	flag.Parse()

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go [--timeout DURATION | -t DURATION] <file1> <file2> ...")
		flag.PrintDefaults()
		return
	}

	urlChan := make(chan string)
	workerPool := make(chan struct{}, 100) // Adjust the number of concurrent workers as needed
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
			fmt.Printf("Error walking path %s: %v\n", filePath, err)
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
