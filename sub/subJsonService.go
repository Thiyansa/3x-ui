package sub

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/Thiyansa/3x-ui/v2/database/model"
	"github.com/Thiyansa/3x-ui/v2/logger"
	"github.com/Thiyansa/3x-ui/v2/util/json_util"
	"github.com/Thiyansa/3x-ui/v2/util/random"
	"github.com/Thiyansa/3x-ui/v2/web/service"
	"github.com/Thiyansa/3x-ui/v2/xray"
)

//go:embed default.json
var defaultJson string

// SubJsonService handles JSON subscription configuration generation and management.
type SubJsonService struct {
	configJson       map[string]any
	defaultOutbounds []json_util.RawMessage
	fragment         string
	noises           string
	mux              string

	inboundService service.InboundService
	SubService     *SubService
	settingService service.SettingService
}

// NewSubJsonService creates a new JSON subscription service with the given configuration.
func NewSubJsonService(fragment string, noises string, mux string, rules string, subService *SubService) *SubJsonService {
	var configJson map[string]any
	var defaultOutbounds []json_util.RawMessage
	json.Unmarshal([]byte(defaultJson), &configJson)
	if outboundSlices, ok := configJson["outbounds"].([]any); ok {
		for _, defaultOutbound := range outboundSlices {
			jsonBytes, _ := json.Marshal(defaultOutbound)
			defaultOutbounds = append(defaultOutbounds, jsonBytes)
		}
	}

	if rules != "" {
		var newRules []any
		routing, _ := configJson["routing"].(map[string]any)
		defaultRules, _ := routing["rules"].([]any)
		json.Unmarshal([]byte(rules), &newRules)
		defaultRules = append(newRules, defaultRules...)
		routing["rules"] = defaultRules
		configJson["routing"] = routing
	}

	if fragment != "" {
		defaultOutbounds = append(defaultOutbounds, json_util.RawMessage(fragment))
	}

	if noises != "" {
		defaultOutbounds = append(defaultOutbounds, json_util.RawMessage(noises))
	}

	return &SubJsonService{
		configJson:       configJson,
		defaultOutbounds: defaultOutbounds,
		fragment:         fragment,
		noises:           noises,
		mux:              mux,
		SubService:       subService,
	}
}

// GetJson generates a JSON subscription configuration for the given subscription ID and host.
func (s *SubJsonService) GetJson(subId string, host string) (string, string, error) {
	inbounds, err := s.SubService.getInboundsBySubId(subId)
	if err != nil || len(inbounds) == 0 {
		return "", "", err
	}

	var header string
	var traffic xray.ClientTraffic
	var clientTraffics []xray.ClientTraffic
	var configArray []json_util.RawMessage

	// Prepare Inbounds
	for _, inbound := range inbounds {
		clients, err := s.inboundService.GetClients(inbound)
		if err != nil {
			logger.Error("SubJsonService - GetClients: Unable to get clients from inbound")
		}
		if clients == nil {
			continue
		}
		if len(inbound.Listen) > 0 && inbound.Listen[0] == '@' {
			listen, port, streamSettings, err := s.SubService.getFallbackMaster(inbound.Listen, inbound.StreamSettings)
			if err == nil {
				inbound.Listen = listen
				inbound.Port = port
				inbound.StreamSettings = streamSettings
			}
		}

		for _, client := range clients {
			if client.Enable && client.SubID == subId {
				clientTraffics = append(clientTraffics, s.SubService.getClientTraffics(inbound.ClientStats, client.Email))
				newConfigs := s.getConfig(inbound, client, host)
				configArray = append(configArray, newConfigs...)
			}
		}
	}

	if len(configArray) == 0 {
		return "", "", nil
	}

	// Prepare statistics
	for index, clientTraffic := range clientTraffics {
		if index == 0 {
			traffic.Up = clientTraffic.Up
			traffic.Down = clientTraffic.Down
			traffic.Total = clientTraffic.Total
			if clientTraffic.ExpiryTime > 0 {
				traffic.ExpiryTime = clientTraffic.ExpiryTime
			}
		} else {
			traffic.Up += clientTraffic.Up
			traffic.Down += clientTraffic.Down
			if traffic.Total == 0 || clientTraffic.Total == 0 {
				traffic.Total = 0
			} else {
				traffic.Total += clientTraffic.Total
			}
			if clientTraffic.ExpiryTime != traffic.ExpiryTime {
				traffic.ExpiryTime = 0
			}
		}
	}

	// Combile outbounds
	var finalJson []byte
	if len(configArray) == 1 {
		finalJson, _ = json.MarshalIndent(configArray[0], "", "  ")
	} else {
		finalJson, _ = json.MarshalIndent(configArray, "", "  ")
	}

	header = fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", traffic.Up, traffic.Down, traffic.Total, traffic.ExpiryTime/1000)
	return string(finalJson), header, nil
}

func (s *SubJsonService) getConfig(inbound *model.Inbound, client model.Client, host string) []json_util.RawMessage {
	var newJsonArray []json_util.RawMessage
	stream := s.streamData(inbound.StreamSettings)

	// Check if this inbound uses a balancer
	balancerTag := s.getBalancerTagForInbound(inbound.Tag)
	balancerServers := s.getBalancerServers(balancerTag)

	externalProxies, ok := stream["externalProxy"].([]any)
	
	// If no external proxies or we have a balancer, handle accordingly
	if !ok || len(externalProxies) == 0 {
		if balancerTag != "" && len(balancerServers) > 0 {
			// For balancer, create external proxies for each server
			externalProxies = make([]any, 0, len(balancerServers))
			for _, server := range balancerServers {
				externalProxies = append(externalProxies, map[string]any{
					"forceTls": "same",
					"dest":     server.Address,
					"port":     float64(server.Port),
					"remark":   server.Remark,
				})
			}
		} else {
			externalProxies = []any{
				map[string]any{
					"forceTls": "same",
					"dest":     host,
					"port":     float64(inbound.Port),
					"remark":   "",
				},
			}
		}
	} else if balancerTag != "" && len(balancerServers) > 0 {
		// If we have a balancer but also external proxies, merge them
		// But prefer balancer servers over external proxies
		externalProxies = make([]any, 0, len(balancerServers))
		for _, server := range balancerServers {
			externalProxies = append(externalProxies, map[string]any{
				"forceTls": "same",
				"dest":     server.Address,
				"port":     float64(server.Port),
				"remark":   server.Remark,
			})
		}
	}

	delete(stream, "externalProxy")

	for _, ep := range externalProxies {
		extPrxy := ep.(map[string]any)
		
		// Get dest with proper handling for fallback
		dest, ok := extPrxy["dest"].(string)
		if !ok || dest == "" {
			dest = host
		}
		
		// If dest is "@" or starts with "@", use host as fallback
		if strings.HasPrefix(dest, "@") {
			dest = host
		}
		
		inbound.Listen = dest
		
		// Get port with proper type handling
		portVal, ok := extPrxy["port"].(float64)
		if !ok {
			portVal = float64(inbound.Port)
		}
		inbound.Port = int(portVal)
		
		// Get remark
		remark, _ := extPrxy["remark"].(string)
		
		newStream := stream
		switch forceTls, _ := extPrxy["forceTls"].(string); forceTls {
		case "tls":
			if newStream["security"] != "tls" {
				newStream["security"] = "tls"
				newStream["tlsSettings"] = map[string]any{}
			}
		case "none":
			if newStream["security"] != "none" {
				newStream["security"] = "none"
				delete(newStream, "tlsSettings")
			}
		}
		streamSettings, _ := json.MarshalIndent(newStream, "", "  ")

		var newOutbounds []json_util.RawMessage

		switch inbound.Protocol {
		case "vmess":
			newOutbounds = append(newOutbounds, s.genVnext(inbound, streamSettings, client))
		case "vless":
			newOutbounds = append(newOutbounds, s.genVless(inbound, streamSettings, client))
		case "trojan", "shadowsocks":
			newOutbounds = append(newOutbounds, s.genServer(inbound, streamSettings, client))
		}

		newOutbounds = append(newOutbounds, s.defaultOutbounds...)
		newConfigJson := make(map[string]any)
		maps.Copy(newConfigJson, s.configJson)

		newConfigJson["outbounds"] = newOutbounds
		newConfigJson["remarks"] = s.SubService.genRemark(inbound, client.Email, remark)

		newConfig, _ := json.MarshalIndent(newConfigJson, "", "  ")
		newJsonArray = append(newJsonArray, newConfig)
	}

	return newJsonArray
}

// getBalancerTagForInbound gets the balancer tag for an inbound
func (s *SubJsonService) getBalancerTagForInbound(inboundTag string) string {
	// Get Xray config template
	config, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return ""
	}

	var xrayConfig map[string]any
	if err := json.Unmarshal([]byte(config), &xrayConfig); err != nil {
		return ""
	}

	// Get routing section
	routing, ok := xrayConfig["routing"].(map[string]any)
	if !ok {
		return ""
	}

	// Get rules
	rules, ok := routing["rules"].([]any)
	if !ok {
		return ""
	}

	// Find a rule that uses this inbound and has a balancer tag
	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]any)
		if !ok {
			continue
		}
		
		// Check if this rule applies to our inbound
		inboundTags, ok := ruleMap["inboundTag"].([]any)
		if !ok {
			continue
		}
		
		for _, tag := range inboundTags {
			tagStr, ok := tag.(string)
			if !ok {
				continue
			}
			if tagStr == inboundTag {
				// Check if this rule has a balancer tag
				if balancerTag, ok := ruleMap["balancerTag"].(string); ok && balancerTag != "" {
					return balancerTag
				}
			}
		}
	}

	return ""
}

// getBalancerServers gets the servers from a balancer configuration
func (s *SubJsonService) getBalancerServers(balancerTag string) []BalancerServer {
	if balancerTag == "" {
		return nil
	}

	// Get Xray config template
	config, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil
	}

	var xrayConfig map[string]any
	if err := json.Unmarshal([]byte(config), &xrayConfig); err != nil {
		return nil
	}

	// Get routing section
	routing, ok := xrayConfig["routing"].(map[string]any)
	if !ok {
		return nil
	}

	// Get balancers
	balancers, ok := routing["balancers"].([]any)
	if !ok {
		return nil
	}

	// Find the balancer with the matching tag
	for _, b := range balancers {
		balancer, ok := b.(map[string]any)
		if !ok {
			continue
		}
		
		tag, ok := balancer["tag"].(string)
		if !ok || tag != balancerTag {
			continue
		}
		
		// Get selector
		selector, ok := balancer["selector"].([]any)
		if !ok {
			return nil
		}
		
		var servers []BalancerServer
		
		// For each selector, find the corresponding outbound
		for _, sel := range selector {
			outboundTag, ok := sel.(string)
			if !ok {
				continue
			}
			
			server := s.getOutboundServer(outboundTag)
			if server != nil {
				servers = append(servers, *server)
			}
		}
		
		return servers
	}

	return nil
}

// getOutboundServer gets the server details from an outbound tag
func (s *SubJsonService) getOutboundServer(outboundTag string) *BalancerServer {
	// Get Xray config template
	config, err := s.settingService.GetXrayConfigTemplate()
	if err != nil {
		return nil
	}

	var xrayConfig map[string]any
	if err := json.Unmarshal([]byte(config), &xrayConfig); err != nil {
		return nil
	}

	// Get outbounds
	outbounds, ok := xrayConfig["outbounds"].([]any)
	if !ok {
		return nil
	}

	// Find the outbound with the matching tag
	for _, ob := range outbounds {
		outbound, ok := ob.(map[string]any)
		if !ok {
			continue
		}
		
		tag, ok := outbound["tag"].(string)
		if !ok || tag != outboundTag {
			continue
		}
		
		// Get address and port from settings
		settings, ok := outbound["settings"].(map[string]any)
		if !ok {
			continue
		}
		
		var address string
		var port int
		
		// Try to get address from different protocol settings
		if vnext, ok := settings["vnext"].([]any); ok && len(vnext) > 0 {
			if v, ok := vnext[0].(map[string]any); ok {
				if addr, ok := v["address"].(string); ok {
					address = addr
				}
				if p, ok := v["port"].(float64); ok {
					port = int(p)
				}
			}
		} else if servers, ok := settings["servers"].([]any); ok && len(servers) > 0 {
			if srv, ok := servers[0].(map[string]any); ok {
				if addr, ok := srv["address"].(string); ok {
					address = addr
				}
				if p, ok := srv["port"].(float64); ok {
					port = int(p)
				}
			}
		} else if addr, ok := settings["address"].(string); ok {
			address = addr
			if p, ok := settings["port"].(float64); ok {
				port = int(p)
			}
		}
		
		if address == "" || port == 0 {
			continue
		}
		
		// Get remark from tag or outbound
		remark := outboundTag
		if tag, ok := outbound["tag"].(string); ok {
			remark = tag
		}
		
		return &BalancerServer{
			Address: address,
			Port:    port,
			Remark:  remark,
		}
	}

	return nil
}

// BalancerServer represents a server in a balancer
type BalancerServer struct {
	Address string
	Port    int
	Remark  string
}

func (s *SubJsonService) streamData(stream string) map[string]any {
	var streamSettings map[string]any
	json.Unmarshal([]byte(stream), &streamSettings)
	security, _ := streamSettings["security"].(string)
	switch security {
	case "tls":
		streamSettings["tlsSettings"] = s.tlsData(streamSettings["tlsSettings"].(map[string]any))
	case "reality":
		streamSettings["realitySettings"] = s.realityData(streamSettings["realitySettings"].(map[string]any))
	}
	delete(streamSettings, "sockopt")

	if s.fragment != "" {
		streamSettings["sockopt"] = json_util.RawMessage(`{"dialerProxy": "fragment", "tcpKeepAliveIdle": 100, "tcpMptcp": true, "penetrate": true}`)
	}

	// remove proxy protocol
	network, _ := streamSettings["network"].(string)
	switch network {
	case "tcp":
		streamSettings["tcpSettings"] = s.removeAcceptProxy(streamSettings["tcpSettings"])
	case "ws":
		streamSettings["wsSettings"] = s.removeAcceptProxy(streamSettings["wsSettings"])
	case "httpupgrade":
		streamSettings["httpupgradeSettings"] = s.removeAcceptProxy(streamSettings["httpupgradeSettings"])
	}
	return streamSettings
}

func (s *SubJsonService) removeAcceptProxy(setting any) map[string]any {
	netSettings, ok := setting.(map[string]any)
	if ok {
		delete(netSettings, "acceptProxyProtocol")
	}
	return netSettings
}

func (s *SubJsonService) tlsData(tData map[string]any) map[string]any {
	tlsData := make(map[string]any, 1)
	tlsClientSettings, _ := tData["settings"].(map[string]any)

	tlsData["serverName"] = tData["serverName"]
	tlsData["alpn"] = tData["alpn"]
	if fingerprint, ok := tlsClientSettings["fingerprint"].(string); ok {
		tlsData["fingerprint"] = fingerprint
	}
	return tlsData
}

func (s *SubJsonService) realityData(rData map[string]any) map[string]any {
	rltyData := make(map[string]any, 1)
	rltyClientSettings, _ := rData["settings"].(map[string]any)

	rltyData["show"] = false
	rltyData["publicKey"] = rltyClientSettings["publicKey"]
	rltyData["fingerprint"] = rltyClientSettings["fingerprint"]
	rltyData["mldsa65Verify"] = rltyClientSettings["mldsa65Verify"]

	// Set random data
	rltyData["spiderX"] = "/" + random.Seq(15)
	shortIds, ok := rData["shortIds"].([]any)
	if ok && len(shortIds) > 0 {
		rltyData["shortId"] = shortIds[random.Num(len(shortIds))].(string)
	} else {
		rltyData["shortId"] = ""
	}
	serverNames, ok := rData["serverNames"].([]any)
	if ok && len(serverNames) > 0 {
		rltyData["serverName"] = serverNames[random.Num(len(serverNames))].(string)
	} else {
		rltyData["serverName"] = ""
	}

	return rltyData
}

func (s *SubJsonService) genVnext(inbound *model.Inbound, streamSettings json_util.RawMessage, client model.Client) json_util.RawMessage {
	outbound := Outbound{}
	usersData := make([]UserVnext, 1)

	usersData[0].ID = client.ID
	usersData[0].Email = client.Email
	usersData[0].Security = client.Security
	vnextData := make([]VnextSetting, 1)
	vnextData[0] = VnextSetting{
		Address: inbound.Listen,
		Port:    inbound.Port,
		Users:   usersData,
	}

	outbound.Protocol = string(inbound.Protocol)
	outbound.Tag = "proxy"
	if s.mux != "" {
		outbound.Mux = json_util.RawMessage(s.mux)
	}
	outbound.StreamSettings = streamSettings
	outbound.Settings = map[string]any{
		"vnext": vnextData,
	}

	result, _ := json.MarshalIndent(outbound, "", "  ")
	return result
}

func (s *SubJsonService) genVless(inbound *model.Inbound, streamSettings json_util.RawMessage, client model.Client) json_util.RawMessage {
	outbound := Outbound{}
	outbound.Protocol = string(inbound.Protocol)
	outbound.Tag = "proxy"
	if s.mux != "" {
		outbound.Mux = json_util.RawMessage(s.mux)
	}
	outbound.StreamSettings = streamSettings
	settings := make(map[string]any)
	settings["address"] = inbound.Listen
	settings["port"] = inbound.Port
	settings["id"] = client.ID
	if client.Flow != "" {
		settings["flow"] = client.Flow
	}

	// Add encryption for VLESS outbound from inbound settings
	var inboundSettings map[string]any
	json.Unmarshal([]byte(inbound.Settings), &inboundSettings)
	if encryption, ok := inboundSettings["encryption"].(string); ok {
		settings["encryption"] = encryption
	}

	outbound.Settings = settings
	result, _ := json.MarshalIndent(outbound, "", "  ")
	return result
}

func (s *SubJsonService) genServer(inbound *model.Inbound, streamSettings json_util.RawMessage, client model.Client) json_util.RawMessage {
	outbound := Outbound{}

	serverData := make([]ServerSetting, 1)
	serverData[0] = ServerSetting{
		Address:  inbound.Listen,
		Port:     inbound.Port,
		Level:    8,
		Password: client.Password,
	}

	if inbound.Protocol == model.Shadowsocks {
		var inboundSettings map[string]any
		json.Unmarshal([]byte(inbound.Settings), &inboundSettings)
		method, _ := inboundSettings["method"].(string)
		serverData[0].Method = method

		// server password in multi-user 2022 protocols
		if strings.HasPrefix(method, "2022") {
			if serverPassword, ok := inboundSettings["password"].(string); ok {
				serverData[0].Password = fmt.Sprintf("%s:%s", serverPassword, client.Password)
			}
		}
	}

	outbound.Protocol = string(inbound.Protocol)
	outbound.Tag = "proxy"
	if s.mux != "" {
		outbound.Mux = json_util.RawMessage(s.mux)
	}
	outbound.StreamSettings = streamSettings
	outbound.Settings = map[string]any{
		"servers": serverData,
	}

	result, _ := json.MarshalIndent(outbound, "", "  ")
	return result
}

type Outbound struct {
	Protocol       string               `json:"protocol"`
	Tag            string               `json:"tag"`
	StreamSettings json_util.RawMessage `json:"streamSettings"`
	Mux            json_util.RawMessage `json:"mux,omitempty"`
	Settings       map[string]any       `json:"settings,omitempty"`
}

type VnextSetting struct {
	Address string      `json:"address"`
	Port    int         `json:"port"`
	Users   []UserVnext `json:"users"`
}

type UserVnext struct {
	ID       string `json:"id"`
	Email    string `json:"email,omitempty"`
	Security string `json:"security,omitempty"`
}

type ServerSetting struct {
	Password string `json:"password"`
	Level    int    `json:"level"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Flow     string `json:"flow,omitempty"`
	Method   string `json:"method,omitempty"`
}