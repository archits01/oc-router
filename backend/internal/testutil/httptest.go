//go:build unit

package testutil

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// NewGinTestContext
// body
func NewGinTestContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	c.Request = httptest.NewRequest(method, path, bodyReader)
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
		c.Request.Header.Set("Content-Type", "application/json")
	}

	return c, rec
}
