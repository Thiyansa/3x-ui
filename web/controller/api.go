// web/controller/api.go
package controller

import (
	"encoding/json"
	"net/http"

	"github.com/Thiyansa/3x-ui/v2/database"
	"github.com/Thiyansa/3x-ui/v2/database/model"
	"github.com/Thiyansa/3x-ui/v2/web/service"
	"github.com/Thiyansa/3x-ui/v2/web/session"

	"github.com/gin-gonic/gin"
)

// APIController handles the main API routes for the 3x-ui panel.
type APIController struct {
	BaseController
	inboundController  *InboundController
	serverController   *ServerController
	routingController  *RoutingController
	Tgbot              service.Tgbot
	xrayService        service.XrayService
	settingService     service.SettingService
}

// NewAPIController creates a new APIController instance and initializes its routes.
func NewAPIController(g *gin.RouterGroup, xrayService service.XrayService) *APIController {
	a := &APIController{
		xrayService:    xrayService,
		settingService: service.SettingService{},
	}
	a.initRouter(g)
	return a
}

// checkAPIAuth is a middleware that returns 404 for unauthenticated API requests.
func (a *APIController) checkAPIAuth(c *gin.Context) {
	if !session.IsLogin(c) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Next()
}

// initRouter sets up the API routes for inbounds, server, and routing.
func (a *APIController) initRouter(g *gin.RouterGroup) {
	// Main API group
	api := g.Group("/panel/api")
	api.Use(a.checkAPIAuth)

	// Inbounds API
	inbounds := api.Group("/inbounds")
	a.inboundController = NewInboundController(inbounds)

	// Server API
	server := api.Group("/server")
	a.serverController = NewServerController(server)

	// Routing API - JSON-based routing configuration
	// Pass settingService and xrayService to RoutingController
	routing := api.Group("/routing")
	a.routingController = NewRoutingController(routing, a.settingService, a.xrayService)

	// Outbounds API for tags
	outbounds := api.Group("/outbounds")
	outbounds.GET("/tags", a.getOutboundTags)

	// Extra routes
	api.GET("/backuptotgbot", a.BackuptoTgbot)
}

// BackuptoTgbot sends a backup of the panel data to Telegram bot admins.
func (a *APIController) BackuptoTgbot(c *gin.Context) {
	a.Tgbot.SendBackupToAdmins()
}

// getOutboundTags returns all outbound tags from the current Xray config template
func (a *APIController) getOutboundTags(c *gin.Context) {
	// Get Xray config template from settings
	templateConfig, err := a.settingService.GetXrayConfigTemplate()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.getSettings"), err)
		return
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal([]byte(templateConfig), &configMap); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.settings.toasts.getSettings"), err)
		return
	}

	// Extract outbound tags
	var tags []string
	if outbounds, ok := configMap["outbounds"].([]interface{}); ok {
		for _, ob := range outbounds {
			if obMap, ok := ob.(map[string]interface{}); ok {
				if tag, ok := obMap["tag"].(string); ok && tag != "" {
					tags = append(tags, tag)
				}
			}
		}
	}

	// Also get outbound tags from database in case they're not in config yet
	db := database.GetDB()
	var dbOutbounds []model.OutboundTraffics
	if err := db.Find(&dbOutbounds).Error; err == nil {
		for _, ob := range dbOutbounds {
			// Only add if not already in the list
			found := false
			for _, t := range tags {
				if t == ob.Tag {
					found = true
					break
				}
			}
			if !found && ob.Tag != "" {
				tags = append(tags, ob.Tag)
			}
		}
	}

	jsonObj(c, map[string]interface{}{
		"tags": tags,
	}, nil)
}