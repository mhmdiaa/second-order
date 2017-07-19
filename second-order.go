package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"net/http"
	"regexp"
	"sync"
	"os"
	"path/filepath"

	"github.com/PuerkitoBio/goquery"
)

type Job struct {
	url   string
	depth int
}

var allResources = make(map[string][]string)
var allInlineScripts = make(map[string][]string)
var allExternalScripts = make(map[string]map[string]string)

func checkErr(err error) {
    if err != nil {
        fmt.Println("ERROR:", err)
    }
}

func dedup(ch chan Job, wg *sync.WaitGroup) {
	seen := make(map[string]bool)
	for job := range ch {
		if seen[job.url] || job.depth <= 0 {
			wg.Done()
			continue
		}
		seen[job.url] = true
		go Crawl(job, ch, wg)
	}
}

func Crawl(job Job, q chan Job, wg *sync.WaitGroup) {
	defer wg.Done()
	fmt.Println(job.url)
	doc, err := goquery.NewDocument(job.url)

	if err != nil {
		return
	}
	var resources []string

	iframes := attrScrape("iframe", "src", doc)
	svgs := attrScrape("svg", "src", doc)
	objects := attrScrape("object", "src", doc)

	resources = append(resources, canTakeover(iframes)...)
	resources = append(resources, canTakeover(svgs)...)
	resources = append(resources, canTakeover(objects)...)

	externalScriptLinks, externalScriptCode, inlineScriptCode := scrapeScripts(doc, job.url)

	resources = append(resources, canTakeover(externalScriptLinks)...)

	allInlineScripts[job.url] = inlineScriptCode
	allExternalScripts[job.url] = externalScriptCode

	if len(resources) > 0 {
		allResources[job.url] = resources
	}

	urls := attrScrape("a", "href", doc)
	tovisit := toVisit(urls, job.url)

	if job.depth <= 1 {
		return
	}

	wg.Add(len(tovisit))
	for _, u := range tovisit {
		q <- Job{u, job.depth - 1}
	}

}

func attrScrape(tag string, attr string, doc *goquery.Document) []string {
	var results []string
	doc.Find(tag).Each(func(index int, tag *goquery.Selection) {
		attrValue, _ := tag.Attr(attr)
		results = append(results, attrValue)
	})
	return results
}

func scrapeScripts(doc *goquery.Document, link string) ([]string, map[string]string, []string) {
    var links []string
    externalScripts := make(map[string]string)
    var inlineScripts []string

    doc.Find("script").Each(func(index int, tag *goquery.Selection) {
        attr, exists := tag.Attr("src")
        if exists {
            links = append(links, attr)
            code := getScript(attr, link)
            externalScripts[attr] = code
        } else {
            inlineScripts = append(inlineScripts, tag.Text())
        }
    })

    return links, externalScripts, inlineScripts
}


func getScript(link string, base string) string{
	link = absUrl(link, base)
    client := &http.Client{}
    req, err := http.NewRequest("GET", link, nil)
    if err != nil {
    	fmt.Println(err)
    	return ""
    }

    resp, err := client.Do(req)
    if err != nil {
    	fmt.Println(err)
    	return ""
    }

    defer resp.Body.Close()
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
    	fmt.Println(err)
    	return ""
    }

    return string(body)
}

func checkOrigin(link, base string) bool {
	linkurl, _ := url.Parse(link)
	linkhost := linkurl.Hostname()

	baseurl, _ := url.Parse(base)
	basehost := baseurl.Hostname()

	// check the main domain not the subdomain
	// checkOrigin ("https://docs.google.com", "https://mail.google.com") => true
	re, _ := regexp.Compile("[\\w-]*\\.[\\w]*$")
	if re.FindString(linkhost) == re.FindString(basehost) {
		return true
	}
	return false
}

func absUrl(href, base string) string {
	url, err := url.Parse(href)
	if err != nil {
		fmt.Println(err)
		return ""
	}
	baseUrl, err := url.Parse(base)
	if err != nil {
		fmt.Println(err)
		return ""
	}
	url = baseUrl.ResolveReference(url)
	return url.String()
}

func toVisit(urls []string, base string) []string {
	var tovisit []string
	for _, u := range urls {
		absolute := absUrl(u, base)
		if absolute == "" {
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
	base := flag.String("base", "http://127.0.0.1", "Base link to start scraping from")
	depth := flag.Int("depth", 5, "Crawling depth")
	outdir := flag.String("output", "output", "directory to save results in")
	flag.Parse()

	wg := new(sync.WaitGroup)
	wg.Add(1)

	q := make(chan Job)
	go dedup(q, wg)
	q <- Job{*base, *depth}
	wg.Wait()

	resourcesJson, _ := json.Marshal(allResources)
	inlineScriptsJson, _ := json.Marshal(allInlineScripts)
	externalScriptsJson, _ := json.Marshal(allExternalScripts)

	os.MkdirAll(*outdir, os.ModePerm)

	err := ioutil.WriteFile(filepath.Join(*outdir, "resources.json"), resourcesJson, 0644)
	checkErr(err)

	err = ioutil.WriteFile(filepath.Join(*outdir, "inline-scripts.json"), inlineScriptsJson, 0644)
	checkErr(err)

	err = ioutil.WriteFile(filepath.Join(*outdir, "external-scripts.json"), externalScriptsJson, 0644)
	checkErr(err)
}
