package server

import (
	"github.com/gin-gonic/gin"
)

// CORSMiddleware adds CORS headers to all responses
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Allow all origins (adjust for production)
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*") //TODO: do not use *

		// Allow common HTTP methods
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS, HEAD")

		// Allow requested headers (permissive for development, consider restricting in production)
		requestedHeaders := c.Request.Header.Get("Access-Control-Request-Headers")
		if requestedHeaders != "" {
			// Echo back the requested headers to allow any custom headers
			c.Writer.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
		} else {
			// Default headers including Authorization for Bearer tokens and MCP/SSE protocol headers
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, Authorization, X-CSRF-Token, Accept, X-Requested-With, mcp-protocol-version, Last-Event-ID")
		}

		// Allow credentials (cookies, authorization headers)
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")

		// Cache preflight response for 12 hours
		c.Writer.Header().Set("Access-Control-Max-Age", "43200")

		// Expose headers that the client can access (including MCP protocol headers)
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, Authorization, mcp-protocol-version")

		// Handle preflight OPTIONS request
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204) // No Content
			return
		}

		c.Next()
	}
}
