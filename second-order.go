package main

import (
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
	"sync"

	"github.com/PuerkitoBio/goquery"
)

type job struct {
	url   string
	depth int
}

var allResources = make(map[string][]string)
var allInlineScripts = make(map[string][]string)
var allExternalScripts = make(map[string]map[string]string)

var (
	base      = flag.String("base", "http://127.0.0.1", "Base link to start scraping from")
	depth     = flag.Int("depth", 5, "crawling depth")
	outdir    = flag.String("output", "output", "Directory to save results in")
	extractJS = flag.Bool("js", false, "Extract JavaScript code from crawled pages")
)

func dedup(ch chan job, wg *sync.WaitGroup) {
	seen := make(map[string]bool)
	for j := range ch {
		if seen[j.url] || j.depth <= 0 {
			wg.Done()
			continue
		}
		seen[j.url] = true
		go crawl(j, ch, wg)
	}
}

func crawl(j job, q chan job, wg *sync.WaitGroup) {
	defer wg.Done()

	res, err := http.Get(j.url)
	if err != nil {
		log.Printf("could not get %s: %v", j.url, err)
		return
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusTooManyRequests {
		log.Printf("you are being rate limited")
		return
	}

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		log.Printf("could not parse page: %v", err)
		return
	}

	var resources []string

	queries := make(map[string]string)
	queries["iframe"] = "src"
	queries["svg"] = "src"
	queries["object"] = "src"
	queries["script"] = "src"

	for t, a := range queries {
		r := attrScrape(t, a, doc)
		resources = append(resources, r...)
	}

	if *extractJS {
		externalScriptCode, inlineScriptCode := scrapeScripts(doc, j.url)

		allInlineScripts[j.url] = inlineScriptCode
		allExternalScripts[j.url] = externalScriptCode
	}

	if len(resources) > 0 {
		allResources[j.url] = resources
	}

	urls := attrScrape("a", "href", doc)
	tovisit := toVisit(urls, j.url)

	fmt.Println(j.url)

	if j.depth <= 1 {
		return
	}

	wg.Add(len(tovisit))
	for _, u := range tovisit {
		q <- job{u, j.depth - 1}
	}

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

func scrapeScripts(doc *goquery.Document, link string) (map[string]string, []string) {
	externalScripts := make(map[string]string)
	var inlineScripts []string

	doc.Find("script").Each(func(index int, tag *goquery.Selection) {
		attr, exists := tag.Attr("src")
		if exists {
			code, err := getScript(attr, link)
			if err != nil {
				log.Printf("couldn't get script: %v", err)
			}
			externalScripts[attr] = code
		} else {
			inlineScripts = append(inlineScripts, tag.Text())
		}
	})

	return externalScripts, inlineScripts
}

func getScript(link string, base string) (string, error) {
	link, err := absURL(link, base)
	if err != nil {
		return "", fmt.Errorf("couldn't parse script URL: %v", err)
	}

	resp, err := http.Get(link)
	if err != nil {
		return "", fmt.Errorf("couldn't load script: %v", err)
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("couldn't read script: %v", err)
	}
	return string(body), nil
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

func toVisit(urls []string, base string) []string {
	var tovisit []string
	for _, u := range urls {
		absolute, err := absURL(u, base)
		if err != nil {
			log.Printf("couldn't parse URL: %v", err)
			continue
		}
		if checkOrigin(absolute, base) {
			tovisit = append(tovisit, absolute)
		}
	}
	return tovisit
}

func canTakeoverLink(link string) bool {
	// TODO: Check if the subdomains are not connected to an account
	providers := []string{"s3.amazon.com", "wufoo.com"}
	for i := range providers {
		providerused, _ := regexp.MatchString(providers[i], link)
		if providerused {
			return true
		}
	}
	return false
}

func canTakeover(links []string) []string {
	var results []string
	for i := range links {
		cantakeover := canTakeoverLink(links[i])
		if cantakeover {
			results = append(results, links[i])
		}
	}
	return results
}

func main() {
	flag.Parse()

	wg := new(sync.WaitGroup)
	wg.Add(1)

	q := make(chan job)
	go dedup(q, wg)
	q <- job{*base, *depth}
	wg.Wait()

	resourcesJSON, _ := json.Marshal(allResources)
	inlineScriptsJSON, _ := json.Marshal(allInlineScripts)
	externalScriptsJSON, _ := json.Marshal(allExternalScripts)

	os.MkdirAll(*outdir, os.ModePerm)

	err := ioutil.WriteFile(filepath.Join(*outdir, "resources.json"), resourcesJSON, 0644)
	if err != nil {
		log.Printf("coudln't write resources to JSON: %v", err)
	}
	if *extractJS {
		err = ioutil.WriteFile(filepath.Join(*outdir, "inline-scripts.json"), inlineScriptsJSON, 0644)
		if err != nil {
			log.Printf("coudln't write inline scripts to JSON: %v", err)
		}

		err = ioutil.WriteFile(filepath.Join(*outdir, "external-scripts.json"), externalScriptsJSON, 0644)
		if err != nil {
			log.Printf("couldn't write external scripts to JSON: %v", err)
		}
	}
}
