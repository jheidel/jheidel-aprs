package email

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/api/gmail/v1"
	"google.golang.org/genproto/googleapis/type/latlng"
)

func toPlainText(m *gmail.Message) (string, error) {
	if m.Payload == nil {
		return "", errors.New("missing payload")
	}
	for _, part := range append(m.Payload.Parts, m.Payload) {
		if part.MimeType != "text/plain" {
			continue
		}
		if part.Body == nil {
			return "", errors.New("missing body")
		}
		text, err := base64.URLEncoding.DecodeString(part.Body.Data)
		if err != nil {
			return "", err
		}
		unix := regexp.MustCompile(`(?:\r)?\n`).ReplaceAllLiteralString(string(text), "\n")
		return unix, nil
	}
	return "", errors.New("plaintext message part not found")
}

var (
	UrlRE = regexp.MustCompile(`\S*jeffheidel\.com\S*`)
)

func toMessage(full string) string {
	if i := strings.Index(full, "\n\n"); i != -1 {
		full = full[:i]
	}
	full = UrlRE.ReplaceAllString(full, "")
	return strings.TrimSpace(full)
}

var (
	CoordRE = regexp.MustCompile(`(?i)Lat\w*?\W+?([-0-9.]+).*?Lon\w*?\W+?([-0-9.]+)`)
)

func extractCoords(full string) (*latlng.LatLng, error) {
	m := CoordRE.FindAllStringSubmatch(full, -1)
	if len(m) != 1 || len(m[0]) != 3 {
		return nil, errors.New("GPS coordinates not found")
	}
	lat, err := strconv.ParseFloat(m[0][1], 64)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse lat from %q", m[0][1])
	}
	lon, err := strconv.ParseFloat(m[0][2], 64)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse lon from %q", m[0][2])
	}
	return &latlng.LatLng{
		Latitude:  lat,
		Longitude: lon,
	}, nil
}
