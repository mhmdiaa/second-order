# second-order

This tool crawls a given URL and returns pages that have external resources embedded from a set of known providers (currently supports Amazon S3 and Wufoo)

### Installation
Go is required.
```
go get github.com/PuerkitoBio/goquery
```

```
cd $GOPATH/src/github.com/
git clone https://github.com/mhmdiaa/second-order.git
cd second-order
go install
go build
cd $GOPATH/bin/
./second-order
```
### Syntax
```
go run second-order.go -base=http://site.com -depth=5 -output=output.json -js
```

When the `-js` flag is set, second-order will attempt to store all the JavaScript code that can be found in crawled pages (both inline and external)

---
Learn more about second-order subdomain takeover: https://shubs.io/high-frequency-security-bug-hunting-120-days-120-bugs/#secondorder
