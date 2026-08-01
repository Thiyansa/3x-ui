// web/controller/routing_controller.go
package controller

import (
	"strconv"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/Thiyansa/3x-ui/v2/util/common"
	"github.com/Thiyansa/3x-ui/v2/web/service"
	"github.com/Thiyansa/3x-ui/v2/web/websocket"
)

// RoutingController handles HTTP requests for routing configuration management.
// All operations work directly on the JSON config file, not the database.
type RoutingController struct {
	BaseController
	settingService service.SettingService
	xrayService    service.XrayService
}

// NewRoutingController creates a new RoutingController and initializes its routes.
func NewRoutingController(g *gin.RouterGroup, settingService service.SettingService, xrayService service.XrayService) *RoutingController {
	a := &RoutingController{
		settingService: settingService,
		xrayService:    xrayService,
	}
	a.initRouter(g)
	return a
}

// initRouter sets up the routes for routing configuration operations.
func (a *RoutingController) initRouter(g *gin.RouterGroup) {
	// Get routing rules only
	g.GET("/rules", a.getRoutingRules)
	
	// Get a single routing rule by index
	g.GET("/rules/:index", a.getRoutingRule)
	
	// Add a new routing rule
	g.POST("/rules", a.addRoutingRule)
	
	// Update a routing rule by index
	g.PUT("/rules/:index", a.updateRoutingRule)
	
	// Delete a routing rule by index
	g.DELETE("/rules/:index", a.deleteRoutingRule)
	
	// Add/Remove users from a routing rule
	g.POST("/rules/:index/users", a.updateRuleUsers)
	
	// Delete all routing rules
	g.DELETE("/rules/all", a.deleteAllRoutingRules)
}

// getRoutingRules retrieves only the routing rules from config.json
func (a *RoutingController) getRoutingRules(c *gin.Context) {
	config, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.obtainConfig"), err)
		return
	}
	
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(config), &configMap); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.parseConfigError"), err)
		return
	}
	
	routing, ok := configMap["routing"].(map[string]interface{})
	if !ok {
		jsonObj(c, []interface{}{}, nil)
		return
	}
	
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		jsonObj(c, []interface{}{}, nil)
		return
	}
	
	// Filter out database marker rules if any (cleanup)
	cleanRules := make([]interface{}, 0)
	for _, rule := range rules {
		if ruleMap, ok := rule.(map[string]interface{}); ok {
			// Remove fromDB marker if present
			delete(ruleMap, "fromDB")
			cleanRules = append(cleanRules, ruleMap)
		} else {
			cleanRules = append(cleanRules, rule)
		}
	}
	
	jsonObj(c, cleanRules, nil)
}

// getRoutingRule retrieves a specific routing rule by index
func (a *RoutingController) getRoutingRule(c *gin.Context) {
	indexStr := c.Param("index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidIndex"), err)
		return
	}
	
	config, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.obtainConfig"), err)
		return
	}
	
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(config), &configMap); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.parseConfigError"), err)
		return
	}
	
	routing, ok := configMap["routing"].(map[string]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.noRoutingSection"), common.NewError("Routing section not found"))
		return
	}
	
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.noRules"), common.NewError("No rules found"))
		return
	}
	
	if index < 0 || index >= len(rules) {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.indexOutOfBounds"), common.NewError("Rule index out of bounds"))
		return
	}
	
	jsonObj(c, rules[index], nil)
}

// addRoutingRule adds a new routing rule to config.json
func (a *RoutingController) addRoutingRule(c *gin.Context) {
	var newRule map[string]interface{}
	if err := c.ShouldBindJSON(&newRule); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidData"), err)
		return
	}
	
	// Validate rule has at least outboundTag or balancerTag
	if _, hasOutbound := newRule["outboundTag"]; !hasOutbound {
		if _, hasBalancer := newRule["balancerTag"]; !hasBalancer {
			jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidRule"), common.NewError("Rule must have outboundTag or balancerTag"))
			return
		}
	}
	
	// Set default type if not present
	if _, ok := newRule["type"]; !ok {
		newRule["type"] = "field"
	}
	
	// Get current config
	config, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.obtainConfig"), err)
		return
	}
	
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(config), &configMap); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.parseConfigError"), err)
		return
	}
	
	// Get routing section
	routing, ok := configMap["routing"].(map[string]interface{})
	if !ok {
		routing = make(map[string]interface{})
		routing["domainStrategy"] = "AsIs"
		routing["rules"] = []interface{}{}
		configMap["routing"] = routing
	}
	
	// Get existing rules
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		rules = []interface{}{}
	}
	
	// Add new rule
	rules = append(rules, newRule)
	routing["rules"] = rules
	
	// Save updated config
	newConfig, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.marshalConfigError"), err)
		return
	}
	
	if err := a.settingService.SaveSetting("xrayTemplateConfig", string(newConfig)); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.saveConfigError"), err)
		return
	}
	
	// Mark Xray for restart
	a.xrayService.SetToNeedRestart()
	
	// Broadcast routing update via WebSocket
	websocket.BroadcastRouting(routing)
	
	jsonMsgObj(c, I18nWeb(c, "pages.routing.toasts.addRuleSuccess"), newRule, nil)
}

// updateRoutingRule updates a routing rule by its index in the rules array
func (a *RoutingController) updateRoutingRule(c *gin.Context) {
	indexStr := c.Param("index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidIndex"), err)
		return
	}
	
	var updatedRule map[string]interface{}
	if err := c.ShouldBindJSON(&updatedRule); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidData"), err)
		return
	}
	
	// Validate rule has at least outboundTag or balancerTag
	if _, hasOutbound := updatedRule["outboundTag"]; !hasOutbound {
		if _, hasBalancer := updatedRule["balancerTag"]; !hasBalancer {
			jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidRule"), common.NewError("Rule must have outboundTag or balancerTag"))
			return
		}
	}
	
	// Get current config
	config, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.obtainConfig"), err)
		return
	}
	
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(config), &configMap); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.parseConfigError"), err)
		return
	}
	
	// Get routing section
	routing, ok := configMap["routing"].(map[string]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.noRoutingSection"), common.NewError("Routing section not found"))
		return
	}
	
	// Get existing rules
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.noRules"), common.NewError("No rules found"))
		return
	}
	
	// Check index bounds
	if index < 0 || index >= len(rules) {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.indexOutOfBounds"), common.NewError("Rule index out of bounds"))
		return
	}
	
	// Update rule
	rules[index] = updatedRule
	routing["rules"] = rules
	
	// Save updated config
	newConfig, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.marshalConfigError"), err)
		return
	}
	
	if err := a.settingService.SaveSetting("xrayTemplateConfig", string(newConfig)); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.saveConfigError"), err)
		return
	}
	
	// Mark Xray for restart
	a.xrayService.SetToNeedRestart()
	
	// Broadcast routing update via WebSocket
	websocket.BroadcastRouting(routing)
	
	jsonMsgObj(c, I18nWeb(c, "pages.routing.toasts.updateRuleSuccess"), updatedRule, nil)
}

// deleteRoutingRule deletes a routing rule by its index
func (a *RoutingController) deleteRoutingRule(c *gin.Context) {
	indexStr := c.Param("index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidIndex"), err)
		return
	}
	
	// Get current config
	config, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.obtainConfig"), err)
		return
	}
	
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(config), &configMap); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.parseConfigError"), err)
		return
	}
	
	// Get routing section
	routing, ok := configMap["routing"].(map[string]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.noRoutingSection"), common.NewError("Routing section not found"))
		return
	}
	
	// Get existing rules
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.noRules"), common.NewError("No rules found"))
		return
	}
	
	// Check index bounds
	if index < 0 || index >= len(rules) {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.indexOutOfBounds"), common.NewError("Rule index out of bounds"))
		return
	}
	
	// Remove rule
	rules = append(rules[:index], rules[index+1:]...)
	routing["rules"] = rules
	
	// Save updated config
	newConfig, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.marshalConfigError"), err)
		return
	}
	
	if err := a.settingService.SaveSetting("xrayTemplateConfig", string(newConfig)); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.saveConfigError"), err)
		return
	}
	
	// Mark Xray for restart
	a.xrayService.SetToNeedRestart()
	
	// Broadcast routing update via WebSocket
	websocket.BroadcastRouting(routing)
	
	jsonMsg(c, I18nWeb(c, "pages.routing.toasts.deleteRuleSuccess"), nil)
}

// updateRuleUsers adds or removes users from a routing rule
// Request body: {"action": "add"|"remove", "users": ["user1@example.com", "user2@example.com"]}
func (a *RoutingController) updateRuleUsers(c *gin.Context) {
	indexStr := c.Param("index")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidIndex"), err)
		return
	}

	var request struct {
		Action string   `json:"action"` // "add" or "remove"
		Users  []string `json:"users"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidData"), err)
		return
	}

	if request.Action != "add" && request.Action != "remove" {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidAction"), common.NewError("Action must be 'add' or 'remove'"))
		return
	}

	if len(request.Users) == 0 {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidData"), common.NewError("Users list cannot be empty"))
		return
	}

	// Get current config
	config, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.obtainConfig"), err)
		return
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(config), &configMap); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.parseConfigError"), err)
		return
	}

	// Get routing section
	routing, ok := configMap["routing"].(map[string]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.noRoutingSection"), common.NewError("Routing section not found"))
		return
	}

	// Get existing rules
	rules, ok := routing["rules"].([]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.noRules"), common.NewError("No rules found"))
		return
	}

	// Check index bounds
	if index < 0 || index >= len(rules) {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.indexOutOfBounds"), common.NewError("Rule index out of bounds"))
		return
	}

	// Get the rule and ensure it has a user field
	rule, ok := rules[index].(map[string]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.invalidRule"), common.NewError("Invalid rule format"))
		return
	}

	// Initialize user list if not present
	var users []string
	if existingUsers, ok := rule["user"].([]interface{}); ok {
		for _, u := range existingUsers {
			if str, ok := u.(string); ok {
				users = append(users, str)
			}
		}
	} else {
		// Create new user list if doesn't exist
		users = []string{}
	}

	// Perform add or remove action
	switch request.Action {
	case "add":
		// Add users (avoid duplicates)
		userSet := make(map[string]bool)
		for _, u := range users {
			userSet[u] = true
		}
		for _, u := range request.Users {
			if !userSet[u] {
				users = append(users, u)
				userSet[u] = true
			}
		}
	case "remove":
		// Remove users
		removeSet := make(map[string]bool)
		for _, u := range request.Users {
			removeSet[u] = true
		}
		var newUsers []string
		for _, u := range users {
			if !removeSet[u] {
				newUsers = append(newUsers, u)
			}
		}
		users = newUsers
	}

	// Update the rule
	if len(users) > 0 {
		userInterfaces := make([]interface{}, len(users))
		for i, u := range users {
			userInterfaces[i] = u
		}
		rule["user"] = userInterfaces
	} else {
		// Remove user field if empty
		delete(rule, "user")
	}

	// Save updated config
	newConfig, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.marshalConfigError"), err)
		return
	}

	if err := a.settingService.SaveSetting("xrayTemplateConfig", string(newConfig)); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.saveConfigError"), err)
		return
	}

	// Mark Xray for restart
	a.xrayService.SetToNeedRestart()

	// Broadcast routing update via WebSocket
	websocket.BroadcastRouting(routing)

	jsonMsgObj(c, I18nWeb(c, "pages.routing.toasts.updateRuleUsersSuccess"), map[string]interface{}{
		"action": request.Action,
		"users":  request.Users,
		"total":  len(users),
	}, nil)
}

// deleteAllRoutingRules deletes all routing rules
func (a *RoutingController) deleteAllRoutingRules(c *gin.Context) {
	// Get current config
	config, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.obtainConfig"), err)
		return
	}
	
	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(config), &configMap); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.parseConfigError"), err)
		return
	}
	
	// Get routing section
	routing, ok := configMap["routing"].(map[string]interface{})
	if !ok {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.noRoutingSection"), common.NewError("Routing section not found"))
		return
	}
	
	// Clear rules
	routing["rules"] = []interface{}{}
	
	// Save updated config
	newConfig, err := json.MarshalIndent(configMap, "", "  ")
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.marshalConfigError"), err)
		return
	}
	
	if err := a.settingService.SaveSetting("xrayTemplateConfig", string(newConfig)); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.routing.toasts.saveConfigError"), err)
		return
	}
	
	// Mark Xray for restart
	a.xrayService.SetToNeedRestart()
	
	// Broadcast routing update via WebSocket
	websocket.BroadcastRouting(routing)
	
	jsonMsg(c, I18nWeb(c, "pages.routing.toasts.deleteAllRulesSuccess"), nil)
}
