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

func visit(client *http.Client, url string, outdir string) {
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
		fmt.Printf("C : %v\n", err)
		return
	}
	defer out.Close()
	io.Copy(out, r.Body)
	fmt.Printf(".")
}

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		fmt.Printf("Command is incomplete please choose `download` or `extract`\n")
		os.Exit(1)
	}
	command := args[0]
	switch command {
	case "download":
		download()
		return
	case "extract":
		extract()
		return
	default:
		flag.Usage()
		os.Exit(1)
	}
}

func extract() {
	fs := flag.NewFlagSet("Extract", flag.ExitOnError)
	plistfile := fs.String("listfile", "scraped.txt", "A file that contains all filenames (faster than scanning the directory). You can run `ls -1 > list.txt` for instance ")
	pdatadir := fs.String("datadir", "scraped", "Directory that contains the downloaded files")
	poutdir := fs.String("outdir", "parsed", "Directory that will contained cleaned text")
	poutfile := fs.String("outfile", "parsed.text", "A file the contains all filenames in `parsed` directory")
	pminLength := fs.Int("min-length", 100, "Minimum size of the strings to be captured")
	fs.Parse(flag.Args()[1:])

	listfile := *plistfile
	datadir := *pdatadir
	outdir := *poutdir
	outfile := *poutfile
	minLength := *pminLength

	os.MkdirAll(outdir, 0755)

	f, err := os.Open(listfile)
	if err != nil {
		log.Fatalf("Can't open list file %v, please check it exists", listfile)
	}
	defer f.Close()

	fileScanner := bufio.NewScanner(f)

	i := 0
	start := time.Now()
	last := start
	checkfile, err := os.Create(outfile)
	if err != nil {
		log.Fatalf("Can't open out file %v, please check it exists", listfile)
	}
	for fileScanner.Scan() {
		filename := strings.TrimSpace(fileScanner.Text())
		parseFile(datadir, filename, minLength, outdir)
		checkfile.WriteString(fmt.Sprintf("%s\n", filename))
		if i%1000 == 0 {
			fmt.Printf("\nScanned %v urls in %v (total : %v)\n", i, time.Since(last), time.Since(start))
			last = time.Now()
		}
		i += 1

	}

}

func parseFile(datadir string, filename string, minLength int, outdir string) {
	full_filename := fmt.Sprintf("%s/%s", datadir, filename)
	htmlfile, err := os.Open(full_filename)
	if err != nil {
		fmt.Printf("Failed to open %v\n", full_filename)
		return
	}
	defer htmlfile.Close()

	var outstring strings.Builder

	z := html.NewTokenizer(htmlfile)
	inBody := false
	inStyleScript := false
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				fmt.Printf("Error parsing %s\n", filename)
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
		// fmt.Printf("Ignored empty %v\n", filename)
		return
	}

	outfilename := fmt.Sprintf("%s/%s", outdir, filename)
	outfile, err := os.Create(outfilename)
	if err != nil {
		fmt.Printf("Failed to open %v\n", outfilename)
		return
	}
	defer outfile.Close()
	outfile.WriteString(outstring.String())
}

func download() {
	fs := flag.NewFlagSet("Download", flag.ExitOnError)
	pmaxDownloads := fs.Int("max-concurrent-downloads", 20, "Don't open more coroutines/downloads than this")
	poutdir := fs.String("outdir", "scraped", "Output directory with all the files (will be huge)")
	pinfile := fs.String("infile", "urls.txt", "The file containing the URLs to parse")
	pcheckfile := fs.String("checkfile", "scraped.txt", "The file containing which urls have been done")
	ptimeout := fs.Int("timeout", 30, "Timeout after which we consider request failed")

	fs.Parse(flag.Args()[1:])

	maxDownloads := *pmaxDownloads
	outdir := *poutdir
	infile := *pinfile
	checkfile := *pcheckfile
	timeout := time.Duration(*ptimeout)

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

				visit(client, url, outdir)
			}()
		}
	}
	wg.Wait()
}
