# second-order

This tool crawls a given URL and returns pages that have external resources embedded from a set of known providers (currently supports Amazon S3 and Wufoo)

### Installation
Go is required.
```
go get github.com/mhmdiaa/second-order
```
This will download the code, compile it, and leave a `second-order` binary in $GOPATH/bin.

### Usage
```
  -base string
        Base link to start scraping from (default "http://127.0.0.1")
  -depth int
        crawling depth (default 5)
  -js
        Extract JavaScript code from crawled pages
  -log
        Log crawled URLs to a file
  -output string
        Directory to save results in (default "output")
```

Learn more about second-order subdomain takeover: https://shubs.io/high-frequency-security-bug-hunting-120-days-120-bugs/#secondorder