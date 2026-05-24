package upstream

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	webVPNGatewayHost = "d.buaa.edu.cn"
	webVPNKeyText     = "wrdvpnisthebest!"
)

type URLRewriter struct {
	useVPN bool
}

func NewURLRewriter() URLRewriter {
	value := strings.TrimSpace(os.Getenv("USE_VPN"))
	useVPN, _ := strconv.ParseBool(value)
	return URLRewriter{useVPN: value != "" && useVPN}
}

func (r URLRewriter) UpstreamURL(raw string) string {
	if !r.useVPN {
		return raw
	}
	return ToWebVPNURL(raw)
}

func ToWebVPNURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return raw
	}
	if strings.EqualFold(parsed.Hostname(), webVPNGatewayHost) {
		return raw
	}
	protocolPart := parsed.Scheme
	port := parsed.Port()
	if port != "" && !((parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443")) {
		protocolPart = parsed.Scheme + "-" + port
	}
	host := encryptHost(parsed.Hostname())
	result := &url.URL{
		Scheme:   "https",
		Host:     webVPNGatewayHost,
		Path:     "/" + protocolPart + "/" + host + parsed.EscapedPath(),
		RawQuery: parsed.RawQuery,
		Fragment: parsed.Fragment,
	}
	return result.String()
}

func FromWebVPNURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(parsed.Hostname(), webVPNGatewayHost) {
		return raw
	}
	segments := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(segments) < 2 {
		return raw
	}
	protocolPart := segments[0]
	encodedHost := segments[1]
	scheme := protocolPart
	port := ""
	if pieces := strings.SplitN(protocolPart, "-", 2); len(pieces) == 2 {
		scheme, port = pieces[0], pieces[1]
	}
	host, err := decryptHost(encodedHost)
	if err != nil {
		return raw
	}
	if port != "" {
		host += ":" + port
	}
	path := ""
	if len(segments) > 2 {
		path = "/" + strings.Join(segments[2:], "/")
	} else if strings.HasSuffix(parsed.EscapedPath(), "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		path = "/"
	}
	result := &url.URL{
		Scheme:   scheme,
		Host:     host,
		Path:     path,
		RawQuery: parsed.RawQuery,
		Fragment: parsed.Fragment,
	}
	return result.String()
}

func IsSSOURL(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	return strings.Contains(strings.ToLower(FromWebVPNURL(raw)), "sso.buaa.edu.cn")
}

func encryptHost(host string) string {
	key := []byte(webVPNKeyText)
	block, err := aes.NewCipher(key)
	if err != nil {
		return host
	}
	plain := []byte(host)
	padded := append([]byte{}, plain...)
	if remainder := len(plain) % aes.BlockSize; remainder != 0 {
		padded = append(padded, []byte(strings.Repeat("0", aes.BlockSize-remainder))...)
	}
	cipherText := make([]byte, len(padded))
	cipher.NewCFBEncrypter(block, key).XORKeyStream(cipherText, padded)
	return hex.EncodeToString(key) + hex.EncodeToString(cipherText)[:len(plain)*2]
}

func decryptHost(encoded string) (string, error) {
	if len(encoded) < 32 {
		return "", url.EscapeError("invalid webvpn host payload")
	}
	iv, err := hex.DecodeString(encoded[:32])
	if err != nil {
		return "", err
	}
	cipherHex := encoded[32:]
	if remainder := len(cipherHex) % 32; remainder != 0 {
		cipherHex += strings.Repeat("0", 32-remainder)
	}
	cipherBytes, err := hex.DecodeString(cipherHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher([]byte(webVPNKeyText))
	if err != nil {
		return "", err
	}
	plain := make([]byte, len(cipherBytes))
	cipher.NewCFBDecrypter(block, iv).XORKeyStream(plain, cipherBytes)
	length := len(encoded)/2 - 16
	if length < 0 || length > len(plain) {
		length = len(plain)
	}
	return string(plain[:length]), nil
}
