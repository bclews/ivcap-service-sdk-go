package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"log"
)

const (
	MAX_ATTEMPTS = 10

	// referencing the otehr packages blows up Docker - keep an eye on this
	ARTIFACT_ID_HEADER            = "X-Artifact-Id"            // tus.ARTIFACT_HEADER
	CACHE_ID_HEADER               = "X-Cache-Id"               // data_proxy/pkg/caddy/cache/cache.go
	NAME_HEADER                   = "X-Name"                   // tus.NAME_HEADER
	META_DATA_FOR_ARTIFACT_HEADER = "X-Meta-Data-For-Artifact" // metadata.META_DATA_FOR_ARTIFACT_HEADER
	META_DATA_SCHEMA_HEADER       = "X-Meta-Data-Schema"       // metadata.META_DATA_SCHEMA_HEADER

	META_DATA_SCHEMA = "urn:schema:testing:image"

	ORDER_ID_ENV = "IVCAP_ORDER_ID"
	// The sidecar routes this API URL through http_proxy.
	BASE_URL_ENV = "IVCAP_BASE_URL"
	BASE_URL_DEF = "http://ivcap.local"
	// Deprecated: The SDK uses this URL only if IVCAP_BASE_URL is empty.
	STORAGE_URL_ENV = "IVCAP_STORAGE_URL"
	// Deprecated: This constant exists for source compatibility.
	STORAGE_URL_DEF = "http://localhost:8888"
	SIDECAR_URL_DEF = "http://localhost:8888"

	ARTIFACT_STATUS_READY = "ready"

	ARTIFACTS_PATH = "/1/artifacts"
	ASPECTS_PATH   = "/1/aspects"
	CACHE_URL_ENV  = "IVCAP_CACHE_URL"

	READYZ = "/readyz"
)

type EnvironmentNotReadyError struct{}

func (e *EnvironmentNotReadyError) Error() string {
	return "IVCAP environment doesn't seem to ready"
}

type ApiError struct {
	Message string
	Err     error
}

func (e *ApiError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("IVCAP API error - %s", e.Message)
	}
	return fmt.Sprintf("IVCAP API error - %s (%s)", e.Message, e.Err)
}

type HttpError struct {
	Request  *http.Request
	Response *http.Response
	Err      error
}

func (e *HttpError) Error() string {
	if e.Response == nil {
		return fmt.Sprintf("IVCAP: http request failed (%v) - %s", e.Request, e.Err)
	} else if e.Err == nil {
		return fmt.Sprintf("IVCAP: http response failed (%d)", e.Response.StatusCode)
	} else {
		return fmt.Sprintf("IVCAP: http response failed (%d) - %s", e.Response.StatusCode, e.Err)
	}
}

type Environment struct {
	localMode bool
	noCaching bool
	logger    LoggerI
	baseURL   string
	cacheURL  string
}

type LoggerI interface {
	Error(format string, v ...any)
	Info(format string, v ...any)
	Debug(format string, v ...any)
}

// When used, run in local environment
func LocalMode(flag bool) func(r *Environment) {
	return func(e *Environment) {
		e.localMode = flag
	}
}

// When set, run in local environment
func Logger(logger LoggerI) func(r *Environment) {
	return func(e *Environment) {
		e.logger = logger
	}
}

// When used, do not use caching of external data
func NoCaching(flag bool) func(r *Environment) {
	return func(e *Environment) {
		e.noCaching = flag
	}
}

func NewEnvironment(options ...func(r *Environment)) (e *Environment) {
	e = &Environment{}
	for _, option := range options {
		option(e)
	}

	e.baseURL = strings.TrimRight(getBaseURL(), "/")

	if !e.noCaching {
		e.cacheURL, _ = os.LookupEnv(CACHE_URL_ENV)
	}
	if e.logger == nil {
		e.logger = &baseLogger{}
	}
	return
}

func (e *Environment) WaitForEnvironmentReady(
	maxAttempts int,
) error {
	if e.localMode {
		return nil
	} else {
		return e.waitForEnvironmentReady(maxAttempts, 1)
	}
}

func (e *Environment) waitForEnvironmentReady(
	maxAttempts int,
	attempt int,
) error {
	endpoint := SIDECAR_URL_DEF

	url := endpoint + READYZ
	e.logger.Debug("checking 'ready' at '%s'", url)
	response, err := http.Get(url)
	if err != nil {
		if attempt > maxAttempts {
			err := &EnvironmentNotReadyError{}
			log.Print(err.Error())
			return err
		} else {
			delay := 10 * time.Second
			e.logger.Debug("Waiting for sidecars: attempt #%d delay: %d sec url: %s", attempt, delay/time.Second, endpoint)
			time.Sleep(delay)
			return e.waitForEnvironmentReady(maxAttempts, attempt+1)
		}
	}
	response.Body.Close()
	return nil
}

func (e *Environment) Publish(
	name string, // TODO needed?
	contentType string,
	reader io.Reader,
	meta interface{},
) (err error) {
	if e.localMode {
		b, _ := ioutil.ReadAll(reader)
		ioutil.WriteFile(name, b, 0644)
		return
	}

	url := e.baseURL + ARTIFACTS_PATH
	req, err := http.NewRequest("POST", url, reader)
	if err != nil {
		e.logger.Error("creating request failed - %s", err)
		return
	}

	req.Header.Set("Content-Type", contentType)
	// The API requires a base64-encoded name.
	req.Header.Set(NAME_HEADER, base64.StdEncoding.EncodeToString([]byte(name)))
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		e.logger.Error("client.Do failed - %v", err)
		return &HttpError{req, res, err}
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		e.logger.Error("save request failed - %d", res.StatusCode)
		return &HttpError{req, res, nil}
	}

	var rec struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rec); err != nil {
		msg := "cannot read the artifact record in the response"
		e.logger.Error("%s - %s", msg, err)
		return &ApiError{msg, err}
	}
	artifactID := rec.ID
	if artifactID == "" {
		artifactID = res.Header.Get(ARTIFACT_ID_HEADER)
	}
	if artifactID == "" {
		msg := "cannot determine id of the created artifact"
		e.logger.Error(msg)
		return &ApiError{msg, nil}
	}
	if rec.Status != ARTIFACT_STATUS_READY {
		msg := fmt.Sprintf("artifact '%s' is not complete - status is '%s'", artifactID, rec.Status)
		e.logger.Error(msg)
		return &ApiError{msg, nil}
	}
	e.logger.Info("Successfully uploaded object as '%s'", artifactID)

	return e.PublishMetaForArtifact(name, artifactID, meta, url)
}

type WriteBodyF func(writer *io.PipeWriter) error

func (e *Environment) PublishAsync(
	name string, // TODO needed?
	contentType string,
	meta interface{},
	writeBodyF WriteBodyF,
) *sync.WaitGroup {
	pr, pw := io.Pipe()

	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer func() {
			wg.Done()
		}()

		if err := writeBodyF(pw); err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
		e.logger.Debug("writing finished '%s'", name)
	}()
	go func() {
		defer wg.Done()
		err := e.Publish(name, contentType, pr, meta)
		pr.CloseWithError(err)
		e.logger.Debug("published '%s' - '%v'", name, err)
	}()
	return &wg
}

// PublishMetaForArtifact adds an aspect to an artifact.
func (e *Environment) PublishMetaForArtifact(name string, artifactID string, meta interface{}, imgUrl string) (err error) {
	if meta == nil {
		return
	}

	q := url.Values{}
	q.Set("entity", artifactID)
	q.Set("schema", META_DATA_SCHEMA)
	u := e.baseURL + ASPECTS_PATH + "?" + q.Encode()
	e.logger.Debug("starting to upload metadata - meta: %v url: %s", meta, u)
	body, err := json.Marshal(meta)
	if err != nil {
		return &ApiError{"cannot encode metadata", err}
	}
	req, err := http.NewRequest("POST", u, bytes.NewBuffer(body))
	if err != nil {
		e.logger.Error("creating request failed - %s", err)
		return &HttpError{req, nil, err}
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		e.logger.Error("upload metadata failed - %s", err)
		return &HttpError{req, resp, err}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		e.logger.Error("upload metadata failed - status: %s", resp.Status)
		return &HttpError{req, resp, nil}
	}
	e.logger.Info("successfully uploaded metadata - status: %s", resp.Status)
	return
}

func (e *Environment) GetResource(url string, handler func(reader io.Reader) error) (err error) {
	if strings.HasPrefix(url, "urn:") {
		e.logger.Info("downloading artifact - urn: %s", url)
		url = e.baseURL + ARTIFACTS_PATH + "/" + url + "/blob"
	} else {
		e.logger.Info("downloading remote content - url: %s, caching?: %t", url, e.cacheURL != "")
		if e.cacheURL != "" {
			url = fmt.Sprintf("%s/%s", e.cacheURL, base64.RawURLEncoding.EncodeToString([]byte(url)))
		}
	}

	response, err := http.Get(url)
	if err != nil {
		e.logger.Error("downloadImage: GET failed - url: %s, err: %s", url, err)
		err = &HttpError{nil, response, err}
		return
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		e.logger.Error("getting resource failed - statusCode: %d url: %s", response.StatusCode, url)
		err = &HttpError{nil, response, nil}
		return
	}
	e.logger.Debug("downloading '%s' succeeded - size: %s cache-id: %s", url,
		response.Header.Get("Content-Length"), response.Header.Get(CACHE_ID_HEADER))
	reader := response.Body
	err = handler(reader)
	return
}

func (e *Environment) GetOrderID() string {
	if oid, ok := os.LookupEnv(ORDER_ID_ENV); ok {
		return oid
	} else {
		return "???"
	}
}

func getBaseURL() string {
	if v, ok := os.LookupEnv(BASE_URL_ENV); ok && v != "" {
		return v
	}
	if v, ok := os.LookupEnv(STORAGE_URL_ENV); ok && v != "" {
		return v
	}
	return BASE_URL_DEF
}

func getOptional(key string, _default string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	} else {
		return _default
	}
}

//**** BASE LOGGER

type baseLogger struct{}

func (l *baseLogger) Error(format string, v ...any) {
	log.Printf("ERROR: "+format, v...)
}

func (l *baseLogger) Info(format string, v ...any) {
	log.Printf("INFO: "+format, v...)
}

func (l *baseLogger) Debug(format string, v ...any) {
	log.Printf("DEBUG: "+format, v...)
}
