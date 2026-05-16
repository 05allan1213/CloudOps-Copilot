package router

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

func limitRequestBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

func registerStaticRoutes(router *gin.Engine, staticDir string) error {
	if staticDir == "" {
		return nil
	}
	if _, err := os.Stat(staticDir); err != nil {
		return nil
	}
	staticHandler, err := serveStatic(staticDir)
	if err != nil {
		return err
	}
	router.Use(staticHandler)
	return nil
}

func serveStatic(staticDir string) (gin.HandlerFunc, error) {
	fileServer := http.FileServer(http.Dir(staticDir))
	absStaticDir, err := filepath.Abs(staticDir)
	if err != nil {
		return nil, err
	}

	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Next()
			return
		}
		if len(path) >= 5 && path[:5] == "/api/" {
			c.Next()
			return
		}
		if len(path) >= 4 && path[:4] == "/ws/" {
			c.Next()
			return
		}
		if path == "/healthz" || path == "/readyz" || strings.HasPrefix(path, "/readyz/") {
			c.Next()
			return
		}

		filePath := filepath.Join(absStaticDir, filepath.Clean(path))
		if !strings.HasPrefix(filePath, absStaticDir+string(os.PathSeparator)) && filePath != absStaticDir {
			c.Next()
			return
		}
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}

		indexPath := filepath.Join(absStaticDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			c.Request.URL.Path = "/"
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		c.Next()
	}, nil
}
