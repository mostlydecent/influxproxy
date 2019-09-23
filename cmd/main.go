package main

import (
	_bufio "bufio"
	_bytes "bytes"
	_flag "flag"
	_format "fmt"
	_io "io"
	_ioutil "io/ioutil"
	_http "net/http"
	_os "os"
	_signal "os/signal"
	_regex "regexp"
	_strings "strings"
	_time "time"

	_snappy "github.com/golang/snappy"
	_prommodel "github.com/prometheus/common/model"
	_prompb "github.com/prometheus/prometheus/prompb"
	_yaml "gopkg.in/yaml.v2"
)

var fHost string
var fPort string
var fPath string

var pattern *_regex.Regexp

type RetentionPolicy struct {
	Duration _time.Duration `yaml:"duration"`
	Name     string         `yaml:"name"`
	Step     _time.Duration `yaml:"step"`
}

type ProxyDefault struct {
	Policy   string         `yaml:"policy"`
	Pattern  string         `yaml:"pattern"`
	Scheme   string         `yaml:"scheme"`
	Step     _time.Duration `yaml:"step"`
	Duration _time.Duration `yaml:"duration"`
}

type ProxyConfiguration struct {
	Policies []RetentionPolicy `yaml:"policies,flow"`
	Default  ProxyDefault      `yaml:"default"`
}

var configuration ProxyConfiguration

func init() {
	_flag.StringVar(&fHost, "host", "localhost", "host name to target")
	_flag.StringVar(&fPort, "port", "8086", "port to target")
	_flag.StringVar(&fPath, "config", "", "configuration file path")
	_flag.Parse()
}

func getProxyAddress() string {
	return _strings.Join([]string{fHost, fPort}, ":")
}

type PolicyScore struct {
	Definition RetentionPolicy
	Score      _time.Duration
}

func absD(d _time.Duration) _time.Duration {
	if d < _time.Duration(0) {
		return -d
	}

	return d
}

func halfD(d _time.Duration) _time.Duration {
	return d / 2
}

func getPolicyForInterval(interval _time.Duration) RetentionPolicy {

	policies := make([]PolicyScore, 0)
	maximum := _time.Duration(0)
	for _, policy := range configuration.Policies {
		score := policy.Duration - interval
		if score < -halfD(policy.Duration) {
			continue
		}

		policies = append(policies, PolicyScore{
			Definition: policy,
			Score:      score,
		})

		if score > maximum {
			maximum = score
		}
	}

	bestPolicy := PolicyScore{
		Definition: RetentionPolicy{
			Name:     configuration.Default.Policy,
			Duration: configuration.Default.Duration,
			Step:     configuration.Default.Step,
		},
		Score: maximum + 1,
	}

	for _, policy := range policies {
		if bestPolicy.Score > policy.Score {
			bestPolicy = policy
		}
	}

	return bestPolicy.Definition
}

func proxyRequestFor(request *_http.Request) (*_http.Response, error) {
	proxyRequest, e := _http.NewRequest(request.Method, request.URL.String(), request.Body)
	if e != nil {
		return nil, e
	}

	proxyResponse, e := _http.DefaultClient.Do(proxyRequest)
	if e != nil {
		return nil, e
	}

	return proxyResponse, nil
}

func respondWith(response *_http.Response, writer _http.ResponseWriter) {
	writer.WriteHeader(response.StatusCode)
	_io.Copy(writer, response.Body)
}

func max(first _time.Duration, second _time.Duration) _time.Duration {
	if first > second {
		return first
	}

	return second
}

func getReadRequest(request *_http.Request) *_prompb.ReadRequest {

	buffer := _bufio.NewReader(request.Body)
	compressed, _ := _ioutil.ReadAll(buffer)
	request.Body.Close()

	bytes, _ := _snappy.Decode(nil, compressed)

	rreq := &_prompb.ReadRequest{}
	rreq.Unmarshal(bytes)

	return rreq
}

func encodeReadRequest(readRequest *_prompb.ReadRequest) []byte {

	bytes, _ := readRequest.Marshal()
	compressed := _snappy.Encode(nil, bytes)
	return compressed
}

func findMaximumDuration(readRequest *_prompb.ReadRequest) _time.Duration {
	maxDuration := _time.Duration(0)
	for _, query := range readRequest.Queries {
		tStart := _prommodel.Time(query.StartTimestampMs)
		tEnd := _prommodel.Time(query.EndTimestampMs)

		maxDuration = max(tEnd.Sub(tStart), maxDuration)
	}

	return maxDuration
}

func updateRetentionPolicy(request *_http.Request, policy string) string {
	values := request.URL.Query()
	values.Set("rp", policy)
	return values.Encode()
}

func updateStepHints(readRequest *_prompb.ReadRequest, step _time.Duration, length _time.Duration) {
	for _, query := range readRequest.Queries {
		query.Hints.StepMs = int64(step.Seconds() * 1000)
		tStart := _prommodel.Time(query.Hints.StartMs)
		query.Hints.EndMs = int64(tStart.Add(length))
	}
}

func proxyRequestHandler(response _http.ResponseWriter, request *_http.Request) {

	readRequest := getReadRequest(request)
	maxDuration := findMaximumDuration(readRequest)
	policy := getPolicyForInterval(maxDuration)
	updateStepHints(readRequest, policy.Step, policy.Duration)

	println(_format.Sprintf("%s -> %s", maxDuration.String(), policy.Name))

	request.URL.Scheme = "http"
	request.URL.Host = getProxyAddress()
	request.URL.RawQuery = updateRetentionPolicy(request, policy.Name)
	request.Body = _ioutil.NopCloser(_bytes.NewBuffer(encodeReadRequest(readRequest)))

	proxyResponse, e := proxyRequestFor(request)
	if e != nil {
		println(e.Error())
	}
	defer proxyResponse.Body.Close()

	println(_format.Sprintf("[%d] %s %s", proxyResponse.StatusCode, proxyResponse.Request.Method, proxyResponse.Request.URL.String()))
	respondWith(proxyResponse, response)
}

func main() {

	file, e := _os.Open(fPath)
	if e != nil {
		println(e.Error())
		return
	}

	bytes, e := _ioutil.ReadAll(file)
	if e != nil {
		println(e.Error())
		return
	}

	e = _yaml.Unmarshal(bytes, &configuration)
	if e != nil {
		println(e.Error())
		return
	}

	pattern, _ = _regex.Compile(configuration.Default.Pattern)

	mux := _http.NewServeMux()
	mux.HandleFunc("/", proxyRequestHandler)

	println("listening on :3030")
	_http.ListenAndServe(":3030", mux)

	signals := make(chan _os.Signal)
	_signal.Notify(signals, _os.Interrupt)

	select {
	case <-signals:
		break
	}
}
