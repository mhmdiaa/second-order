# Second Order

Scans web applications for second-order subdomain takeover by crawling the app, and collecting URLs (and other data) that match some specific rules, or respond in a specific way.

### Installation
Go version >= 1.8 is required.
```
go get github.com/mhmdiaa/second-order
```
This will download the code, compile it, and leave a `second-order` binary in $GOPATH/bin.

### Command line options
```
 -base string
       Base link to start scraping from (default "http://127.0.0.1")
 -config string
       Configuration file (default "config.json")
 -debug
       Print visited links in real-time to stdout
 -output string
       Directory to save results in (default "output")
```

### Example
```
go run second-order.go -base https://example.com -config config.json -output example.com -concurrency 10
```

### Configuration File
**Example configuration file included (config.json)**
- `Headers`: A map of headers that will be sent with every request.
- `Depth`: Crawling depth.
- `LogCrawledURLs`: If this is set to true, Second Order will log the URL of every crawled page.
- `LogQueries`: A map of tag-attribute queries that will be searched for in crawled pages. For example, `"a": "href"` means log every `href` attribute of every `a` tag.
- `LogURLRegex`: A list of regular expressions that will be matched against the URLs that are extracted using the queries in `LogQueries`; if left empty, all URLs will be logged.
- `LogNon200Queries`: A map of tag-attribute queries that will be searched for in crawled pages, and logged only if they don't return a `200` status code.
- `ExcludedURLRegex`: A list of regular expressions whose matching URLs will not be accessed by the tool.
- `ExcludedStatusCodes`: A list of status codes; if any page responds with one of these, it will be excluded from the results of `LogNon200Queries`; if left empty, all non-200 pages' URLs will be logged.
- `LogInlineJS`: If this is set to true, Second Order will log the contents of every `script` tag that doesn't have a `src` attribute.

### Output Directory Structure
All results are saved in JSON files that specify what and where data was found
```
OUTPUT
      logged-queries.json         -> The results of `LogQueries`
      logged-non-200-queries.json -> The results of `LogNon200Queries`
      inline-scripts.json         -> The results of `LogInlineJS`
```

### Usage Ideas
This is a list of tips and ideas (not necessarily related to second-order subdomain takeover) on what to use Second Order for.
- Check for second-order subdomain takeover. (Duh!)
- Collect JS code by setting `LogInlineJS` to true, and adding `"script": "src"` to `LogQueries`.
- Find a target's online assets by using `LogURLRegex`. (S3 buckets, anyone?)
- Collect SWF files by adding `"object": "src"` to `LogQueries`.
- Collect `<input>` names by adding `"input": "name"` to `LogQueries`.


### References
https://shubs.io/high-frequency-security-bug-hunting-120-days-120-bugs/#secondorder

https://edoverflow.com/2017/broken-link-hijacking/
