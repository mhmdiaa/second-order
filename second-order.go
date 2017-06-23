package main

import (
    "fmt"
    "log"
    "net/url"
    "regexp"
    "sync"

    "github.com/PuerkitoBio/goquery"
)

type Job struct {
    url   string
    depth int
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
      log.Fatal(err)
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

func absUrl(href, base string) (string) {
  url, err := url.Parse(href)
  if err != nil {
    return ""
  }
  baseUrl, err := url.Parse(base)
  if err != nil {
    return ""
  }
  url = baseUrl.ResolveReference(url)
  return url.String()
}

func toVisit(urls []string, base string) []string{
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

func main() {
    wg := new(sync.WaitGroup)
    wg.Add(1)

    q := make(chan Job)
    go dedup(q, wg)
    q <- Job{"http://google.com/search?q=hello", 4}
    wg.Wait()
}