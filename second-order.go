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

	"github.com/PuerkitoBio/goquery"
)

// Configuration holds all the data passed from the config file
// the target is specified in a flag so we don't have to edit the configuration file every time we run the tool
type Configuration struct {
	Headers             map[string]string
	Depth               int
	LogCrawledURLs      bool
	LogQueries          map[string]string
	LogURLRegex         []string
	LogNon200Queries    map[string]string
	ExcludedURLRegex    []string
	ExcludedStatusCodes []int
	LogInlineJS         bool
}

type job struct {
	URL                 string
	Headers             map[string]string
	Depth               int
	LogQueries          map[string]string
	LogURLRegex         []string
	LogNon200Queries    map[string]string
	ExcludedURLRegex    []string
	ExcludedStatusCodes []int
	LogInlineJS         bool
}

// global variables to store the gathered info
var loggedQueries = struct {
	sync.RWMutex
	content map[string][]string
}{content: make(map[string][]string)}

var loggedNon200Queries = struct {
	sync.RWMutex
	content map[string][]string
}{content: make(map[string][]string)}

var loggedInlineJS = struct {
	sync.RWMutex
	content map[string][]string
}{content: make(map[string][]string)}

var (
	base       = flag.String("base", "http://127.0.0.1", "Base link to start scraping from")
	configFile = flag.String("config", "config.json", "Configuration file")
	outdir     = flag.String("output", "output", "Directory to save results in")
	debug      = flag.Bool("debug", false, "Print visited links in real-time to stdout")
    insecure   = flag.Bool("insecure", false, "Accept untrusted SSL/TLS certificates")
)

var seen = make(map[string]bool)

// store configuration in a global variable accessible to all functions so we don't have to pass it around all the time
var config Configuration

func main() {
	flag.Parse()

	wg := new(sync.WaitGroup)
	wg.Add(1)

	config, err := getConfigFile(*configFile)
	if err != nil {
		log.Fatal(err)
	}

	q := make(chan job)
	go dedup(q, wg)
	q <- job{*base, config.Headers, config.Depth, config.LogQueries, config.LogURLRegex, config.LogNon200Queries, config.ExcludedURLRegex, config.ExcludedStatusCodes, config.LogInlineJS}
	wg.Wait()

	os.MkdirAll(*outdir, os.ModePerm)

	if config.LogQueries != nil {
		err = writeResults("logged-queries.json", loggedQueries.content)
		if err != nil {
			log.Printf("Error writing query results: %v", err)
		}
	}
	if config.LogInlineJS {
		err = writeResults("inline-scripts.json", loggedInlineJS.content)
		if err != nil {
			log.Printf("Error writing inline scripts: %v", err)
		}
	}
	if config.LogNon200Queries != nil {
		err = writeResults("logged-non-200-queries.json", loggedNon200Queries.content)
		if err != nil {
			log.Printf("Error writing non-200 query results: %v", err)
		}
	}
	if config.LogCrawledURLs {
		URLs := []string{}
		for u := range seen {
			URLs = append(URLs, u)
		}
		l := strings.Join(URLs, "\n")
		err = ioutil.WriteFile(filepath.Join(*outdir, "log.txt"), []byte(l), 0644)
		if err != nil {
			log.Printf("couldn't write to URL log: %v", err)
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

func dedup(ch chan job, wg *sync.WaitGroup) {

	for j := range ch {
		if seen[j.URL] || j.Depth <= 0 {
			wg.Done()
			continue
		}
		seen[j.URL] = true
		go crawl(j, ch, wg)
	}
}

func crawl(j job, q chan job, wg *sync.WaitGroup) {
	defer wg.Done()

	res, err := httpGET(j.URL, j.Headers)
	if err != nil {
		log.Print(err)
		return
	}

	if res.StatusCode == http.StatusTooManyRequests {
		log.Printf("you are being rate limited")
		return
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Printf("could not parse page: %v", err)
		return
	}
	res.Body.Close()

	if j.LogQueries != nil {
		var foundResources []string
		for t, a := range j.LogQueries {
			resources := attrScrape(t, a, doc)
			if j.LogURLRegex != nil {
				resources = matchURLRegex(resources, j.LogURLRegex)
			}
			foundResources = append(foundResources, resources...)
		}

		if len(foundResources) > 0 {
			loggedQueries.Lock()
			loggedQueries.content[j.URL] = foundResources
			loggedQueries.Unlock()
		}
	}

	if j.LogInlineJS {
		inlineScriptCode := scrapeScripts(doc)

		if len(inlineScriptCode) > 0 {
			loggedInlineJS.Lock()
			loggedInlineJS.content[j.URL] = inlineScriptCode
			loggedInlineJS.Unlock()
		}
	}

	if j.LogNon200Queries != nil {
		var foundResources []string
		for t, a := range j.LogNon200Queries {
			links := attrScrape(t, a, doc)
			for _, link := range links {
				absolute, _ := absURL(link, j.URL)
				if isNon200(absolute, j.Headers, j.ExcludedStatusCodes, j.ExcludedURLRegex) {
					foundResources = append(foundResources, absolute)
				}
			}
		}

		if len(foundResources) > 0 {
			loggedNon200Queries.Lock()
			loggedNon200Queries.content[j.URL] = foundResources
			loggedNon200Queries.Unlock()
		}
	}

	urls := attrScrape("a", "href", doc)
	tovisit := toVisit(urls, j.URL, j.ExcludedURLRegex)

	if *debug {
		fmt.Println(j.URL)
	}

	if j.Depth <= 1 {
		return
	}

	wg.Add(len(tovisit))
	for _, u := range tovisit {
		q <- job{u, j.Headers, j.Depth - 1, j.LogQueries, j.LogURLRegex, j.LogNon200Queries, j.ExcludedURLRegex, j.ExcludedStatusCodes, j.LogInlineJS}
	}
}

func httpGET(url string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create request for %s: %v", url, err)
	}

	for key, value := range headers {
		req.Header.Add(key, value)
	}

    client := &http.Client{}

    if *insecure {
        tr := &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        }
        client = &http.Client{Transport: tr}
    }

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not request %s: %v", url, err)
	}
	return res, nil
}

func writeResults(filename string, content map[string][]string) error {
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

func attrScrape(tag string, attr string, doc *goquery.Document) []string {
	var results []string
	doc.Find(tag).Each(func(index int, tag *goquery.Selection) {
		attr, exists := tag.Attr(attr)
		if exists {
			results = append(results, attr)
		}
	})
	return results
}

func scrapeScripts(doc *goquery.Document) []string {
	var inlineScripts []string

	doc.Find("script").Each(func(index int, tag *goquery.Selection) {
		// check if the tag does not have a src attribute
		// if it doesn't, assume it's an inline script
		_, exists := tag.Attr("src")
		if !exists {
			inlineScripts = append(inlineScripts, tag.Text())
		}
	})

	return inlineScripts
}

func checkOrigin(link, base string) bool {
	linkurl, _ := url.Parse(link)
	linkhost := linkurl.Hostname()

	baseURL, _ := url.Parse(base)
	basehost := baseURL.Hostname()

	// check the main domain not the subdomain
	// checkOrigin ("https://docs.google.com", "https://mail.google.com") => true
	re, _ := regexp.Compile("[\\w-]*\\.[\\w]*$")
	if re.FindString(linkhost) == re.FindString(basehost) {
		return true
	}
	return false
}

func absURL(href, base string) (string, error) {
	url, err := url.Parse(href)
	if err != nil {
		return "", fmt.Errorf("couldn't parse URL: %v", err)
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("couldn't parse URL: %v", err)
	}
	url = baseURL.ResolveReference(url)
	return url.String(), nil
}

func toVisit(urls []string, base string, excludedRegex []string) []string {
	var tovisit []string
	for _, u := range urls {
		absolute, err := absURL(u, base)
		if err != nil {
			log.Printf("couldn't parse URL: %v", err)
			continue
		}
		if !(strings.HasPrefix(absolute, "http://") || strings.HasPrefix(absolute, "https://")) {
			continue
		}
		if matchURLRegexLink(u, excludedRegex) {
			continue
		}
		if checkOrigin(absolute, base) {
			tovisit = append(tovisit, absolute)
		}
	}
	return tovisit
}

func matchURLRegexLink(link string, regex []string) bool {
	for _, re := range regex {
		matches, _ := regexp.MatchString(re, link)
		if matches {
			return true
		}
	}
	return false
}

func matchURLRegex(links []string, regex []string) []string {
	var results []string
	for _, link := range links {
		matches := matchURLRegexLink(link, regex)
		if matches {
			results = append(results, link)
		}
	}
	return results
}

func isNon200(link string, headers map[string]string, excludedStatusCodes []int, excludedURLRegex []string) bool {
	// check if the link matches any excluded regex
	for _, regex := range excludedURLRegex {
		matches, _ := regexp.MatchString(regex, link)
		if matches {
			return false
		}
	}

	res, err := httpGET(link, headers)

	// check if the link doesn't respond properly
	if err != nil {
		return false
	}

	if res.StatusCode == 200 {
		return false
	}

	// check if the link responds with an excluded status code
	for _, code := range excludedStatusCodes {
		if res.StatusCode == code {
			return false
		}
	}
	return true
}
