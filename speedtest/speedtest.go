package speedtest

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"cfspeedtest/stats"
	"cfspeedtest/timeCalculations"
)

var (
	// tr = &http.Transport{
	// 	TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	// }
	umt             = time.Now()
	start_timestamp = umt.Unix()

	//client = &http.Client{Transport: tr, Timeout: 10 * time.Second}

	latencyreps = 20
	userAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/113.0.0.0 Safari/537.36"
	// Command line flags.
	httpMethod      string
	postBody        string
	followRedirects bool
	onlyHeader      bool
	insecure        bool
	httpHeaders     headers
	saveOutput      bool
	outputFile      string
	showVersion     bool
	clientCertFile  string
	fourOnly        bool
	sixOnly         bool

	// number of redirects followed
	redirectsFollowed int

	version = "devel" // for -v flag, updated during the release process with -ldflags=-X=main.version=...
)

type headers []string

func (h headers) String() string {
	var o []string
	for _, v := range h {
		o = append(o, "-H "+v)
	}
	return strings.Join(o, " ")
}

func (h *headers) Set(v string) error {
	*h = append(*h, v)
	return nil
}

func (h headers) Len() int      { return len(h) }
func (h headers) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h headers) Less(i, j int) bool {
	a, b := h[i], h[j]

	// server always sorts at the top
	if a == "Server" {
		return true
	}
	if b == "Server" {
		return false
	}

	endtoend := func(n string) bool {
		// https://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html#sec13.5.1
		switch n {
		case "Connection",
			"Keep-Alive",
			"Proxy-Authenticate",
			"Proxy-Authorization",
			"TE",
			"Trailers",
			"Transfer-Encoding",
			"Upgrade":
			return false
		default:
			return true
		}
	}

	x, y := endtoend(a), endtoend(b)
	if x == y {
		// both are of the same class
		return a < b
	}
	return x
}

type Speedtest struct {
	UploadTests, DownloadTests []Test
}

func NewSpeedtest(UploadTests []Test, DownloadTests []Test) *Speedtest {
	return &Speedtest{
		UploadTests:   UploadTests,
		DownloadTests: DownloadTests,
	}
}

type Test struct {
	NumBytes, Iterations int
	Name                 string
}

func NewTest(NumBytes int, Iterations int, Name string) *Test {
	return &Test{
		NumBytes:   NumBytes,
		Iterations: Iterations,
		Name:       Name,
	}
}

func PrintToStderr(a ...interface{}) {
	// Here a is the array holding the objects
	// passed as the argument of the function
	fmt.Fprintln(os.Stderr, a...)
}

func (s *Speedtest) PrintOut(label string, MetricValue float64) {
	fmt.Printf("%v %f", label, MetricValue)
}

type transport struct {
	current *http.Request
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.current = req
	return http.DefaultTransport.RoundTrip(req)
}

func readClientCert(filename string) []tls.Certificate {
	if filename == "" {
		return nil
	}
	var (
		pkeyPem []byte
		certPem []byte
	)

	// read client certificate file (must include client private key and certificate)
	certFileBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatalf("failed to read client certificate file: %v", err)
	}

	for {
		block, rest := pem.Decode(certFileBytes)
		if block == nil {
			break
		}
		certFileBytes = rest

		if strings.HasSuffix(block.Type, "PRIVATE KEY") {
			pkeyPem = pem.EncodeToMemory(block)
		}
		if strings.HasSuffix(block.Type, "CERTIFICATE") {
			certPem = pem.EncodeToMemory(block)
		}
	}

	cert, err := tls.X509KeyPair(certPem, pkeyPem)
	if err != nil {
		log.Fatalf("unable to load client cert and key pair: %v", err)
	}
	return []tls.Certificate{cert}
}

func parseURL(uri string) *url.URL {
	if !strings.Contains(uri, "://") && !strings.HasPrefix(uri, "//") {
		uri = "//" + uri
	}

	url, err := url.Parse(uri)
	if err != nil {
		log.Fatalf("could not parse url %q: %v", uri, err)
	}

	if url.Scheme == "" {
		url.Scheme = "http"
		if !strings.HasSuffix(url.Host, ":80") {
			url.Scheme += "s"
		}
	}
	return url
}

func headerKeyValue(h string) (string, string) {
	i := strings.Index(h, ":")
	if i == -1 {
		fmt.Printf("Header '%s' has invalid format, missing ':'", h)
	}
	return strings.TrimRight(h[:i], " "), strings.TrimLeft(h[i:], " :")
}

func dialContext(network string) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, _, addr string) (net.Conn, error) {
		return (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: false,
		}).DialContext(ctx, network, addr)
	}
}
func isRedirect(resp *http.Response) bool {
	return resp.StatusCode > 299 && resp.StatusCode < 400
}

func newRequest(method string, url *url.URL, body string) *http.Request {
	req, err := http.NewRequest(method, url.String(), createBody(body))
	if err != nil {
		log.Fatalf("unable to create request: %v", err)
	}
	for _, h := range httpHeaders {
		k, v := headerKeyValue(h)
		if strings.EqualFold(k, "host") {
			req.Host = v
			continue
		}
		req.Header.Add(k, v)
	}
	return req
}

func createBody(body string) io.Reader {
	if strings.HasPrefix(body, "@") {
		filename := body[1:]
		f, err := os.Open(filename)
		if err != nil {
			log.Fatalf("failed to open data file %s: %v", filename, err)
		}
		return f
	}
	return strings.NewReader(body)
}

// getFilenameFromHeaders tries to automatically determine the output filename,
// when saving to disk, based on the Content-Disposition header.
// If the header is not present, or it does not contain enough information to
// determine which filename to use, this function returns "".
func getFilenameFromHeaders(headers http.Header) string {
	// if the Content-Disposition header is set parse it
	if hdr := headers.Get("Content-Disposition"); hdr != "" {
		// pull the media type, and subsequent params, from
		// the body of the header field
		mt, params, err := mime.ParseMediaType(hdr)

		// if there was no error and the media type is attachment
		if err == nil && mt == "attachment" {
			if filename := params["filename"]; filename != "" {
				return filename
			}
		}
	}

	// return an empty string if we were unable to determine the filename
	return ""
}

// readResponseBody consumes the body of the response.
// readResponseBody returns an informational message about the
// disposition of the response body's contents.
func readResponseBody(req *http.Request, resp *http.Response) string {
	if isRedirect(resp) || req.Method == http.MethodHead {
		return ""
	}

	w := ioutil.Discard
	msg := "Body discarded"

	if saveOutput || outputFile != "" {
		filename := outputFile

		if saveOutput {
			// try to get the filename from the Content-Disposition header
			// otherwise fall back to the RequestURI
			if filename = getFilenameFromHeaders(resp.Header); filename == "" {
				filename = path.Base(req.URL.RequestURI())
			}

			if filename == "/" {
				log.Fatalf("No remote filename; specify output filename with -o to save response body")
			}
		}

		f, err := os.Create(filename)
		if err != nil {
			log.Fatalf("unable to create file %s: %v", filename, err)
		}
		defer f.Close()
		w = f
		msg = "Body read"
	}

	if _, err := io.Copy(w, resp.Body); err != nil && w != ioutil.Discard {
		fmt.Fprintf(os.Stderr, "failed to read response body "+err.Error())
	}

	return msg
}

// runs download tests
func (s *Speedtest) Download(numbytes int, iterations int) ([]time.Duration, []time.Duration, []time.Duration, []time.Duration, []time.Duration) {
	var fulltimes []time.Duration
	var servertimes []time.Duration
	var dnstimes []time.Duration
	var tcptimes []time.Duration
	var transfertimes []time.Duration

	download_url := parseURL("http://speed.cloudflare.com/__down?bytes=" + strconv.Itoa(numbytes))

	//visit(url)

	for i := 0; i < iterations; i++ {
		//t := &transport{}
		req, _ := http.NewRequest("GET", download_url.String(), nil)
		var t0, t1, t2, t3, t4, t5, t6 time.Time
		trace := &httptrace.ClientTrace{
			DNSStart: func(_ httptrace.DNSStartInfo) { t0 = time.Now() },
			DNSDone:  func(_ httptrace.DNSDoneInfo) { t1 = time.Now() },
			ConnectStart: func(_, _ string) {
				if t1.IsZero() {
					// connecting to IP
					t1 = time.Now()
				}
			},
			ConnectDone: func(net, addr string, err error) {
				if err != nil {
					fmt.Fprintf(os.Stderr, "unable to connect to host "+addr+" "+err.Error())
				}
				t2 = time.Now()
				//printf("\n%s%s\n", color.GreenString("Connected to "), color.CyanString(addr))
			},
			GotConn:              func(_ httptrace.GotConnInfo) { t3 = time.Now() },
			GotFirstResponseByte: func() { t4 = time.Now() },
			TLSHandshakeStart:    func() { t5 = time.Now() },
			TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { t6 = time.Now() },
		}
		req = req.WithContext(httptrace.WithClientTrace(context.Background(), trace))
		req.Header.Set("User-Agent", userAgent)
		t := &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		}
		t.DialContext = dialContext("tcp4")
		switch download_url.Scheme {
		case "https":
			host, _, err := net.SplitHostPort(req.Host)
			if err != nil {
				host = req.Host
			}

			t.TLSClientConfig = &tls.Config{
				ServerName:         host,
				InsecureSkipVerify: insecure,
				Certificates:       readClientCert(clientCertFile),
				MinVersion:         tls.VersionTLS12,
			}
		}

		client := &http.Client{
			Transport: t,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// always refuse to follow redirects, visit does that
				// manually if required.
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "request failed "+err.Error())
		}
		// bodyMsg := readResponseBody(req, resp)
		// if bodyMsg != "" {
		// 	fmt.Printf("Download \n%s\n", bodyMsg)
		// }
		body, err := io.ReadAll(resp.Body)

		resp.Body.Close()

		t7 := time.Now() // after read body
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading body "+strconv.Itoa(len(body))+" "+err.Error())
		}
		if resp.StatusCode == 200 {
			//fmt.Printf("download %v\n", resp.Status)
			if t0.IsZero() {
				// we skipped DNS
				t0 = t1
			}

			//fmt.Println(t0.UnixMilli(), t1.UnixMilli(), t2.UnixMilli(), t3.UnixMilli(), t4.UnixMilli(), t7.UnixMilli())
			//fmt.Printf("t4: %v, t7: %v, sub: %v\n", t4, t7, t7.Sub(t4))
			switch download_url.Scheme {
			case "https":
				_ = t6.Sub(t5)                          // tls handshake
				_ = t2.Sub(t0)                          // connect
				dnstimes = append(dnstimes, t1.Sub(t0)) // dns lookup
				tcptimes = append(tcptimes, t3.Sub(t1))
				servertimes = append(servertimes, t4.Sub(t3))     // server processing
				fulltimes = append(fulltimes, t7.Sub(t0))         // total
				transfertimes = append(transfertimes, t7.Sub(t4)) // GET content transfer starts after server responds

				// t1.Sub(t0) // namelookup
				// t2.Sub(t0) // connect
				// 	t4.Sub(t0) // starttransfer
			case "http":
				_ = t3.Sub(t1)                                    // tcp connection
				_ = t2.Sub(t0)                                    // connected
				dnstimes = append(dnstimes, t1.Sub(t0))           // dns lookup
				tcptimes = append(tcptimes, t3.Sub(t1))           // tcp connection
				servertimes = append(servertimes, t4.Sub(t3))     // server processing
				fulltimes = append(fulltimes, t7.Sub(t0))         // total
				transfertimes = append(transfertimes, t7.Sub(t4)) // GET content transfer starts after server responds

				// t1.Sub(t0) // namelookup
				// t2.Sub(t0) // connection complete
				// 	t4.Sub(t0) // starttransfer

			}
		} else {
			fmt.Fprintf(os.Stderr, download_url.String()+" "+strconv.Itoa(resp.StatusCode))
		}

	}
	return fulltimes, servertimes, tcptimes, dnstimes, transfertimes
}

// runs upload tests
func (s *Speedtest) Upload(numbytes int, iterations int) ([]time.Duration, []time.Duration, []time.Duration, []time.Duration, []time.Duration) {
	var fulltimes []time.Duration
	var servertimes []time.Duration
	var dnstimes []time.Duration
	var tcptimes []time.Duration
	var transfertimes []time.Duration
	thedata := make([]byte, numbytes)

	upload_url := parseURL("http://speed.cloudflare.com/__up")

	for i := 0; i < iterations; i++ {
		data := url.Values{}
		data.Set("data", string(thedata))
		req, _ := http.NewRequest("POST", upload_url.String(), strings.NewReader(data.Encode()))

		var t0, t1, t2, t3, t4, t5, t6 time.Time
		trace := &httptrace.ClientTrace{
			DNSStart: func(_ httptrace.DNSStartInfo) { t0 = time.Now() },
			DNSDone:  func(_ httptrace.DNSDoneInfo) { t1 = time.Now() },
			ConnectStart: func(_, _ string) {
				if t1.IsZero() {
					// connecting to IP
					t1 = time.Now()
				}
			},
			ConnectDone: func(net, addr string, err error) {
				if err != nil {
					fmt.Fprintf(os.Stderr, "unable to connect to host "+addr+" "+err.Error())
				}
				t2 = time.Now()
				//printf("\n%s%s\n", color.GreenString("Connected to "), color.CyanString(addr))
			},
			GotConn:              func(_ httptrace.GotConnInfo) { t3 = time.Now() },
			GotFirstResponseByte: func() { t4 = time.Now() },
			TLSHandshakeStart:    func() { t5 = time.Now() },
			TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { t6 = time.Now() },
		}
		req = req.WithContext(httptrace.WithClientTrace(context.Background(), trace))

		req.Header.Set("User-Agent", userAgent)
		t := &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		}
		t.DialContext = dialContext("tcp4")
		switch upload_url.Scheme {
		case "https":
			host, _, err := net.SplitHostPort(req.Host)
			if err != nil {
				host = req.Host
			}

			t.TLSClientConfig = &tls.Config{
				ServerName:         host,
				InsecureSkipVerify: insecure,
				Certificates:       readClientCert(clientCertFile),
				MinVersion:         tls.VersionTLS12,
			}
		}

		client := &http.Client{
			Transport: t,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// always refuse to follow redirects, visit does that
				// manually if required.
				return http.ErrUseLastResponse
			},
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("failed to read response: %v\n", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		t7 := time.Now() // after read body
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading body "+strconv.Itoa(len(body))+" "+err.Error())
		}
		if resp.StatusCode == 200 {
			if t0.IsZero() {
				// we skipped DNS
				t0 = t1
			}
			//fmt.Println(t0.UnixMilli(), t1.UnixMilli(), t2.UnixMilli(), t3.UnixMilli(), t4.UnixMilli(), t7.UnixMilli())

			switch upload_url.Scheme {
			case "https":
				_ = t6.Sub(t5)                          // tls handshake
				_ = t2.Sub(t0)                          // connect
				dnstimes = append(dnstimes, t1.Sub(t0)) // dns lookup
				tcptimes = append(tcptimes, t3.Sub(t1))
				servertimes = append(servertimes, t4.Sub(t3))     // server processing
				fulltimes = append(fulltimes, t7.Sub(t0))         // total
				transfertimes = append(transfertimes, t7.Sub(t3)) // POST content transfer starts when connection completes

				// t1.Sub(t0) // namelookup
				// t2.Sub(t0) // connect
				// 	t4.Sub(t0) // starttransfer
			case "http":
				_ = t3.Sub(t1)                                    // tcp connection
				_ = t2.Sub(t0)                                    // connected
				dnstimes = append(dnstimes, t1.Sub(t0))           // dns lookup
				tcptimes = append(tcptimes, t3.Sub(t1))           // tcp connection
				servertimes = append(servertimes, t4.Sub(t3))     // server processing
				fulltimes = append(fulltimes, t7.Sub(t0))         // total
				transfertimes = append(transfertimes, t7.Sub(t3)) // POST content transfer starts when connection completes

				// t1.Sub(t0) // namelookup
				// t2.Sub(t0) // connection complete
				// 	t4.Sub(t0) // starttransfer

			}
		} else {
			fmt.Fprintf(os.Stderr, upload_url.String()+" "+strconv.Itoa(resp.StatusCode))
		}

	}
	return fulltimes, servertimes, tcptimes, dnstimes, transfertimes
}

func (s *Speedtest) RunAllTests() {

	fmt.Printf("cf_start_timestamp %v\n", start_timestamp)
	_, _, tcptimes, dnstimes, _ := s.Download(0, latencyreps)

	calc_tcp_jitter := timeCalculations.CalculateCorrectedDeviation(tcptimes)
	avg_latency := timeCalculations.CalculateAverageDuration(tcptimes)
	avg_dnstime := timeCalculations.CalculateAverageDuration(dnstimes)
	fmt.Printf("cf_latency_ms %.2f\n", avg_latency/1e6)
	fmt.Printf("cf_tcp_jitter_ms %.2f\n", calc_tcp_jitter/1e6)
	fmt.Printf("cf_dnslookup_ms %.2f\n", avg_dnstime/1e6)

	download_results := make([]float64, 0)
	upload_results := make([]float64, 0)

	for t := 0; t < len(s.DownloadTests); t++ {
		downfulltimes, _, downtcptimes, _, downtransfertimes := s.Download(s.DownloadTests[t].NumBytes, s.DownloadTests[t].Iterations)
		down_tcp_jitter := timeCalculations.CalculateCorrectedDeviation(downtcptimes)
		avg_downtransfer := timeCalculations.CalculateAverageDurationSeconds(downtransfertimes)
		//fmt.Printf("download %v\n", avg_downtransfer)
		downspeed := (float64(s.DownloadTests[t].NumBytes*8) / avg_downtransfer) / 1e6
		down_latency := timeCalculations.CalculateAverageDuration(downfulltimes)
		fmt.Printf("cf_%v_download_latency_ms %.2f\n", s.DownloadTests[t].Name, down_latency/1e6)
		fmt.Printf("cf_%v_download_Mbps %.2f\n", s.DownloadTests[t].Name, downspeed)
		fmt.Printf("cf_%v_download_tcp_jitter %.2f\n", s.DownloadTests[t].Name, down_tcp_jitter/1e6)
		download_results = append(download_results, downspeed)
	}
	down_per, _ := stats.Percentile(download_results, 90.0)
	fmt.Printf("cf_90th_percentile_download_speed %.2f\n", down_per)

	for t := 0; t < len(s.UploadTests); t++ {
		upfulltimes, _, uptcptimes, _, uptransfertimes := s.Upload(s.UploadTests[t].NumBytes, s.UploadTests[t].Iterations)
		up_tcp_jitter := timeCalculations.CalculateCorrectedDeviation(uptcptimes)
		avg_uptransfer := timeCalculations.CalculateAverageDurationSeconds(uptransfertimes)
		//fmt.Printf("upload %v s\n", avg_uptransfer)
		upspeed := (float64(s.UploadTests[t].NumBytes*8) / avg_uptransfer) / 1e6
		up_latency := timeCalculations.CalculateAverageDuration(upfulltimes)
		fmt.Printf("cf_%v_upload_latency_ms %.2f\n", s.UploadTests[t].Name, up_latency/1e6)
		fmt.Printf("cf_%v_upload_Mbps %.2f\n", s.UploadTests[t].Name, upspeed)
		fmt.Printf("cf_%v_upload_tcp_jitter %.2f\n", s.UploadTests[t].Name, up_tcp_jitter/1e6)
		upload_results = append(upload_results, upspeed)
	}
	up_per, _ := stats.Percentile(upload_results, 90.0)
	fmt.Printf("cf_90th_percentile_upload_speed %.2f\n", up_per)

}
