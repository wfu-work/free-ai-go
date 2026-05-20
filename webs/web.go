package webs

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

var fileContentTypeMap = map[string]string{
	".js":       "application/javascript",
	".mjs":      "application/javascript",
	".css":      "text/css",
	".manifest": "text/cache-manifest",
	".png":      "image/png",
	".jpg":      "image/jpeg",
	".jpeg":     "image/jpeg",
	".svg":      "image/svg+xml",
	".ico":      "image/x-icon",
	".json":     "application/json",
	".html":     "text/html; charset=utf-8",
	".htm":      "text/html; charset=utf-8",
	".txt":      "text/plain; charset=utf-8",
	".wasm":     "application/wasm",
}

func InitStatic(engine *gin.Engine) error {
	return StaticFile(Static(), func(fileMap map[string][]byte) {
		indexHTML, ok := fileMap["freeai-web/browser/index.html"]
		if !ok {
			panic("freeai-web/browser/index.html 文件不存在")
		}

		for fileKey, fileBytes := range fileMap {
			fileKey := fileKey
			fileBytes := fileBytes
			ginStaticFilePath := strings.TrimPrefix(fileKey, "freeai-web/browser/")
			if ginStaticFilePath == "" {
				continue
			}
			engine.GET(ginStaticFilePath,
				cacheControlMiddleware(),
				serveFileHandler(fileKey, fileBytes),
			)
		}

		engine.NoRoute(func(c *gin.Context) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
		})
	})
}

func StaticFile(zipFile []byte, callback func(fileMap map[string][]byte)) error {
	result := make(map[string][]byte)
	zipReader, err := zip.NewReader(bytes.NewReader(zipFile), int64(len(zipFile)))
	if err != nil {
		return fmt.Errorf("加载静态资源失败: %w", err)
	}

	for _, file := range zipReader.File {
		open, err := file.Open()
		if err != nil {
			return fmt.Errorf("打开文件 %s 失败: %w", file.Name, err)
		}

		data, err := io.ReadAll(open)
		_ = open.Close()
		if err != nil {
			return fmt.Errorf("读取文件 %s 失败: %w", file.Name, err)
		}

		result[file.Name] = data
	}

	callback(result)
	return nil
}

func cacheControlMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
		c.Next()
	}
}

func serveFileHandler(fileKey string, fileBytes []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Data(http.StatusOK, MatchFile(fileKey), fileBytes)
	}
}

func MatchFile(fileName string) string {
	ext := strings.ToLower(filepath.Ext(fileName))
	if contentType, ok := fileContentTypeMap[ext]; ok {
		return contentType
	}
	return "application/octet-stream"
}
