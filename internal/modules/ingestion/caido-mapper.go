package ingestion

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/francisconeves97/jxscout/internal/core/errutil"
)

type header struct {
	key   string
	value string
}

func parseHeader(line string) (header, error) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return header{}, fmt.Errorf("header line should be split in two. line: %s", line)
	}

	return header{
		key:   http.CanonicalHeaderKey(strings.TrimSpace(parts[0])),
		value: strings.TrimSpace(parts[1]),
	}, nil
}

func parseHeaders(lines []string) (map[string]string, error) {
	res := map[string]string{}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		header, err := parseHeader(line)
		if err != nil {
			return nil, errutil.Wrap(err, "failed to parse header")
		}

		res[header.key] = header.value
	}

	return res, nil
}

type rawHTTP struct {
	Header string
	Body   string
}

func splitHeaderFromBody(httpStr string) (rawHTTP, error) {
	for _, separator := range []string{"\r\n\r\n", "\n\n"} {
		if index := strings.Index(httpStr, separator); index >= 0 {
			return rawHTTP{
				Header: httpStr[:index],
				Body:   httpStr[index+len(separator):],
			}, nil
		}
	}
	return rawHTTP{Header: strings.TrimRight(httpStr, "\r\n")}, nil
}

func parseRequest(req string, reqURL string) (Request, error) {
	httpReq, err := splitHeaderFromBody(req)
	if err != nil {
		return Request{}, errutil.Wrap(err, "failed to split header from body")
	}

	lines := strings.Split(httpReq.Header, "\n")

	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return Request{}, errors.New("not enough lines to be a valid request")
	}

	requestDescriptionLine := lines[0]
	requestDescLineParts := strings.Fields(requestDescriptionLine)
	if len(requestDescLineParts) < 3 {
		return Request{}, errors.New("request description line should have at least len 3")
	}

	method := strings.TrimSpace(requestDescLineParts[0])

	headers, err := parseHeaders(lines[1:])
	if err != nil {
		return Request{}, errutil.Wrap(err, "failed to parse headers")
	}
	if recoveredURL, err := recoverPortFromHost(reqURL, headers["Host"]); err != nil {
		return Request{}, errutil.Wrap(err, "failed to recover request port")
	} else {
		reqURL = recoveredURL
	}

	return Request{
		Method:  method,
		URL:     reqURL,
		Headers: headers,
	}, nil
}

func parseResponse(res string) (Response, error) {
	httpRes, err := splitHeaderFromBody(res)
	if err != nil {
		return Response{}, errutil.Wrap(err, "failed to split header from body")
	}

	lines := strings.Split(httpRes.Header, "\n")

	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return Response{}, errors.New("not enough lines to be a valid response")
	}

	linesWithoutStatus := lines[1:]

	headers, err := parseHeaders(linesWithoutStatus)
	if err != nil {
		return Response{}, errutil.Wrap(err, "failed to parse headers")
	}

	statusParts := strings.Fields(lines[0])
	if len(statusParts) < 2 {
		return Response{}, fmt.Errorf("status part should have at least len 2. line: %s", lines[0])
	}

	status := statusParts[1]
	statusInt, err := strconv.Atoi(status)
	if err != nil {
		return Response{}, errutil.Wrap(err, "failed to parse status")
	}

	return Response{
		Status:  statusInt,
		Body:    httpRes.Body,
		Headers: headers,
	}, nil
}

func recoverPortFromHost(rawURL string, hostHeader string) (string, error) {
	if hostHeader == "" {
		return rawURL, nil
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	if parsedURL.Port() != "" || parsedURL.Hostname() == "" {
		return rawURL, nil
	}
	parsedHost, err := url.Parse("//" + hostHeader)
	if err != nil {
		return rawURL, nil
	}
	port := parsedHost.Port()
	if port == "" || !strings.EqualFold(parsedURL.Hostname(), parsedHost.Hostname()) {
		return rawURL, nil
	}
	parsedURL.Host = parsedHost.Host
	return parsedURL.String(), nil
}

func mapCaidoRequest(req caidoIngestRequest) (IngestionRequest, error) {
	request, err := parseRequest(req.Request, req.RequestURL)
	if err != nil {
		return IngestionRequest{}, errutil.Wrap(err, "failed to parse request")
	}

	response, err := parseResponse(req.Response)
	if err != nil {
		return IngestionRequest{}, errutil.Wrap(err, "failed to parse response")
	}

	return IngestionRequest{
		Request:  request,
		Response: response,
	}, nil
}
