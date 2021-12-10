package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/gocolly/colly/v2"
)

// Configuration holds all the data passed from the config file
// the target is specified in a flag so we don't have to edit the configuration file every time we run the tool
type Configuration struct {
	Headers          map[string]string
	Depth            int
	LogCrawledURLs   bool
	LogQueries       map[string]string
	LogURLRegex      []string
	LogNon200Queries map[string]string
	LogInline        []string
}

// global variables to store the gathered info
var loggedQueries = struct {
	sync.RWMutex
	content map[string]map[string][]string
}{content: make(map[string]map[string][]string)}

var loggedNon200Queries = struct {
	sync.RWMutex
	content map[string]map[string][]string
}{content: make(map[string]map[string][]string)}

var loggedInline = struct {
	sync.RWMutex
	content map[string]map[string][]string
}{content: make(map[string]map[string][]string)}

var (
	target     = flag.String("target", "http://127.0.0.1", "Target URL")
	configFile = flag.String("config", "config.json", "Configuration file")
	outdir     = flag.String("output", "output", "Directory to save results in")
	insecure   = flag.Bool("insecure", false, "Accept untrusted SSL/TLS certificates")
	depth      = flag.Int("depth", 1, "Depth to crawl")
	threads    = flag.Int("threads", 10, "Number of threads")
)

func main() {
	flag.Parse()

	config, err := getConfigFile(*configFile)
	if err != nil {
		log.Fatal(err)
	}

	hostname, err := getHostname(*target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Target URL is invalid: %v", err)
		os.Exit(1)
	}

	// Instantiate default collector
	c := colly.NewCollector(
		colly.MaxDepth(*depth),
		colly.Async(),
	)
	c.Limit(&colly.LimitRule{DomainGlob: "*", Parallelism: *threads})

	// Allow URLs from the same domain and its subdomains
	c.URLFilters = []*regexp.Regexp{
		regexp.MustCompile(".*" + strings.ReplaceAll(hostname, ".", "\\.") + ".*"),
	}

	// Add headers
	c.OnRequest(func(r *colly.Request) {
		for header, value := range config.Headers {
			r.Headers.Set(header, value)
		}
	})

	// Accept untrusted SSL/TLS certificates based on the value of `-insecure` flag
	c.WithTransport(&http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: *insecure},
	})

	// On every a element which has href attribute call callback
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Attr("href")

		// Print link if it's in-scope
		if checkOrigin(link, *target) {
			fmt.Println(link)
		}

		// Visit link found on page on a new thread
		e.Request.Visit(link)
	})

	// Register a function that logs HTML attributes
	for tag, attribute := range config.LogQueries {
		querySelector := createQuerySelector(tag, attribute)
		c.OnHTML(querySelector, func(e *colly.HTMLElement) {
			u := e.Request.URL.String()
			_, attr := unpackQuerySelector(querySelector)
			value := e.Attr(attr)
			loggedQueries.Lock()
			if _, ok := loggedQueries.content[u]; !ok {
				loggedQueries.content[u] = make(map[string][]string)
			}
			loggedQueries.content[u][querySelector] = append(loggedQueries.content[u][querySelector], value)
			loggedQueries.Unlock()
		})
	}

	// Register a function that logs URLs from HTML attributes if they return a non-200 response code
	for tag, attribute := range config.LogNon200Queries {
		querySelector := createQuerySelector(tag, attribute)
		c.OnHTML(querySelector, func(e *colly.HTMLElement) {
			u := e.Request.URL.String()
			_, attr := unpackQuerySelector(querySelector)
			value := e.Attr(attr)

			if isValidURL(value) && isNon200(value) {
				loggedNon200Queries.Lock()
				if _, ok := loggedNon200Queries.content[u]; !ok {
					loggedNon200Queries.content[u] = make(map[string][]string)
				}
				loggedNon200Queries.content[u][querySelector] = append(loggedNon200Queries.content[u][querySelector], value)
				loggedNon200Queries.Unlock()
			}
		})
	}

	for _, tag := range config.LogInline {
		c.OnHTML(tag, func(e *colly.HTMLElement) {
			u := e.Request.URL.String()
			value := e.Text
			loggedInline.Lock()
			if _, ok := loggedInline.content[u]; !ok {
				loggedInline.content[u] = make(map[string][]string)
			}
			loggedInline.content[u][tag] = append(loggedInline.content[u][tag], value)
			loggedInline.Unlock()

		})
	}

	// Start scraping
	c.Visit(*target)
	// Wait until threads are finished
	c.Wait()

	os.MkdirAll(*outdir, os.ModePerm)

	if config.LogQueries != nil {
		err = writeResults("attributes.json", loggedQueries.content)
		if err != nil {
			log.Printf("Error writing attributes: %v", err)
		}
	}
	if config.LogInline != nil {
		err = writeResults("inline.json", loggedInline.content)
		if err != nil {
			log.Printf("Error writing inline text: %v", err)
		}
	}
	if config.LogNon200Queries != nil {
		err = writeResults("non-200-url-attributes.json", loggedNon200Queries.content)
		if err != nil {
			log.Printf("Error writing non-200 URL attributes: %v", err)
		}
	}
}

func getConfigFile(location string) (Configuration, error) {
	f, err := os.Open(location)
	if err != nil {
		return Configuration{}, fmt.Errorf("could not open Configuration file: %v", err)
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	config := Configuration{}
	err = decoder.Decode(&config)
	if err != nil {
		return Configuration{}, fmt.Errorf("could not decode Configuration file: %v", err)
	}

	return config, nil
}

func createQuerySelector(tag, attribute string) string {
	return fmt.Sprintf("%s[%s]", tag, attribute)
}

// a[href] -> a, href
func unpackQuerySelector(q string) (string, string) {
	parts := strings.Split(q, "[")
	tag := parts[0]
	attribute := strings.Trim(parts[1], "]")

	return tag, attribute
}

func writeResults(filename string, content map[string]map[string][]string) error {
	JSON, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("could not marshal the JSON object: %v", err)
	}
	err = ioutil.WriteFile(filepath.Join(*outdir, filename), JSON, 0644)
	if err != nil {
		return fmt.Errorf("coudln't write resources to JSON: %v", err)
	}
	return nil
}

func getHostname(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	return u.Hostname(), nil
}

func checkOrigin(link, base string) bool {
	linkurl, err := url.Parse(link)
	if err != nil {
		return false
	}

	linkhost := linkurl.Hostname()

	baseURL, err := url.Parse(base)
	if err != nil {
		return false
	}
	basehost := baseURL.Hostname()

	// check the main domain not the subdomain
	// checkOrigin ("https://docs.google.com", "https://mail.google.com") => true
	re, _ := regexp.Compile(`[\w-]*\.[\w]*$`)
	return re.FindString(linkhost) == re.FindString(basehost)
}

func isValidURL(s string) bool {
	_, err := url.ParseRequestURI(s)
	return err == nil
}

func isNon200(link string) bool {
	// Golang's native HTTP client can't read URLs in this format: //example.com
	if strings.HasPrefix(link, "//") {
		return isNon200("http:" + link)
	}

	res, err := http.Get(link)
	// If it doesn't respond at all, it could be an unregistered domain
	if err != nil {
		return true
	}

	if res.StatusCode == 200 {
		return false
	}

	return true
}
