# httping

A ping-like tool for HTTP(S). Written in Go.

![Preview](preview.png)

## Install

### Precompiled binaries

You can download precompiled binaries for Windows (amd64) and Linux (amd64,
arm64) [here](https://github.com/GitRowin/httping/releases).

### From source

Requires Go 1.21 or higher.

```
git clone https://github.com/GitRowin/httping.git
cd httping
go install
```

## Usage

```
Usage: httping [options] <url>
  -count uint
        Number of requests to send
  -delay uint
        Minimum delay between requests in milliseconds (default 1000)
  -disable-compression
        Whether to disable compression
  -disable-h2
        Whether to disable HTTP/2
  -enable-keep-alive
        Whether to use keep-alive
  -no-new-conn-count
        Whether to not count requests that did not reuse a connection towards the final statistics
  -timeout uint
        Request timeout in milliseconds (default 5000)
  -user-agent string
        Change the User-Agent header (default "httping (https://github.com/GitRowin/httping)")
```

Example: `httping -count 10 -disable-compression -timeout 1000 https://example.com/`

## Fields explained

- dns: Time taken to resolve the domain
- conn: Time taken to create the TCP connection
- tls: Time taken to complete the TLS handshake
- ttfb: Time taken to receive the first byte of the response ("Time To First Byte")
- total: Total time taken (DNS, TCP, TLS, send request, receive response)
- reused: Whether the TCP connection was reused to send the request
- proto: Used HTTP protocol
- status: The status returned by the server
- error: The error message
