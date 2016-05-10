package mockhttpclient

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"reflect"

	"go.skia.org/infra/go/util"
)

// URLMock implements http.RoundTripper but returns mocked responses. It
// provides two methods for mocking responses to requests for particular URLs:
//
// - Mock: Adds a fake response for the given URL to be used every time a
//         request is made for that URL.
//
// - MockOnce: Adds a fake response for the given URL to be used one time.
//         MockOnce may be called multiple times for the same URL in order to
//         simulate the response changing over time. Takes precedence over mocks
//         specified using Mock.
//
// Examples:
//
//    // Mock out a URL to always respond with the same body.
//    m := NewURLMock()
//    m.Mock("https://www.google.com", []byte("Here's a response.")
//    res, _ := m.Client().Get("https://www.google.com")
//    respBody, _ := ioutil.ReadAll(res.Body)  // respBody == []byte("Here's a response.")
//
//    // Mock out a URL to give different responses.
//    m.MockOnce("https://www.google.com", []byte("hi"))
//    m.MockOnce("https://www.google.com", []byte("Second response."))
//    res1, _ := m.Client().Get("https://www.google.com")
//    body1, _ := ioutil.ReadAll(res1.Body)  // body1 == []byte("hi")
//    res2, _ := m.Client().Get("https://www.google.com")
//    body2, _ := ioutil.ReadAll(res2.Body)  // body2 == []byte("Second response.")
//    // Fall back on the value previously set using Mock():
//    res3, _ := m.Client().Get("https://www.google.com")
//    body3, _ := ioutil.ReadAll(res3.Body)  // body3 == []byte("Here's a response.")
type URLMock struct {
	mockAlways map[string]MockDialogue
	mockOnce   map[string][]MockDialogue
}

var DONT_CARE_REQUEST = []byte{0, 1, 2, 3, 4}

type MockDialogue struct {
	requestMethod  string
	requestType    string
	requestPayload []byte

	responseStatus  string
	responseCode    int
	responsePayload []byte
}

func MockGetDialogue(responseBody []byte) MockDialogue {
	return MockDialogue{
		requestMethod:  "GET",
		requestType:    "",
		requestPayload: nil,

		responseStatus:  "OK",
		responseCode:    http.StatusOK,
		responsePayload: responseBody,
	}
}

func MockPostDialogue(requestType string, requestBody, responseBody []byte) MockDialogue {
	return MockDialogue{
		requestMethod:  "POST",
		requestType:    requestType,
		requestPayload: requestBody,

		responseStatus:  "OK",
		responseCode:    http.StatusOK,
		responsePayload: responseBody,
	}
}

// Mock adds a mocked response for the given URL; whenever this URLMock is used
// as a transport for an http.Client, requests to the given URL will always
// receive the given body in their responses. Mocks specified using Mock() are
// independent of those specified MockOnce(), except that those specified using
// MockOnce() take precedence when present.
func (m *URLMock) Mock(url string, md MockDialogue) {
	m.mockAlways[url] = md
}

// MockOnce adds a mocked response for the given URL, to be used exactly once.
// Mocks are stored in a FIFO queue and removed from the queue as they are
// requested. Therefore, multiple requests to the same URL must each correspond
// to a call to MockOnce, in the same order that the requests will be made.
// Mocks specified this way are independent of those specified using Mock(),
// except that those specified using MockOnce() take precedence when present.
func (m *URLMock) MockOnce(url string, md MockDialogue) {
	if _, ok := m.mockOnce[url]; !ok {
		m.mockOnce[url] = []MockDialogue{}
	}
	m.mockOnce[url] = append(m.mockOnce[url], md)
}

// Client returns an http.Client instance which uses the URLMock.
func (m *URLMock) Client() *http.Client {
	return &http.Client{
		Transport: m,
	}
}

// RoundTrip is an implementation of http.RoundTripper.RoundTrip. It fakes
// responses for requests to URLs based on past calls to Mock() and MockOnce().
func (m *URLMock) RoundTrip(r *http.Request) (*http.Response, error) {
	url := r.URL.String()
	var md *MockDialogue
	if resps, ok := m.mockOnce[url]; ok {
		if resps != nil && len(resps) > 0 {
			md = &resps[0]
			m.mockOnce[url] = m.mockOnce[url][1:]
		}
	} else if data, ok := m.mockAlways[url]; ok {
		md = &data
	}
	if md == nil {
		return nil, fmt.Errorf("Unknown URL %q", url)
	}
	if md.requestMethod != r.Method {
		return nil, fmt.Errorf("Wrong Method, expected %q, but was %q", md.requestMethod, r.Method)
	}
	if md.requestPayload == nil {
		if r.Body != nil {
			requestBody, _ := ioutil.ReadAll(r.Body)
			return nil, fmt.Errorf("No request payload expected, but was %s (%#v) ", string(requestBody), r.Body)
		}
	} else {
		if ct := r.Header.Get("Content-Type"); md.requestType != ct {
			return nil, fmt.Errorf("Content-Type was wrong, expected %q, but was %q", md.requestType, ct)
		}
		defer util.Close(r.Body)
		requestBody, err := ioutil.ReadAll(r.Body)
		if err != nil {
			return nil, fmt.Errorf("Error reading request body: %s", err)
		}
		if !reflect.DeepEqual(md.requestPayload, DONT_CARE_REQUEST) && !reflect.DeepEqual(md.requestPayload, requestBody) {
			return nil, fmt.Errorf("Wrong request payload, expected \n%s, but was \n%s", md.requestPayload, requestBody)
		}
	}

	return &http.Response{
		Body:       &respBodyCloser{bytes.NewReader(md.responsePayload)},
		Status:     md.responseStatus,
		StatusCode: md.responseCode,
	}, nil
}

// Empty returns true iff all of the URLs registered via MockOnce() have been
// used.
func (m *URLMock) Empty() bool {
	for _, resps := range m.mockOnce {
		if resps != nil && len(resps) > 0 {
			return false
		}
	}
	return true
}

// respBodyCloser is a wrapper which lets us pretend to implement io.ReadCloser
// by wrapping a bytes.Reader.
type respBodyCloser struct {
	io.Reader
}

// Close is a stub method which lets us pretend to implement io.ReadCloser.
func (r respBodyCloser) Close() error {
	return nil
}

// NewURLMock returns an empty URLMock instance.
func NewURLMock() *URLMock {
	return &URLMock{
		mockAlways: map[string]MockDialogue{},
		mockOnce:   map[string][]MockDialogue{},
	}
}

// New returns a new mocked HTTPClient.
func New(urlMap map[string]MockDialogue) *http.Client {
	m := NewURLMock()
	for k, v := range urlMap {
		m.Mock(k, v)
	}
	return m.Client()
}
