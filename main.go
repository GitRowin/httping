package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"github.com/montanaflynn/stats"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

var (
	targetUrl          string
	count              uint
	delay              uint
	timeout            uint
	enableKeepAlive    bool
	disableCompression bool
	disableHttp2       bool
	noNewConnCount     bool
)

func init() {
	flag.UintVar(&count, "count", 0, "Number of requests to send")
	flag.UintVar(&delay, "delay", 1000, "Minimum delay between requests in milliseconds")
	flag.UintVar(&timeout, "timeout", 5000, "Request timeout in milliseconds")
	flag.BoolVar(&enableKeepAlive, "enable-keep-alive", false, "Whether to use keep-alive")
	flag.BoolVar(&disableCompression, "disable-compression", false, "Whether to disable compression")
	flag.BoolVar(&disableHttp2, "disable-h2", false, "Whether to disable HTTP/2")
	flag.BoolVar(&noNewConnCount, "no-new-conn-count", false, "Whether to not count requests that did not reuse a connection towards the final statistics")
}

type TLSNextProtoMap = map[string]func(authority string, c *tls.Conn) http.RoundTripper

// Statistics stores all request statistics.
// All pointer fields are optional. If a field is nil or an empty string, "N/A" is printed.
type Statistics struct {
	DNS          *time.Duration
	Connect      *time.Duration
	TLSHandshake *time.Duration
	TTFB         *time.Duration
	Download     *time.Duration
	Total        *time.Duration
	Reused       *bool
	Proto        string
	Status       string
}

func main() {
	flag.Parse()

	targetUrl = flag.Arg(0)

	if targetUrl == "" {
		fmt.Fprintln(os.Stderr, "Usage: httping [options] <url>")
		flag.PrintDefaults()
		os.Exit(-1)
	}

	var tlsNextProto TLSNextProtoMap

	if disableHttp2 {
		// Setting TLSNextProto to an empty map disables HTTP/2
		tlsNextProto = TLSNextProtoMap{}
	}

	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives:  !enableKeepAlive,
			DisableCompression: disableCompression,
			TLSNextProto:       tlsNextProto,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // Do not follow redirects
		},
		Timeout: time.Duration(timeout) * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	// Asynchronously wait for an interruption
	go func() {
		<-c
		cancel()
	}()

	// Amount of requests sent
	var requests, successful, failed uint

	// Slice of total latency of every request
	var totals []float64

	for {
		statistics, err := sendRequest(client, ctx, targetUrl)

		// The program was interrupted while sending the request, break out of the for loop
		if errors.Is(err, context.Canceled) {
			break
		}

		requests++

		if err != nil {
			failed++
		} else {
			successful++

			// If noNewConnCount is enabled, only append if the connection was reused
			if !(noNewConnCount && !*statistics.Reused) {
				totals = append(totals, float64(*statistics.Total)/float64(time.Millisecond))
			}
		}

		var errMsg string

		if err != nil {
			errMsg = err.Error()

			// Trim: Get "https://example.com/": dial tcp: lookup example.com: no such host
			// To: dial tcp: lookup example.com: no such host
			// TODO: Make this more strict?
			if strings.HasPrefix(errMsg, "Get ") {
				errMsg = errMsg[strings.Index(errMsg, ": ")+2:]
			}
		}

		fmt.Printf("dns=%s conn=%s tls=%s ttfb=%s dl=%s total=%s reused=%s proto=%s status=%s error=%s\n",
			formatPtrDuration(statistics.DNS),
			formatPtrDuration(statistics.Connect),
			formatPtrDuration(statistics.TLSHandshake),
			formatPtrDuration(statistics.TTFB),
			formatPtrDuration(statistics.Download),
			formatPtrDuration(statistics.Total),
			formatPtrBool(statistics.Reused),
			formatString(statistics.Proto),
			formatString(statistics.Status),
			formatErrMsg(errMsg),
		)

		// The requested amount of requests has been reached, break out of the for loop
		if requests == count {
			break
		}

		done := false

		select {
		case <-ctx.Done():
			done = true // The program was interrupted while sleeping, break out of the for loop
		case <-time.After(max(time.Duration(delay)*time.Millisecond-*statistics.Total, 0)):
		}

		if done {
			break
		}
	}

	min_, _ := stats.Min(totals)
	max_, _ := stats.Max(totals)
	average, _ := stats.Mean(totals)

	percentile99, _ := stats.Percentile(totals, 99)
	percentile95, _ := stats.Percentile(totals, 95)
	percentile90, _ := stats.Percentile(totals, 90)
	percentile75, _ := stats.Percentile(totals, 75)
	percentile50, _ := stats.Percentile(totals, 50)

	fmt.Println()
	fmt.Printf("Requests: %d (%d successful, %d failed)\n", requests, successful, failed)

	if len(totals) > 0 {
		fmt.Println()
		fmt.Printf("Min: %.1fms\n", min_)
		fmt.Printf("Max: %.1fms\n", max_)
		fmt.Printf("Average: %.1fms\n", average)

		fmt.Println()
		fmt.Printf("99th Percentile: %.1fms\n", percentile99)
		fmt.Printf("95th Percentile: %.1fms\n", percentile95)
		fmt.Printf("90th Percentile: %.1fms\n", percentile90)
		fmt.Printf("75th Percentile: %.1fms\n", percentile75)
		fmt.Printf("50th Percentile: %.1fms\n", percentile50)
	}
}

func sendRequest(client *http.Client, ctx context.Context, targetUrl string) (*Statistics, error) {
	statistics := &Statistics{}
	startTime := time.Now()

	defer func() {
		diff := time.Now().Sub(startTime)
		statistics.Total = &diff
	}()

	var dnsStart, connectStart, tlsHandshakeStart time.Time

	trace := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			diff := time.Now().Sub(dnsStart)
			statistics.DNS = &diff
		},
		ConnectStart: func(network, addr string) {
			connectStart = time.Now()
		},
		ConnectDone: func(network, addr string, err error) {
			diff := time.Now().Sub(connectStart)
			statistics.Connect = &diff
		},
		TLSHandshakeStart: func() {
			tlsHandshakeStart = time.Now()
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			diff := time.Now().Sub(tlsHandshakeStart)
			statistics.TLSHandshake = &diff
		},
		GotFirstResponseByte: func() {
			diff := time.Now().Sub(startTime)
			statistics.TTFB = &diff
		},
		GotConn: func(info httptrace.GotConnInfo) {
			statistics.Reused = &info.Reused
		},
	}

	// Make a new GET request with the client trace
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), "GET", targetUrl, nil)

	// Send the request
	res, err := client.Do(req)

	if err != nil {
		return statistics, err
	}

	defer res.Body.Close()

	statistics.Proto = res.Proto
	statistics.Status = res.Status

	downloadStart := time.Now()

	_, err = io.Copy(io.Discard, res.Body)

	if err != nil {
		return statistics, err
	}

	diff := time.Now().Sub(downloadStart)
	statistics.Download = &diff
	return statistics, nil
}

const (
	reset  = "\u001B[0m"
	red    = "\u001B[91m"
	green  = "\u001B[92m"
	format = "%s%-9s%s"
)

func formatPtrDuration(duration *time.Duration) string {
	if duration == nil {
		return fmt.Sprintf(format, red, "N/A", reset)
	}
	return fmt.Sprintf(format, green, fmt.Sprintf("%.1fms", float64(*duration)/float64(time.Millisecond)), reset)
}

func formatPtrBool(b *bool) string {
	if b == nil {
		return fmt.Sprintf(format, red, "N/A", reset)
	} else if *b {
		return fmt.Sprintf(format, green, strconv.FormatBool(*b), reset)
	} else {
		return fmt.Sprintf(format, red, strconv.FormatBool(*b), reset)
	}
}

func formatString(s string) string {
	if s == "" {
		return fmt.Sprintf(format, red, "N/A", reset)
	}
	return fmt.Sprintf(format, green, s, reset)
}

func formatErrMsg(s string) string {
	if s == "" {
		return fmt.Sprintf(format, green, "N/A", reset)
	}
	return fmt.Sprintf(format, red, s, reset)
}
