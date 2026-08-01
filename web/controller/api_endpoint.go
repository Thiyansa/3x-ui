// web/controller/api_endpoint.go
package controller

import (
	"github.com/gin-gonic/gin"
)

// APIEndpointController handles the API endpoints documentation page
type APIEndpointController struct {
	BaseController
}

// NewAPIEndpointController creates a new APIEndpointController instance
func NewAPIEndpointController(g *gin.RouterGroup) *APIEndpointController {
	a := &APIEndpointController{}
	a.initRouter(g)
	return a
}

// initRouter sets up the routes for the API endpoints page
func (a *APIEndpointController) initRouter(g *gin.RouterGroup) {
	// The main page is already registered in XUIController
	// This is just a placeholder for any additional routes if needed
}

// getEndpointsPage renders the API endpoints documentation page
func (a *APIEndpointController) getEndpointsPage(c *gin.Context) {
	html(c, "api_endpoints.html", "pages.apiEndpoints.title", nil)
}