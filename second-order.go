package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

// Configuration holds all the data passed from the config file
// the target is specified in a flag so we don't have to edit the configuration file every time we run the tool
type Configuration struct {
	LogQueries       map[string]string
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
	target     string
	configFile string
	outdir     string
	insecure   bool
	depth      int
	threads    int
	headers    Headers
)

type Headers map[string]string

func (h *Headers) String() string {
	return "my string representation"
}

func (headers *Headers) Set(h string) error {
	parts := strings.Split(h, ":")
	if len(parts) == 2 {
		name := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		(*headers)[name] = value
	}

	return nil
}

func main() {

	flag.StringVar(&target, "target", "", "Target URL")
	flag.StringVar(&configFile, "config", "", "Configuration file")
	flag.StringVar(&outdir, "output", "output", "Directory to save results in")
	flag.BoolVar(&insecure, "insecure", false, "Accept untrusted SSL/TLS certificates")
	flag.IntVar(&depth, "depth", 1, "Depth to crawl")
	flag.IntVar(&threads, "threads", 10, "Number of threads")
	headers = make(Headers)
	flag.Var(&headers, "header", "Header name and value separated by a colon 'Name: Value' (can be used more than once)")
	flag.Parse()

	if target == "" || configFile == "" {
		fmt.Println("[*] You need to specify a target and a config file")
		flag.PrintDefaults()
		os.Exit(1)
	}

	config, err := getConfigFile(configFile)
	if err != nil {
		log.Fatal(err)
	}

	// Run a goroutine to catch interrupt signals and save the found results before exiting
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		for sig := range interrupt {
			fmt.Printf("[*] Received a kill signal: %s, saving the results before exiting\n", sig)
			writeAllResults(config)
			os.Exit(0)
		}
	}()

	hostname, err := getHostname(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Target URL is invalid: %v", err)
		os.Exit(1)
	}

	// Instantiate default collector
	c := colly.NewCollector(
		colly.MaxDepth(depth),
		colly.Async(),
	)
	c.Limit(&colly.LimitRule{DomainGlob: "*", Parallelism: threads})

	// Allow URLs from the same domain and its subdomains
	c.URLFilters = []*regexp.Regexp{
		regexp.MustCompile(".*" + strings.ReplaceAll(hostname, ".", "\\.") + ".*"),
	}

	// Add headers
	c.OnRequest(func(r *colly.Request) {
		// Set a random user agent for each request
		rand.Seed(time.Now().Unix())
		n := rand.Intn(len(userAgents))
		r.Headers.Set("User-Agent", userAgents[n])
		// Add other headers
		for header, value := range headers {
			r.Headers.Set(header, value)
		}
	})

	// Accept untrusted SSL/TLS certificates based on the value of `-insecure` flag
	c.WithTransport(&http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	})

	// On every a element which has href attribute call callback
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		// Print link if it's in-scope and has not been visited
		visited, _ := c.HasVisited(link)
		if checkOrigin(link, target) && !visited {
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

			if isValidURL(value) && isNotFound(value) {
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
	c.Visit(target)
	// Wait until threads are finished
	c.Wait()

	writeAllResults(config)
}

func writeAllResults(config Configuration) {
	os.MkdirAll(outdir, os.ModePerm)

	if config.LogQueries != nil {
		err := writeResults("attributes.json", loggedQueries.content, "LogQueries")
		if err != nil {
			log.Printf("Error writing attributes: %v", err)
		}
	}
	if config.LogInline != nil {
		err := writeResults("inline.json", loggedInline.content, "LogInline")
		if err != nil {
			log.Printf("Error writing inline text: %v", err)
		}
	}
	if config.LogNon200Queries != nil {
		err := writeResults("non-200-url-attributes.json", loggedNon200Queries.content, "LogNon200Queries")
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

func writeResults(filename string, content map[string]map[string][]string, resultType string) error {
	output := make(map[string]map[string]map[string][]string)
	output[resultType] = content
	JSON, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("could not marshal the JSON object: %v", err)
	}
	err = ioutil.WriteFile(filepath.Join(outdir, filename), JSON, 0644)
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
	u, err := url.ParseRequestURI(s)
	if err != nil {
		return false
	}
	if u.IsAbs() {
		return true
	}
	return false
}

func isNotFound(url string) bool {
	// Golang's native HTTP client can't read URLs in this format: "//example.com"
	if strings.HasPrefix(url, "//") {
		return isNotFound("http:" + url)
	}
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	res, err := client.Do(req)
	// If it doesn't respond at all, it could be an unregistered domain
	if err != nil {
		return true
	}
	if res.StatusCode == 404 {
		return true
	}
	return false
}

var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36 Edg/108.0.1462.54",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:108.0) Gecko/20100101 Firefox/108.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.2 Safari/605.1.15",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_14_6) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_14_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.2 Safari/605.1.15",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36 Edg/108.0.1462.46",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:108.0) Gecko/20100101 Firefox/108.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/92.0.4515.131 Safari/537.36 Edg/92.0.902.67",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.82 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 11_0_0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.82 Safari/537.36",
	"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/89.0.4389.82 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:87.0) Gecko/20100101 Firefox/87.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 11_0_0; rv:87.0) Gecko/20100101 Firefox/87.0",
	"Mozilla/5.0 (X11; Linux x86_64; rv:87.0) Gecko/20100101 Firefox/87.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.198 Safari/537.36 Edg/87.0.664.59",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.198 Safari/537.36 Edg/87.0.664.56",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:85.0) Gecko/20100101 Firefox/85.0",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 11_0_0; rv:85.0) Gecko/20100101 Firefox/85.0",
	"Mozilla/5.0 (X11; Linux x86_64; rv:85.0) Gecko/20100101 Firefox/85.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Safari/537.36 Edg/87.0.664.75",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Safari/537.36 Edg/87.0.664.72",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Safari/537.36",
}
