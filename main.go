package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gosimple/slug"
	"golang.org/x/net/html"
)

func urlToFilename(url string) string {
	slug := slug.Make(url)
	if len(slug) > 200 {
		slug = slug[:200]
	}
	return fmt.Sprintf("%s.txt", slug)
}

func visit(client *http.Client, url string, outdir string, minLength int, parsedfile io.StringWriter) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("R")
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.77 Safari/537.36")
	r, err := client.Do(req)
	if e, ok := err.(net.Error); ok && e.Timeout() {
		// This was a timeout
		fmt.Printf("T")
		return
	} else if err != nil {
		// This was an error, but not a timeout
		fmt.Printf("F")
		return
	}
	if r != nil {
		defer r.Body.Close()
	} else {
		fmt.Printf("E")
		return
	}

	if r.StatusCode >= 200 && r.StatusCode <= 299 {
	} else if r.StatusCode == 404 {
		fmt.Printf("4")
	} else {
		fmt.Printf("S")
		return
	}
	filename := urlToFilename(url)
	full_filename := fmt.Sprintf("%s/%s", outdir, filename)
	out, err := os.Create(full_filename)
	if err != nil {
		fmt.Printf("C")
		return
	}
	defer out.Close()
	outstring := parse(r.Body, minLength)
	parsedfile.WriteString(fmt.Sprintf("%s\n", filename))
	out.WriteString(outstring)
	fmt.Printf(".")
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		fmt.Printf("Command is incomplete please choose `download`\n")
		os.Exit(1)
	}
	command := args[0]
	switch command {
	case "download":
		download()
		return
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func parse(file io.Reader, minLength int) string {
	var outstring strings.Builder

	z := html.NewTokenizer(file)
	inBody := false
	inStyleScript := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				fmt.Printf("Error parsing\n")
			}
			break
		}
		switch tt {
		case html.StartTagToken:
			tag, _ := z.TagName()
			tagName := strings.ToLower(string(tag))
			switch tagName {
			case "body":
				inBody = true
			case "style":
				fallthrough
			case "script":
				fallthrough
			case "noscript":
				inStyleScript = true
				continue
			default:
				// fmt.Printf("Found reading %s, %s, %s\n", tagName, filename, z.Text())
			}
		case html.TextToken:
			if inBody && !inStyleScript {
				text := strings.TrimSpace(string(z.Text()))
				if len(text) > minLength && !strings.Contains(text, "/") {
					// fmt.Printf("%s\n", text)
					outstring.WriteString(text)
					outstring.WriteString("\n")
				}
			}
		case html.EndTagToken:
			tag, _ := z.TagName()
			tagName := strings.ToLower(string(tag))
			switch tagName {
			case "body":
				inBody = false
			case "style":
				fallthrough
			case "script":
				fallthrough
			case "noscript":
				inStyleScript = false
				continue
			}
		}
	}
	if outstring.Len() == 0 {
		fmt.Printf("E")
	} else {
		fmt.Printf(".")
	}
	return outstring.String()
}

func download() {
	fs := flag.NewFlagSet("Download", flag.ExitOnError)
	pmaxDownloads := fs.Int("max-concurrent-downloads", 20, "Don't open more coroutines/downloads than this")
	poutdir := fs.String("outdir", "parsed", "Output directory with all the files (will be huge)")
	pinfile := fs.String("infile", "urls.txt", "The file containing the URLs to parse")
	pcheckfile := fs.String("checkfile", "scraped.txt", "The file containing which urls have been done")
	pparsedfile := fs.String("parsedfile", "parsed.txt", "The file containing which urls have been extracted")
	ptimeout := fs.Int("timeout", 30, "Timeout after which we consider request failed")
	pminLength := fs.Int("min-length", 100, "Minimum length of text to be extracted (removes links and button nodes)")

	fs.Parse(flag.Args()[1:])

	maxDownloads := *pmaxDownloads
	outdir := *poutdir
	infile := *pinfile
	checkfile := *pcheckfile
	parsedfile := *pparsedfile
	timeout := time.Duration(*ptimeout)
	minLength := *pminLength

	openCoroutines := make(chan bool, maxDownloads)

	for i := 0; i < maxDownloads; i++ {
		openCoroutines <- true
	}

	os.MkdirAll(outdir, 0755)

	fileHandle, err := os.Open(infile)
	if err != nil {
		log.Fatalf("Can't open %s : %v", infile, err)
	}
	defer fileHandle.Close()
	fileScanner := bufio.NewScanner(fileHandle)

	n := 0
	f, err := os.Open(checkfile)
	if err != nil {
		// Can't read file
	} else {
		checkScanner := bufio.NewScanner(f)
		for checkScanner.Scan() {
			fileScanner.Scan()
			url := strings.TrimSpace(fileScanner.Text())
			checkUrl := strings.TrimSpace(checkScanner.Text())
			n += 1
			if url != checkUrl {
				log.Fatalf("Check file seems to be corrupted %v != %v", url, checkUrl)
			}
		}
	}
	f.Close()
	fmt.Printf("Skipped %v already checked urls", n)

	f, err = os.OpenFile(checkfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Can't open %s : %v", checkfile, err)
	}
	pf, err := os.OpenFile(parsedfile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Can't open %s : %v", parsedfile, err)
	}

	tr := &http.Transport{
		IdleConnTimeout: timeout * time.Second,
	}
	client := &http.Client{Transport: tr}

	var wg sync.WaitGroup
	i := 0
	start := time.Now()
	last := start
	for fileScanner.Scan() {
		url := strings.TrimSpace(fileScanner.Text())
		f.WriteString(fmt.Sprintf("%v\n", url))
		full_filename := fmt.Sprintf("%s/%s", outdir, urlToFilename(url))
		if i%1000 == 0 {
			fmt.Printf("\nScanned %v urls in %v (total : %v)\n", i, time.Since(last), time.Since(start))
			last = time.Now()
		}
		i += 1
		if _, err := os.Stat(full_filename); err == nil {
			// File exists
		} else {
			wg.Add(1)
			b := <-openCoroutines
			go func() {
				defer func() {
					wg.Done()
					openCoroutines <- b
				}()

				visit(client, url, outdir, minLength, pf)
			}()
		}
	}
	wg.Wait()
}
