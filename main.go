package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gocolly/colly"
	"github.com/gosimple/slug"
)

func urlToFilename(url string) string {
	slug := slug.Make(url)
	if len(slug) > 200 {
		slug = slug[:200]
	}
	return fmt.Sprintf("%s.txt", slug)
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		flag.Usage()
		fmt.Printf("Command is incomplete please choose `download`\n")
		os.Exit(1)
	}
	command := args[0]
	switch command {
	case "download":
		download()
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func download() {
	fs := flag.NewFlagSet("Download", flag.ExitOnError)
	maxOpenFiles := *fs.Int("max-open-files", 1000, "Don't open more files than this")
	chunkSize := *fs.Int("chunk-size", 10000, "Attempt to download that many URLs every timeout seconds")
	outdir := *fs.String("outdir", "scraped", "Output directory with all the files (will be huge)")
	infile := *fs.String("infile", "urls.txt", "The file containing the URLs to parse")
	timeout := time.Duration(*fs.Int("timeout", 30, "Timeout after which we move on next chunk."))

	openFiles := make(chan bool, maxOpenFiles)

	for i := 0; i < maxOpenFiles; i++ {
		openFiles <- true
	}
	os.MkdirAll(outdir, 0755)
	// Instantiate default collector
	c := colly.NewCollector()
	c.WithTransport(&http.Transport{
		DisableKeepAlives: true,
	})
	c.SetRequestTimeout(timeout * time.Second)

	c.OnScraped(func(r *colly.Response) {
		if len(r.Body) == 0 {
			fmt.Printf("X")
			return
		}
		url := r.Request.URL.String()
		filename := urlToFilename(url)
		full_filename := fmt.Sprintf("%s/%s", outdir, filename)
		b := <-openFiles
		err := r.Save(full_filename)
		if err != nil {
			// log.Printf("Error creating file ...%s\n", err)
			fmt.Printf("F")
		} else {
			fmt.Printf(".")
		}
		openFiles <- b

	})

	fileHandle, err := os.Open(infile)
	if err != nil {
		log.Fatalf("Can't open %s : %v", infile, err)
	}
	defer fileHandle.Close()
	fileScanner := bufio.NewScanner(fileHandle)

	n := 0
	for fileScanner.Scan() {
		// Add URLs to the queue
		url := strings.TrimSpace(fileScanner.Text())
		full_filename := fmt.Sprintf("%s/%s", outdir, urlToFilename(url))
		if _, err := os.Stat(full_filename); err == nil {
			// File exists
		} else {
			go c.Visit(url)
		}
		n += 1
		if n >= chunkSize {
			start := time.Now()
			fmt.Printf("\nStarting %v files to download \n", n)
			c.Wait()
			n = 0
			fmt.Printf("\n\tDONE in %s\n", time.Since(start))
		}
	}

}
