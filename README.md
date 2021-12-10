# Second Order

Scans web applications for second-order subdomain takeover by crawling the app, and collecting URLs (and other data) that match certain rules, or respond in a certain way.

## Installation
### From binary
Download a prebuilt binary from the [releases page](https://github.com/mhmdiaa/second-order/releases/latest) and unzip it.

### From source
Go version 1.17 is recommended.
```
go install -v github.com/mhmdiaa/second-order@latest
```

### Docker
```
docker pull mhmdiaa/second-order
```

## Command line options
```
  -target string
        Target URL
  -config string
        Configuration file (default "config.json")
  -depth int
        Depth to crawl (default 1)
  -insecure
        Accept untrusted SSL/TLS certificates
  -output string
        Directory to save results in (default "output")
  -threads int
        Number of threads (default 10)
```

## Example
```
go run second-order.go -base https://example.com -config config.json -output example.com -concurrency 10
```

## Configuration File
**Example configuration files are in [config](/config/)**
- `Headers`: A map of headers that will be sent with every request.
- `LogQueries`: A map of tag-attribute queries that will be searched for in crawled pages. For example, `"a": "href"` means log every `href` attribute of every `a` tag.
- `LogNon200Queries`: A map of tag-attribute queries that will be searched for in crawled pages, and logged only if they contain a valid URL that doesn't return a `200` status code.
- `LogInline`: A list of tags whose inline content (between the opening and closing tags) will be logged, like `title` and `script`

## Output
All results are saved in JSON files that specify what and where data was found

- The results of `LogQueries` are saved in `attributes.json`
```
{
    "https://example.com/": {
        "input[name]": [
            "user",
            "id",
            "debug"
        ]
    }
}
```
- The results of `LogNon200Queries` are saved in `non-200-url-attributes.json`
```
{
    "https://example.com/": {
        "script[src]": [
            "https://cdn.old_abandoned_domain.com/app.js",
        ]
    }
}
```
- The results of `LogInline` are saved in `inline.json`
{
    "https://example.com/": {
        "title": [
            "Example - Home"
        ]
    },
      "https://example.com/login": {
        "title": [
            "Example - login"
        ]
    }
}

## Usage Ideas
This is a list of tips and ideas (not necessarily related to second-order subdomain takeover) on what to use Second Order for.
- Check for second-order subdomain takeover: [takeover.json](config/takeover.json). (Duh!)
- Collect inline and imported JS code: [javascript.json](config/javascript.json).
- Find where a target hosts static files [cdn.json](config/cdn.json). (S3 buckets, anyone?)
- Collect `<input>` names to build a tailored parameter bruteforcing wordlist: [parameters.json](config/parameters.json).
- Feel free to contribute more ideas!

## References
https://shubs.io/high-frequency-security-bug-hunting-120-days-120-bugs/#secondorder

https://edoverflow.com/2017/broken-link-hijacking/
