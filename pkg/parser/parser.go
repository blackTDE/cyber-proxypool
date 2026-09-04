package parser

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cyberproxypool/pkg/model"
	"gopkg.in/yaml.v3"
)

// FetchSubscription pulls subscription content from a remote URL
func FetchSubscription(subURL string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	req, err := http.NewRequest("GET", subURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid subscription url: %w", err)
	}

	// Use Clash/v2ray User-Agent to ensure servers return node lists
	req.Header.Set("User-Agent", "clash-verge/v1.7.7 (Clash.Meta)")
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch subscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("server returned status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(body), nil
}

// ClashConfig represents top-level Clash/Mihomo YAML structure
type ClashConfig struct {
	Proxies []map[string]any `yaml:"proxies"`
}

// ParseContent parses raw subscription text into a list of nodes
func ParseContent(content string, subID, subName string) ([]model.Node, string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, "", fmt.Errorf("subscription content is empty")
	}

	// 1. Attempt Clash YAML parsing first
	if isLikelyYAML(content) {
		nodes, err := parseClashYAML(content, subID, subName)
		if err == nil && len(nodes) > 0 {
			return nodes, "clash", nil
		}
	}

	// 2. Attempt Base64 decoding
	decoded, err := tryBase64Decode(content)
	if err == nil && len(decoded) > 0 {
		// Check if decoded content is YAML
		if isLikelyYAML(decoded) {
			nodes, err := parseClashYAML(decoded, subID, subName)
			if err == nil && len(nodes) > 0 {
				return nodes, "clash", nil
			}
		}
		// Otherwise parse decoded lines
		nodes := parseURILines(decoded, subID, subName)
		if len(nodes) > 0 {
			return nodes, "base64", nil
		}
	}

	// 3. Fallback: Parse raw content directly as lines of URIs
	nodes := parseURILines(content, subID, subName)
	if len(nodes) > 0 {
		return nodes, "links", nil
	}

	return nil, "unknown", fmt.Errorf("no valid proxy nodes could be extracted")
}

func isLikelyYAML(s string) bool {
	return strings.Contains(s, "proxies:") || strings.HasPrefix(s, "---") || strings.Contains(s, "proxy-groups:")
}

func parseClashYAML(content string, subID, subName string) ([]model.Node, error) {
	var cfg ClashConfig
	err := yaml.Unmarshal([]byte(content), &cfg)
	if err != nil {
		return nil, err
	}

	var nodes []model.Node
	for idx, p := range cfg.Proxies {
		node := convertClashProxyToNode(p, subID, subName, idx+1)
		if node != nil {
			nodes = append(nodes, *node)
		}
	}

	return nodes, nil
}

func convertClashProxyToNode(p map[string]any, subID, subName string, idx int) *model.Node {
	name, _ := p["name"].(string)
	pType, _ := p["type"].(string)
	server, _ := p["server"].(string)
	portVal := p["port"]

	if server == "" || portVal == nil {
		return nil
	}

	port := 0
	switch v := portVal.(type) {
	case int:
		port = v
	case float64:
		port = int(v)
	case string:
		port, _ = strconv.Atoi(v)
	}

	if port <= 0 || port > 65535 {
		return nil
	}

	pTypeLower := strings.ToLower(pType)
	var protocol model.ProxyProtocol
	switch pTypeLower {
	case "trojan":
		protocol = model.ProtoTrojan
	case "ss", "shadowsocks":
		protocol = model.ProtoShadowsocks
	case "vmess":
		protocol = model.ProtoVMess
	case "vless":
		protocol = model.ProtoVLESS
	case "socks5", "socks":
		protocol = model.ProtoSocks5
	case "http":
		protocol = model.ProtoHTTP
	default:
		// Unsupported protocol in Clash
		return nil
	}

	if name == "" {
		name = fmt.Sprintf("%s-%d", protocol, idx)
	}

	node := &model.Node{
		SubID:    subID,
		SubName:  subName,
		Name:     strings.TrimSpace(name),
		Protocol: protocol,
		Server:   strings.TrimSpace(server),
		Port:     port,
	}

	// Extract password or UUID
	if pw, ok := p["password"].(string); ok {
		node.Password = pw
	} else if uuid, ok := p["uuid"].(string); ok {
		node.Password = uuid
	}

	// Extract cipher
	if cipher, ok := p["cipher"].(string); ok {
		node.Cipher = cipher
	}

	// TLS settings
	if tlsVal, ok := p["tls"].(bool); ok {
		node.TLS = tlsVal
	} else if protocol == model.ProtoTrojan {
		node.TLS = true
	}

	if sni, ok := p["sni"].(string); ok {
		node.SNI = sni
	} else if serverName, ok := p["servername"].(string); ok {
		node.SNI = serverName
	}

	if skipVerify, ok := p["skip-cert-verify"].(bool); ok {
		node.SkipCertVerify = skipVerify
	}

	// Network / Transport
	if network, ok := p["network"].(string); ok {
		node.Network = strings.ToLower(network)
	}

	// WebSocket options
	if wsOpts, ok := p["ws-opts"].(map[string]any); ok {
		if path, ok := wsOpts["path"].(string); ok {
			node.Path = path
		}
		if headers, ok := wsOpts["headers"].(map[string]any); ok {
			if host, ok := headers["Host"].(string); ok {
				node.Host = host
			}
		}
	}

	// VMess alterId
	if aid, ok := p["alterId"].(int); ok {
		node.AlterID = aid
	}

	// Flow settings (e.g. "xtls-rprx-vision")
	if flow, ok := p["flow"].(string); ok {
		node.Flow = strings.TrimSpace(flow)
	}
	if node.Flow == "" && node.Protocol == model.ProtoVLESS {
		nameLower := strings.ToLower(node.Name)
		if strings.Contains(nameLower, "vision") || strings.Contains(nameLower, "xtls") {
			node.Flow = "xtls-rprx-vision"
		}
	}

	geo := ExtractGeo(node.Name)
	node.Country = geo.Country
	node.Flag = geo.Flag
	node.ID = generateNodeID(node)

	return node
}

// parseURILines splits text into lines and extracts proxy nodes
func parseURILines(content, subID, subName string) []model.Node {
	lines := strings.Split(content, "\n")
	var nodes []model.Node

	for idx, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		node := parseSingleURI(line, subID, subName, idx+1)
		if node != nil {
			nodes = append(nodes, *node)
		}
	}

	return nodes
}

func parseSingleURI(rawURI, subID, subName string, idx int) *model.Node {
	u, err := url.Parse(rawURI)
	if err != nil {
		return nil
	}

	switch u.Scheme {
	case "trojan":
		return parseTrojanURI(u, subID, subName, idx)
	case "ss":
		return parseShadowsocksURI(rawURI, subID, subName, idx)
	case "vmess":
		return parseVMessURI(rawURI, subID, subName, idx)
	case "vless":
		return parseVLESSURI(u, subID, subName, idx)
	case "socks5", "socks":
		return parseSocks5URI(u, subID, subName, idx)
	case "http":
		return parseHTTPURI(u, subID, subName, idx)
	default:
		return nil
	}
}

func parseTrojanURI(u *url.URL, subID, subName string, idx int) *model.Node {
	password := u.User.Username()
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 443
	}

	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("trojan-%s:%d", host, port)
	} else if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}

	q := u.Query()
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("peer")
	}
	if sni == "" {
		sni = host
	}

	network := q.Get("type")
	if network == "" {
		network = "tcp"
	}

	skipVerify := q.Get("allowInsecure") == "1" || q.Get("insecure") == "1"

	node := &model.Node{
		SubID:          subID,
		SubName:        subName,
		Name:           strings.TrimSpace(name),
		Protocol:       model.ProtoTrojan,
		Server:         host,
		Port:           port,
		Password:       password,
		TLS:            true,
		SNI:            sni,
		Network:        network,
		Path:           q.Get("path"),
		Host:           q.Get("host"),
		SkipCertVerify: skipVerify,
	}

	geo := ExtractGeo(node.Name)
	node.Country = geo.Country
	node.Flag = geo.Flag
	node.ID = generateNodeID(node)
	return node
}

func parseShadowsocksURI(rawURI, subID, subName string, idx int) *model.Node {
	// ss://[base64(method:password@server:port)]#name
	// or SIP002: ss://base64(method:password)@server:port#name
	trimmed := strings.TrimPrefix(rawURI, "ss://")
	parts := strings.SplitN(trimmed, "#", 2)
	mainPart := parts[0]
	name := ""
	if len(parts) > 1 {
		name, _ = url.PathUnescape(parts[1])
	}

	var cipher, password, server string
	var port int

	if strings.Contains(mainPart, "@") {
		// SIP002 format: base64(method:password)@server:port
		atIdx := strings.LastIndex(mainPart, "@")
		userInfoPart := mainPart[:atIdx]
		serverPortPart := mainPart[atIdx+1:]

		// UserInfo might be base64 encoded
		decodedUserInfo, err := tryBase64Decode(userInfoPart)
		if err == nil && strings.Contains(decodedUserInfo, ":") {
			userInfoPart = decodedUserInfo
		}

		userParts := strings.SplitN(userInfoPart, ":", 2)
		if len(userParts) == 2 {
			cipher = userParts[0]
			password = userParts[1]
		}

		// Split server and port (remove query parameters if any)
		if qIdx := strings.Index(serverPortPart, "?"); qIdx != -1 {
			serverPortPart = serverPortPart[:qIdx]
		}

		host, pStr, err := splitHostPort(serverPortPart)
		if err == nil {
			server = host
			port, _ = strconv.Atoi(pStr)
		}
	} else {
		// Legacy format: base64(method:password@server:port)
		decoded, err := tryBase64Decode(mainPart)
		if err == nil {
			// Expected method:password@server:port
			atIdx := strings.LastIndex(decoded, "@")
			if atIdx != -1 {
				userPart := decoded[:atIdx]
				serverPart := decoded[atIdx+1:]

				userSplit := strings.SplitN(userPart, ":", 2)
				if len(userSplit) == 2 {
					cipher = userSplit[0]
					password = userSplit[1]
				}

				host, pStr, err := splitHostPort(serverPart)
				if err == nil {
					server = host
					port, _ = strconv.Atoi(pStr)
				}
			}
		}
	}

	if server == "" || port == 0 {
		return nil
	}

	if name == "" {
		name = fmt.Sprintf("ss-%s:%d", server, port)
	}

	node := &model.Node{
		SubID:    subID,
		SubName:  subName,
		Name:     strings.TrimSpace(name),
		Protocol: model.ProtoShadowsocks,
		Server:   server,
		Port:     port,
		Cipher:   cipher,
		Password: password,
	}

	geo := ExtractGeo(node.Name)
	node.Country = geo.Country
	node.Flag = geo.Flag
	node.ID = generateNodeID(node)
	return node
}

type vmessJSON struct {
	V    any    `json:"v"`
	Ps   string `json:"ps"`
	Add  string `json:"add"`
	Port any    `json:"port"`
	ID   string `json:"id"`
	Aid  any    `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	TLS  string `json:"tls"`
	Sni  string `json:"sni"`
}

func parseVMessURI(rawURI, subID, subName string, idx int) *model.Node {
	b64 := strings.TrimPrefix(rawURI, "vmess://")
	decoded, err := tryBase64Decode(b64)
	if err != nil {
		return nil
	}

	var vj vmessJSON
	if err := json.Unmarshal([]byte(decoded), &vj); err != nil {
		return nil
	}

	port := 0
	switch v := vj.Port.(type) {
	case float64:
		port = int(v)
	case int:
		port = v
	case string:
		port, _ = strconv.Atoi(v)
	}

	aid := 0
	switch v := vj.Aid.(type) {
	case float64:
		aid = int(v)
	case int:
		aid = v
	case string:
		aid, _ = strconv.Atoi(v)
	}

	if vj.Add == "" || port <= 0 {
		return nil
	}

	name := vj.Ps
	if name == "" {
		name = fmt.Sprintf("vmess-%s:%d", vj.Add, port)
	}

	cipher := vj.Scy
	if cipher == "" {
		cipher = "auto"
	}

	node := &model.Node{
		SubID:    subID,
		SubName:  subName,
		Name:     strings.TrimSpace(name),
		Protocol: model.ProtoVMess,
		Server:   vj.Add,
		Port:     port,
		Password: vj.ID,
		Cipher:   cipher,
		AlterID:  aid,
		Network:  vj.Net,
		Path:     vj.Path,
		Host:     vj.Host,
		SNI:      vj.Sni,
		TLS:      vj.TLS == "tls",
	}

	geo := ExtractGeo(node.Name)
	node.Country = geo.Country
	node.Flag = geo.Flag
	node.ID = generateNodeID(node)
	return node
}

func parseVLESSURI(u *url.URL, subID, subName string, idx int) *model.Node {
	uuid := u.User.Username()
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 443
	}

	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("vless-%s:%d", host, port)
	} else if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}

	q := u.Query()
	security := q.Get("security")
	sni := q.Get("sni")
	if sni == "" {
		sni = host
	}

	flow := q.Get("flow")
	if flow == "" {
		nameLower := strings.ToLower(name)
		if strings.Contains(nameLower, "vision") || strings.Contains(nameLower, "xtls") {
			flow = "xtls-rprx-vision"
		}
	}

	node := &model.Node{
		SubID:    subID,
		SubName:  subName,
		Name:     strings.TrimSpace(name),
		Protocol: model.ProtoVLESS,
		Server:   host,
		Port:     port,
		Password: uuid,
		TLS:      security == "tls" || security == "reality",
		SNI:      sni,
		Flow:     strings.TrimSpace(flow),
		Network:  q.Get("type"),
		Path:     q.Get("path"),
		Host:     q.Get("host"),
	}

	geo := ExtractGeo(node.Name)
	node.Country = geo.Country
	node.Flag = geo.Flag
	node.ID = generateNodeID(node)
	return node
}

func parseSocks5URI(u *url.URL, subID, subName string, idx int) *model.Node {
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 1080
	}

	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("socks5-%s:%d", host, port)
	} else if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}

	pw, _ := u.User.Password()
	node := &model.Node{
		SubID:    subID,
		SubName:  subName,
		Name:     strings.TrimSpace(name),
		Protocol: model.ProtoSocks5,
		Server:   host,
		Port:     port,
		Password: pw,
		Cipher:   u.User.Username(), // reuse Cipher for username
	}

	geo := ExtractGeo(node.Name)
	node.Country = geo.Country
	node.Flag = geo.Flag
	node.ID = generateNodeID(node)
	return node
}

func parseHTTPURI(u *url.URL, subID, subName string, idx int) *model.Node {
	host := u.Hostname()
	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 8080
	}

	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("http-%s:%d", host, port)
	} else if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}

	pw, _ := u.User.Password()
	node := &model.Node{
		SubID:    subID,
		SubName:  subName,
		Name:     strings.TrimSpace(name),
		Protocol: model.ProtoHTTP,
		Server:   host,
		Port:     port,
		Password: pw,
		Cipher:   u.User.Username(),
	}

	geo := ExtractGeo(node.Name)
	node.Country = geo.Country
	node.Flag = geo.Flag
	node.ID = generateNodeID(node)
	return node
}

func tryBase64Decode(s string) (string, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")

	// Fix padding
	if rem := len(s) % 4; rem > 0 {
		s += strings.Repeat("=", 4-rem)
	}

	// Try Standard Encoding
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return string(b), nil
	}

	// Try URL Encoding
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return string(b), nil
	}

	return "", fmt.Errorf("failed to decode base64")
}

func splitHostPort(hp string) (string, string, error) {
	// Handle [ipv6]:port or host:port
	if strings.HasPrefix(hp, "[") {
		closeBracket := strings.Index(hp, "]")
		if closeBracket != -1 && len(hp) > closeBracket+2 && hp[closeBracket+1] == ':' {
			return hp[1:closeBracket], hp[closeBracket+2:], nil
		}
	}

	idx := strings.LastIndex(hp, ":")
	if idx == -1 {
		return "", "", fmt.Errorf("missing port in %s", hp)
	}
	return hp[:idx], hp[idx+1:], nil
}

func generateNodeID(n *model.Node) string {
	raw := fmt.Sprintf("%s|%s|%s|%d|%s|%s", n.SubID, n.Protocol, n.Server, n.Port, n.Password, n.Name)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:8])
}
