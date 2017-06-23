# second-order

This tool crawls a given URL and returns pages that have external resources embedded from a set of known providers (currently supports Amazon S3 and Wufoo)

### Installation
Go is required.
```
go get github.com/PuerkitoBio/goquery
```

### Syntax
```
go run second-order.go -base=http://site.com -depth=5 -output=output.json
```

Learn more about second-order subdomain takeover: https://shubs.io/high-frequency-security-bug-hunting-120-days-120-bugs/#secondorder