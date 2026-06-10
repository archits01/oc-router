//go:build unit

package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	errors2 "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ----------

// parseResponseBody
func parseResponseBody(t *testing.T, w *httptest.ResponseRecorder) Response {
	t.Helper()
	var got Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	return got
}

// parsePaginatedBody
func parsePaginatedBody(t *testing.T, w *httptest.ResponseRecorder) (Response, PaginatedData) {
	t.Helper()
	//
	var raw struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Reason  string          `json:"reason,omitempty"`
		Data    json.RawMessage `json:"data,omitempty"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))

	var pd PaginatedData
	require.NoError(t, json.Unmarshal(raw.Data, &pd))

	return Response{Code: raw.Code, Message: raw.Message, Reason: raw.Reason}, pd
}

// newContextWithQuery
func newContextWithQuery(query string) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/?"+query, nil)
	return w, c
}

// ----------

func TestErrorWithDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		statusCode int
		message    string
		reason     string
		metadata   map[string]string
		want       Response
	}{
		{
			name:       "plain_error",
			statusCode: http.StatusBadRequest,
			message:    "invalid request",
			want: Response{
				Code:    http.StatusBadRequest,
				Message: "invalid request",
			},
		},
		{
			name:       "structured_error",
			statusCode: http.StatusForbidden,
			message:    "no access",
			reason:     "FORBIDDEN",
			metadata:   map[string]string{"k": "v"},
			want: Response{
				Code:     http.StatusForbidden,
				Message:  "no access",
				Reason:   "FORBIDDEN",
				Metadata: map[string]string{"k": "v"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			ErrorWithDetails(c, tt.statusCode, tt.message, tt.reason, tt.metadata)

			require.Equal(t, tt.statusCode, w.Code)

			var got Response
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestErrorFrom(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		err          error
		wantWritten  bool
		wantHTTPCode int
		wantBody     Response
	}{
		{
			name:        "nil_error",
			err:         nil,
			wantWritten: false,
		},
		{
			name:         "application_error",
			err:          errors2.Forbidden("FORBIDDEN", "no access").WithMetadata(map[string]string{"scope": "admin"}),
			wantWritten:  true,
			wantHTTPCode: http.StatusForbidden,
			wantBody: Response{
				Code:     http.StatusForbidden,
				Message:  "no access",
				Reason:   "FORBIDDEN",
				Metadata: map[string]string{"scope": "admin"},
			},
		},
		{
			name:         "bad_request_error",
			err:          errors2.BadRequest("INVALID_REQUEST", "invalid request"),
			wantWritten:  true,
			wantHTTPCode: http.StatusBadRequest,
			wantBody: Response{
				Code:    http.StatusBadRequest,
				Message: "invalid request",
				Reason:  "INVALID_REQUEST",
			},
		},
		{
			name:         "unauthorized_error",
			err:          errors2.Unauthorized("UNAUTHORIZED", "unauthorized"),
			wantWritten:  true,
			wantHTTPCode: http.StatusUnauthorized,
			wantBody: Response{
				Code:    http.StatusUnauthorized,
				Message: "unauthorized",
				Reason:  "UNAUTHORIZED",
			},
		},
		{
			name:         "not_found_error",
			err:          errors2.NotFound("NOT_FOUND", "not found"),
			wantWritten:  true,
			wantHTTPCode: http.StatusNotFound,
			wantBody: Response{
				Code:    http.StatusNotFound,
				Message: "not found",
				Reason:  "NOT_FOUND",
			},
		},
		{
			name:         "conflict_error",
			err:          errors2.Conflict("CONFLICT", "conflict"),
			wantWritten:  true,
			wantHTTPCode: http.StatusConflict,
			wantBody: Response{
				Code:    http.StatusConflict,
				Message: "conflict",
				Reason:  "CONFLICT",
			},
		},
		{
			name:         "unknown_error_defaults_to_500",
			err:          errors.New("boom"),
			wantWritten:  true,
			wantHTTPCode: http.StatusInternalServerError,
			wantBody: Response{
				Code:    http.StatusInternalServerError,
				Message: errors2.UnknownMessage,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			written := ErrorFrom(c, tt.err)
			require.Equal(t, tt.wantWritten, written)

			if !tt.wantWritten {
				require.Equal(t, 200, w.Code)
				require.Empty(t, w.Body.String())
				return
			}

			require.Equal(t, tt.wantHTTPCode, w.Code)
			var got Response
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			require.Equal(t, tt.wantBody, got)
		})
	}
}

// ----------

func TestSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		data     any
		wantCode int
		wantBody Response
	}{
		{
			name:     "returns string data",
			data:     "hello",
			wantCode: http.StatusOK,
			wantBody: Response{Code: 0, Message: "success", Data: "hello"},
		},
		{
			name:     "returns nil data",
			data:     nil,
			wantCode: http.StatusOK,
			wantBody: Response{Code: 0, Message: "success"},
		},
		{
			name:     "returns map data",
			data:     map[string]string{"key": "value"},
			wantCode: http.StatusOK,
			wantBody: Response{Code: 0, Message: "success"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			Success(c, tt.data)

			require.Equal(t, tt.wantCode, w.Code)

			//
			got := parseResponseBody(t, w)
			require.Equal(t, 0, got.Code)
			require.Equal(t, "success", got.Message)

			if tt.data == nil {
				require.Nil(t, got.Data)
			} else {
				require.NotNil(t, got.Data)
			}
		})
	}
}

func TestCreated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		data     any
		wantCode int
	}{
		{
			name:     "creation success returns data",
			data:     map[string]int{"id": 42},
			wantCode: http.StatusCreated,
		},
		{
			name:     "creation success nil data",
			data:     nil,
			wantCode: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			Created(c, tt.data)

			require.Equal(t, tt.wantCode, w.Code)

			got := parseResponseBody(t, w)
			require.Equal(t, 0, got.Code)
			require.Equal(t, "success", got.Message)
		})
	}
}

func TestError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		statusCode int
		message    string
	}{
		{
			name:       "400 error",
			statusCode: http.StatusBadRequest,
			message:    "bad request",
		},
		{
			name:       "500 error",
			statusCode: http.StatusInternalServerError,
			message:    "internal error",
		},
		{
			name:       "custom status code",
			statusCode: 418,
			message:    "I'm a teapot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			Error(c, tt.statusCode, tt.message)

			require.Equal(t, tt.statusCode, w.Code)

			got := parseResponseBody(t, w)
			require.Equal(t, tt.statusCode, got.Code)
			require.Equal(t, tt.message, got.Message)
			require.Empty(t, got.Reason)
			require.Nil(t, got.Metadata)
			require.Nil(t, got.Data)
		})
	}
}

func TestBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	BadRequest(c, "invalid parameters")

	require.Equal(t, http.StatusBadRequest, w.Code)
	got := parseResponseBody(t, w)
	require.Equal(t, http.StatusBadRequest, got.Code)
	require.Equal(t, "invalid parameters", got.Message)
}

func TestUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	Unauthorized(c, "not authenticated")

	require.Equal(t, http.StatusUnauthorized, w.Code)
	got := parseResponseBody(t, w)
	require.Equal(t, http.StatusUnauthorized, got.Code)
	require.Equal(t, "not authenticated", got.Message)
}

func TestForbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	Forbidden(c, "no permission")

	require.Equal(t, http.StatusForbidden, w.Code)
	got := parseResponseBody(t, w)
	require.Equal(t, http.StatusForbidden, got.Code)
	require.Equal(t, "no permission", got.Message)
}

func TestNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	NotFound(c, "resource not found")

	require.Equal(t, http.StatusNotFound, w.Code)
	got := parseResponseBody(t, w)
	require.Equal(t, http.StatusNotFound, got.Code)
	require.Equal(t, "resource not found", got.Message)
}

func TestInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	InternalError(c, "internal server error")

	require.Equal(t, http.StatusInternalServerError, w.Code)
	got := parseResponseBody(t, w)
	require.Equal(t, http.StatusInternalServerError, got.Code)
	require.Equal(t, "internal server error", got.Message)
}

func TestPaginated(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		items        any
		total        int64
		page         int
		pageSize     int
		wantPages    int
		wantTotal    int64
		wantPage     int
		wantPageSize int
	}{
		{
			name:         "standard pagination multiple pages",
			items:        []string{"a", "b"},
			total:        25,
			page:         1,
			pageSize:     10,
			wantPages:    3,
			wantTotal:    25,
			wantPage:     1,
			wantPageSize: 10,
		},
		{
			name:         "total count evenly divisible",
			items:        []string{"a"},
			total:        20,
			page:         2,
			pageSize:     10,
			wantPages:    2,
			wantTotal:    20,
			wantPage:     2,
			wantPageSize: 10,
		},
		{
			name:         "total count 0 pages at least 1",
			items:        []string{},
			total:        0,
			page:         1,
			pageSize:     10,
			wantPages:    1,
			wantTotal:    0,
			wantPage:     1,
			wantPageSize: 10,
		},
		{
			name:         "single page data",
			items:        []int{1, 2, 3},
			total:        3,
			page:         1,
			pageSize:     20,
			wantPages:    1,
			wantTotal:    3,
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "total count is 1",
			items:        []string{"only"},
			total:        1,
			page:         1,
			pageSize:     10,
			wantPages:    1,
			wantTotal:    1,
			wantPage:     1,
			wantPageSize: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			Paginated(c, tt.items, tt.total, tt.page, tt.pageSize)

			require.Equal(t, http.StatusOK, w.Code)

			resp, pd := parsePaginatedBody(t, w)
			require.Equal(t, 0, resp.Code)
			require.Equal(t, "success", resp.Message)
			require.Equal(t, tt.wantTotal, pd.Total)
			require.Equal(t, tt.wantPage, pd.Page)
			require.Equal(t, tt.wantPageSize, pd.PageSize)
			require.Equal(t, tt.wantPages, pd.Pages)
		})
	}
}

func TestPaginatedWithResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		items        any
		pagination   *PaginationResult
		wantTotal    int64
		wantPage     int
		wantPageSize int
		wantPages    int
	}{
		{
			name:  "normal pagination result",
			items: []string{"a", "b"},
			pagination: &PaginationResult{
				Total:    50,
				Page:     3,
				PageSize: 10,
				Pages:    5,
			},
			wantTotal:    50,
			wantPage:     3,
			wantPageSize: 10,
			wantPages:    5,
		},
		{
			name:         "pagination nil uses default",
			items:        []string{},
			pagination:   nil,
			wantTotal:    0,
			wantPage:     1,
			wantPageSize: 20,
			wantPages:    1,
		},
		{
			name:  "single page result",
			items: []int{1},
			pagination: &PaginationResult{
				Total:    1,
				Page:     1,
				PageSize: 20,
				Pages:    1,
			},
			wantTotal:    1,
			wantPage:     1,
			wantPageSize: 20,
			wantPages:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			PaginatedWithResult(c, tt.items, tt.pagination)

			require.Equal(t, http.StatusOK, w.Code)

			resp, pd := parsePaginatedBody(t, w)
			require.Equal(t, 0, resp.Code)
			require.Equal(t, "success", resp.Message)
			require.Equal(t, tt.wantTotal, pd.Total)
			require.Equal(t, tt.wantPage, pd.Page)
			require.Equal(t, tt.wantPageSize, pd.PageSize)
			require.Equal(t, tt.wantPages, pd.Pages)
		})
	}
}

func TestParsePagination(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		query        string
		wantPage     int
		wantPageSize int
	}{
		{
			name:         "no params uses default",
			query:        "",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "only specify page",
			query:        "page=3",
			wantPage:     3,
			wantPageSize: 20,
		},
		{
			name:         "only specify page_size",
			query:        "page_size=50",
			wantPage:     1,
			wantPageSize: 50,
		},
		{
			name:         "specify both page and page_size",
			query:        "page=2&page_size=30",
			wantPage:     2,
			wantPageSize: 30,
		},
		{
			name:         "use limit instead of page_size",
			query:        "limit=15",
			wantPage:     1,
			wantPageSize: 15,
		},
		{
			name:         "page_size takes priority over limit",
			query:        "page_size=25&limit=50",
			wantPage:     1,
			wantPageSize: 25,
		},
		{
			name:         "page is 0 uses default",
			query:        "page=0",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "page_size exceeds 1000 uses default",
			query:        "page_size=1001",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "page_size exactly 1000 is valid",
			query:        "page_size=1000",
			wantPage:     1,
			wantPageSize: 1000,
		},
		{
			name:         "page is non-numeric uses default",
			query:        "page=abc",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "page_size is non-numeric uses default",
			query:        "page_size=xyz",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "limit is non-numeric uses default",
			query:        "limit=abc",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "page_size is 0 uses default",
			query:        "page_size=0",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "limit is 0 uses default",
			query:        "limit=0",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "large page number",
			query:        "page=999&page_size=100",
			wantPage:     999,
			wantPageSize: 100,
		},
		{
			name:         "page_size is 1 minimum valid value",
			query:        "page_size=1",
			wantPage:     1,
			wantPageSize: 1,
		},
		{
			name:         "mixed digits and letters for page",
			query:        "page=12a",
			wantPage:     1,
			wantPageSize: 20,
		},
		{
			name:         "limit exceeds 1000 uses default",
			query:        "limit=2000",
			wantPage:     1,
			wantPageSize: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newContextWithQuery(tt.query)

			page, pageSize := ParsePagination(c)

			require.Equal(t, tt.wantPage, page, "page does not match expected")
			require.Equal(t, tt.wantPageSize, pageSize, "pageSize does not match expected")
		})
	}
}

func Test_parseInt(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantVal int
		wantErr bool
	}{
		{
			name:    "normal number",
			input:   "123",
			wantVal: 123,
			wantErr: false,
		},
		{
			name:    "zero",
			input:   "0",
			wantVal: 0,
			wantErr: false,
		},
		{
			name:    "single digit",
			input:   "5",
			wantVal: 5,
			wantErr: false,
		},
		{
			name:    "large number",
			input:   "99999",
			wantVal: 99999,
			wantErr: false,
		},
		{
			name:    "contains letters returns 0",
			input:   "abc",
			wantVal: 0,
			wantErr: false,
		},
		{
			name:    "starts with digits then letters returns 0",
			input:   "12a",
			wantVal: 0,
			wantErr: false,
		},
		{
			name:    "contains negative sign returns 0",
			input:   "-1",
			wantVal: 0,
			wantErr: false,
		},
		{
			name:    "contains decimal point returns 0",
			input:   "1.5",
			wantVal: 0,
			wantErr: false,
		},
		{
			name:    "contains spaces returns 0",
			input:   "1 2",
			wantVal: 0,
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantVal: 0,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := parseInt(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tt.wantVal, val)
		})
	}
}
